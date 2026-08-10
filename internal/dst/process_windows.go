//go:build windows

package dst

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func prepareCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup, HideWindow: true}
}

func terminateCommand(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	if err := exec.Command("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run(); err != nil {
		return fmt.Errorf("terminate DST process tree: %w", err)
	}
	return nil
}

func processRunning(name string) bool {
	output, err := exec.Command("tasklist.exe", "/FI", "IMAGENAME eq "+name, "/NH").Output()
	return err == nil && strings.Contains(strings.ToLower(string(output)), strings.ToLower(name))
}
