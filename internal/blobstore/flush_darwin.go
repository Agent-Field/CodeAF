//go:build darwin

package blobstore

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// flushFiles makes a batch of written files, or of directories that gained
// names, durable as one group commit, the same one syncfs gives a batch on
// Linux (flush_linux.go): every file's data is written to the device with a
// barrier fsync, ordered after the writes before it, and one final full sync
// flushes the disk's write cache, which is what commits the batch as a whole.
// A relay on a Mac pays a full disk flush per fsync the same way Linux pays a
// journal commit per one, and a large frame's pointers — one file per object —
// were what outlasted the client's deadline: the first publish of a chat was
// the best part of a second behind the call it carried. One file keeps its own
// full sync: the batch of one is the common case, and a barrier alone does not
// flush the write cache.
func flushFiles(files []*os.File) error {
	var errs []error
	for _, f := range files {
		errs = append(errs, fcntl(int(f.Fd()), unix.F_BARRIERFSYNC))
	}
	if len(files) > 0 {
		errs = append(errs, fcntl(int(files[0].Fd()), unix.F_FULLFSYNC))
	}
	return errors.Join(errs...)
}

// fcntl asks for one of the fsync kinds the plain interface has no name for.
func fcntl(fd, op int) error {
	if _, _, errno := unix.Syscall(unix.SYS_FCNTL, uintptr(fd), uintptr(op), 0); errno != 0 {
		return errno
	}
	return nil
}
