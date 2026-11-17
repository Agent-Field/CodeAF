package fsreplace

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileRenameInfo follows the Windows SDK's FILE_RENAME_INFO (winbase.h) layout
// for FileRenameInfoEx: DWORD Flags, HANDLE RootDirectory, DWORD FileNameLength,
// WCHAR FileName[1]. The filename is a variable-length UTF-16 tail.
// https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_rename_info
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

func rename(old, new string) error {
	err := renamePOSIX(old, new)
	switch err {
	case nil:
		return nil
	case windows.ERROR_INVALID_PARAMETER, windows.ERROR_NOT_SUPPORTED, windows.ERROR_INVALID_FUNCTION:
		// Older Windows and filesystems without POSIX rename retain the native
		// replacement path. The source handle is closed before this fallback.
		return os.Rename(old, new)
	default:
		return &os.LinkError{Op: "rename", Old: old, New: new, Err: err}
	}
}

func renamePOSIX(old, new string) error {
	var names [2][]uint16
	for i, path := range []string{old, new} {
		name, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		// Match open_windows.go's extended-length drive and UNC paths.
		if len(name) >= 248 && !strings.HasPrefix(name, `\\?\`) {
			if strings.HasPrefix(name, `\\`) {
				name = `\\?\UNC\` + name[2:]
			} else {
				name = `\\?\` + name
			}
		}
		names[i], err = windows.UTF16FromString(name)
		if err != nil {
			return err
		}
	}
	handle, err := windows.CreateFile(&names[0][0], windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var attributes windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &attributes); err != nil {
		return err
	}
	if attributes.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		// Keep directory moves on os.Rename, including its replacement rules.
		return windows.ERROR_NOT_SUPPORTED
	}
	name := names[1]
	offset := unsafe.Offsetof(fileRenameInfo{}.FileName)
	buffer := make([]byte, int(offset)+len(name)*2)
	info := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.FileNameLength = uint32((len(name) - 1) * 2)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)
	return windows.SetFileInformationByHandle(handle, windows.FileRenameInfoEx, &buffer[0], uint32(len(buffer)))
}
