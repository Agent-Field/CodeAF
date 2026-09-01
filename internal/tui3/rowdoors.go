package tui3

import "github.com/charmbracelet/x/ansi"

// ── THE STATUS ROW'S TWO NEW DOORS ──────────────────────────────────────────
//
// The row's numbers are doors now, and each one opens the sheet that carries
// what the number is a summary of — the law the money segment set (moneydoor.go)
// and the keeping segment before it (standdoor.go). Two more follow it here:
// the context percent, and the open·want-you clause. Both are recorded by
// [app.statusRows] as the row is laid out, which is the bargain every span on
// this surface makes: the row that lays the terms out is the row that knows
// where they landed.

// markCtxDoor records where the context segment landed, so the press that may
// follow resolves against this frame rather than the one before it. It is
// [app.markMoneyDoor] for the meter: base is the column the right-hand cluster
// starts at and row is which of the status row's rows it is on.
func (a *app) markCtxDoor(parts []hudPart, base, row int) {
	at := base
	for i, part := range parts {
		if i > 0 {
			at += 3 // the " · " every cluster is joined on ([app.paintParts])
		}
		if part.kind == segCtx {
			a.ctxSpan = hudSpan{from: at, to: at + ansi.StringWidth(part.text)}
			a.ctxRow = row
			return
		}
		at += ansi.StringWidth(part.text)
	}
}

// ctxPress prints /status into the transcript from the segment, and reports
// whether it took the click. The percent is a meter, and the meter's own sheet
// is the note that explains every number at once — the same body the /status
// command runs, reached by the pointer for the person whose eye is already on
// the number that moved.
func (a *app) ctxPress(x, y int) bool {
	if !a.ctxDoorAt(x, y) {
		return false
	}
	text := a.statusText()
	a.noteFacts(text, a.statusFacts(text)...)
	return true
}

// ctxDoorAt is that question on its own, because the pointer asks it too: the
// set that LIGHTS has to be the set the press acts on (hover.go's own law).
func (a *app) ctxDoorAt(x, y int) bool {
	return a.ctxSpan.pressable() && y == a.ctxRow && a.ctxSpan.holds(x)
}

// markOpenDoor records where the open·want-you clause landed. It is
// [app.markCtxDoor] for the clause: the same walk, the same bargain.
func (a *app) markOpenDoor(parts []hudPart, base, row int) {
	at := base
	for i, part := range parts {
		if i > 0 {
			at += 3 // the " · " every cluster is joined on ([app.paintParts])
		}
		if part.kind == segOpen {
			a.openSpan = hudSpan{from: at, to: at + ansi.StringWidth(part.text)}
			a.openRow = row
			return
		}
		at += ansi.StringWidth(part.text)
	}
}

// openPress opens the conversations list from the clause, and reports whether
// it took the click. The list is the one tab walks — the same door in its list
// form, which is what a count beside a door is for: the number says how many,
// the press says which.
func (a *app) openPress(x, y int) bool {
	if !a.openDoorAt(x, y) {
		return false
	}
	a.openResume()
	return true
}

// openDoorAt is that question on its own, for [app.ctxDoorAt]'s reason.
func (a *app) openDoorAt(x, y int) bool {
	return a.openSpan.pressable() && y == a.openRow && a.openSpan.holds(x)
}
