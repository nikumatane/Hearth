package palworld

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneBackupsAppliesAgeAndCapacityAndPreservesForeignFiles(t *testing.T) {
	directory := t.TempDir()
	location := time.FixedZone("test", 8*60*60)
	now := time.Date(2026, 7, 31, 16, 0, 0, 0, location)
	old := writeManagedBackup(t, directory, now.Add(-40*24*time.Hour), 4)
	capacityOldest := writeManagedBackup(t, directory, now.Add(-10*24*time.Hour), 8)
	recent := writeManagedBackup(t, directory, now.Add(-24*time.Hour), 8)
	newest := writeManagedBackup(t, directory, now, 8)
	foreign := filepath.Join(directory, "manual-backup.zip")
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := pruneBackups(directory, newest, 30*24*time.Hour, 16, now)
	if err != nil {
		t.Fatalf("pruneBackups() error = %v", err)
	}
	if result.Removed != 2 || result.FreedBytes != 12 {
		t.Fatalf("retention result = %#v", result)
	}
	assertFileMissing(t, old)
	assertFileMissing(t, capacityOldest)
	assertFileExists(t, recent)
	assertFileExists(t, newest)
	assertFileExists(t, foreign)
}

func TestPruneBackupsAlwaysKeepsNewBackupEvenWhenItExceedsCapacity(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 7, 31, 16, 0, 0, 0, time.Local)
	older := writeManagedBackup(t, directory, now.Add(-time.Hour), 4)
	newest := writeManagedBackup(t, directory, now, 10)

	result, err := pruneBackups(directory, newest, 30*24*time.Hour, 5, now)
	if err != nil {
		t.Fatalf("pruneBackups() error = %v", err)
	}
	if result.Removed != 1 || result.FreedBytes != 4 {
		t.Fatalf("retention result = %#v", result)
	}
	assertFileMissing(t, older)
	assertFileExists(t, newest)
}

func writeManagedBackup(t *testing.T, directory string, createdAt time.Time, size int) string {
	t.Helper()
	path := filepath.Join(directory, "palworld-"+createdAt.Format(backupTimestampLayout)+".zip")
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, stat error = %v", path, err)
	}
}
