//go:build !windows

package palworld

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallDedicatedServerRequiresPalworldCompletionMarker(t *testing.T) {
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd.exe")
	script := "#!/bin/sh\necho \"Success! App '2394010' fully installed.\"\n"
	if err := os.WriteFile(steamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "install.log")
	stages := make([]string, 0, 4)
	err := InstallDedicatedServer(
		steamCmd, filepath.Join(directory, "PalServer"), logPath, time.Second,
		func(stage string, _ int, _ string) { stages = append(stages, stage) },
	)
	if err != nil {
		t.Fatalf("InstallDedicatedServer() error = %v", err)
	}
	if len(stages) == 0 || stages[len(stages)-1] != "下载完成" {
		t.Fatalf("stages = %#v", stages)
	}
}

func TestInstallDedicatedServerRejectsCleanExitWithoutMarker(t *testing.T) {
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd.exe")
	if err := os.WriteFile(steamCmd, []byte("#!/bin/sh\necho done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := InstallDedicatedServer(
		steamCmd, filepath.Join(directory, "PalServer"), filepath.Join(directory, "install.log"),
		time.Second, nil,
	)
	if err == nil {
		t.Fatal("InstallDedicatedServer() accepted output without an app completion marker")
	}
}
