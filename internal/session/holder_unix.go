//go:build !windows

package session

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// stopProcess sends SIGTERM and never SIGKILL: every build answers the first
// by leaving through its ordinary road and the second by exiting at once
// (internal/leave), and a kill would take the journals with it.
func stopProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find the window holding this conversation: %w", err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("ask the window holding this conversation to stop: %w", err)
	}
	return nil
}

// psField is one column of the process table for one pid.
func psField(pid int, field string) (string, error) {
	out, err := exec.Command("ps", "-o", field, "-p", strconv.Itoa(pid)).Output()
	return string(out), err
}
