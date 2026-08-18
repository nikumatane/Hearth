//go:build windows

package steamapp

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const installCreateNewProcessGroup = 0x00000200

func prepareInstallCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: installCreateNewProcessGroup, HideWindow: true}
}

func waitForSteamCMDExit(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := exec.Command("tasklist.exe", "/FI", "IMAGENAME eq steamcmd.exe", "/FO", "CSV", "/NH").Output()
		if err == nil && !strings.Contains(strings.ToLower(string(output)), "steamcmd.exe") {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("SteamCMD child process did not exit within %s", timeout)
}

func terminateInstallCommand(command *exec.Cmd) error {
	output, err := exec.Command("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").CombinedOutput()
	if err == nil {
		return nil
	}
	if killErr := command.Process.Kill(); killErr != nil {
		return fmt.Errorf("taskkill failed: %v (%s); direct termination failed: %v", err, strings.TrimSpace(string(output)), killErr)
	}
	return fmt.Errorf("taskkill failed; only the root process was terminated: %v (%s)", err, strings.TrimSpace(string(output)))
}
