//go:build !windows

package palworld

import "os/exec"

func prepareManagedCommand(_ *exec.Cmd) {}

func terminateManagedCommand(command *exec.Cmd) error {
	return command.Process.Kill()
}
