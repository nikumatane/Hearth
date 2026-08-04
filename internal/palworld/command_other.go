//go:build !windows

package palworld

import (
	"os/exec"
	"time"
)

func prepareManagedCommand(_ *exec.Cmd) {}

func waitForSteamCMDExit(_ time.Duration) error { return nil }

func terminateManagedCommand(command *exec.Cmd) error {
	return command.Process.Kill()
}
