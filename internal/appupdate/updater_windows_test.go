//go:build windows

package appupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFileActivatesNewContent(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.exe")
	target := filepath.Join(directory, "target.exe")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(source, target); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("target = %q", content)
	}
	for _, suffix := range []string{".new", ".replacing"} {
		if _, err := os.Stat(target + suffix); !os.IsNotExist(err) {
			t.Fatalf("stale replacement file %s", target+suffix)
		}
	}
}

func TestReplaceFileRecoversInterruptedPredecessor(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.exe")
	target := filepath.Join(directory, "target.exe")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".replacing", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(source, target); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("target = %q", content)
	}
}
