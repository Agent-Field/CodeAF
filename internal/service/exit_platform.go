//go:build linux || darwin

package service

import (
	"errors"
	"os/exec"
	"syscall"
)

// exitOf is a reaped leader's exit. A leader ended by a signal exits with
// 128 plus the signal, the shell's own spelling of it.
func exitOf(err error) Exit {
	if err == nil {
		return Exit{Code: 0, Known: true}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return Exit{Code: 128 + int(status.Signal()), Known: true}
		}
		return Exit{Code: exitErr.ExitCode(), Known: true}
	}
	return Exit{Code: -1, Known: false}
}
