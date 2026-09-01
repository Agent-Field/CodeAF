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
	if at, w, ok := hudPartAt(parts, base, segCtx); ok {
		a.ctxSpan = hudSpan{from: at, to: at + w}
		a.ctxRow = row
	}
}

// hudPartAt walks a painted cluster and answers where one segment landed: the
// column it starts at and how wide it is, or false when the width ladder
// dropped it. Both doors on this row ask it, so the arithmetic is said once —
// and it is [legendJoin]'s own width rather than a 3, because a separator whose
// spelling lives in one place and whose WIDTH lived in another is a separator
// that can be changed correctly and still move every door on the row.
func hudPartAt(parts []hudPart, base int, want hudSeg) (at, width int, ok bool) {
	at = base
	for i, part := range parts {
		if i > 0 {
			at += ansi.StringWidth(legendJoin)
		}
		w := ansi.StringWidth(part.text)
		if part.kind == want {
			return at, w, true
		}
		at += w
	}
	return 0, 0, false
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
	// A DOOR THAT CANNOT WORK IS NOT A DOOR. This build may have been handed no
	// way to open a conversation at all (app.go's [app.canOpen] — the seam is a
	// pair of optional functions), and a span registered anyway would be a count
	// that brightens under the pointer and then prints a refusal. The count is
	// still drawn: how many conversations are open is true either way.
	if !a.canOpen() {
		return
	}
	if at, w, ok := hudPartAt(parts, base, segOpen); ok {
		a.openSpan = hudSpan{from: at, to: at + w}
		a.openRow = row
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
