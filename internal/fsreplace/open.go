package fsreplace

import "os"

// Open is os.Open with delete sharing on Windows. Use it for readers held
// across another writer's replacement or removal of the same state file.
func Open(path string) (*os.File, error) { return OpenFile(path, os.O_RDONLY, 0) }

// OpenFile opens a state handle that permits replacement while it is held.
// It supports reading, writing or appending, optionally with O_CREATE/O_EXCL.
// Other flags are refused so callers cannot silently acquire different semantics.
func OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	const supported = os.O_WRONLY | os.O_RDWR | os.O_APPEND | os.O_CREATE | os.O_EXCL
	if flag & ^supported != 0 || flag&(os.O_WRONLY|os.O_RDWR) == os.O_WRONLY|os.O_RDWR {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrInvalid}
	}
	return openFile(path, flag, perm)
}
