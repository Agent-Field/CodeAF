package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The frame is five regions and four of them are one row:
//
//	status        aforge · title · model · place · cost · ctx · state
//	(blank)
//	conversation  everything that has happened, scrolled
//	(blank)
//	(consent)     the approval question, when one is waiting
//	(follow)      after yield · N, when something is queued
//	input         › the draft — one row, or up to six of a pasted block
//	(overlay)     the open list, when one is open
//
// The two optional regions above the box are there for the same reason the box
// is: they are about the sentence a person is holding. A question that blocks
// the model and a message waiting for it to finish both belong next to where
// the answer is typed, not somewhere in the scrollback.
//
// The input is a BLOCK and not a line as of wave 2, which is why the frame
// counts back from the bottom through it rather than assuming its height: the
// rows a multi-line draft takes come out of the conversation, and out of
// nothing else.
//
// There is no rail and no border. The rail arrives with the tasker; the
// borders are not coming at all (docs/CHAT-V3.md, and the design doctrine
// behind it: a line drawn around text is a line the reader has to ignore).
//
// A terminal too short for all five gives up its breathing room first and its
// status line last: where you are and what you are typing are the two facts a
// one-inch window still has to carry.
const chromeRows = 4

// View declares the frame and the terminal state it wants. Alt screen, because
// the conversation scrolls under our own anchor; ALL motion, because a tool
// line is a thing you click — cell motion would deliver the wheel and the
// press, and this is the v2 spelling of the program option that used to be
// tea.WithMouseAllMotion, so the cmd wiring stays a wiring.
func (a *app) View() tea.View {
	frame, caretX, caretY := a.frame()
	v := tea.NewView(frame)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = false
	// Bracketed paste stays ON — a pasted stack trace arrives as one
	// tea.PasteMsg with its newlines intact instead of as a stack of enters,
	// each of which would submit. v2 enables it unless this says otherwise, and
	// it says so out loud because the default is the thing being relied on.
	v.DisableBracketedPasteMode = false
	v.Cursor = &tea.Cursor{
		Position: tea.Position{X: caretX, Y: caretY},
		Shape:    tea.CursorBar,
		Blink:    true,
	}
	return v
}

// frame is the whole screen and where the caret sits in it.
//
// The model overlay enters HERE and nowhere else: its filter box takes the
// input line's place — one line at the bottom either way — and its list is
// drawn under that, which is why the caret's row is returned rather than
// assumed to be the last one.
func (a *app) frame() (string, int, int) {
	width, height := a.size()
	input, caretX, caretRow := a.inputBlock(width)
	overlay := a.overlayHeight()
	body, pad := a.window(width, a.viewHeight())

	rows := make([]string, 0, height)
	switch {
	case height >= 6:
		rows = append(rows, a.status(width), "")
	case height >= 2:
		rows = append(rows, a.status(width))
	}
	for i := 0; i < pad; i++ {
		rows = append(rows, "")
	}
	for _, r := range body {
		rows = append(rows, r.text)
	}
	if height >= 6 {
		rows = append(rows, "")
	}
	rows = append(rows, a.consentRows(width)...)
	if line := a.followRow(width); line != "" {
		rows = append(rows, line)
	}
	rows = append(rows, input...)
	rows = append(rows, a.overlayRows(width, overlay)...)
	if len(rows) > height {
		rows = rows[len(rows)-height:]
	}
	// The list is the tail of the frame and the box sits directly above it, so
	// the caret's row counts back from the bottom through both — true before
	// the truncation above and after it.
	return strings.Join(rows, "\n"), caretX, height - overlay - len(input) + caretRow
}

// size is the frame's working size: the terminal's, or the classic default
// when nobody has said. A headless boot and a terminal that answers zero are
// the same case, and drawing into a zero-by-zero frame would mean drawing
// nothing at all.
func (a *app) size() (int, int) {
	width, height := a.width, a.height
	if width < 8 {
		width = 8
	}
	if height < 1 {
		height = 1
	}
	return width, height
}

// window is the visible slice of the row list and the padding above it.
//
// Every geometric question on this surface goes through here — what the frame
// draws, where the wheel lands, which row a click hit — so a row's position on
// screen has exactly ONE definition. The padding is why a short conversation
// sits next to the input, the way a terminal session grows upward, instead of
// hanging under the status line.
func (a *app) window(width, height int) ([]row, int) {
	if height <= 0 {
		return nil, 0
	}
	rows := a.visible(width)
	offset := a.offsetFor(len(rows), height)
	end := min(offset+height, len(rows))
	visible := rows[offset:end]
	if pad := height - len(visible); pad > 0 {
		return visible, pad
	}
	return visible, 0
}

// bodyTop is the screen row the conversation starts on, or -1 when the frame is
// too short to have one. It walks the same ladder [app.frame] does.
func (a *app) bodyTop() int {
	_, height := a.size()
	switch {
	case height >= 6:
		return 2
	case height >= 3:
		return 1
	default:
		return -1
	}
}

// rowAt resolves a screen line to the row drawn on it.
func (a *app) rowAt(y int) (row, bool) {
	top := a.bodyTop()
	if top < 0 {
		return row{}, false
	}
	width, _ := a.size()
	body, pad := a.window(width, a.viewHeight())
	at := y - top - pad
	if at < 0 || at >= len(body) {
		return row{}, false
	}
	return body[at], true
}

// viewHeight is how many rows the conversation gets under the same ladder
// frame walks down, minus whatever the model overlay is holding.
//
// The subtraction belongs here rather than at the call sites because this is
// the number every geometric question is answered from — what the frame draws,
// where the wheel lands, which row a click hit. An overlay the layout knew
// about but the hit-testing did not would deliver clicks to rows twelve lines
// from where they were drawn.
func (a *app) viewHeight() int {
	_, height := a.size()
	var body int
	switch {
	case height >= 6:
		body = height - chromeRows
	case height >= 3:
		body = height - 2
	default:
		return 0
	}
	body -= a.overlayHeight()
	// The approval question and the follow-up count are the same kind of claim
	// on the frame as the overlay: rows that come out of the conversation, and
	// out of nothing else.
	body -= a.consentHeight() + a.followHeight()
	// A draft that grew past one row takes those rows from the conversation and
	// from nothing else: the status line and the box are what a one-inch window
	// still has to carry, and a six-line paste must not push either off screen.
	if body -= a.inputHeight() - 1; body < 0 {
		return 0
	}
	return body
}

func (a *app) page() int {
	if p := a.viewHeight() - 1; p > 1 {
		return p
	}
	return 1
}

// offsetFor resolves the scroll position for a rendered length. Sticking is
// resolved here rather than stored, so a turn that streams six lines while the
// reader is at the bottom keeps them at the bottom without anybody recomputing
// an offset per delta.
func (a *app) offsetFor(total, height int) int {
	bottom := total - height
	if bottom < 0 {
		bottom = 0
	}
	if a.stick || a.offset > bottom {
		return bottom
	}
	if a.offset < 0 {
		return 0
	}
	return a.offset
}

// scroll moves the window by delta rows and re-decides whether the reader is
// following the live edge. Reaching the bottom re-arms sticking: leaving it
// off would mean a reader who scrolled up once never sees a new reply again.
func (a *app) scroll(delta int) {
	height := a.viewHeight()
	total := len(a.visible(a.width))
	bottom := total - height
	if bottom < 0 {
		bottom = 0
	}
	at := a.offsetFor(total, height) + delta
	switch {
	case at >= bottom:
		a.offset, a.stick = bottom, true
	case at <= 0:
		a.offset, a.stick = 0, false
	default:
		a.offset, a.stick = at, false
	}
}

// reveal scrolls just enough to put an entry's first row on screen. It is what
// keeps ↑/↓ selection from walking off the top of the window.
func (a *app) reveal(entry int) {
	height := a.viewHeight()
	if height <= 0 {
		return
	}
	rows := a.visible(a.width)
	at := -1
	for i, r := range rows {
		if r.entry == entry {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	offset := a.offsetFor(len(rows), height)
	switch {
	case at < offset:
		a.offset, a.stick = at, false
	case at >= offset+height:
		a.offset, a.stick = at-height+1, false
	}
}

// clampScroll keeps the offset legal after a resize.
func (a *app) clampScroll() {
	a.offset = a.offsetFor(len(a.visible(a.width)), a.viewHeight())
}

// follow is what every append calls: content grew, and a reader at the live
// edge stays at the live edge.
func (a *app) follow() {
	if a.stick {
		a.offset = 0 // resolved from the bottom by offsetFor
	}
}
