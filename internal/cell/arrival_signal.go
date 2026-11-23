//go:build !windows

package cell

import (
	"os"
	"syscall"
)

// signalAlive asks the kernel with signal 0, which delivers nothing. It is the
// probe for a Unix that has no richer answer, and the fallback where the richer
// one is unavailable. A probe that cannot be made counts as alive, never dead.
func signalAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	return signalProbeAlive(proc.Signal(syscall.Signal(0)))
}
