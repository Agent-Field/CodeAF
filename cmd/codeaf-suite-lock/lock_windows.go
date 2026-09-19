//go:build windows

package main

import (
	"os"
	"os/exec"
)

func inheritLock(_ *exec.Cmd, _ *os.File) {}
