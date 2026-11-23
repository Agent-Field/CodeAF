//go:build unix

package plandb

import (
	"os"
	"syscall"
)

// statStamp is the size, modification time and inode of path, or the zero
// stamp for a file that is not there, which is a stamp too.
func statStamp(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	stamp := fileStamp{size: info.Size(), mtime: info.ModTime().UnixNano()}
	if raw, ok := info.Sys().(*syscall.Stat_t); ok {
		stamp.inode = uint64(raw.Ino)
	}
	return stamp
}
