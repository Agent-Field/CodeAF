//go:build linux || darwin

package taskcopy

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func portablePlatformMetadata(path string, info fs.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("filesystem ownership metadata cannot be verified")
	}
	if stat.Uid != uint32(os.Getuid()) || stat.Gid != uint32(os.Getgid()) {
		return errors.New("portable apply cannot preserve a different owner or group")
	}
	if !info.IsDir() && stat.Nlink > 1 {
		return errors.New("portable apply cannot preserve hardlink topology")
	}
	return portableUnsupportedNativeMetadata(path, info)
}

func portableUnsupportedNativeMetadata(path string, info fs.FileInfo) error {
	size, err := unix.Llistxattr(path, nil)
	if err != nil && !errors.Is(err, unix.ENOTSUP) {
		return err
	}
	if size > 0 {
		return errors.New("portable apply cannot preserve extended attributes or access-control metadata")
	}
	return portableFilesystemFlags(path, info)
}
