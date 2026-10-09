//go:build linux

package tui3

import "golang.org/x/sys/unix"

const (
	ttyGetMode    = unix.TCGETS
	ttySetModeNow = unix.TCSETS
)
