package tui3

import (
	"log"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

// ── THE TERMINAL'S MODE IS THE RENDERER'S PRECONDITION ──────────────────────
//
// The owner's floor, 2026-10-09: on a wide terminal, with the peek open, the
// peek's lines were painted at column 0 over the rows (`touches workspace
// identity      touches engine path resolution` across `#1100`), the stage
// row and the body text with them, and `thinking  —` landed in the middle of
// a word of the body (`session trees, wthinking  —`). Every frame this
// surface built was clean — every line exactly its width, no newline or
// control byte in a cell, the divider in one column (factory_render_test.go)
// — so the fault was below the frame.
//
// IT IS THE TERMINAL'S MODE. Bubble Tea puts the terminal in raw mode before
// the first frame and its renderer draws on that promise: to go down a row it
// writes a bare line feed and expects the column to stay where it was. When
// anything else sharing the terminal puts the cooked mode back (a child that
// saved the mode before the surface took it and restores it after, a tool
// that runs `stty sane`), the kernel turns every line feed into carriage
// return and line feed, and every row drawn after a move down starts at
// column 0. Put `stty opost onlcr` on a running floor and the owner's screen
// comes back exactly. The same restore turns the keyboard back to lines:
// keys and pointer reports wait in the kernel until a newline, which reads as
// a surface that answers late or in bursts.
//
// SO THE MODE THE PROGRAM SET IS WRITTEN DOWN AND HELD. The first message
// after the program started records the terminal's mode, and only when it is
// raw: a terminal Bubble Tea did not make raw is not this guard's to fight.
// Every message after that compares the mode, at most once per
// [ttyGuardEvery], and when anything changed it the recorded mode is put back
// and the whole screen is drawn again, because what was drawn while the mode
// was wrong is wherever the line feeds put it. It is checked on the loop and
// only there, so it can never race the program's own restore on the way out:
// no message is handled after that.

// ttyGuardEvery is how often the guard asks the terminal for its mode. A
// sweep is hundreds of messages a second and the ask is one system call; a
// mode changed under the surface is repaired within this much of the next
// message.
const ttyGuardEvery = 100 * time.Millisecond

// ttyGuardSays is how many repairs the log is told about.
const ttyGuardSays = 3

// ttyGuard holds the terminal's mode while the surface draws on it.
type ttyGuard struct {
	fd      uintptr
	on      bool
	held    bool
	want    ttyMode
	askedAt time.Time
	// repaired counts the times the mode was put back, for a test to state.
	repaired int
}

// newTTYGuard is the guard for a program drawing on the process's own
// terminal: stdin and stdout both a terminal, the ordinary launch. Every
// other launch (a test, a pipe, a hosted window with its own streams) gets a
// guard that does nothing.
func newTTYGuard(opts Options) ttyGuard {
	if opts.Input != nil || opts.Output != nil {
		return ttyGuard{}
	}
	in, out := os.Stdin.Fd(), os.Stdout.Fd()
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return ttyGuard{}
	}
	return ttyGuard{fd: out, on: ttyModeSupported}
}

// check is the guard on one message: nil while the mode is the one the
// program set, and a full repaint when it had to be put back.
func (g *ttyGuard) check(now time.Time) tea.Cmd {
	if !g.on || now.Sub(g.askedAt) < ttyGuardEvery {
		return nil
	}
	g.askedAt = now
	mode, ok := ttyModeOf(g.fd)
	if !ok {
		return nil
	}
	if !g.held {
		// THE FIRST ASK RECORDS THE MODE, and only a raw one: the program
		// made it raw before its first message, and a mode it did not make
		// raw is somebody's deliberate choice.
		if !mode.raw() {
			g.on = false
			return nil
		}
		g.want, g.held = mode, true
		return nil
	}
	if mode.same(g.want) {
		return nil
	}
	if !ttySetMode(g.fd, g.want) {
		return nil
	}
	g.repaired++
	// WHAT CHANGED IT IS NOT KNOWABLE FROM HERE, so what it changed is
	// written to the surface's log, the first few times, for whoever reads
	// the next report.
	if g.repaired <= ttyGuardSays {
		log.Printf("tui3: the terminal's mode was changed under the surface (%s); put back", mode.diff(g.want))
	}
	return tea.ClearScreen
}
