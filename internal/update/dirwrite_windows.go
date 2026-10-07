//go:build windows

package update

import (
	"os"

	"golang.org/x/sys/windows"
)

// dirWritable reports whether this account may create files in dir, the
// permission an in-place replacement needs. A failed create in an unwritable
// folder leaves nothing behind, so a probe is the honest answer here.
func dirWritable(dir string) bool {
	if _, err := os.Stat(dir); err != nil {
		return true
	}
	file, err := windows.CreateFile(
		windows.StringToUTF16Ptr(dir+`\.codeaf-writecheck`),
		windows.GENERIC_WRITE, 0, nil, windows.CREATE_ALWAYS, windows.FILE_ATTRIBUTE_TEMPORARY, 0,
	)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(file)
	_ = windows.DeleteFile(windows.StringToUTF16Ptr(dir + `\.codeaf-writecheck`))
	return true
}
