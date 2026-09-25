//go:build unix

package skills

import (
	"os"
	"syscall"
)

// openNonblocking prevents a path swapped to a FIFO after Stat from hanging
// in open before the descriptor check can refuse it. O_CLOEXEC is what
// os.Open would have set: this process starts shells and workers of its own,
// and a descriptor opened here must not leak into any of them.
func openNonblocking(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
