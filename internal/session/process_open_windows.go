//go:build windows

package session

import "os"

// openRegularNoFollow refuses a link or special file at the path, then checks
// the opened file is the one that was inspected (process_open_unix.go).
func openRegularNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		_ = file.Close()
		if err == nil {
			err = errNotRegular
		}
		return nil, err
	}
	return file, nil
}

