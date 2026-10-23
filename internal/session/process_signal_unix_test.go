//go:build !windows

package session

import "syscall"

// C10 keeps the existing Unix signal probes unchanged while allowing the native
// Windows contract tests to compile in the same test package.
func testProcessSignal(pid int, signal syscall.Signal) error { return syscall.Kill(pid, signal) }
