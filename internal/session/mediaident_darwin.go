//go:build darwin

package session

import (
	"os"
	"syscall"
)

// fileIdentity: see mediaident_linux.go.
func fileIdentity(info os.FileInfo) (ctimeNS int64, dev, ino uint64) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0
	}
	return stat.Ctimespec.Nano(), uint64(stat.Dev), uint64(stat.Ino)
}
