package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE POINTER ON A PLACE ──────────────────────────────────────────────────
//
// Three gestures, one law each, and every one of them is the same law on all
// seven places:
//
//	a press on a tab word   goes to that place
//	a press on a body row   moves that place's cursor, and never acts
//	the pointer resting     previews the row it is over, and moves nothing —
//	                        over a tab word, that preview is the word's own ink
//	the wheel               walks the cursor, three rows a tick, and over the
//	                        tab bar walks the places, one room a tick
//
// THIS FILE IS THE ARITHMETIC AND NEVER THE PLACES. Which place answers which
// gesture is pages.go's — it is the one file that may know them all by name —
// and what is here is what does not vary between them: turning a row of the
// terminal into a line of a body, keeping a window over that body under the
// cursor, and repainting only when the pointer actually moved.
//
// WHY THE SPANS AND THE WINDOWS ARE WRITTEN BY THE DRAW. A press resolves
// against what was PAINTED, never against a second computation of what should
// have been painted: the tab bar gives up words as the frame narrows, and a
// body scrolls, so a span or a line computed independently would open the wrong
// room or act on the wrong row exactly when a person could not tell why.

// placeTabRow is the row of the frame the tab bar is drawn on: under the pulse
// and over the rule (pages.go's [placeFrame] draws the ladder). The frame
// records where it actually landed in [app.tabRow], because a frame too short
// for its own contents drops it — this is the number that is true on every
// frame that has one.
const placeTabRow = 1

// placeWheelRows is how far one turn of the wheel walks a place's cursor. It is
// three because three is what every other list on this surface moves by
// (app.go's wheel ladder, copy mode, the task page, home), and a wheel that
// meant a different distance on the places would be one gesture with seven
// speeds.
const placeWheelRows = 3

// placeWheelDelta is one turn of the wheel as a distance, and zero for a button
// that is not the wheel at all.
func placeWheelDelta(button tea.MouseButton) int {
	switch button {
	case tea.MouseWheelUp:
		return -placeWheelRows
	case tea.MouseWheelDown:
		return placeWheelRows
	}
	return 0
}

// placeTabPress is a press on the tab bar: the place whose chip it landed in.
//
// A TAB WORD IS A DOOR. The bar names seven rooms and bands the one you are
// standing in; a bar that answered a press with nothing would be seven labels,
// which is what the owner met in the built binary. The gap between two chips
// belongs to no room and is swallowed — a bar that rounded a miss to its
// nearest neighbour would open the wrong place for a one-cell slip.
//
// AND PRESSING THE PLACE YOU ARE ALREADY IN DOES NOTHING AT ALL. Going there is
// closing and reopening it, which throws away the filter somebody typed and the
// row they were standing on; pressing where you are standing is not a gesture.
func (a *app) placeTabPress(x, y int) (tea.Cmd, bool) {
	// ROW ZERO IS THE PULSE AND CAN NEVER BE THE BAR, so anything under one is
	// "no frame has drawn a bar yet" — the state a window is in before its first
	// paint, and the state [app.frame] puts it back into for every surface that
	// does not go through [placeFrame].
	if !a.pageShowing() || a.tabRow < 1 || y != a.tabRow {
		return nil, false
	}
	for _, span := range a.tabs {
		if x < span.from || x >= span.to {
			continue
		}
		if span.id == a.page {
			return nil, true
		}
		return a.showPage(span.id), true
	}
	// THE REST OF THE ROW IS STILL THE BAR'S. A press in the gap, or out past
	// the last chip, is a press on a row that has nothing under it — swallowing
	// it is what stops it falling through to a body row it visually is not.
	return nil, true
}

// placeTabHover is the pointer resting over the tab bar: THE WORD UNDER IT LIFTS
// ONE INK TIER AND NOTHING ELSE ON THE FRAME MOVES.
//
// A TAB WORD IS A DOOR AND A DOOR SHOULD LOOK BACK. The bar answered a press
// ([app.placeTabPress]) and said nothing at all while a pointer crossed it, so
// seven words that open seven rooms read as a label strip until somebody
// gambled a click on one. The lift is the selected word's own ink and weight
// with no band under it (pages.go's [app.tabBarAt]) — a band would make the
// hovered word look like the room a person is standing in, and a bar with two
// grounds on it says nothing clearly.
//
// AND IT MOVES NOTHING. No cursor, no place, no window: the pointer previews and
// the cursor selects, which is the law home wrote and every place inherited
// (pages.go's [app.placeBodyHover]). Leaving the bar puts the ink back.
func (a *app) placeTabHover(x, y int) bool {
	if !a.pageShowing() || a.tabRow < 1 || y != a.tabRow {
		// THE POINTER LEAVING THE ROW IS NEWS TOO, and it is the half that is easy
		// to forget: a word left lifted after the hand moved away is a door that
		// claims to be under a pointer that is somewhere else.
		a.barHover(pageNone)
		return false
	}
	under := pageNone
	for _, span := range a.tabs {
		if x >= span.from && x < span.to {
			under = span.id
			break
		}
	}
	a.barHover(under)
	// THE ROW IS THE BAR'S WHETHER OR NOT A WORD WAS UNDER THE POINTER, for
	// [app.placeTabPress]'s reason: the gap between two chips belongs to no room,
	// and letting it fall through would light a body row the pointer visually is
	// not on.
	return true
}

// placeTabWheel is the wheel turned over the tab bar: IT WALKS THE PLACES.
//
// The wheel means "the next one of these" everywhere else on this surface — it
// walks a list's cursor, three rows a tick (placeWheelRows) — and over a row
// whose contents are the seven rooms, the next one of these is the next room. It
// is `tab` and `shift+tab` under the hand that is already on the bar, which is
// the same bargain the chips already struck with the left button.
//
// ONE ROOM PER TICK AND NOT THREE. A wheel notch that walked three places would
// open two rooms nobody asked to see on its way to the third, and each of those
// openings closes the last room and throws away its filter.
func (a *app) placeTabWheel(y int, delta int) (tea.Cmd, bool) {
	if !a.pageShowing() || a.tabRow < 1 || y != a.tabRow || delta == 0 {
		return nil, false
	}
	return a.walkPage(delta < 0), true
}

// placeBodyLine turns a row of the terminal into a line of a place's body,
// given the window the last draw put over it.
//
// THE HEAD IS ONE NUMBER FOR EVERY PLACE ([placeHeadRows]) and the window is
// two more: which line was drawn first, and how many were drawn. A row past the
// last drawn one belongs to the foot — the rule, the note, the composer, the
// strip, the hint — and answers nothing, which is what stops a press on the
// hint line acting on a body row that scrolled off the top.
func placeBodyLine(y, top, shown int) (int, bool) {
	at := y - placeHeadRows
	if at < 0 || at >= shown {
		return 0, false
	}
	return top + at, true
}

// placeTop is the window a place's body is drawn through: the first line of it
// that fits on screen with the cursor still on screen.
//
// THE WINDOW FOLLOWS THE CURSOR AND IS NEVER SCROLLED ON ITS OWN. It is the
// bargain the task page, home and the standing place all already struck, and it
// is what makes the wheel and `↓` one gesture rather than two — a place with an
// offset of its own would need a second key to bring the cursor back into view.
// A body that fits whole has no window at all, which is the common case and
// costs nothing.
func placeTop(top, cursor, rows, room int) int {
	if room < 1 || rows <= room {
		return 0
	}
	if top > rows-room {
		top = rows - room
	}
	if top < 0 {
		top = 0
	}
	if cursor < top {
		top = cursor
	}
	if cursor >= top+room {
		top = cursor - room + 1
	}
	return top
}

// placeHoverMoved records where the pointer is and reports whether that is
// news, repainting only when it is.
//
// MOTION IS THE CHEAPEST AND COMMONEST MESSAGE THIS SURFACE GETS — a pointer
// crossing the window sends one per cell — so a hover that repainted on every
// one of them would be a screen redrawn eighty times for a highlight that did
// not move (hover.go states the same rule for the conversation). The answer is
// always true: the place TOOK the motion either way, and what is being reported
// is whether anything has to be drawn again.
func placeHoverMoved(at *int, next int, a *app) bool {
	if *at == next {
		return true
	}
	*at = next
	a.touch()
	return true
}

// walkPage is `tab` and `shift+tab`: the next place, and round again from the
// last.
//
// IT USED TO ASK WHETHER THE NEXT ROOM WOULD LET IT IN. Tasks refused on a
// machine that had run no work and standing refused with no orders on it, so
// `tab` walked into the refusal, was put straight back where it started, and did
// it again on the next press — on a fresh machine the circle had one member and
// the key read as broken, which is what the owner met ("left right does not seem
// to move tabs, only shift does", shift+tab happening to land on settings, which
// always opened). The walk answered it by asking a `pageReady` before turning any
// handle.
//
// EVERY ROOM OPENS NOW (pages.go's [app.showPage]), so there is nothing to ask:
// the next place is the next place, and a room with nothing in it spends the
// frame saying what it is for rather than bouncing anybody out of it.
func (a *app) walkPage(back bool) tea.Cmd {
	return a.showPage(nextPage(a.page, back))
}

// ── the pointer, in the box ─────────────────────────────────────────────────

// placeBoxPress answers a click on the composer a place draws at its foot, and
// reports whether it took it. It is the ordinary text-field gesture the message
// box already answers (draftclick.go) reaching the other seven screens: a press
// puts the caret on the letter it landed on, at the row's end past the end of a
// line, and at the start of the text left of it.
//
// IT IS THE ROUTER'S ROW, WHICH IS WHY IT LIVES HERE AND IS READ WHERE IT IS.
// The box is drawn by [placeFrameWithBar] on every place, in the same cells, so
// a press answered by whichever place happens to be standing would be the same
// row meaning different things in different rooms — the argument the tab bar's
// own press is read first for.
//
// THE ARITHMETIC IS THE FRAME'S OWN AND NEVER A SECOND COPY OF IT.
// [draftClickIndex] is the pure inverse of the layout [draftBlock] drew the
// rows with, and it is handed exactly the numbers the frame handed the drawing:
// the same room, the same row cap, the same head. A press and a paint that
// each did their own wrap would disagree on any line that wrapped.
func (a *app) placeBoxPress(x, y int) bool {
	// A LAYER THAT HAS TAKEN THE KEYBOARD HAS TAKEN THE CARET WITH IT
	// (composerlayer.go). The box is still on the frame while the layer is up —
	// that is the layer's whole promise, the box does not move — but every key
	// belongs to the layer, so a caret placed under the pointer would be a caret
	// nothing can move. draftclick.go refuses the thinking ladder for this exact
	// reason.
	if a.composer.open || a.boxRows < 1 {
		return false
	}
	box := a.placeBox()
	if box == nil || box.empty() {
		return false
	}
	at := y - a.boxRow
	if at < 0 || at >= a.boxRows {
		return false
	}
	box.cursor = a.placeBoxOffsetIn(at, x)
	// AND THE PRESS ARMS THE SWEEP over this box, on the message box's own
	// terms and for its reason (boxselect.go, draftclick.go).
	a.boxPressed(box, true, x, y)
	return true
}

// placeBoxOffsetIn is the rune offset a pointer names on row `at` of a place's
// composer, at column x of the frame — the arithmetic the press and the sweep
// share, so the two can never disagree about which letter is under the pointer.
//
// The same three numbers [placeFrameWithBar] handed [draftBlock]: the box gets
// the frame less its one-cell margin, the rows are capped at [homeDraftRows],
// and the head is the prompt with no lead in front of it. The margin is why the
// column starts one cell in.
func (a *app) placeBoxOffsetIn(at, x int) int {
	box := a.placeBox()
	if box == nil {
		return 0
	}
	width, _ := a.size()
	head := ansi.StringWidth(prompt)
	room := width - 2 - head
	if room < 4 {
		room = 4
	}
	return draftClickIndex(box.value, box.cursor, at, x-1-head, room, homeDraftRows)
}
