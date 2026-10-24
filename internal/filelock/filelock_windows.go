//go:build windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// LockFileEx is mandatory, so locking content bytes would prevent independent
// journal readers and appenders from working while a chat is held. Coordinate
// over one reserved byte beyond any file content instead (K3); every lock and
// unlock uses this same range, preserving shared/exclusive holder exclusion.
func Lock(file *os.File, exclusive, nonBlocking bool) error {
	var flags uint32
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	if nonBlocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	overlapped := lockRange()
	return windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &overlapped)
}

func Unlock(file *os.File) error {
	overlapped := lockRange()
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}

func IsBusy(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING)
}

// lockRange reserves a byte at 2^62; NTFS supports locks beyond end-of-file.
// This does not extend the file or prevent its contents from being read.
func lockRange() windows.Overlapped { return windows.Overlapped{OffsetHigh: 1 << 30} }
