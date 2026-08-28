//go:build !windows

package enginehost

// signal_unix.go ends a host that cannot be asked to end itself.
//
// SIGTERM AND NEVER SIGKILL. Every build of the host has answered these two
// signals the same way since the first one — the handler in host.go stops the
// listener, closes every conversation and flushes every journal — so a term is
// a clean retirement even for a build that has never heard of the version
// exchange. A kill would take the journals with it, which is the one thing the
// whole persistent lane exists to protect.

import (
	"fmt"
	"os"
	"syscall"
)

func signalHost(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find the process holding that socket: %w", err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("ask the process holding that socket to stop: %w", err)
	}
	return nil
}
