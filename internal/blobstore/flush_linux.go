package blobstore

import (
	"os"

	"golang.org/x/sys/unix"
)

// flushFiles makes a batch of written files, or of directories that gained
// names, durable. One file gets its own fsync. Several share one syncfs of
// their filesystem, because on a journalling disk each fsync waits for a full
// journal commit, and thousands of them in a row are what made a large frame
// outlast every client deadline. Group commit pays that wait once per batch.
func flushFiles(files []*os.File) error {
	if len(files) == 1 {
		return files[0].Sync()
	}
	if len(files) == 0 {
		return nil
	}
	return unix.Syncfs(int(files[0].Fd()))
}
