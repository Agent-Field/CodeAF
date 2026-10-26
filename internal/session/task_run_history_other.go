//go:build !linux && !darwin

package session

import (
	"errors"
	"os"
	"syscall"
)

func openTaskHistoryPath(root, relative string, directory bool) (*os.File, error) {
	if !noSymlinkAncestors(root) {
		return nil, errors.New("task history root is redirected")
	}
	base, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	info, err := base.Lstat(relative)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("task history is redirected or absent")
	}
	file, err := base.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || directory && !opened.IsDir() || !directory && !opened.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("task history is not an ordinary local file or directory")
	}
	return file, nil
}
