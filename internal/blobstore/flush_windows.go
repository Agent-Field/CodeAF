package blobstore

import (
	"errors"
	"os"
)

// flushFiles uses FlushFileBuffers through File.Sync for ordinary files (K3).
// Windows does not expose directory fsync through Go; directory flushes are a
// deliberate no-op, matching the native engine's durability seam.
func flushFiles(files []*os.File) error {
	var errs []error
	for _, f := range files {
		info, err := f.Stat()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.IsDir() {
			errs = append(errs, f.Sync())
		}
	}
	return errors.Join(errs...)
}
