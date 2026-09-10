package tui3

// THE VISIBLE DOOR: `◦ 2 standing orders` is a thing you can press.
//
// The count is the only sign, anywhere in a conversation, that this place has
// standing orders over it (homestanding.go's [app.keepingSegment]) — and it was
// a NUMBER: a person who read it and wanted to know which two had to already
// know that /standing exists, and the whole reason to draw the count is that
// they do not. So it opens the page.
//
// IT IS A LINE AT THE FOOT OF THE TASK COLUMN SINCE 2026-09-09, and it was a
// segment of the status row before that. The move is the owner's ruling and it
// puts the count where the rest of this project's live work is already written
// down: directly under the column's own tally of what is running, waiting,
// parked and done (task.go's [app.railFootRows] draws it, [app.railStandingLine]
// paints it). The status row is numbers about the conversation in front of you;
// this is not one.
//
// The keyboard door is unchanged and stays the documented one for a surface
// with the mouse turned off — /standing, and /orders beside it (commands.go).
//
// AND IT BREATHES WHILE ONE OF THEM IS IN SOMEBODY'S HANDS, which it always
// did: the glyph is the spinner for as long as a pass holds a RunningMark for an
// order that reaches this place, and a still mark otherwise
// ([app.keepingCount], [app.keepingWord]). That is the same treatment the task
// column gives live work, one line up.

// railStandingAt reports whether the pointer is on that line. It is the hover's
// guard, and the press resolves the same fact through [railLine.keeping] — one
// geometry asked twice, because the line is found by the layout either way
// (task.go's [app.railDoorAt] makes the same bargain for the column's own door).
//
// THE SEAM IS NOT IT. The two cells at the column's left edge are the resize
// handle wherever a tier can be pulled to, footer lines included, so they are
// refused here for the reason every other rail target refuses them.
func (a *app) railStandingAt(x, y int) bool {
	if !a.railAt(x, y) || a.railSeamAt(x, y) {
		return false
	}
	line, ok := a.railLineAt(y)
	return ok && line.keeping
}

// hoveringRailStanding is whether the pointer is on the door right now
// (task.go's [app.railStandingLine] brightens it).
func (a *app) hoveringRailStanding() bool { return a.hot.kind == hoverRailStanding }
