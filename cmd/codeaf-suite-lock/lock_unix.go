//go:build unix

package main

import (
	"os"
	"os/exec"
)

func inheritLock(cmd *exec.Cmd, lock *os.File) {
	cmd.ExtraFiles = append(cmd.ExtraFiles, lock)
}
