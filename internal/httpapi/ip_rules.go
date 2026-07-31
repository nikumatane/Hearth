package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	ipRuleAllow          = "allow"
	ipRuleDeny           = "deny"
	maxIPRules           = 256
	ipRuleHitAuditWindow = time.Minute
)

var (
	errIPRuleNotFound = errors.New("IP rule not found")
	errIPRuleLimit    = errors.New("IP rule limit reached")
	errInvalidIPRule  = errors.New("invalid IP rule")
)

type ipRule struct {
	ID        string     `json:"id"`
	IP        string     `json:"ip"`
	Kind      string     `json:"kind"`
	Note      string     `json:"note,omitempty"`
	CreatedBy string     `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	HitCount  uint64     `json:"hitCount"`
	LastHitAt *time.Time `json:"lastHitAt,omitempty"`

	lastAuditAt time.Time
}

type ipRuleDocument struct {
	Version int      `json:"version"`
	Rules   []ipRule `json:"rules"`
}

type ipRuleStore struct {
	mu    sync.Mutex
	path  string
	rules []ipRule
}

func newIPRuleStore(path string) (*ipRuleStore, error) {
	store := &ipRuleStore{path: path, rules: []ipRule{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *ipRuleStore) all(now time.Time) []ipRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	result := slices.Clone(s.rules)
	slices.SortFunc(result, func(left, right ipRule) int {
		return right.CreatedAt.Compare(left.CreatedAt)
	})
	return result
}

func (s *ipRuleStore) add(
	ip, kind, note, actor string,
	expiresAt *time.Time,
	now time.Time,
) (ipRule, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return ipRule{}, fmt.Errorf("%w: IP 地址格式不正确", errInvalidIPRule)
	}
	normalizedIP := address.Unmap().String()
	if kind != ipRuleAllow && kind != ipRuleDeny {
		return ipRule{}, fmt.Errorf("%w: 规则类型不正确", errInvalidIPRule)
	}
	note = strings.TrimSpace(note)
	if len(note) > 200 {
		return ipRule{}, fmt.Errorf("%w: 备注不能超过 200 个字符", errInvalidIPRule)
	}
	if expiresAt != nil && !expiresAt.After(now) {
		return ipRule{}, fmt.Errorf("%w: 到期时间必须晚于当前时间", errInvalidIPRule)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	previous := slices.Clone(s.rules)
	for index := range s.rules {
		if s.rules[index].IP != normalizedIP {
			continue
		}
		if s.rules[index].Kind != kind {
			s.rules[index].HitCount = 0
			s.rules[index].LastHitAt = nil
			s.rules[index].lastAuditAt = time.Time{}
		}
		s.rules[index].Kind = kind
		s.rules[index].Note = note
		s.rules[index].CreatedBy = actor
		s.rules[index].CreatedAt = now
		s.rules[index].ExpiresAt = cloneTimePointer(expiresAt)
		if err := s.persistLocked(); err != nil {
			s.rules = previous
			return ipRule{}, err
		}
		return s.rules[index], nil
	}
	if len(s.rules) >= maxIPRules {
		return ipRule{}, errIPRuleLimit
	}
	rule := ipRule{
		ID: newAuditID(), IP: normalizedIP, Kind: kind, Note: note,
		CreatedBy: actor, CreatedAt: now, ExpiresAt: cloneTimePointer(expiresAt),
	}
	s.rules = append(s.rules, rule)
	if err := s.persistLocked(); err != nil {
		s.rules = previous
		return ipRule{}, err
	}
	return rule, nil
}

func (s *ipRuleStore) remove(id string) (ipRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, rule := range s.rules {
		if rule.ID != id {
			continue
		}
		previous := slices.Clone(s.rules)
		s.rules = append(s.rules[:index], s.rules[index+1:]...)
		if err := s.persistLocked(); err != nil {
			s.rules = previous
			return ipRule{}, err
		}
		return rule, nil
	}
	return ipRule{}, errIPRuleNotFound
}

func (s *ipRuleStore) match(ip string, now time.Time) (ipRule, bool, bool) {
	address, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return ipRule{}, false, false
	}
	normalizedIP := address.Unmap().String()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	for index := range s.rules {
		if s.rules[index].IP != normalizedIP {
			continue
		}
		s.rules[index].HitCount++
		hitAt := now
		s.rules[index].LastHitAt = &hitAt
		shouldAudit := false
		if s.rules[index].Kind == ipRuleDeny {
			shouldAudit = s.rules[index].lastAuditAt.IsZero() ||
				now.Sub(s.rules[index].lastAuditAt) >= ipRuleHitAuditWindow
			if shouldAudit {
				s.rules[index].lastAuditAt = now
			}
		}
		return s.rules[index], true, shouldAudit
	}
	return ipRule{}, false, false
}

func (s *ipRuleStore) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read IP rules: %w", err)
	}
	var document ipRuleDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode IP rules: %w", err)
	}
	if document.Version != 1 {
		return fmt.Errorf("unsupported IP rule version %d", document.Version)
	}
	if len(document.Rules) > maxIPRules {
		return fmt.Errorf("IP rule count exceeds %d", maxIPRules)
	}
	seen := make(map[string]bool, len(document.Rules))
	for index := range document.Rules {
		rule := &document.Rules[index]
		address, parseErr := netip.ParseAddr(rule.IP)
		if parseErr != nil || (rule.Kind != ipRuleAllow && rule.Kind != ipRuleDeny) ||
			rule.ID == "" || seen[address.Unmap().String()] {
			return fmt.Errorf("invalid IP rule at index %d", index)
		}
		rule.IP = address.Unmap().String()
		rule.lastAuditAt = time.Time{}
		seen[rule.IP] = true
	}
	s.rules = document.Rules
	s.pruneExpiredLocked(time.Now())
	return nil
}

func (s *ipRuleStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ipRuleDocument{Version: 1, Rules: s.rules}, "", "  ")
	if err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := replaceCredentialFile(temp, s.path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func (s *ipRuleStore) pruneExpiredLocked(now time.Time) {
	s.rules = slices.DeleteFunc(s.rules, func(rule ipRule) bool {
		return rule.ExpiresAt != nil && !rule.ExpiresAt.After(now)
	})
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
