//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// folderRoom is whether this account may create files in dir and how many
// bytes its disk still offers this account. Windows has no access query that
// matches what a clone does, so a temporary file is made and removed.
func folderRoom(dir string) (writable bool, free int64) {
	var avail, total, all uint64
	if path, err := windows.UTF16PtrFromString(dir); err == nil && windows.GetDiskFreeSpaceEx(path, &avail, &total, &all) == nil {
		free = int64(avail)
	}
	if f, err := os.CreateTemp(dir, ".codeaf-folder-check-*"); err == nil {
		writable = true
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	return writable, free
}
