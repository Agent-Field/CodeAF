package fsreplace

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Append handles retain append-only writes; lock a read handle to coordinate
// them, since LockFileEx requires data access rather than FILE_APPEND_DATA.
func openFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	if path == "" {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	name, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if len(name) >= 248 && !strings.HasPrefix(name, `\\?\`) {
		if strings.HasPrefix(name, `\\`) {
			name = `\\?\UNC\` + name[2:]
		} else {
			name = `\\?\` + name
		}
	}
	wide, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	access := uint32(windows.GENERIC_READ)
	switch flag & (os.O_WRONLY | os.O_RDWR) {
	case os.O_WRONLY:
		access = windows.GENERIC_WRITE
	case os.O_RDWR:
		access = windows.GENERIC_READ | windows.GENERIC_WRITE
	}
	if flag&os.O_CREATE != 0 {
		access |= windows.GENERIC_WRITE
	}
	if flag&os.O_APPEND != 0 {
		access &^= windows.GENERIC_WRITE
		access |= windows.FILE_APPEND_DATA | windows.FILE_WRITE_ATTRIBUTES | windows.FILE_WRITE_EA | windows.STANDARD_RIGHTS_WRITE | windows.SYNCHRONIZE
	}
	disposition := uint32(windows.OPEN_EXISTING)
	attrs := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	if flag&os.O_CREATE != 0 {
		disposition = windows.OPEN_ALWAYS
		if flag&os.O_EXCL != 0 {
			disposition = windows.CREATE_NEW
			attrs |= windows.FILE_FLAG_OPEN_REPARSE_POINT
		}
	}
	if perm&0200 == 0 && flag&os.O_CREATE != 0 {
		attrs |= windows.FILE_ATTRIBUTE_READONLY
	}
	if flag&(os.O_WRONLY|os.O_RDWR) == os.O_RDONLY {
		attrs |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(wide, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, disposition, attrs, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}
