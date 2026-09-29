//go:build !linux

package blobstore

import (
	"errors"
	"os"
)

// flushFiles makes a batch of written files, or of directories that gained
// names, durable, with one fsync each. Only Linux offers a call that commits a
// whole batch at once, and this platform is not what a relay runs on.
func flushFiles(files []*os.File) error {
	var errs []error
	for _, f := range files {
		errs = append(errs, f.Sync())
	}
	return errors.Join(errs...)
}
