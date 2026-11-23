//go:build unix

package main

import (
	"os"
	"syscall"
)

// appLeafOpenFlags opens a contained leaf without following a link swapped in
// for it and without blocking on a pipe (appcheckoutopen.go).
const appLeafOpenFlags = os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOFOLLOW
