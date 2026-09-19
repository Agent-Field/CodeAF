//go:build windows

package main

import (
	"os"
	"os/exec"
)

func inheritLock(_ *exec.Cmd, _ *os.File) {}

// holderAlive is a no-op on Windows: the lock is not inherited by the child
// (inheritLock does nothing), so the recorded holder is the only holder.
func holderAlive(_ int) bool { return true }
