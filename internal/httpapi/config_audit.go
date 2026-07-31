package httpapi

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hearth/internal/panel"
)

const (
	maxConfigAuditEntries = 1000
	maxConfigAuditSize    = 5 << 20
	maxConfigAuditLine    = 1 << 20
	maxConfigAuditValue   = 256
)

type configAuditChange struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

type configAuditEntry struct {
	ID             string              `json:"id"`
	GameID         string              `json:"gameId"`
	Source         string              `json:"source"`
	CredentialID   string              `json:"credentialId"`
	Role           string              `json:"role"`
	IP             string              `json:"ip"`
	RevisionBefore string              `json:"revisionBefore"`
	RevisionAfter  string              `json:"revisionAfter"`
	Changes        []configAuditChange `json:"changes"`
	CreatedAt      time.Time           `json:"createdAt"`
}

type configAuditStore struct {
	mu      sync.Mutex
	path    string
	entries []configAuditEntry
}

func newConfigAuditStore(path string) (*configAuditStore, error) {
	store := &configAuditStore{path: path, entries: []configAuditEntry{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *configAuditStore) record(entry configAuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append([]configAuditEntry{entry}, s.entries...)
	if len(s.entries) > maxConfigAuditEntries {
		s.entries = s.entries[:maxConfigAuditEntries]
	}
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create parameter audit directory: %w", err)
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode parameter audit: %w", err)
	}
	line = append(line, '\n')
	if err := rotateAuditLogIfNeeded(s.path, int64(len(line)), maxConfigAuditSize); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open parameter audit log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		return fmt.Errorf("append parameter audit log: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync parameter audit log: %w", err)
	}
	return nil
}

func rotateAuditLogIfNeeded(path string, incoming, maxSize int64) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat audit log: %w", err)
	}
	if info.Size()+incoming <= maxSize {
		return nil
	}
	rotated := path + ".1"
	if err := os.Remove(rotated); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old audit log: %w", err)
	}
	if err := os.Rename(path, rotated); err != nil {
		return fmt.Errorf("rotate audit log: %w", err)
	}
	return nil
}

func (s *configAuditStore) all() []configAuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := slices.Clone(s.entries)
	for index := range entries {
		entries[index].Changes = slices.Clone(entries[index].Changes)
	}
	return entries
}

func (s *configAuditStore) load() error {
	if s.path == "" {
		return nil
	}
	entries := make([]configAuditEntry, 0, maxConfigAuditEntries)
	for _, path := range []string{s.path + ".1", s.path} {
		fileEntries, err := readConfigAuditFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, fileEntries...)
	}
	if len(entries) > maxConfigAuditEntries {
		entries = entries[len(entries)-maxConfigAuditEntries:]
	}
	slices.Reverse(entries)
	s.entries = entries
	return nil
}

func readConfigAuditFile(path string) ([]configAuditEntry, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read parameter audit log: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxConfigAuditLine)
	entries := make([]configAuditEntry, 0)
	for scanner.Scan() {
		var entry configAuditEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil &&
			entry.ID != "" && !entry.CreatedAt.IsZero() && len(entry.Changes) > 0 {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan parameter audit log: %w", err)
	}
	return entries, nil
}

func buildConfigAuditEntry(
	before, after panel.PalworldSettings,
	patch panel.PalworldSettingsPatch,
	identity principal,
	ip string,
) (configAuditEntry, bool) {
	if before.Revision == after.Revision {
		return configAuditEntry{}, false
	}
	beforeSettings := settingsByKey(before)
	afterSettings := settingsByKey(after)
	keys := make([]string, 0, len(patch.Changes))
	for key := range patch.Changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changes := make([]configAuditChange, 0, len(keys))
	for _, key := range keys {
		oldSetting, oldOK := beforeSettings[key]
		newSetting, newOK := afterSettings[key]
		if !oldOK || !newOK {
			continue
		}
		change := configAuditChange{
			Key: key, Label: newSetting.Label,
			Sensitive: oldSetting.Sensitive || newSetting.Sensitive,
		}
		if change.Sensitive {
			changes = append(changes, change)
			continue
		}
		if reflect.DeepEqual(oldSetting.Value, newSetting.Value) {
			continue
		}
		change.Before = configAuditValue(oldSetting.Value)
		change.After = configAuditValue(newSetting.Value)
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		return configAuditEntry{}, false
	}
	return configAuditEntry{
		ID: newAuditID(), GameID: "palworld", Source: "PalWorldSettings.ini",
		CredentialID: identity.CredentialID, Role: identity.Role, IP: ip,
		RevisionBefore: before.Revision, RevisionAfter: after.Revision,
		Changes: changes, CreatedAt: time.Now(),
	}, true
}

func settingsByKey(document panel.PalworldSettings) map[string]panel.Setting {
	settings := make(map[string]panel.Setting)
	for _, group := range document.Groups {
		for _, setting := range group.Settings {
			settings[setting.Key] = setting
		}
	}
	return settings
}

func configAuditValue(value any) string {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case bool:
		text = strconv.FormatBool(typed)
	case float64:
		text = strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			text = fmt.Sprint(value)
		} else {
			text = string(encoded)
		}
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= maxConfigAuditValue {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxConfigAuditValue]) + "…"
}

func recordConfigAudit(store *configAuditStore, entry configAuditEntry) {
	if err := store.record(entry); err != nil {
		slog.Error("persist parameter audit", "error", err)
	}
}
