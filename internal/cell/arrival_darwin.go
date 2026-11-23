//go:build darwin

package cell

import (
	"errors"
	"strconv"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/darwinproc"
)

// platformProcessStatus asks the kernel's process table, which has no /proc to
// read here. It answers the start time that tells a recycled pid from the owner
// and shows a zombie for what it is. When the table will not answer for this
// process (another user's, for one) it falls back to signal 0, which works on a
// Mac, and when neither can tell the owner is taken to be alive.
func platformProcessStatus(pid int) (startToken string, alive bool) {
	info, err := darwinproc.Task(pid)
	switch {
	case err == nil:
		if info.Status == darwinproc.StatusZombie {
			return "", false
		}
		return strconv.FormatUint(info.Start, 10), true
	case errors.Is(err, syscall.ESRCH):
		return "", false
	}
	return "", signalAlive(pid)
}
