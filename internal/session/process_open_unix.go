//go:build !windows

package session

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// openRegularNoFollow opens one file for reading WITHOUT following a link at
// its last component and without blocking on a FIFO, and answers it only when
// the opened descriptor is a regular file.
func openRegularNoFollow(path string) (*os.File, error) {
	return openRegularIn(filepath.Dir(path), filepath.Base(path))
}

// openRegularIn opens anchor/rel… for reading. The anchor is a folder the
// caller trusts (a conversation's own folder) and may itself be reached
// through a link the person made; NOTHING BELOW IT IS FOLLOWED — each
// intermediate folder is opened with no link following, relative to the one
// before — and the last component is opened non-blocking, so a FIFO swapped in
// after any check cannot hold the caller (or the record lock it holds) until a
// writer appears. Only an opened regular file is answered.
func openRegularIn(anchor string, rel ...string) (*os.File, error) {
	if len(rel) == 0 {
		return nil, errNotRegular
	}
	const dirFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_DIRECTORY
	parent, err := unix.Open(anchor, dirFlags, 0)
	if err != nil {
		return nil, notExist(err)
	}
	defer func() { _ = unix.Close(parent) }()
	for _, name := range rel[:len(rel)-1] {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return nil, errNotRegular
		}
		fd, err := unix.Openat(parent, name, dirFlags|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, notExist(err)
		}
		_ = unix.Close(parent)
		parent = fd
	}
	last := rel[len(rel)-1]
	if last == "" || last == "." || last == ".." || filepath.Base(last) != last {
		return nil, errNotRegular
	}
	fd, err := unix.Openat(parent, last, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, notExist(err)
	}
	file := os.NewFile(uintptr(fd), filepath.Join(append([]string{anchor}, rel...)...))
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

// notExist keeps "nothing there" distinguishable from a refusal.
func notExist(err error) error {
	if errors.Is(err, unix.ENOENT) {
		return os.ErrNotExist
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return errNotRegular
	}
	return err
}

// rootReadFlags are added when a record is opened through the store's root:
// no link at the last name, and no waiting on a FIFO.
const rootReadFlags = unix.O_NOFOLLOW | unix.O_NONBLOCK

// linkRefused says an open failed because the name was a link it would not
// follow.
func linkRefused(err error) bool { return errors.Is(err, unix.ELOOP) }
