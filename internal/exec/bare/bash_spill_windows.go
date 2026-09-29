//go:build windows

package bare

import (
	"os"

	"golang.org/x/sys/windows"
)

func bashSpillSingleLink(file *os.File) bool {
	var info windows.ByHandleFileInformation
	return windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info) == nil && info.NumberOfLinks == 1
}
