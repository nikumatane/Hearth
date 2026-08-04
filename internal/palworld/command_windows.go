//go:build windows

package palworld

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func prepareManagedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup,
		HideWindow:    true,
	}
}

func waitForSteamCMDExit(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastInspectErr error
	for time.Now().Before(deadline) {
		process, err := sampleProcess("steamcmd.exe")
		if err != nil {
			lastInspectErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if !process.Running {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastInspectErr != nil {
		return fmt.Errorf("SteamCMD exit could not be confirmed within %s: %w", timeout, lastInspectErr)
	}
	return fmt.Errorf("SteamCMD child process did not exit within %s", timeout)
}

func terminateManagedCommand(command *exec.Cmd) error {
	output, err := exec.Command(
		"taskkill.exe",
		"/PID", strconv.Itoa(command.Process.Pid),
		"/T",
		"/F",
	).CombinedOutput()
	if err == nil {
		return nil
	}
	if killErr := command.Process.Kill(); killErr != nil {
		return fmt.Errorf(
			"%w: taskkill failed: %v (%s); direct termination failed: %v",
			errSteamProcessTreeUncertain,
			err,
			strings.TrimSpace(string(output)),
			killErr,
		)
	}
	return fmt.Errorf(
		"%w: taskkill failed; only the root process was terminated: %v (%s)",
		errSteamProcessTreeUncertain,
		err,
		strings.TrimSpace(string(output)),
	)
}
