//go:build darwin

package resident

import (
	"io/fs"
	"syscall"
)

// fileStamp: see skills_stamp_linux.go.
func fileStamp(info fs.FileInfo) (ctimeNanos int64, inode, device uint64, ok bool) {
	st, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, 0, false
	}
	return int64(st.Ctimespec.Sec)*1e9 + int64(st.Ctimespec.Nsec), uint64(st.Ino), uint64(st.Dev), true
}
