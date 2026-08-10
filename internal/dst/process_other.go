//go:build !windows

package dst

import (
	"os/exec"
	"strings"
	"syscall"
)

func prepareCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateCommand(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}

func processRunning(name string) bool {
	output, err := exec.Command("pgrep", "-f", name).Output()
	return err == nil && strings.TrimSpace(string(output)) != ""
}
