//go:build linux || darwin

package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"golang.org/x/sys/unix"
)

// Resolve every directory through held descriptors with O_NOFOLLOW. No mutation
// follows an ancestor swapped for a symlink between admission and publication.
func workspaceApplicationParent(root, relative string) (int, string, error) {
	if !filepath.IsAbs(root) {
		return -1, "", errors.New("application root must be absolute")
	}
	path := filepath.Join(root, filepath.Dir(filepath.FromSlash(relative)))
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" || part == "." {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if err != nil {
			return -1, "", err
		}
		fd = next
	}
	return fd, filepath.Base(filepath.FromSlash(relative)), nil
}
func workspaceApplicationMoveOut(root, relative, staging, held string) error {
	from, name, err := workspaceApplicationParent(root, relative)
	if err != nil {
		return err
	}
	defer unix.Close(from)
	to, target, err := workspaceApplicationParent(staging, held)
	if err != nil {
		return err
	}
	defer unix.Close(to)
	return workspaceApplicationRenameAtExclusive(from, name, to, target)
}
func workspaceApplicationCreateDirectory(root, relative string, mode os.FileMode) error {
	fd, name, err := workspaceApplicationParent(root, relative)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return workspaceApplicationMkdir(fd, name, mode)
}
func workspaceApplicationMkdir(parent int, name string, mode os.FileMode) error {
	if err := unix.Mkdirat(parent, name, 0700); err != nil {
		return err
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var opened, named unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return err
	}
	if err := unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if opened.Uid != uint32(os.Getuid()) || opened.Dev != named.Dev || opened.Ino != named.Ino {
		return errors.New("new output directory ownership changed; recovery retained")
	}
	if err := unix.Fchown(fd, -1, os.Getgid()); err != nil {
		return errors.Join(err, discardWorkspaceApplicationEmptyDirectory(parent, name, opened))
	}
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		return errors.Join(err, discardWorkspaceApplicationEmptyDirectory(parent, name, opened))
	}
	return nil
}

// Only the still-held created inode may be removed, and only while empty.
func discardWorkspaceApplicationEmptyDirectory(parent int, name string, created unix.Stat_t) error {
	var actual unix.Stat_t
	if err := unix.Fstatat(parent, name, &actual, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if actual.Uid != created.Uid || actual.Dev != created.Dev || actual.Ino != created.Ino {
		return errors.New("new directory changed outside the transaction; recovery retained")
	}
	return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
}

func workspaceApplicationRenameExclusive(source, root, relative string) error {
	from, name, err := workspaceApplicationParent(filepath.Dir(source), filepath.Base(source))
	if err != nil {
		return err
	}
	defer unix.Close(from)
	to, target, err := workspaceApplicationParent(root, relative)
	if err != nil {
		return err
	}
	defer unix.Close(to)
	return workspaceApplicationRenameAtExclusive(from, name, to, target)
}
func workspaceApplicationRemove(root, relative string, directory bool) error {
	fd, name, err := workspaceApplicationParent(root, relative)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	flags := 0
	if directory {
		flags = unix.AT_REMOVEDIR
	}
	return unix.Unlinkat(fd, name, flags)
}
func workspaceApplicationChmod(root, relative string, mode os.FileMode) error {
	parent, name, err := workspaceApplicationParent(root, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fchmod(fd, uint32(mode.Perm()))
}

func workspaceApplicationDevice(path string) (uint64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("filesystem identity cannot be verified")
	}
	return uint64(stat.Dev), nil
}

func workspaceApplicationSetSymlinkMode(root, relative string, mode os.FileMode) error {
	parent, name, err := workspaceApplicationParent(root, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	return taskcopy.SetPortableSymlinkModeAt(parent, name, mode)
}

func workspaceApplicationDirectoryIdentity(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("application staging directory replaced; retained")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("staging identity cannot be verified")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
