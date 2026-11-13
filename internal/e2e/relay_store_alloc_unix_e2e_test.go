//go:build e2e && (linux || darwin)

package e2e

import (
	"os"
	"syscall"
)

func wanAllocated(info os.FileInfo) (int64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Blocks * 512, true
}
