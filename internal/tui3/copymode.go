package tui3

import (
	"encoding/base64"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// COPY MODE: ctrl+b, and the reason it exists is the alt screen.
//
// This surface runs in the alt screen (view.go), which is what lets the
// conversation scroll under its own anchor — and which takes the terminal's own
// scrollback and selection away in the same breath. A person who wants the
// stack trace that just went past has, without this, exactly two options: drag
// the mouse across it while the surface is also tracking the mouse, or scroll
// up and read it out loud to themselves.
//
// So: ctrl+b freezes the viewport and hands the keyboard to a reader.
//
//	↑ ↓ pgup pgdn   move the cursor through the frozen rows
//	v               drop a mark, or lift it
//	y               yank — the cursor's line, or the marked span
//	esc             leave, and rejoin the live edge
//	COPY            in the status line, for as long as it is up
//
// ── WHAT "FREEZES" MEANS ──
//
// The rows are SNAPSHOTTED on entry, painted and plain, and the frozen list is
// what the frame draws until esc. The conversation underneath keeps going — a
// turn that was running keeps streaming, tool rows keep landing, the follow-up
// queue keeps draining — and none of it moves the rows being read. That is the
// whole point: a viewport that reflowed under somebody trying to copy line 14
// would hand them line 19.
//
// The snapshot is also why a click does nothing while it is up (app.go): row 14
// of a frozen list is not row 14 of the conversation, and a click that expanded
// "whatever is there now" would open a call the person cannot see.
//
// ── WHY OSC 52 AND NOT A CLIPBOARD LIBRARY ──
//
// Because the terminal may not be on this machine. OSC 52 is a clipboard write
// carried in-band, over the same pipe the drawing goes down, so it works
// through ssh and through a container without a display, and it is the only
// mechanism that does. Inside tmux it needs the passthrough wrapper — tmux
// eats sequences it does not recognize unless they are addressed to it — hence
// [tmuxTerm] and the doubled ESC below.
//
// Bubble Tea has [tea.SetClipboard], which sends the bare form. This file
// builds its own because the bare form is the one that silently does nothing
// inside a multiplexer, which is where a lot of these sessions live.

// copyMode is the frozen viewport's whole state. The zero value is off, except
// for mark, which [newApp] sets to -1 — nothing is marked.
type copyMode struct {
	on bool
	// rows is the snapshot as it is drawn, and text the same rows stripped of
	// every escape sequence. Two slices rather than one strip-per-yank because
	// what a person copies must be what a person could paste: SGR in a paste
	// buffer is line noise in whatever they paste it into.
	rows []string
	text []string
	// at is the cursor's row, top the first row on screen, and mark the other
	// end of the selection or -1.
	at, top, mark int
}

// enterCopy freezes the viewport. It snapshots the CURRENT row list and parks
// the cursor on the last row a person can see, which is where their eye is —
// the live edge is what they were watching when they reached for the key.
func (a *app) enterCopy() {
	if a.copy.on {
		return
	}
	width := a.bodyWidth()
	height := a.viewHeight()
	rows := a.visible(width)
	if len(rows) == 0 {
		return
	}
	snapshot := make([]string, 0, len(rows))
	plain := make([]string, 0, len(rows))
	for _, r := range rows {
		snapshot = append(snapshot, r.text)
		plain = append(plain, ansi.Strip(r.text))
	}
	top := a.offsetFor(len(rows), height)
	at := min(top+height-1, len(rows)-1)
	a.copy = copyMode{on: true, rows: snapshot, text: plain, at: at, top: top, mark: -1}
	a.touch()
}

// exitCopy thaws it and rejoins the live edge, because a reader who has
// finished reading wants the conversation back.
//
// WHICHEVER EDGE WAS FROZEN. A room's rows are what [app.freezeRoom] snapshots,
// so thawing back onto the transcript's edge would drop the reader out of the
// page they were reading and lose the conversation's scroll on the way (room.go
// carried this as a known seam; the room's own stick is what closes it).
func (a *app) exitCopy() {
	a.copy = copyMode{mark: -1}
	if a.room != nil {
		a.room.stick = true
		a.roomTouched()
		return
	}
	a.stick = true
	a.follow()
	a.touch()
}

// copyKey routes the frozen viewport's keys and says whether it took one.
//
// It takes EVERYTHING except the keys read above it (ctrl+c is the door and is
// never modal), because copy mode is a reading mode: a keystroke that fell
// through to the draft would type into a box the person cannot see the effect
// of.
func (a *app) copyKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.copy.on {
		return nil, false
	}
	switch msg.String() {
	case "esc", "ctrl+b", "q":
		a.exitCopy()
	case "up", "k":
		a.copyScroll(-1)
	case "down", "j":
		a.copyScroll(1)
	case "pgup":
		a.copyScroll(-a.page())
	case "pgdown":
		a.copyScroll(a.page())
	case "home":
		a.copyScroll(-len(a.copy.rows))
	case "end":
		a.copyScroll(len(a.copy.rows))
	case "v":
		a.copyMark()
	case "y":
		return a.copyYank(), true
	}
	return nil, true
}

// copyScroll moves the cursor and keeps it on screen. The window follows the
// CURSOR rather than the other way round: there is no second position to keep
// in step, so there is nothing for the two to disagree about.
func (a *app) copyScroll(delta int) {
	c := &a.copy
	c.at = clampInt(c.at+delta, 0, len(c.rows)-1)
	height := a.viewHeight()
	if height < 1 {
		height = 1
	}
	switch {
	case c.at < c.top:
		c.top = c.at
	case c.at >= c.top+height:
		c.top = c.at - height + 1
	}
	c.top = clampInt(c.top, 0, max(len(c.rows)-height, 0))
	a.touch()
}

// copyMark drops the far end of a selection, or lifts it. The cursor is always
// the NEAR end: v then ↓↓↓ grows the span downward, exactly as it does in every
// other reader that has this key.
func (a *app) copyMark() {
	if a.copy.mark >= 0 {
		a.copy.mark = -1
	} else {
		a.copy.mark = a.copy.at
	}
	a.touch()
}

// copySpan is the selected range, inclusive, low first.
func (a *app) copySpan() (int, int) {
	if a.copy.mark < 0 {
		return a.copy.at, a.copy.at
	}
	if a.copy.mark <= a.copy.at {
		return a.copy.mark, a.copy.at
	}
	return a.copy.at, a.copy.mark
}

// copyYank writes the selection to the system clipboard and lifts the mark.
//
// It stays IN copy mode: a person copying a stack trace out of a log usually
// wants the next thing under it too, and esc is right there. The mark is lifted
// because leaving it would make the next y copy the same span again by
// accident.
func (a *app) copyYank() tea.Cmd {
	from, to := a.copySpan()
	if from < 0 || to >= len(a.copy.text) {
		return nil
	}
	// The trailing spaces are the hover padding and the row padding, and neither
	// is anything a person meant to copy.
	lines := make([]string, 0, to-from+1)
	for _, line := range a.copy.text[from : to+1] {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	a.copy.mark = -1
	a.touch()
	return tea.Raw(osc52(strings.Join(lines, "\n"), a.tmux))
}

// copyRows is what the frame draws while the viewport is frozen: the visible
// slice of the snapshot, with the selection highlighted.
//
// The selection wears the HOVER background — the same one step up the pointer
// draws — because it is the same statement: this row is the one. The cursor is
// the moving end of it, which is visible in the moving, and a terminal below
// ANSI256 gets no highlight at all and reads the span off the status line's
// count instead.
func (a *app) copyRows(width, height int) ([]row, int) {
	if height <= 0 || len(a.copy.rows) == 0 {
		return nil, 0
	}
	from, to := a.copySpan()
	// The window is clamped HERE as well as in [app.copyScroll], because the
	// frame can shrink between the two: a resize while the viewport is frozen
	// leaves a top that was legal for the old height, and a slice taken from it
	// would draw an empty screen rather than the rows somebody is reading.
	top := clampInt(a.copy.top, 0, max(len(a.copy.rows)-height, 0))
	end := min(top+height, len(a.copy.rows))
	out := make([]row, 0, end-top)
	for i := top; i < end; i++ {
		text := a.copy.rows[i]
		if i >= from && i <= to {
			text = a.pal.hover(text, width)
		}
		out = append(out, row{text: text, entry: -1})
	}
	if pad := height - len(out); pad > 0 {
		return out, pad
	}
	return out, 0
}

// copyWord is what the status line says while this is up. It carries the count
// as well as the mode, because a marked span longer than the screen is a span a
// person cannot otherwise measure.
func (a *app) copyWord() string {
	from, to := a.copySpan()
	if n := to - from + 1; n > 1 {
		return "COPY · " + itoa(n) + " lines"
	}
	return "COPY"
}

// ── OSC 52 ──────────────────────────────────────────────────────────────────

// osc52 is a clipboard write, in the form the terminal in front of us speaks.
//
//	ESC ] 52 ; c ; <base64> BEL                     the sequence itself
//	ESC P tmux ; <the sequence, ESC doubled> ESC \  the same, addressed to tmux
//
// The "c" is the CLIPBOARD selection rather than "p" (primary): a yank is a
// deliberate copy, and primary is what a mouse drag fills.
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
