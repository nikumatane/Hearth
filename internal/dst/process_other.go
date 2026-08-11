//go:build !windows

package dst

import (
	"os/exec"
	"strconv"
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
	if err != nil {
		return false
	}
	for _, line := range strings.Fields(string(output)) {
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 {
			continue
		}
		command, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
		if err != nil {
			continue
		}
		// Parallel status checks run their own pgrep with the same pattern. Those
		// helper processes are not DST and must not be treated as external shards.
		if strings.EqualFold(strings.TrimSpace(string(command)), "pgrep") {
			continue
		}
		return true
	}
	return false
}
