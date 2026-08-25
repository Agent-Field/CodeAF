package tui3

import "github.com/charmbracelet/x/ansi"

// THE VISIBLE DOOR: `◦ keeping an eye on 2` is a thing you can press.
//
// The segment has been on the status row for a wave (homestanding.go's
// [app.keepingSegment]). It is the only sign, anywhere in a conversation, that
// this place has standing orders over it — and it was a NUMBER: a person who
// read it and wanted to know which two had to already know that /standing
// exists, and the whole reason to draw the count is that they do not.
//
// So it opens the page. The keyboard door is unchanged and stays the documented
// one for a surface with the mouse turned off — /standing, and /orders beside it
// (commands.go) — exactly as the model segment's press left /model alone
// (app.go's [app.statusPress] states that bargain).
//
// AND IT BREATHES WHILE ONE OF THEM IS IN SOMEBODY'S HANDS, which it already
// did: the glyph is the spinner for as long as a pass holds a RunningMark for an
// order that reaches this place, and a still mark otherwise
// ([app.keepingCount], [app.keepingWord]). That is the same treatment the task
// rail gives live work, and it is the whole of the movement — the FOLDED
// EXPANSION of the rail's own standing line is a later wave and is not here.

// markKeepingDoor records where the segment landed, so the press that may follow
// resolves against this frame rather than the one before it.
//
// It is written AS THE ROW IS LAID OUT, which is the law [app.statusPress] states
// about the model's name and the one thing that keeps a door and its paint in
// step. base is the column the right-hand cluster starts at and row is which of
// the status row's rows it is on.
func (a *app) markKeepingDoor(parts []hudPart, base, row int) {
	at := base
	for i, part := range parts {
		if i > 0 {
			at += 3 // the " · " every cluster is joined on ([app.paintParts])
		}
		if part.kind == segKeeping {
			// THE WIDTH IS THE DRAWN ONE. The segment's glyph breathes while a
			// firing is in flight, so what is on the frame is [app.keepingWord]
			// and not the still text this list was measured with — and the two are
			// the same width by construction, which is the reason that function
			// swaps only the glyph.
			a.keepSpan = hudSpan{from: at, to: at + ansi.StringWidth(part.text)}
			a.keepRow = row
			return
		}
		at += ansi.StringWidth(part.text)
	}
}

// keepingPress opens /standing from the segment, and reports whether it took the
// click.
//
// A press anywhere else on the status row falls through, for the reason
// [app.statusPress] gives about the same row: the rest of it is telemetry —
// figures, not controls — and the conversation above has its own gestures.
func (a *app) keepingPress(x, y int) bool {
	if !a.keepingDoorAt(x, y) {
		return false
	}
	a.openStanding()
	return true
}

// keepingDoorAt is that question on its own, because the pointer asks it too:
// the set that LIGHTS has to be the set the press acts on, or the segment
// brightens and then does nothing (hover.go's own law).
//
// THE ROW IS RESOLVED BEFORE THE COLUMN. [app.chromeAt] lays the chrome out to
// answer, and laying it out is what writes the span — read the other way round,
// this would be testing a column from the frame before this one.
func (a *app) keepingDoorAt(x, y int) bool {
	if a.copy.on || a.sheet.open || a.pick.open || a.standPage.up {
		return false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeStatus {
		return false
	}
	return mark.index == a.keepRow && a.keepSpan.holds(x)
}

// hoveringKeeping is whether the pointer is on the door right now (render.go's
// [app.paintPart] brightens it).
func (a *app) hoveringKeeping() bool { return a.hot.kind == hoverKeeping }
