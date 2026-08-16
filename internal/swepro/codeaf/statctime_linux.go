//go:build linux

// Linux spelling of the stat ctime field read by durable_sessions.go's projection mark.
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
	changed := int64(stat.Ctim.Sec)*int64(time.Second) + int64(stat.Ctim.Nsec)
	return uint64(stat.Dev), stat.Ino, changed, true
}
