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

// pidVisibleHere reports whether a process with this pid exists in THIS pid
// namespace. kill(pid, 0) signals nothing: nil or EPERM means it is visible
// here, ESRCH means it is not, which says nothing about whether it is alive in
// another namespace or gone, only that its pid cannot be read from here.
func pidVisibleHere(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
