//go:build windows

package bare

import (
	"golang.org/x/sys/windows"
	"os"
)

func bashSpillSingleLink(file *os.File) bool {
	var info windows.ByHandleFileInformation
	return windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info) == nil && info.NumberOfLinks == 1
}
