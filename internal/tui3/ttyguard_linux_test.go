//go:build linux

package tui3

import (
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ttyLab is a pseudo-terminal's far end, standing in for the terminal the
// surface draws on, put in the raw mode Bubble Tea puts a terminal in.
func ttyLab(t *testing.T) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("the pseudo-terminal would not unlock: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("the pseudo-terminal has no number: %v", err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("the pseudo-terminal's far end would not open: %v", err)
	}
	t.Cleanup(func() { slave.Close() })
	mode, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	mode.Oflag &^= unix.OPOST
	mode.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
	if err := unix.IoctlSetTermios(int(slave.Fd()), unix.TCSETS, mode); err != nil {
		t.Fatal(err)
	}
	return slave
}

// ttyLabCook puts back the cooked mode a shell's `stty sane` would, the way
// something sharing the terminal did under the owner's floor.
func ttyLabCook(t *testing.T, tty *os.File) {
	t.Helper()
	mode, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	mode.Oflag |= unix.OPOST | unix.ONLCR
	mode.Lflag |= unix.ICANON | unix.ECHO
	if err := unix.IoctlSetTermios(int(tty.Fd()), unix.TCSETS, mode); err != nil {
		t.Fatal(err)
	}
}

// A MODE PUT BACK UNDER THE SURFACE IS UNDONE, AND THE SCREEN DRAWN WHOLE:
// the guard records the raw mode on its first look, leaves it alone while it
// holds, and when the terminal is cooked under it puts the raw mode back and
// asks for a full repaint, once.
func TestTheTerminalsModeIsPutBackWhenSomethingCooksIt(t *testing.T) {
	tty := ttyLab(t)
	g := ttyGuard{fd: tty.Fd(), on: true}
	at := time.Unix(1_000_000, 0)
	if cmd := g.check(at); cmd != nil || !g.held {
		t.Fatalf("the first look did not record the raw mode quietly: held %v", g.held)
	}
	if cmd := g.check(at.Add(time.Second)); cmd != nil {
		t.Fatal("an untouched raw mode asked for a repaint")
	}
	ttyLabCook(t, tty)
	if cmd := g.check(at.Add(time.Second + ttyGuardEvery/2)); cmd != nil {
		t.Fatal("the guard asked the terminal again inside its interval")
	}
	if cmd := g.check(at.Add(2 * time.Second)); cmd == nil {
		t.Fatal("a cooked terminal was left cooked under the surface")
	}
	mode, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if mode.Oflag&unix.OPOST != 0 || mode.Lflag&(unix.ICANON|unix.ECHO) != 0 {
		t.Fatalf("the raw mode was not put back: oflag %#x lflag %#x", mode.Oflag, mode.Lflag)
	}
	if cmd := g.check(at.Add(3 * time.Second)); cmd != nil || g.repaired != 1 {
		t.Fatalf("the repaired mode was repaired again: %d repairs", g.repaired)
	}
}

// A TERMINAL THE PROGRAM DID NOT MAKE RAW IS NOT THE GUARD'S TO FIGHT: a
// cooked mode on the first look stands the guard down for good.
func TestTheGuardLeavesACookedTerminalAlone(t *testing.T) {
	tty := ttyLab(t)
	ttyLabCook(t, tty)
	g := ttyGuard{fd: tty.Fd(), on: true}
	if cmd := g.check(time.Unix(1_000_000, 0)); cmd != nil || g.on {
		t.Fatalf("the guard took a cooked terminal for its own: on %v", g.on)
	}
	mode, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if mode.Oflag&unix.OPOST == 0 {
		t.Fatal("the guard changed a terminal it does not own")
	}
}
