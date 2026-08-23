//go:build darwin

// Darwin names the stat ctime field Ctimespec, not Ctim; same value, same units.
package codeaf

import (
	"os"
	"syscall"
	"time"
)

func projectionFileIdentity(info os.FileInfo) (uint64, uint64, int64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, false
	}
	changed := int64(stat.Ctimespec.Sec)*int64(time.Second) + int64(stat.Ctimespec.Nsec)
	return uint64(stat.Dev), stat.Ino, changed, true
}
