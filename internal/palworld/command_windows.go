//go:build windows

package palworld

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func prepareManagedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup,
		HideWindow:    true,
	}
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
