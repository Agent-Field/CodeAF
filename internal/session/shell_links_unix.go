//go:build unix

package session

import (
	"os"
	"syscall"
)

// linkCount is how many names the file has. A file with more than one is not
// shown to be new by being absent at THIS name: it may be another file, linked.
func linkCount(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Nlink)
	}
	return 1
}
