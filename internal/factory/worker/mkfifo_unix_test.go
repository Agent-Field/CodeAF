//go:build !windows

package worker_test

import "syscall"

// mkfifo makes a named pipe at path. The capacity rig calls it so that only
// this file, not the rig, needs a platform without named pipes left out.
func mkfifo(path string, mode uint32) error { return syscall.Mkfifo(path, mode) }
