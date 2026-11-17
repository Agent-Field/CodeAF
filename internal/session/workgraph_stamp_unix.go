//go:build !windows

package session

import (
	"fmt"
	"os"
	"syscall"
)

// wgStamp names a file's present state by size, modification instant and inode,
// and "-" for a file that is not there. The checkpoint is replaced by rename, so
// a rewrite changes the inode even when size and second are the same.
func wgStamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	var ino uint64
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		ino = uint64(st.Ino)
	}
	return fmt.Sprintf("%d/%d/%d", info.Size(), info.ModTime().UnixNano(), ino)
}
