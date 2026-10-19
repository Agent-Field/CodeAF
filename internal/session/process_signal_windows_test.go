//go:build windows

package session

import (
	"syscall"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// C10 uses the platform's own process probes; Windows has no kill syscall.
func testProcessSignal(pid int, signal syscall.Signal) error {
	group := pid < 0
	if group {
		pid = -pid
	}
	if signal == 0 {
		alive := processgroup.ProcessAlive(pid)
		if group {
			alive = processgroup.Alive(pid)
		}
		if alive {
			return nil
		}
		return syscall.ESRCH
	}
	if signal == syscall.SIGKILL {
		return processgroup.Kill(pid)
	}
	return processgroup.Terminate(pid)
}
