//go:build linux

package resident

import (
	"io/fs"
	"syscall"
)

// fileStamp reads the identity fields beyond size and mtime that a content
// edit cannot hide from: the change time moves on every write even when a
// tool restored the modification time afterwards, and the inode and device
// move when a file is replaced. ok is false when the platform gave none.
func fileStamp(info fs.FileInfo) (ctimeNanos int64, inode, device uint64, ok bool) {
	st, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, 0, false
	}
	return int64(st.Ctim.Sec)*1e9 + int64(st.Ctim.Nsec), uint64(st.Ino), uint64(st.Dev), true
}
