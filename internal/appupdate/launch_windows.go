//go:build windows

package appupdate

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
)

func launchUpdateHelper(helper string) error {
	plan := filepath.Join(filepath.Dir(helper), updatePlanName)
	command := exec.Command(helper, "-plan", plan)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008 | 0x01000000, // DETACHED_PROCESS | CREATE_BREAKAWAY_FROM_JOB
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("launch independent updater: %w", err)
	}
	return command.Process.Release()
}
