//go:build linux || darwin

package session

import "syscall"

func mkfifo(path string) error { return syscall.Mkfifo(path, 0o600) }
