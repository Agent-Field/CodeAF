//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// folderRoom is whether this account may create files in dir and how many
// bytes its disk still offers this account. Windows has no access query that
// matches what a clone does, so it makes one temporary probe file in dir and
// removes it; a probe that cannot be removed is reported by name, never left
// behind in silence.
func folderRoom(dir string) (writable bool, free int64, err error) {
	var avail, total, all uint64
	if path, e := windows.UTF16PtrFromString(dir); e == nil && windows.GetDiskFreeSpaceEx(path, &avail, &total, &all) == nil {
		free = int64(avail)
	}
	f, e := os.CreateTemp(dir, ".codeaf-folder-check-*")
	if e != nil {
		return false, free, nil
	}
	name := f.Name()
	_ = f.Close()
	if e = os.Remove(name); e != nil {
		return true, free, fmt.Errorf("codeaf could not remove its check file %s", filepath.Base(name))
	}
	return true, free, nil
}
