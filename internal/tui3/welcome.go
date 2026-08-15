package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// THE WELCOME BOX: the first thing an empty session shows, and the last time it
// shows it.
//
// A terminal that opens on a bare prompt is a terminal that tells a first-time
// reader nothing and a returning one less: which model is answering, which
// directory this is, and — the fact a chat surface is worst at — WHAT WAS I
// DOING YESTERDAY. So an empty conversation opens with one box above the input:
// the wordmark and where you are on the left, the four sessions you were last
// in on the right, and enter or a click on one of them opens it.
//
// Three rules, and the first two are the whole of why this is not chrome:
//
//   - IT SHOWS ONCE. The first submit, the first key, the first click — any of
//     them and the box is gone for the life of the surface. Nothing brings it
//     back, because a box that returns is a box a person has to dismiss twice.
//   - IT NEVER SHOWS OVER A CONVERSATION. A resumed session has a transcript,
//     and the transcript is the answer to "where was I" — a welcome box above it
//     would be the surface answering a question the screen already answered.
//   - IT ANIMATES IN ONCE AND THEN IS STILL. A slow matte sweep across the
//     letters, easing out to static over about a second and a quarter, and then
//     nothing moves on this surface again until the person types. No loop, no
//     flash, no second run: motion that repeats is motion a reader has to learn
//     to ignore, which is the definition of noise.
//
// The animation is counted in FRAMES rather than measured against the clock
// (app.go's [frameInterval] is the only clock here), so what it looks like does
// not depend on how busy the machine was — and so a test can assert the settled
// state without sleeping through it.

// The animation's three lengths, in frames of the 33ms paint clock.
const (
	// welcomeSlide is the box arriving: about 300ms of a one-cell slide and a
	// fade up from dim.
	welcomeSlide = 9
	// welcomeSweep is the wordmark's breath: about 1.2s for the pastel to cross
	// the letters, easing out.
	welcomeSweep = 36
	// welcomeFrames is when everything is still. The paint clock stops asking
	// for ticks here (see [app.paint]) — nothing on this surface animates by
	// itself afterwards.
	welcomeFrames = welcomeSweep + 4
)

// welcomeSlots is how many recent sessions the right column holds. It is FIXED:
// the box is the same height with four sessions, with one, and with none, so
// the input line under it does not move while a person is reading the box above
// it.
const welcomeSlots = 4

// Session is one row of the welcome box's right column: a conversation this
// directory has had before.
type Session struct {
	// Title is the name the session gave itself. Empty falls back to the file's
	// own name, because a row with no words is a row nobody can choose between.
	Title string
	// File is the transcript, and it is what [Options.Resume] is handed.
	File string
	// At is when it was last written.
	At time.Time
}

// welcome is the box's whole state. The zero value is a surface that never had
// one, which is what a resumed session is.
type welcome struct {
	open bool
	// spent says the box has already been dismissed. It is separate from open
	// so that nothing — a resize, a /new, a stray frame — can put it back.
	spent bool
	// step counts frames since it opened, and stops at [welcomeFrames].
	step int
	// sel is the recent row under the cursor, or -1. Only ↑/↓ on an empty draft
	// move it (see [app.welcomeKey]).
	sel    int
	recent []Session
}

func (w *welcome) animating() bool { return w.open && w.step < welcomeFrames }

// tick advances the animation by one frame and clamps at the end.
func (w *welcome) tick() {
	if w.open && w.step < welcomeFrames {
		w.step++
	}
}

// openWelcome decides, once, whether this surface gets a box. It is called
// from [newApp] after the replay, so "empty" means what a reader means by it:
// there is nothing on screen.
func (a *app) openWelcome() {
	if a.resumed || len(a.entries) > 0 {
		return
	}
	a.welcome = welcome{open: true, sel: -1}
	if a.recentSessions != nil {
		list := a.recentSessions()
		if len(list) > welcomeSlots {
			list = list[:welcomeSlots]
		}
		a.welcome.recent = list
	}
}

// dismissWelcome puts the box away for good.
func (a *app) dismissWelcome() {
	if !a.welcome.open {
		return
	}
	a.welcome = welcome{spent: true}
	a.touch()
}

// welcomeKey is the box's claim on the keyboard, and it is deliberately two
// keys wide.
//
// Everything dismisses the box — that is the contract — EXCEPT the walk through
// the recent list, which would otherwise be unreachable: a person cannot select
// a row with an arrow key if the arrow key closes the thing the row is in. So
// ↑/↓ over an empty draft move the selection, enter on a selected row opens it,
// and every other key is the person starting work, which is what dismissal
// means.
//
// It reports whether it took the key. A key it did not take still dismisses,
// and then goes on to do whatever it always does.
func (a *app) welcomeKey(name string) bool {
	if !a.welcome.open {
		return false
	}
	switch name {
	case "up", "down":
		if !a.input.empty() || len(a.welcome.recent) == 0 {
			return false
		}
		delta := 1
		if name == "up" {
			delta = -1
		}
		if a.welcome.sel < 0 {
			// From nowhere, either arrow takes the most recent session: it is
			// the top of the list and it is the row a person reaching for this
			// list means nine times in ten.
			a.welcome.sel = 0
		} else {
			a.welcome.sel = moveCursor(a.welcome.sel, delta, len(a.welcome.recent))
		}
		a.touch()
		return true

	case "enter":
		if a.welcome.sel < 0 || a.welcome.sel >= len(a.welcome.recent) {
			return false
		}
		chosen := a.welcome.recent[a.welcome.sel]
		a.dismissWelcome()
		a.resumeSession(chosen)
		return true
	}
	return false
}

// resumeSession swaps this surface onto an earlier conversation.
//
// It is [app.renew] with the sign flipped: the same close, the same wholesale
// reset of everything that belonged to the session being left, and then a
// replay instead of an empty screen. The two share no code because they share
// no seam — one asks the door for a NEW agent, the other for a named one — and
// the reset is written out here rather than factored so that a field added to
// the surface is a compile error in both places rather than a stale value in
// one.
func (a *app) resumeSession(chosen Session) {
	if a.resume == nil {
		a.note("resuming is unavailable here")
		return
	}
	if a.state == stateWorking && a.agent != nil {
		a.agent.Interrupt()
	}
	if a.agent != nil {
		if err := a.agent.Close(); err != nil {
			a.note("close failed: " + err.Error())
		}
	}
	agent, err := a.resume(chosen.File)
	if err != nil {
		a.note("resume failed: " + err.Error())
		return
	}
	a.agent, a.file = agent, chosen.File
	a.entries = nil
	a.live, a.sel, a.think = -1, -1, -1
	a.asks, a.follows = nil, nil
	a.turn = 0
	a.unfolded = map[int]bool{}
	a.dropHover()
	a.stream = nil
	a.gen++
	a.state = stateIdle
	a.resetMeters()
	a.model = agent.Model()
	a.title = strings.TrimSpace(agent.Title())
	a.resumed = true
	a.endRecall()
	a.offset, a.stick = 0, true
	a.replay()
	a.measureContext()
	a.note("resumed " + chosen.File)
}

// welcomePress is a click inside the box: on a recent row it opens that
// session, anywhere else it is the person reaching past the box, which
// dismisses it.
func (a *app) welcomePress(slot int) {
	if !a.welcome.open {
		return
	}
	if slot < 0 || slot >= len(a.welcome.recent) {
		a.dismissWelcome()
		return
	}
	chosen := a.welcome.recent[slot]
	a.dismissWelcome()
	a.resumeSession(chosen)
}

// ── the drawing ─────────────────────────────────────────────────────────────

// The wordmark, three rows of it, one entry per letter of [product]. It is
// drawn from box-drawing characters rather than from a figlet font because a
// figlet 'openaf' is nine rows of hash marks and this surface owns two: the
// letterform here is the same vocabulary the rail and the rules are drawn in,
// which is the whole reason it reads as part of the surface rather than as
// something pasted onto it.
var wordmarkGlyphs = map[rune][3]string{
	'o': {"┌─┐", "│ │", "└─┘"},
	'p': {"┌─┐", "├─┘", "│  "},
	'e': {"┌─┐", "├─ ", "└─┘"},
	'n': {"┌─┐", "│ │", "│ │"},
	'a': {"┌─┐", "├─┤", "└─┘"},
	'f': {"┌─ ", "├─ ", "│  "},
}

// wordmarkRows is the wordmark as three unpainted rows, and the column each
// letter starts on. A terminal that cannot draw the box characters gets the
// word itself — the same information, one row instead of three, and no
// mojibake (styles.go's [detectASCII] is the same veto the rail obeys).
func wordmarkRows(ascii bool) []string {
	if ascii {
		return []string{product}
	}
	rows := [3]string{}
	for i, letter := range product {
		glyph, ok := wordmarkGlyphs[letter]
		if !ok {
			continue
		}
		for r := 0; r < 3; r++ {
			if i > 0 {
				rows[r] += " "
			}
			rows[r] += glyph[r]
		}
	}
	return rows[:]
}

// sweepAt is where the breath has reached, as a column, easing out.
//
// The easing is 1-(1-t)² — fast at the start, almost stopped at the end — which
// is the difference between a sweep that arrives and one that simply travels.
// Past [welcomeSweep] it is off the end of the word and every letter is at rest.
func sweepAt(step, span int) int {
	if step >= welcomeSweep {
		return span + welcomeHead
	}
	if step <= 0 {
		return -welcomeHead
	}
	// Fixed point: the fractions here are small and integers are exact.
	t := step * 1000 / welcomeSweep
	eased := 1000 - (1000-t)*(1000-t)/1000
	return (span+2*welcomeHead)*eased/1000 - welcomeHead
}

// welcomeHead is how many cells of the sweep are lit at once. Three is a soft
// edge on a terminal that only has three tiers of ink to spend.
const welcomeHead = 3

// paintWordmark paints one row of the wordmark for this frame: accent where the
// sweep has been and where it is going, ink under its head. At rest — and on a
// terminal that has no hues — the whole word is the accent, which is the state
// this animation exists to arrive at rather than to decorate.
func (w *welcome) paintWordmark(row string, pal palette) string {
	head := sweepAt(w.step, ansi.StringWidth(row))
	if w.step >= welcomeSweep {
		return pal.accent(row)
	}
	out := ""
	for i, cell := range []rune(row) {
		text := string(cell)
		if i >= head-welcomeHead && i <= head {
			out += pal.ink(text)
			continue
		}
		out += pal.accent(text)
	}
	return out
}

// welcomeHeight is how many rows the box takes. It is a constant of the frame:
// the border, the wordmark or its ascii stand-in, the place line, and the four
// fixed slots beside them.
func (a *app) welcomeHeight() int {
	if !a.welcome.open {
		return 0
	}
	rows := a.welcomeRows(a.widthOr())
	return len(rows)
}

func (a *app) widthOr() int {
	width, _ := a.size()
	return width
}

// welcomeRows draws the box. The slot each row belongs to is answered by
// [app.welcomeSlotAt], from the same geometry, so a click cannot land on a
// session the frame drew somewhere else.
func (a *app) welcomeRows(width int) []string {
	if !a.welcome.open {
		return nil
	}
	// A frame this small has no room to be greeted in. The box would take the
	// conversation, the rule and half the input line with it, and a welcome
	// that leaves nowhere to type is not a welcome — so a small window simply
	// opens on the prompt, which is what it would have done anyway.
	if _, height := a.size(); height < 12 || width < 40 {
		return nil
	}
	w := &a.welcome
	pal := a.pal

	left := wordmarkRows(pal.ascii)
	body := make([]string, 0, len(left)+2)
	for _, row := range left {
		body = append(body, w.paintWordmark(row, pal))
	}
	body = append(body, "")
	// The place line is capped: a long model slug is a fact about the model and
	// not a reason for the wordmark's column to eat the sessions beside it.
	body = append(body, pal.dim(fit(a.placeLine(), 34)))

	right := w.recentRows(pal, a.hoveredSlot())
	for len(body) < len(right) {
		body = append(body, "")
	}
	for len(right) < len(body) {
		right = append(right, "")
	}

	// The box is inset by one cell, and by one more while it is arriving: the
	// whole slide is a single column, which is a movement a person notices
	// without watching.
	inset := 1
	if w.step < welcomeSlide {
		inset = 2
	}
	// The left column is as wide as what it holds — the wordmark, or the place
	// line under it, whichever is longer — plus a gutter. It is measured rather
	// than chosen so that the right column starts where the left one ACTUALLY
	// ends: a fixed split puts the sessions through the middle of the wordmark
	// on the day somebody's model slug is long.
	inner := width - inset - 2
	leftWidth := 0
	for _, line := range body {
		if w := ansi.StringWidth(ansi.Strip(line)); w > leftWidth {
			leftWidth = w
		}
	}
	leftWidth += 2
	rightWidth := inner - leftWidth - 2
	if rightWidth < 22 {
		// Too narrow for two columns. The sessions go rather than being wrapped
		// into stubs nobody could choose between — the box still says what this
		// is and what is answering, which is the half that fits.
		right = make([]string, len(body))
		rightWidth = 0
	}

	pad := strings.Repeat(" ", inset)
	out := make([]string, 0, len(body)+2)
	out = append(out, pad+pal.dim(boxTop(inner, pal.ascii)))
	for i, line := range body {
		cell := line
		if gap := leftWidth - ansi.StringWidth(ansi.Strip(line)); gap > 0 {
			cell += strings.Repeat(" ", gap)
		}
		row := cell + fitPainted(right[i], rightWidth)
		if gap := inner - 2 - ansi.StringWidth(ansi.Strip(row)); gap > 0 {
			row += strings.Repeat(" ", gap)
		}
		out = append(out, pad+pal.dim(boxSide(pal.ascii))+" "+row+" "+pal.dim(boxSide(pal.ascii)))
	}
	out = append(out, pad+pal.dim(boxBottom(inner, pal.ascii)))
	return out
}

// fitPainted truncates a row that is already painted. It measures the plain
// text and gives up rather than cutting an escape sequence in half.
func fitPainted(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(ansi.Strip(text)) <= width {
		return text
	}
	return ansi.Truncate(text, width, glyphMore)
}

// placeLine is the left column's second half: what is answering, and where.
func (a *app) placeLine() string {
	model := a.model
	if model == "" {
		model = "no model"
	}
	place := a.place
	if place == "" {
		place = "here"
	}
	return model + " · " + place
}

// hoveredSlot is the recent session the pointer is over, or -1.
func (a *app) hoveredSlot() int {
	if a.hot.kind == hoverWelcome {
		return a.hot.index
	}
	return -1
}

// recentRows is the right column: the heading, then [welcomeSlots] rows whether
// or not there is anything to put in them.
func (w *welcome) recentRows(pal palette, hover int) []string {
	out := make([]string, 0, welcomeSlots+2)
	out = append(out, pal.dim("recent sessions"))
	out = append(out, "")
	if len(w.recent) == 0 {
		out = append(out, pal.dim("no recent sessions"))
		for len(out) < welcomeSlots+2 {
			out = append(out, "")
		}
		return out
	}
	for i := 0; i < welcomeSlots; i++ {
		if i >= len(w.recent) {
			out = append(out, "")
			continue
		}
		out = append(out, w.recentRow(i, pal, i == hover))
	}
	return out
}

func (w *welcome) recentRow(i int, pal palette, hovered bool) string {
	session := w.recent[i]
	name := strings.TrimSpace(session.Title)
	if name == "" {
		name = baseName(session.File)
	}
	when := since(session.At)
	line := fit(name, 24)
	if when != "" {
		line += "  " + when
	}
	switch {
	case i == w.sel:
		return pal.accent(glyphYou) + pal.bold(pal.ink(line))
	case hovered:
		// The pointer's own lead, the same one every list on this surface draws
		// under a pointer (palette.go's overlayRow).
		return pal.accent("· ") + pal.ink(line)
	}
	return "  " + pal.dim(line)
}

// welcomeSlotAt resolves one row of the box to the recent session drawn on it,
// or -1. It counts from the SAME layout [app.welcomeRows] draws: the border,
// the heading, the blank, then one row per slot.
func (a *app) welcomeSlotAt(row int) int {
	if !a.welcome.open || len(a.welcome.recent) == 0 {
		return -1
	}
	// border(1) + heading(1) + blank(1)
	slot := row - 3
	if slot < 0 || slot >= len(a.welcome.recent) {
		return -1
	}
	return slot
}

func baseName(path string) string {
	if at := strings.LastIndexByte(path, '/'); at >= 0 {
		return path[at+1:]
	}
	if path == "" {
		return "session"
	}
	return path
}

// since is the relative time in the right column. It is coarse on purpose: the
// question a person asks of this list is "which one was I in", and "3d" answers
// it where a timestamp would have to be read.
func since(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	case d < 30*24*time.Hour:
		return itoa(int(d/(24*time.Hour))) + "d"
	default:
		return at.Format("2 Jan")
	}
}

// The box's own furniture. It is the only border this surface draws, and it is
// drawn for exactly one reason: the welcome box is an OBJECT that goes away,
// and the rule above the input — this surface's one line — is a seam that
// stays. Two different things, two different marks.
func boxTop(inner int, ascii bool) string {
	if ascii {
		return "+" + strings.Repeat("-", inner) + "+"
	}
	return "╭" + strings.Repeat("─", inner) + "╮"
}

func boxBottom(inner int, ascii bool) string {
	if ascii {
		return "+" + strings.Repeat("-", inner) + "+"
	}
	return "╰" + strings.Repeat("─", inner) + "╯"
}

func boxSide(ascii bool) string {
	if ascii {
		return "|"
	}
	return "│"
}
