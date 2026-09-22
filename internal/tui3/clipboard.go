package tui3

import (
	"encoding/base64"
	"strings"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// GETTING TEXT OUT: the mouse, and the wire a copy goes down.
//
// This surface runs in the alt screen (view.go), which is what lets the
// conversation scroll under its own anchor — and which takes the terminal's
// own scrollback and selection away in the same breath. Two doors give a
// person their text back, and both are the mouse's: a drag across the rows
// copies what it covers the moment the button is released (dragselect.go), and
// ctrl+s hands the pointer to the terminal so its own drag works
// ([app.releaseMouse]). Every copy this surface makes — the sweep, a path
// under a row, a sign-in link, a job's log path — leaves through [osc52].
//
// THERE WAS A KEYBOARD DOOR, AND IT IS GONE. Copy mode — ctrl+b and /copy, a
// frozen viewport read with ↑↓, marked with v, taken by block with a and
// yanked with y — stood here from the alt screen's first day until 2026-09-22,
// when the owner judged it no use beside the drag and had it removed whole:
// the mode, the command, the status word, the keys row, the tip that taught
// it, and every gate the rest of the surface kept on it. ctrl+b is bound to
// nothing in a conversation now, and home's box keeps it as the caret's left.
//
// ── ctrl+s ──────────────────────────────────────────────────────────────────
//
// The surface takes the pointer by default (view.go): while it holds it,
// dragging across an answer scrolls or hovers, and the drag every person alive
// already knows selects nothing. dragselect.go answers that with a sweep of
// its own; this is the other answer, for a person who wants THEIR terminal's
// selection — its word-doubling, its rectangle, its paste buffer.
//
// ctrl+s gives the pointer to the terminal. Drag, copy the way that terminal
// copies, and the next key pressed here takes it back — there is no mode to
// leave and nothing to remember, because the gesture that ends it is the
// gesture that follows it anyway. While it is out, one dim line says so.
//
// It is deliberately NOT the ui.mouse setting under another name. The setting
// is a standing decision about how this surface behaves; this is a person
// reaching for one paragraph, which is a thing they do between two keystrokes
// and should not have to open a panel for.
//
// ── WHY OSC 52 AND NOT A CLIPBOARD LIBRARY ──────────────────────────────────
//
// Because the terminal may not be on this machine. OSC 52 is a clipboard write
// carried in-band, over the same pipe the drawing goes down, so it works
// through ssh and through a container without a display, and it is the only
// mechanism that does. Inside tmux it needs the passthrough wrapper — tmux
// eats sequences it does not recognize unless they are addressed to it —
// hence [tmuxTerm] and the doubled ESC below.
//
// Bubble Tea has [tea.SetClipboard], which sends the bare form. This file
// builds its own because the bare form is the one that silently does nothing
// inside a multiplexer, which is where a lot of these sessions live.

// selectKey hands the pointer over. ctrl+s survives the trip: the terminal is
// in raw mode while this surface is up, and raw mode is exactly what turns off
// the flow control that would otherwise have eaten it.
const selectKey = "ctrl+s"

// releaseMouse toggles the handover, and reports whether the surface had a
// pointer to hand over at all. With ui.mouse off the terminal already has it,
// so there is nothing to do and nothing to say — the drag being asked for
// works already.
func (a *app) releaseMouse() bool {
	if !a.mouse {
		return false
	}
	a.released = !a.released
	// THE HOVER GOES WITH IT. Nothing reports where the pointer is any more, so
	// whatever row was lit stays lit — a band under a pointer that has since
	// moved somewhere else entirely, sitting on the screen for the whole of the
	// drag somebody is trying to make (hover.go).
	if a.released {
		a.dropHover()
	}
	a.touch()
	return true
}

// takeMouseBack ends the handover on the person's next keystroke. It reports
// whether it did anything so the caller can stay quiet when it did not.
func (a *app) takeMouseBack() bool {
	if !a.released {
		return false
	}
	a.released = false
	a.touch()
	return true
}

// ── WHAT A COPY CARRIES ─────────────────────────────────────────────────────
//
// What a person copies must be what a person could PASTE. The sweep keeps the
// rows plain as well as painted, and it lifts the column the renderer draws
// down the left of a block — the stem under an expanded tool call, the
// hairline beside a fence. Those cells are the frame saying "these rows are
// one thing"; in a paste buffer they are a box-drawing character welded to the
// front of every line of somebody's stack trace.

// copyRails are the columns this surface draws down the LEFT of a block and
// repeats on every one of its rows: the stem an expanded tool's output hangs
// from (styles.go), under both its glyph sets, and the hairline beside a fenced
// code block or a blockquote (markdown.go).
//
// The one-off marks are NOT here and must not be. "› " on a message and "· " on
// a note sit on the first row of a block and say who is speaking, which is a
// fact somebody quoting a conversation usually wants kept. A rail says nothing
// except "these rows are one thing", which the paste already shows.
// The wrapped-code row's lead is here for [copyCodeRow]'s reason: a line the
// renderer split is still one line of source, and a paste that carried `↳ ` into
// the middle of it would be a paste that does not compile.
var copyRails = []string{railCont, railContASCII,
	tokens.GlyphCodeGutter + " ", mdContMark + tokens.GlyphCodeGutter + " "}

// copyClean is one drawn row as it should reach a clipboard: the drawn left
// rail lifted, and the trailing cells — hover padding, row padding — with it.
func copyClean(line string, gut int) string {
	// THE READING GUTTER IS FRAME FURNITURE AND NEVER TEXT (gutter.go), so it
	// comes off before anything else is decided. It is dropped by width rather
	// than by trimming, because what is left of the indent below IS text about
	// the block — a tool's output sits two columns in, and a copy that lost that
	// would paste a diff with its hierarchy flattened.
	line = strings.TrimPrefix(line, strings.Repeat(" ", gut))
	trimmed := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(trimmed)]
	for _, rail := range copyRails {
		if rest, ok := strings.CutPrefix(trimmed, rail); ok {
			// The indent BEFORE the rail goes too. It is the block's own inset on
			// the frame, not anything the text said about itself, and code inside a
			// fence keeps its own indentation because that sits after the rail.
			return strings.TrimRight(rest, " ")
		}
	}
	return strings.TrimRight(indent+trimmed, " ")
}

// copyCodeRow reports whether a drawn row belongs to a fenced block: it sits
// behind the hairline markdown puts down the left of one.
//
// IT ALSO KNOWS THE CONTINUATION MARKER, and it has to. A code line too long
// for the frame is wrapped rather than cut (markdown.go's [segmentedMarkdown]),
// and the row carrying the rest of it opens on [mdContMark] where its
// neighbours open on spaces — so a run of code rows read by the gutter alone
// ENDED at the first wrapped line (markdownwrap_test.go holds the case).
func copyCodeRow(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	trimmed = strings.TrimPrefix(trimmed, mdContMark)
	return strings.HasPrefix(trimmed, tokens.GlyphCodeGutter)
}

// ── OSC 52 ──────────────────────────────────────────────────────────────────

// osc52 is a clipboard write, in the form the terminal in front of us speaks.
//
//	ESC ] 52 ; c ; <base64> BEL                     the sequence itself
//	ESC P tmux ; <the sequence, ESC doubled> ESC \  the same, addressed to tmux
//
// The "c" is the CLIPBOARD selection rather than "p" (primary): a copy made on
// purpose, and primary is what a terminal's own drag fills.
func osc52(payload string, tmux bool) string {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(payload)) + "\a"
	if !tmux {
		return seq
	}
	// tmux forwards a DCS passthrough to the terminal underneath it verbatim,
	// with one rule: every ESC inside must be doubled, or tmux reads the first
	// one as the end of the passthrough.
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// tmuxTerm reports whether this surface is inside a multiplexer, from TERM
// alone. TERM is what tmux and screen both set for the session they host
// ("screen-256color", "tmux-256color"), and it is the one answer that is true
// whether the multiplexer was started before this process or around it —
// $TMUX, the other candidate, is unset in a pane that inherited its environment
// from somewhere else.
func tmuxTerm(env func(string) string) bool {
	if env == nil {
		return false
	}
	term := strings.ToLower(strings.TrimSpace(env("TERM")))
	return strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")
}

func clampInt(v, low, high int) int {
	if high < low {
		return low
	}
	return min(max(v, low), high)
}
