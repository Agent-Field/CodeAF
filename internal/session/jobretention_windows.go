//go:build windows

package session

import (
	"golang.org/x/sys/windows"
	"os"
)

func jobRetentionSingleLink(file *os.File) bool {
	var info windows.ByHandleFileInformation
	return windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info) == nil && info.NumberOfLinks == 1
}

// Windows does not support FlushFileBuffers on a directory opened for reading.
// The counter file itself is flushed before atomic rooted replacement.
func jobRetentionSyncDir(root *os.Root) error { return nil }
