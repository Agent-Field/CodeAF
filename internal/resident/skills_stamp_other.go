//go:build !linux && !darwin

package resident

import "io/fs"

// fileStamp: no change time is available here, so no folder signature is
// trusted and every pass reads the content, exactly as it did before the
// digest cache existed.
func fileStamp(fs.FileInfo) (int64, uint64, uint64, bool) { return 0, 0, 0, false }
