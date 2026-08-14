package gamemanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hearth/internal/panel"
)

const (
	taskHistoryVersion    = 1
	maxTaskHistoryEntries = 100
	maxTaskHistoryAge     = 30 * 24 * time.Hour
	maxTaskHistoryBytes   = 2 << 20
)

type taskHistoryDocument struct {
	Version    int              `json:"version"`
	Activities []panel.Activity `json:"activities"`
}

// taskHistoryStore persists only the small task-to-log index. Log contents stay
// in their game-specific files so frequent output never rewrites this document.
type taskHistoryStore struct {
	mu             sync.Mutex
	path           string
	activities     []panel.Activity
	dirty          bool
	pendingRemoved []panel.LogRef
}

func openTaskHistory(path string, now time.Time) (*taskHistoryStore, []panel.LogRef, error) {
	store := &taskHistoryStore{path: path, activities: []panel.Activity{}}
	if strings.TrimSpace(path) == "" {
		return store, nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil, nil
	}
	if err != nil {
		return store, nil, fmt.Errorf("read task history: %w", err)
	}
	if len(data) > maxTaskHistoryBytes {
		return store, nil, errors.New("task history exceeds the safe size limit")
	}
	var document taskHistoryDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return store, nil, fmt.Errorf("decode task history: %w", err)
	}
	if document.Version != taskHistoryVersion {
		return store, nil, fmt.Errorf("unsupported task history version %d", document.Version)
	}
	for _, activity := range document.Activities {
		if validPersistedActivity(activity) {
			store.activities = append(store.activities, cloneActivity(activity))
		}
	}
	changed := false
	for index := range store.activities {
		if store.activities[index].Status != "running" {
			continue
		}
		store.activities[index].Status = "error"
		store.activities[index].Stage = "已中断"
		store.activities[index].Progress = min(store.activities[index].Progress, 99)
		store.activities[index].Detail = "Hearth 重启时该任务仍在运行；请查看关联日志确认实际结果"
		store.activities[index].UpdatedAt = now
		changed = true
	}
	removed := store.pruneLocked(now)
	if changed || len(removed) > 0 {
		store.dirty = true
		store.pendingRemoved = appendUniqueLogRefs(store.pendingRemoved, removed)
		if err := store.saveLocked(); err != nil {
			// Keep the files while the previous on-disk index can still reference
			// them. A later successful reconciliation will replace the index.
			return store, nil, err
		}
		removed = store.pendingRemoved
		store.pendingRemoved = nil
		store.dirty = false
	}
	return store, removed, nil
}

func (s *taskHistoryStore) reconcile(live []panel.Activity, now time.Time) ([]panel.Activity, []panel.LogRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before, _ := json.Marshal(s.activities)
	byID := make(map[string]int, len(s.activities))
	for index, activity := range s.activities {
		byID[activity.ID] = index
	}
	for _, activity := range live {
		if !validPersistedActivity(activity) {
			continue
		}
		activity = cloneActivity(activity)
		if index, exists := byID[activity.ID]; exists {
			s.activities[index] = activity
			continue
		}
		s.activities = append(s.activities, activity)
		byID[activity.ID] = len(s.activities) - 1
	}
	removed := s.pruneLocked(now)
	s.pendingRemoved = appendUniqueLogRefs(s.pendingRemoved, removed)
	after, _ := json.Marshal(s.activities)
	if string(before) != string(after) {
		s.dirty = true
	}
	if s.dirty {
		if err := s.saveLocked(); err != nil {
			return cloneActivities(s.activities), nil, err
		}
		removed = s.pendingRemoved
		s.pendingRemoved = nil
		s.dirty = false
	} else {
		removed = nil
	}
	return cloneActivities(s.activities), removed, nil
}

func (s *taskHistoryStore) snapshot() []panel.Activity {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneActivities(s.activities)
}

func (s *taskHistoryStore) pruneLocked(now time.Time) []panel.LogRef {
	sort.SliceStable(s.activities, func(left, right int) bool {
		return activityTime(s.activities[left]).After(activityTime(s.activities[right]))
	})
	cutoff := now.Add(-maxTaskHistoryAge)
	kept := make([]panel.Activity, 0, min(len(s.activities), maxTaskHistoryEntries))
	removed := make([]panel.LogRef, 0)
	keptLogs := make(map[string]struct{})
	for _, activity := range s.activities {
		if len(kept) < maxTaskHistoryEntries && !activityTime(activity).Before(cutoff) {
			kept = append(kept, activity)
			for _, ref := range activity.Logs {
				keptLogs[ref.ID] = struct{}{}
			}
			continue
		}
		removed = append(removed, activity.Logs...)
	}
	s.activities = kept
	unique := make([]panel.LogRef, 0, len(removed))
	seen := make(map[string]struct{})
	for _, ref := range removed {
		if _, retained := keptLogs[ref.ID]; retained {
			continue
		}
		if _, duplicate := seen[ref.ID]; duplicate {
			continue
		}
		seen[ref.ID] = struct{}{}
		unique = append(unique, ref)
	}
	return unique
}

func (s *taskHistoryStore) saveLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	data, err := json.MarshalIndent(taskHistoryDocument{Version: taskHistoryVersion, Activities: s.activities}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode task history: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create task history directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".hearth-task-history-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary task history: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary task history: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary task history: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary task history: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary task history: %w", err)
	}
	return replaceTaskHistoryFile(temporaryPath, s.path)
}

func replaceTaskHistoryFile(temporary, destination string) error {
	backup := destination + ".previous"
	_ = os.Remove(backup)
	if _, err := os.Stat(destination); err == nil {
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("preserve previous task history: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect task history: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, destination)
		}
		return fmt.Errorf("activate task history: %w", err)
	}
	return nil
}

func validPersistedActivity(activity panel.Activity) bool {
	if strings.TrimSpace(activity.ID) == "" || len(activity.ID) > 160 || activity.CreatedAt.IsZero() {
		return false
	}
	switch activity.Status {
	case "running", "success", "neutral", "warning", "error":
	default:
		return false
	}
	for _, ref := range activity.Logs {
		if filepath.Base(ref.ID) != ref.ID || filepath.Ext(ref.ID) != ".log" || strings.ContainsAny(ref.ID, `/\\`) || len(ref.ID) > 200 {
			return false
		}
	}
	return true
}

func activityTime(activity panel.Activity) time.Time {
	if !activity.UpdatedAt.IsZero() {
		return activity.UpdatedAt
	}
	return activity.CreatedAt
}

func cloneActivity(activity panel.Activity) panel.Activity {
	activity.Logs = append([]panel.LogRef(nil), activity.Logs...)
	return activity
}

func appendUniqueLogRefs(existing, additional []panel.LogRef) []panel.LogRef {
	seen := make(map[string]struct{}, len(existing)+len(additional))
	result := make([]panel.LogRef, 0, len(existing)+len(additional))
	for _, refs := range [][]panel.LogRef{existing, additional} {
		for _, ref := range refs {
			if _, ok := seen[ref.ID]; ok {
				continue
			}
			seen[ref.ID] = struct{}{}
			result = append(result, ref)
		}
	}
	return result
}
