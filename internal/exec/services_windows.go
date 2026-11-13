//go:build windows

package exec

import (
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// ProcessStartTime reads native creation time, including a tracked launch whose
// leader has exited while descendants still live.
func ProcessStartTime(pid int) (time.Time, error) {
	started, err := processgroup.StartedAt(pid)
	return started.Truncate(time.Second), err
}

func serviceTreeAlive(pid int) bool { return processgroup.Alive(pid) }

func serviceTerminate(pid int) error {
	// A console process can refuse taskkill's non-force round. Still give it
	// the existing grace, then escalate and verify through job accounting.
	_ = processgroup.Terminate(pid)
	return nil
}

func unverifiedServiceStop(pid int) error {
	if processgroup.Alive(pid) {
		return fmt.Errorf("process tree %d is still running; its identity could not be verified", pid)
	}
	return nil
}

func finishServiceStop(pid int) error {
	if err := processgroup.Kill(pid); err != nil {
		return err
	}
	return processgroup.WaitEmpty(pid, serviceTerminateGrace)
}

func waitProcessGroupStop(done <-chan struct{}) error {
	timer := time.NewTimer(jobTerminateGrace)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("process tree is still running after hard kill")
	}
}
