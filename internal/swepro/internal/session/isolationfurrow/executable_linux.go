//go:build linux

// Executable-file check for src/session/isolation-furrow.ts:24-34.
package isolationfurrow

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

func fileExecutable(path string, _ fs.FileInfo) bool {
	return unix.Access(path, unix.X_OK) == nil
}
