package tui3

import "github.com/charmbracelet/x/ansi"

// THE JUMP CHIP: the one thing on this surface that says a scroll happened.
//
//	                                                      ↓ latest · ctrl+l
//
// A transcript that streams keeps the reader at the live edge on its own
// (view.go's [app.offsetFor]), and a reader who scrolls up to read something is
// dropped out of that arrangement until they scroll back down. That is the right
// bargain — a window that yanked itself to the bottom mid-sentence is a window
// nobody can read on — but it left the surface with a state it never mentioned:
// a reply can be arriving three screens below the last row on the frame, and the
// only sign of it was the status line saying something was working.
//
// So the surface says it, in the smallest way it can: one dim chip, right
// aligned, floating on a row of whitespace that already existed. It is chrome —
// dim — until the pointer is on it, and then it is accent, because at that
// moment it is the live thing on the frame. Clicking it rejoins the live edge
// and the chip is gone on the next frame, which is the whole of its behaviour.
//
// IT NEVER TAKES A ROW OF ITS OWN. The chip is drawn into the first row of the
// frame's breathing gap ([app.breathingRows]), so the transcript is exactly as
// tall with it as without it and nothing below it moves when it appears. On a
// window too short to have a gap the chip is not drawn at all — a surface that
// bought an affordance with a row of conversation would have spent the wrong
// thing — and the key below still works, because a key costs nothing.
//
// THE KEY IS ON THE CHIP because the mouse is opt-in on this surface (view.go's
// [app.View]): a person with ui.mouse off can see the chip and can never press
// it, and a control that cannot be operated by the only input the surface is
// guaranteed to have is a decoration. ctrl+l is the chord because every other
// way back to the bottom is unreachable while there is a sentence in the box —
// `end` and ctrl+e are end-of-line, ↓ moves the caret, pgdown is a page at a
// time — and because nothing on this keyboard was spelling it: in an alt-screen
// application the terminal's own ctrl+l (repaint the screen) means nothing at
// all, so the chord arrives here free and with its "latest" mnemonic intact.

// jumpKey is the chord that rejoins the live edge, written down once: the key
// router binds it and the chip prints it, and a chip that named a key nobody
// bound would be the surface lying about itself.
const jumpKey = "ctrl+l"

// jumpLabel is the chip, unpainted. The arrow is the direction the gesture
// moves the window, and the ASCII stand-in is `v` for the linear tier, where a
// glyph nobody named is a glyph read out as its codepoint (styles.go).
func jumpLabel(pal palette) string {
	arrow := "↓"
	if pal.linear {
		arrow = "v"
	}
	return arrow + " latest · " + jumpKey
}

// jumpShowing reports whether the conversation is parked away from its live
// edge, which is the one condition the chip exists for.
//
// IT DERIVES THE ANSWER, it does not store one. The scroll position is resolved
// rather than kept (view.go's [app.offsetFor] says why), so a second flag saying
// "the reader has scrolled" would be a second answer to a question that already
// has one — and the two would disagree the first time a turn streamed six rows
// into a window nobody had touched. Sticking is not enough on its own either: a
// conversation shorter than the window has nothing below it, and a chip offering
// to jump to a row that is already on screen is a chip that does nothing.
func (a *app) jumpShowing() bool {
	// THE BODY REGION HAS TO BE THE CONVERSATION. A frozen viewport manages its
	// own edge and rejoins it on the way out (copymode.go's [app.exitCopy]), and
	// a room or the fullscreen roster has put the transcript off the frame
	// entirely — a chip that offered to scroll a list nobody can see is the same
	// mistake [app.rowAt] refuses to make.
	if a.copy.on || a.roomOpen() || a.railFull() {
		return false
	}
	height := a.viewHeight()
	if height <= 0 {
		return false
	}
	total := len(a.visible(a.bodyWidth()))
	bottom := total - height
	if bottom <= 0 {
		return false
	}
	return a.offsetFor(total, height) < bottom
}

// jumpChip is the chip as one painted row, or "" when there is nothing to jump
// to. It also records the columns it was drawn on.
//
// THE SPAN IS WRITTEN BY THE RENDER, which is the same bargain the status row's
// model segment makes (render.go's [app.modelSpan]): the layout is the only
// thing that knows where a right-aligned cluster landed, and the alternative is
// a second copy of the arithmetic kept in step with the first by nothing but
// attention. The press reads the row first for exactly this reason — laying the
// chrome out is what writes the span.
func (a *app) jumpChip(width int) string {
	a.jumpSpan = hudSpan{}
	if width < 1 || !a.jumpShowing() {
		return ""
	}
	label := jumpLabel(a.pal)
	if ansi.StringWidth(label) > width {
		return ""
	}
	paint := a.pal.dim
	if a.hoveringJump() {
		// ACCENT IS THE HOVER, not a background band: the chip is floating in
		// whitespace rather than sitting in a list, and a highlighted rectangle
		// around three words would be the one boxed thing on a surface with no
		// boxes. Brightening the words says the same thing in the vocabulary the
		// rest of the frame already speaks.
		paint = a.pal.accent
	}
	a.jumpSpan = hudSpan{from: width - ansi.StringWidth(label), to: width}
	return rightAlign(paint(label), label, width)
}

// hoveringJump reports whether the pointer is on the chip (hover.go).
func (a *app) hoveringJump() bool { return a.hot.kind == hoverJump }

// jumpPress is a click on the chip: the conversation rejoins its live edge.
//
// The row is resolved through [app.chromeAt] and the column through the span
// that laying it out just wrote, in that order, for the reason
// [app.statusPress] states about the model segment: reading the column first
// would be reading where the chip was drawn on the frame BEFORE this one.
func (a *app) jumpPress(x, y int) bool {
	if !a.jumpShowing() {
		return false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeJump || !a.jumpSpan.holds(x) {
		return false
	}
	a.toLatest()
	return true
}

// toLatest puts the reader back on the live edge of whatever the body region is
// drawing. It is what the chip does and what [jumpKey] does, so the two can
// never mean slightly different things.
//
// The edge is rejoined by ARMING THE STICK rather than by computing a bottom
// offset, which is how every other path back does it (welcome.go, app.go's /new,
// copymode.go's [app.exitCopy]): the offset is resolved from the row count at
// draw time, so a stick armed here survives the four rows the turn streams
// between this keystroke and the next frame.
func (a *app) toLatest() {
	// A ROOM IS THE BODY REGION WHILE IT IS OPEN, and it keeps its own edge
	// (room.go). Arming the transcript's stick from here would rejoin a live edge
	// the person is not looking at and lose the page they are.
	if a.room != nil {
		a.room.stick = true
		a.roomTouched()
		return
	}
	// The chip is about to stop being drawn, so a hover pointing at it is a
	// hover on a thing that is not there — the same claim [app.dropHover] makes
	// when the rows under the pointer are replaced.
	if a.hot.kind == hoverJump {
		a.hot = hoverAt{}
	}
	a.offset, a.stick = 0, true // resolved from the bottom by offsetFor
	a.touch()
}
