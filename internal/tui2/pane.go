package tui2

import (
	"image"

	tea "charm.land/bubbletea/v2"
)

// The seam between the shell and everything that will eventually fill it.
//
// SEAM — internal/tui2/blocks: a block renderer satisfies Pane and is bound to
// a layer with Shell.SetPane. The shell owns geometry, focus and dispatch; a
// pane owns bytes. Nothing crosses that line: a pane is never told where it is
// on screen, only how big it is, and it never receives an absolute mouse
// coordinate — which is why a pane cannot subtract the wrong origin, because
// it is never given one to subtract.
//
// The contract a pane must keep:
//
//   - Render(w, h) returns at most h lines, each at most w cells wide. Over-run
//     is clipped by the compositor rather than trusted, so a bug here is a
//     visual truncation and never a broken frame.
//   - Render is a pure function of the pane's state and the size. It must not
//     mutate anything the shell can see; the old surface's render-mutates-model
//     habit is exactly what 4.4 rebuilt away from.
//   - When a pane's content changes for a reason the shell cannot observe (a
//     stream arriving, a timer), it calls Shell.Invalidate. The shell repaints
//     when a fact moved and not otherwise — that is what makes the bytes on the
//     wire proportional to what actually changed.
//   - A pane is given its WHOLE rectangle and the shell reserves nothing
//     inside it. This is the truncation law's structural half (12.5.2): a turn
//     cut short by a length cap or a dropped stream has to render visibly cut,
//     and a mark the shell had quietly borrowed the last row for is a mark the
//     reader never sees. The frame that carries the cut turn owes it a row, so
//     the shell never takes one.

// Pane renders one layer.
type Pane interface {
	Render(width, height int) string
}

// PaneMouse is implemented by a pane that wants clicks and wheel events inside
// its own rectangle. The point is pane-local: (0,0) is the pane's top-left.
type PaneMouse interface {
	Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd
}

// PaneHover is implemented by a pane that previews what a click would do.
//
// It is deliberately NOT the same door as [PaneMouse]. A hover is not an event
// the pane acts on; it is a position the pane may draw differently, and it must
// never move the real cursor or take focus — 5.14 allows exactly one cursor,
// and a pointer resting somewhere is not it. Splitting the door is what makes
// that a property of the shape: nothing reachable from Hover returns a command,
// so a hover cannot post, answer, navigate or spend.
//
// The shell calls Hover(pt, true) on the pane under the pointer and
// Hover(_, false) on the pane the pointer just left, so a pane never has to
// work out that it stopped being hovered. It returns whether the frame moved;
// false costs the terminal nothing, which is the whole reason a pointer may
// cross the screen at all (10.1.1: bytes over SSH is the metric).
type PaneHover interface {
	Hover(local image.Point, inside bool) bool
}

// PaneKeys is implemented by a pane that takes the keyboard while it holds
// focus. The shell keeps the bindings that must work everywhere (interrupt,
// quit) and offers the rest here.
type PaneKeys interface {
	Key(msg tea.KeyPressMsg) tea.Cmd
}

// PaneFocus is implemented by a pane that renders differently when it is the
// one being talked to. "You talk to what you're looking at" (5.15) is the
// shell's rule to enforce; this is how a pane hears the outcome.
type PaneFocus interface {
	Focus(focused bool)
}

// paneFunc adapts a plain function into a Pane. The skeleton's own placeholder
// content uses it, and it keeps test panes to one line.
type paneFunc func(width, height int) string

func (f paneFunc) Render(width, height int) string { return f(width, height) }
