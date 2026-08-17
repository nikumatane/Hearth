package gamemanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hearth/internal/config"
	"hearth/internal/panel"
)

func TestTaskHistoryPersistsAndMarksInterruptedTask(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "task-history.json")
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.Local)
	store, _, err := openTaskHistory(path, now)
	if err != nil {
		t.Fatal(err)
	}
	activity := panel.Activity{
		ID: "dst-running", GameID: dstID, Action: "update", Title: "更新 DST",
		Detail: "下载中", Status: "running", Stage: "SteamCMD", Progress: 72,
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
		Logs: []panel.LogRef{{ID: "dst-update.log", Label: "DST 更新日志"}},
	}
	if _, _, err := store.reconcile([]panel.Activity{activity}, now); err != nil {
		t.Fatalf("persist task history: %v", err)
	}
	reloaded, _, err := openTaskHistory(path, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("reload task history: %v", err)
	}
	entries := reloaded.snapshot()
	if len(entries) != 1 || entries[0].Status != "error" || entries[0].Stage != "已中断" || entries[0].Progress != 72 {
		t.Fatalf("reloaded activities = %#v", entries)
	}
	if len(entries[0].Logs) != 1 || entries[0].Logs[0].ID != "dst-update.log" {
		t.Fatalf("reloaded log references = %#v", entries[0].Logs)
	}
}

func TestTaskHistoryCoalescesProgressButFlushesTerminalState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-history.json")
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.Local)
	store, _, err := openTaskHistory(path, now)
	if err != nil {
		t.Fatal(err)
	}
	activities := []panel.Activity{
		{ID: "pal-running", GameID: palworldID, Title: "Pal", Status: "running", Progress: 10, CreatedAt: now, UpdatedAt: now},
		{ID: "dst-running", GameID: dstID, Title: "DST", Status: "running", Progress: 10, CreatedAt: now, UpdatedAt: now},
	}
	if _, _, err := store.reconcile(activities, now); err != nil {
		t.Fatal(err)
	}
	activities[0].Progress = 20
	activities[0].UpdatedAt = now.Add(time.Second)
	activities[1].Progress = 20
	activities[1].UpdatedAt = now.Add(time.Second)
	if _, _, err := store.reconcile(activities, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	document := readTaskHistoryDocument(t, path)
	if document.Activities[0].Progress != 10 || document.Activities[1].Progress != 10 {
		t.Fatalf("intermediate progress was not coalesced: %#v", document.Activities)
	}

	activities[0].Status = "success"
	activities[0].Stage = "完成"
	activities[0].Progress = 100
	activities[0].UpdatedAt = now.Add(1500 * time.Millisecond)
	if _, _, err := store.reconcile(activities, now.Add(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	document = readTaskHistoryDocument(t, path)
	statuses := make(map[string]string)
	for _, activity := range document.Activities {
		statuses[activity.ID] = activity.Status
	}
	if statuses["pal-running"] != "success" || statuses["dst-running"] != "running" {
		t.Fatalf("terminal state was deferred while another task ran: %#v", statuses)
	}
}

func readTaskHistoryDocument(t *testing.T, path string) taskHistoryDocument {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document taskHistoryDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestTaskHistoryRollsByAgeAndCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-history.json")
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.Local)
	store, _, err := openTaskHistory(path, now)
	if err != nil {
		t.Fatal(err)
	}
	activities := make([]panel.Activity, 0, maxTaskHistoryEntries+2)
	activities = append(activities, panel.Activity{
		ID: "expired", Title: "expired", Status: "success",
		CreatedAt: now.Add(-maxTaskHistoryAge - time.Hour), UpdatedAt: now.Add(-maxTaskHistoryAge - time.Hour),
		Logs: []panel.LogRef{{ID: "expired.log", Label: "expired"}},
	})
	for index := 0; index < maxTaskHistoryEntries+1; index++ {
		at := now.Add(-time.Duration(index) * time.Minute)
		activities = append(activities, panel.Activity{
			ID: "task-" + at.Format("150405.000000000"), Title: "task", Status: "success",
			CreatedAt: at, UpdatedAt: at,
			Logs: []panel.LogRef{{ID: "task-" + at.Format("150405.000000000") + ".log", Label: "task"}},
		})
	}
	kept, removed, err := store.reconcile(activities, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != maxTaskHistoryEntries {
		t.Fatalf("kept %d activities, want %d", len(kept), maxTaskHistoryEntries)
	}
	removedIDs := make(map[string]bool)
	for _, ref := range removed {
		removedIDs[ref.ID] = true
	}
	if !removedIDs["expired.log"] || len(removedIDs) != 2 {
		t.Fatalf("removed logs = %#v", removedIDs)
	}
}

func TestTaskHistoryRejectsUnsafeLogReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-history.json")
	now := time.Now()
	store, _, err := openTaskHistory(path, now)
	if err != nil {
		t.Fatal(err)
	}
	unsafe := panel.Activity{
		ID: "unsafe", Title: "unsafe", Status: "success", CreatedAt: now, UpdatedAt: now,
		Logs: []panel.LogRef{{ID: "../outside.log", Label: "unsafe"}},
	}
	kept, _, err := store.reconcile([]panel.Activity{unsafe}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 0 {
		t.Fatalf("unsafe activity persisted: %#v", kept)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unsafe history unexpectedly created: %v", err)
	}
}

func TestTaskLogPathSurvivesAdapterRestart(t *testing.T) {
	root := t.TempDir()
	manager := &Service{
		config: config.Config{
			Management: config.ManagementConfig{SteamCmdRoot: filepath.Join(root, "steamcmd")},
			Games: config.GamesConfig{
				Palworld:           config.GameConfig{InstallDir: filepath.Join(root, "palworld")},
				DontStarveTogether: config.GameConfig{ClusterDir: filepath.Join(root, "cluster")},
			},
		},
		logPaths: map[string]string{},
	}
	tests := []struct {
		id   string
		want string
	}{
		{"dst-old-update.log", filepath.Join(root, "cluster", "panel-logs", "dst-old-update.log")},
		{"steamcmd-install-old.log", filepath.Join(root, "steamcmd", "hearth-logs", "steamcmd-install-old.log")},
		{"steamcmd-update-old.log", filepath.Join(root, "palworld", "panel-logs", "steamcmd-update-old.log")},
	}
	for _, test := range tests {
		got, ok := manager.TaskLogPath(test.id)
		if !ok || got != test.want {
			t.Fatalf("TaskLogPath(%q) = %q, %v; want %q, true", test.id, got, ok, test.want)
		}
	}
}
