//go:build windows

package session

import (
	"os"
	"path/filepath"
)

// openRegularNoFollow refuses a link or special file at the path, then checks
// the opened file is the one that was inspected (process_open_unix.go).
func openRegularNoFollow(path string) (*os.File, error) {
	return openRegularIn(filepath.Dir(path), filepath.Base(path))
}

// openRegularIn refuses a link at any component below the trusted anchor and
// answers only a regular file that is the one inspected. There are no FIFOs to
// block on here.
func openRegularIn(anchor string, rel ...string) (*os.File, error) {
	if len(rel) == 0 {
		return nil, errNotRegular
	}
	path := anchor
	var last os.FileInfo
	for i, name := range rel {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return nil, errNotRegular
		}
		path = filepath.Join(path, name)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(rel)-1 && !info.IsDir()) {
			return nil, errNotRegular
		}
		last = info
	}
	if !last.Mode().IsRegular() {
		return nil, errNotRegular
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(last, opened) {
		_ = file.Close()
		if err == nil {
			err = errNotRegular
		}
		return nil, err
	}
	return file, nil
}

// rootReadFlags has nothing to add here: links are refused by the inspection
// in readSmall, and there are no FIFOs to wait on.
const rootReadFlags = 0
