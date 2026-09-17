//go:build unix

package plandb

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// THE STORE IS WRITTEN BY MORE THAN ONE PROCESS. The runtime holds the graph
// and opens the store in process; every `plandb` call a worker makes is a
// separate process against the same file. The in-process mutex orders the
// runtime's own calls and nothing else, so every read-modify-write
// transaction takes an advisory lock on a sidecar file around load, change
// and rename. An advisory lock is enough because every writer in the picture
// is this package: a reader that never writes needs no lock, and a writer
// that skips this file would be a second store pretending to be the first.
//
// The lock file is created and never removed, which is the honest cost: a
// removed lock file is a race window between the unlink and the next open,
// and one empty sidecar file beside the store is cheaper than that race.
func lockFile(path string) (*os.File, error) {
	// The lock is part of the store's file footprint, so it is the one that
	// makes sure the store's directory exists — O_CREATE does not create
	// parents, and the store's own path may sit one or two directories deep
	// in a fresh session.
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create plan store directory: %w", err)
		}
	}
	return os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
}

func lockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func unlockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
