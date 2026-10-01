//go:build !windows

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// ownedByThisAccount says the folder info describes belongs to the account
// codeaf runs as ([privateCopyRoot]).
func ownedByThisAccount(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

// openProgramMessage pins the copy and notes directory before opening the
// message. NO LINKS ARE FOLLOWED, and a FIFO cannot block open between a
// path check and a descriptor check; only an opened regular file is read.
func openProgramMessage(dir, notes string) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	root, err := unix.Open(dir, flags|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	// Each directory component gets its own no-follow open so a notes
	// directory replaced with a link cannot reach outside the copy either.
	parent := root
	var owned []int
	defer func() {
		for _, fd := range owned {
			_ = unix.Close(fd)
		}
	}()
	for _, name := range strings.Split(filepath.ToSlash(notes), "/") {
		if name == "" || name == "." || name == ".." {
			return nil, fmt.Errorf("commit message notes path leaves the copy")
		}
		fd, err := unix.Openat(parent, name, flags|unix.O_DIRECTORY, 0)
		if err != nil {
			return nil, err
		}
		owned = append(owned, fd)
		parent = fd
	}
	fd, err := unix.Openat(parent, programCommitMessageFile, flags|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(dir, notes, programCommitMessageFile))
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("commit message is not a regular file")
	}
	return file, nil
}
