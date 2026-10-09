//go:build !(darwin || freebsd || netbsd || openbsd || dragonfly || linux)

package tui3

// ttyModeSupported is false where the terminal's mode is not a termios: the
// guard does nothing there.
const ttyModeSupported = false

type ttyMode struct{}

func (ttyMode) raw() bool               { return false }
func (ttyMode) same(ttyMode) bool       { return true }
func (ttyMode) diff(ttyMode) string     { return "" }
func ttyModeOf(uintptr) (ttyMode, bool) { return ttyMode{}, false }
func ttySetMode(uintptr, ttyMode) bool  { return false }
