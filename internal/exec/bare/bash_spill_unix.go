//go:build !windows

package bare

import (
	"os"
	"syscall"
)

func bashSpillSingleLink(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}
