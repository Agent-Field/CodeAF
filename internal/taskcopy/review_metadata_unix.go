//go:build linux || darwin

package taskcopy

import (
	"errors"
	"io/fs"
	"os"
	"strings"
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
		names, err := portableAttributeNames(path, size)
		if err != nil {
			return err
		}
		for _, name := range names {
			if !OSManagedAttribute(name) {
				return errors.New("portable apply cannot preserve extended attributes or access-control metadata")
			}
		}
	}
	return portableFilesystemFlags(path, info)
}

// portableAttributeNames reads the extended attribute names on path without
// following a symlink. The list can grow between the size query and the read,
// so a short buffer is retried once with the new size before it is an error.
func portableAttributeNames(path string, size int) ([]string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		buffer := make([]byte, size)
		n, err := unix.Llistxattr(path, buffer)
		if errors.Is(err, unix.ERANGE) {
			if size, err = unix.Llistxattr(path, nil); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		var names []string
		for _, name := range strings.Split(string(buffer[:n]), "\x00") {
			if name != "" {
				names = append(names, name)
			}
		}
		return names, nil
	}
	return nil, errors.New("extended attributes changed while they were being read")
}
