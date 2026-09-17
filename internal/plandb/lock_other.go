//go:build !unix

package plandb

import (
	"fmt"
	"os"
	"path/filepath"
)

// The non-unix road keeps the same shape and gives up the cross-process
// ordering: the platforms this build ships on that have no flock are single
// surface and single process, which is what the in-process mutex already
// orders. A second process on such a platform is a second store pretending
// to be the first, and the store says so in its own error rather than by
// losing a write quietly.
func lockFile(path string) (*os.File, error) {
	// Same footprint as the unix road: the directory is made here, so a
	// caller's first write on this platform cannot fail on a missing parent
	// while the unix road succeeds.
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create plan store directory: %w", err)
		}
	}
	return os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
}

func lockExclusive(f *os.File) error   { return nil }
func unlockExclusive(f *os.File) error { return nil }
