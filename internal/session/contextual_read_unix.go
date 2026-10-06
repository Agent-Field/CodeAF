//go:build !windows

package session

import (
	"os"

	"golang.org/x/sys/unix"
)

// contextualOpenRead opens a source path for reading WITHOUT EVER BLOCKING ON A
// FIFO. An ordinary os.Open on a FIFO with no writer blocks until one appears,
// so a path swapped for a FIFO after its receipt was taken could stall the
// post-turn observation forever. O_NONBLOCK is ignored for a regular file and
// turns a FIFO into an immediate open whose descriptor then fails the
// regular-file check in [contextualReadFile]. O_CLOEXEC keeps the descriptor
// from leaking into a child.
func contextualOpenRead(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
