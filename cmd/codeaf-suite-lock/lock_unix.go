//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func inheritLock(cmd *exec.Cmd, lock *os.File) {
	cmd.ExtraFiles = append(cmd.ExtraFiles, lock)
}

// holderAlive probes whether the recorded lock holder still exists. kill(pid, 0)
// signals nothing: nil or EPERM means the process is there, ESRCH means it is
// gone and the lock is being kept alive by a descriptor it leaked to a child.
func holderAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
