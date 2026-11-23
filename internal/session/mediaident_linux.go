//go:build linux

package session

import (
	"os"
	"syscall"
)

// fileIdentity is the parts of a file's identity that stat knows beyond its
// size and modification time: the change time, the device and the inode. A
// file replaced by another of the same size and mtime still has a new inode,
// so a version built from these notices what a size alone would not.
func fileIdentity(info os.FileInfo) (ctimeNS int64, dev, ino uint64) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0
	}
	return stat.Ctim.Nano(), uint64(stat.Dev), uint64(stat.Ino) //nolint:unconvert // the widths differ by platform
}
