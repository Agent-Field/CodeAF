//go:build !windows

package main

import "syscall"

// mkfifo makes a named pipe at path. The FIFO cases call it so that only this
// file, not the tests that use it, needs a platform without named pipes left out.
func mkfifo(path string, mode uint32) error { return syscall.Mkfifo(path, mode) }
