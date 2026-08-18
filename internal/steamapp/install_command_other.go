//go:build !windows

package steamapp

import (
	"os/exec"
	"time"
)

func prepareInstallCommand(_ *exec.Cmd) {}

func waitForSteamCMDExit(_ time.Duration) error { return nil }

func terminateInstallCommand(command *exec.Cmd) error { return command.Process.Kill() }
