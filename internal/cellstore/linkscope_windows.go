package cellstore

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// needsLinkScope compensates for engines whose portable Windows metadata does
// not report link counts yet. A changed hardlink can expand a delta's scope on
// Unix; request the same full plan natively rather than miss the C9 fallback.
func needsLinkScope(tree string, changed []string) bool {
	if changed == nil {
		return false
	}
	if len(changed) > maxChangedArgs {
		return true
	}
	for _, path := range changed {
		if !filepath.IsAbs(path) {
			path = filepath.Join(tree, filepath.FromSlash(path))
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return true
		}
		if !info.Mode().IsRegular() {
			continue
		}
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return true
		}
		handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			return true
		}
		var native windows.ByHandleFileInformation
		err = windows.GetFileInformationByHandle(handle, &native)
		_ = windows.CloseHandle(handle)
		if err != nil || native.NumberOfLinks != 1 {
			return true
		}
	}
	return false
}
