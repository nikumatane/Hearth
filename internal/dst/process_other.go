//go:build !windows

package dst

import (
	"os/exec"
	"path/filepath"
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
	return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}

func processRunning(executable string) bool {
	executable = filepath.Clean(executable)
	name := filepath.Base(executable)
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
		arguments, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
		if err != nil {
			continue
		}
		// A process with the same basename may belong to another installation
		// (or to another package's parallel test). Match the configured absolute
		// executable path before treating it as this managed DST/SteamCMD process.
		if strings.Contains(string(arguments), executable) {
			return true
		}
	}
	return false
}
