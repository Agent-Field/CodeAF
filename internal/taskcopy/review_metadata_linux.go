//go:build linux

package taskcopy

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func portableFilesystemFlags(path string, info fs.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return errors.New("filesystem metadata identity changed")
	}
	flags, err := unix.IoctlGetInt(fd, unix.FS_IOC_GETFLAGS)
	if err != nil {
		return errors.New("portable apply cannot verify filesystem flags")
	}
	// Extents and directory indexing describe filesystem implementation, not
	// portable user metadata. All other native flags require a retained case.
	const implementationFlags = 0x80000 | 0x1000
	if flags & ^implementationFlags != 0 {
		return errors.New("portable apply cannot preserve native filesystem flags")
	}
	return nil
}

func portablePlatformMetadataBatch(ctx context.Context, paths []string) error { return ctx.Err() }

// SetPortableSymlinkModeAt refuses modes Linux cannot represent on link inodes.
func SetPortableSymlinkModeAt(parentFD int, name string, mode fs.FileMode) error {
	if mode != 0777 {
		return errors.New("this platform cannot preserve the reviewed symlink permissions")
	}
	return nil
}

// OSManagedAttribute reports whether an extended attribute is one the
// operating system stamps on its own and that portable apply may leave
// behind. Linux stamps none, so every attribute is refused as before.
func OSManagedAttribute(string) bool { return false }
