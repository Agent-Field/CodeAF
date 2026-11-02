//go:build linux || darwin

package taskdoor

import (
	"os/exec"
	"syscall"
)

func separateProcessGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func signalChildGroup(pid int, force bool) error {
	signal := syscall.SIGINT
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-pid, signal)
}
