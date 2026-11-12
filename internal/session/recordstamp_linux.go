//go:build linux

package session

import (
	"os"
	"syscall"
)

// recordStampOf is a regular file's stamp, read off the stat Linux answers
// with: the inode change time is kept to the nanosecond and no ordinary write
// can set it back.
func recordStampOf(info os.FileInfo) (recordStamp, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return recordStamp{}, false
	}
	return recordStamp{
		dev:   uint64(st.Dev),
		ino:   uint64(st.Ino),
		size:  info.Size(),
		mtime: info.ModTime().UnixNano(),
		ctime: st.Ctim.Nano(),
	}, true
}

// openRecordFile opens a journal without waiting on it: a path swapped for a
// named pipe after it was stat'ed opens at once instead of blocking for a
// writer, and the stamp taken through the handle then refuses it.
func openRecordFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
