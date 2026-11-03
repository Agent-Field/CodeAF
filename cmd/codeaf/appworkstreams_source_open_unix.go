//go:build unix

package main

import (
	"errors"
	"os"
	"syscall"
)

// Refuse final-component links atomically and keep FIFO replacement reads nonblocking.
func workstreamAgentOpenSourcePath(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, errWorkstreamSourceLink
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
