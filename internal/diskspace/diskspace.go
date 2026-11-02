// Package diskspace answers how big a path's filesystem is and how much of it
// is free, with one system call and no shelling out.
package diskspace

import (
	"errors"
	"os"
	"path/filepath"
)

// Usage is one filesystem's size and the bytes free on it to this user.
type Usage struct {
	FreeBytes  uint64
	TotalBytes uint64
}

// Of is the filesystem path is on. A path not made yet is answered for the
// nearest folder above it that exists, which is where it would be made.
func Of(path string) (Usage, error) {
	if path == "" {
		return Usage{}, errors.New("diskspace: no path")
	}
	path = filepath.Clean(path)
	for {
		if _, err := os.Stat(path); err == nil {
			return of(path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return Usage{}, errors.New("diskspace: no folder of " + path + " exists")
		}
		path = parent
	}
}
