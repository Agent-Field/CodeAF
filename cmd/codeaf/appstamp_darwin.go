//go:build darwin

package main

import (
	"os"
	"syscall"
)

// appFileIdentity includes change time because a writer can restore mtime.
func appFileIdentity(info os.FileInfo) (uint64, uint64, int64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), st.Ctimespec.Nano(), true
}

// openAppRestFile cannot block on a pipe swapped in after the size check.
func openAppRestFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
