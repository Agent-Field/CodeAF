//go:build linux

// Linux FICLONE implementation for src/session/isolation.ts:138-159.
package isolation

import (
	"os"

	"golang.org/x/sys/unix"
)

func cloneFile(dst, src *os.File) error {
	return unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
}
