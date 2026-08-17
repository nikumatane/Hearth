//go:build !windows

package dst

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessRunningMatchesConfiguredExecutablePath(t *testing.T) {
	const name = "hearth-dst-process-probe"
	runningPath := filepath.Join(t.TempDir(), name)
	otherPath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(runningPath, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(runningPath)
	prepareCommand(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = terminateCommand(command)
		_ = command.Wait()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for !processRunning(runningPath) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !processRunning(runningPath) {
		t.Fatal("configured executable was not detected")
	}
	if processRunning(otherPath) {
		t.Fatal("same basename from another installation was treated as the configured executable")
	}
}
