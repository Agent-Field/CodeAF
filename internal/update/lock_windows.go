//go:build windows

package update

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFileExclusive takes the target's lock without blocking, on the same terms
// as the unix door: held by the OS until the handle closes, so a dead installer
// releases it and nothing has to be taken over by age.
func lockFileExclusive(file *os.File) bool {
	overlapped := new(windows.Overlapped)
	return windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped,
	) == nil
}

func unlockFile(file *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped))
}
