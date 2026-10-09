//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package tui3

import "golang.org/x/sys/unix"

const (
	ttyGetMode    = unix.TIOCGETA
	ttySetModeNow = unix.TIOCSETA
)
