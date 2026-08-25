//go:build !windows

package enginehost

import (
	"os/exec"
	"syscall"
)

// detach gives the child a session of its own, so that the death of the ssh
// connection that spawned it is not also its death. Without it sshd's teardown
// signals the whole process group and the host goes down with the surface that
// asked for it — which is the exact failure this package exists to remove.
func detach(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
