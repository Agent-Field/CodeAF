//go:build darwin || freebsd || netbsd || openbsd || dragonfly || linux

package tui3

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// ttyModeSupported says this platform can read and set a terminal's mode.
const ttyModeSupported = true

// ttyMode is a terminal's mode, the termios the kernel holds for it.
type ttyMode struct{ t unix.Termios }

// raw says the mode is the raw one a full-screen program draws in: no output
// processing, no line discipline, no echo.
func (m ttyMode) raw() bool {
	return m.t.Oflag&unix.OPOST == 0 && m.t.Lflag&(unix.ICANON|unix.ECHO) == 0
}

// same compares the flags that decide how bytes cross the terminal; the
// speeds and the control characters are not the renderer's concern.
func (m ttyMode) same(o ttyMode) bool {
	return m.t.Iflag == o.t.Iflag && m.t.Oflag == o.t.Oflag && m.t.Lflag == o.t.Lflag && m.t.Cflag == o.t.Cflag
}

// diff names the flags that differ from want, as the kernel spells them.
func (m ttyMode) diff(want ttyMode) string {
	return fmt.Sprintf("iflag %#x want %#x, oflag %#x want %#x, lflag %#x want %#x",
		m.t.Iflag, want.t.Iflag, m.t.Oflag, want.t.Oflag, m.t.Lflag, want.t.Lflag)
}

func ttyModeOf(fd uintptr) (ttyMode, bool) {
	t, err := unix.IoctlGetTermios(int(fd), ttyGetMode)
	if err != nil {
		return ttyMode{}, false
	}
	return ttyMode{t: *t}, true
}

func ttySetMode(fd uintptr, m ttyMode) bool {
	t := m.t
	return unix.IoctlSetTermios(int(fd), ttySetModeNow, &t) == nil
}
