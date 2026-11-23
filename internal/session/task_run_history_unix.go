//go:build linux || darwin

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openTaskHistoryPath(root, relative string, directory bool) (*os.File, error) {
	parent, name, err := workspaceApplicationParent(root, relative)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(parent, name, flags, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil || directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("task history is not an ordinary local file or directory")
	}
	return file, nil
}
