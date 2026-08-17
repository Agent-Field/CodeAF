//go:build !windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Lock takes an advisory lock over file. A non-blocking lock reports a busy
// error when another process owns the conflicting range.
func Lock(file *os.File, exclusive, nonBlocking bool) error {
	operation := unix.LOCK_SH
	if exclusive {
		operation = unix.LOCK_EX
	}
	if nonBlocking {
		operation |= unix.LOCK_NB
	}
	return unix.Flock(int(file.Fd()), operation)
}

func Unlock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func IsBusy(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)
}
