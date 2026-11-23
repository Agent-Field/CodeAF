//go:build !windows

package run

import "syscall"

// C10 retains the Unix check's original process-existence probe.
func killZero(pid int) error { return syscall.Kill(pid, 0) }
