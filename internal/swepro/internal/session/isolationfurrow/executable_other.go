//go:build !linux

// Portable executable-file check for src/session/isolation-furrow.ts:24-34.
package isolationfurrow

import "io/fs"

func fileExecutable(_ string, info fs.FileInfo) bool {
	return info.Mode().Perm()&0o111 != 0
}
