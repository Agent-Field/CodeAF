//go:build !windows

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// openRegularNoFollow opens one file for reading WITHOUT following a link at
// its last component and without blocking on a FIFO, and answers it only when
// the opened descriptor is a regular file. A process log is read by the path
// its own record names, and that path must not become a door to anything else.
func openRegularNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err == nil {
			err = errNotRegular
		}
		return nil, err
	}
	return file, nil
}
