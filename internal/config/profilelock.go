package config

import (
	"fmt"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// profileLockWait bounds how long a profile write waits for another process to
// release the lock before it gives up, so a write never hangs a chat forever.
const profileLockWait = 2 * time.Second

// lockProfileConfig takes a cross-process exclusive lock on the profile so that
// two codeaf processes writing config.json at once (a person running chats side
// by side) cannot lose each other's keys. The lock is a stable <config>.lock
// file that is never unlinked: the hold lives in the operating system's lock on
// the open file, released when the file is closed and, when a holder crashes,
// released by the kernel closing it for the dead process. There is no stale
// lock file to reclaim. filelock.Lock is cross-platform, so this holds on unix
// and Windows alike. Closing the returned file releases the lock.
func lockProfileConfig(path string) (*os.File, error) {
	lockPath := path + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open profile lock: %w", err)
	}
	deadline := time.Now().Add(profileLockWait)
	for {
		err = filelock.Lock(file, true, true)
		if err == nil {
			return file, nil
		}
		if !filelock.IsBusy(err) {
			_ = file.Close()
			return nil, fmt.Errorf("lock profile: %w", err)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			_ = file.Close()
			return nil, fmt.Errorf("lock profile: timed out after %s", profileLockWait)
		}
		pause := 10 * time.Millisecond
		if remaining < pause {
			pause = remaining
		}
		time.Sleep(pause)
	}
}
