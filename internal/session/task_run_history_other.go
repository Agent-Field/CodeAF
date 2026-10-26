//go:build !linux && !darwin

package session

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
	target, name := base, relative
	if !directory {
		target, name, err = pinTaskHistoryParent(base, relative)
		if err != nil {
			return nil, err
		}
		defer target.Close()
	}
	info, err := target.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("task history is redirected or absent")
	}
	file, err := target.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
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

// Pin the single task directory before opening its header. A symlink swapped
// between listing and reading must not redirect a known task to another task.
func pinTaskHistoryParent(base *os.Root, relative string) (*os.Root, string, error) {
	parent := filepath.Dir(relative)
	id, err := strconv.ParseUint(parent, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != parent {
		return nil, "", errors.New("task history parent is not a task directory")
	}
	before, err := base.Lstat(parent)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, "", errors.New("task history parent is redirected or absent")
	}
	pinned, err := base.OpenRoot(parent)
	if err != nil {
		return nil, "", err
	}
	opened, err := pinned.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		_ = pinned.Close()
		return nil, "", errors.New("task history parent changed while opening")
	}
	return pinned, filepath.Base(relative), nil
}
