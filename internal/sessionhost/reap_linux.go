//go:build linux

package sessionhost

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

type reapMode string

const (
	reapAll  reapMode = ""
	reapTmp  reapMode = "--tmp-only"
	reapInit reapMode = "--init"
)

func reap(args []string) error {
	if os.Geteuid() < minSessionUID || len(args) < 1 || len(args) > 2 {
		return errLayout
	}
	path, mode := args[0], reapAll
	if len(args) == 2 {
		mode = reapMode(args[1])
		if !filepath.IsAbs(path) {
			path, mode = args[1], reapMode(args[0])
		}
		if mode != reapTmp && mode != reapInit {
			return errLayout
		}
	}
	if !filepath.IsAbs(path) {
		return errLayout
	}
	dir, err := openReapDir(path)
	if err != nil {
		return errLayout
	}
	defer dir.Close()
	if err := reapAt(int(dir.Fd()), mode); err != nil {
		return errLayout
	}
	// The shared areas are swept in every mode, for the same reason the chat's
	// tmp folder is: a stopped chat, a deleted one and a new one on a recycled
	// Linux user all begin with nothing of an earlier chat's in them.
	if err := sweepShared(os.Geteuid(), sharedAreas, sysvDir); err != nil {
		return errLayout
	}
	return nil
}

func openReapDir(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func reapAt(fd int, mode reapMode) error {
	// Repair only a directory opened without following a link. Using its
	// proc descriptor also works when its owner removed all permissions.
	if err := os.Chmod(procFDPath(fd), 0o700); err != nil {
		return err
	}
	switch mode {
	case reapInit:
		for _, name := range []string{"home", "work", "tmp"} {
			if err := mkdirPrivateAt(fd, name); err != nil {
				return err
			}
		}
		return nil
	case reapTmp:
		if err := removeAt(fd, "tmp"); err != nil {
			return err
		}
		return mkdirPrivateAt(fd, "tmp")
	default:
		return removeContents(fd)
	}
}

func procFDPath(fd int) string {
	return "/proc/self/fd/" + strconv.Itoa(fd)
}

func mkdirPrivateAt(parent int, name string) error {
	if err := unix.Mkdirat(parent, name, 0o700); err != nil {
		return err
	}
	return unix.Fchmodat(parent, name, 0o700, 0)
}

func removeContents(fd int) error {
	// Read batches so the amount of memory does not depend on directory size.
	dir, err := os.Open(procFDPath(fd))
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		names, err := dir.Readdirnames(128)
		for _, name := range names {
			if err := removeAt(fd, name); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func removeAt(parent int, name string) error {
	// Unlinking first handles ordinary files and every kind of symlink
	// without opening them. Only directories need to be walked.
	err := unix.Unlinkat(parent, name, 0)
	if err == nil || errors.Is(err, unix.ENOENT) {
		return nil
	}
	if !errors.Is(err, unix.EISDIR) {
		return err
	}
	fd, err := unix.Openat(parent, name, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := reapAt(fd, reapAll); err != nil {
		return err
	}
	return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
}
