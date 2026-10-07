//go:build !windows

package update

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFileExclusive takes the target's lock without blocking. The kernel holds
// it for the life of the process, so a crashed installer can never wedge a
// future one and no watchdog has to guess when to take a lock over.
func lockFileExclusive(file *os.File) bool {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) == nil
}

func unlockFile(file *os.File) {
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
