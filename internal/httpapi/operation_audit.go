package httpapi

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	operationEventMemberCreated         = "member_created"
	operationEventMemberUpdated         = "member_updated"
	operationEventMemberDeleted         = "member_deleted"
	operationEventIPRuleAdded           = "ip_rule_added"
	operationEventIPRuleRemoved         = "ip_rule_removed"
	operationEventGameAdopted           = "game_adopted"
	operationEventGameInstall           = "game_install_started"
	operationEventDSTTokenUpdated       = "dst_token_updated"
	operationEventDSTConfigUpdated      = "dst_config_updated"
	operationEventSystemUpdated         = "system_settings_updated"
	operationEventPanelUpdateChecked    = "panel_update_checked"
	operationEventPanelUpdateStarted    = "panel_update_started"
	operationEventPanelUpdateSucceeded  = "panel_update_succeeded"
	operationEventPanelUpdateRolledBack = "panel_update_rolled_back"
	operationEventPanelUpdateFailed     = "panel_update_failed"

	operationTargetMember = "member"
	operationTargetIPRule = "ip_rule"
	operationTargetGame   = "game"
	operationTargetSystem = "system"

	maxOperationAuditEntries = 1000
	maxOperationAuditSize    = 5 << 20
	maxOperationAuditLine    = 1 << 20
)

type operationAuditEntry struct {
	ID                 string     `json:"id"`
	Event              string     `json:"event"`
	ActorCredentialID  string     `json:"actorCredentialId"`
	ActorRole          string     `json:"actorRole"`
	ActorIP            string     `json:"actorIp"`
	TargetType         string     `json:"targetType"`
	TargetID           string     `json:"targetId,omitempty"`
	TargetIP           string     `json:"targetIp,omitempty"`
	RuleKind           string     `json:"ruleKind,omitempty"`
	ExpiresAt          *time.Time `json:"expiresAt,omitempty"`
	PasswordChanged    bool       `json:"passwordChanged,omitempty"`
	PermissionsChanged bool       `json:"permissionsChanged,omitempty"`
	CurrentPermissions []string   `json:"currentPermissions,omitempty"`
	Success            bool       `json:"success"`
	UpdateVersion      string     `json:"updateVersion,omitempty"`
	PreviousVersion    string     `json:"previousVersion,omitempty"`
	Detail             string     `json:"detail,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
}

type operationAuditStore struct {
	mu      sync.Mutex
	path    string
	entries []operationAuditEntry
}

func newOperationAuditStore(path string) (*operationAuditStore, error) {
	store := &operationAuditStore{path: path, entries: []operationAuditEntry{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *operationAuditStore) record(entry operationAuditEntry) error {
	if !validOperationAuditEntry(entry) {
		return errors.New("invalid security operation audit entry")
	}
	entry.CurrentPermissions = slices.Clone(entry.CurrentPermissions)
	entry.ExpiresAt = cloneTimePointer(entry.ExpiresAt)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append([]operationAuditEntry{entry}, s.entries...)
	if len(s.entries) > maxOperationAuditEntries {
		s.entries = s.entries[:maxOperationAuditEntries]
	}
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create security operation audit directory: %w", err)
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode security operation audit: %w", err)
	}
	line = append(line, '\n')
	if err := rotateAuditLogIfNeeded(s.path, int64(len(line)), maxOperationAuditSize); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open security operation audit: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		return fmt.Errorf("append security operation audit: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync security operation audit: %w", err)
	}
	return nil
}

func (s *operationAuditStore) all() []operationAuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := slices.Clone(s.entries)
	for index := range entries {
		entries[index].CurrentPermissions = slices.Clone(entries[index].CurrentPermissions)
		entries[index].ExpiresAt = cloneTimePointer(entries[index].ExpiresAt)
	}
	return entries
}

func (s *operationAuditStore) load() error {
	if s.path == "" {
		return nil
	}
	entries := make([]operationAuditEntry, 0, maxOperationAuditEntries)
	for _, path := range []string{s.path + ".1", s.path} {
		fileEntries, err := readOperationAuditFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, fileEntries...)
	}
	if len(entries) > maxOperationAuditEntries {
		entries = entries[len(entries)-maxOperationAuditEntries:]
	}
	slices.Reverse(entries)
	s.entries = entries
	return nil
}

func readOperationAuditFile(path string) ([]operationAuditEntry, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read security operation audit log: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxOperationAuditLine)
	entries := make([]operationAuditEntry, 0)
	for scanner.Scan() {
		var entry operationAuditEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && validOperationAuditEntry(entry) {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan security operation audit log: %w", err)
	}
	return entries, nil
}

func validOperationAuditEntry(entry operationAuditEntry) bool {
	if entry.ID == "" || entry.ActorCredentialID == "" ||
		entry.TargetType == "" || entry.CreatedAt.IsZero() {
		return false
	}
	if entry.ActorRole != roleAdmin && entry.ActorRole != roleMember {
		return false
	}
	if _, err := netip.ParseAddr(entry.ActorIP); err != nil {
		return false
	}
	switch entry.Event {
	case operationEventMemberCreated, operationEventMemberUpdated, operationEventMemberDeleted:
		return entry.TargetType == operationTargetMember && entry.TargetID != ""
	case operationEventIPRuleAdded, operationEventIPRuleRemoved:
		_, err := netip.ParseAddr(entry.TargetIP)
		return err == nil && entry.TargetType == operationTargetIPRule && entry.TargetID != "" &&
			(entry.RuleKind == ipRuleAllow || entry.RuleKind == ipRuleDeny)
	case operationEventGameAdopted, operationEventGameInstall:
		return entry.TargetType == operationTargetGame && entry.TargetID != ""
	case operationEventDSTTokenUpdated:
		return entry.ActorRole == roleAdmin && entry.TargetType == operationTargetGame && entry.TargetID == "dont-starve-together"
	case operationEventSystemUpdated, operationEventPanelUpdateChecked, operationEventPanelUpdateStarted:
		return entry.TargetType == operationTargetSystem && entry.TargetID == "hearth"
	case operationEventPanelUpdateSucceeded, operationEventPanelUpdateRolledBack, operationEventPanelUpdateFailed:
		return entry.TargetType == operationTargetSystem && entry.TargetID == "hearth" &&
			entry.UpdateVersion != "" && entry.PreviousVersion != "" && len(entry.Detail) <= 4096
	default:
		return false
	}
}

func recordOperationAudit(store *operationAuditStore, entry operationAuditEntry) {
	if err := store.record(entry); err != nil {
		slog.Error("persist security operation audit", "error", err)
	}
}
