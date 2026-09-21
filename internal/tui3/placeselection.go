package tui3

import tea "charm.land/bubbletea/v2"

// placePointer remembers physical motion separately from the row highlight.
// A stationary pointer must not reclaim a selection after a keyboard move.
type placePointer struct {
	x, y      int
	known     bool
	suspended bool
}

func (a *app) singleSelectionPlace() bool {
	if a.hopShowing() {
		return true
	}
	// Settings retains its separate hover preview; list places share selection.
	return a.pageShowing() && !a.at(pageSettings)
}

// keyboardPlaceSelection retires both the old highlight and motion still
// waiting for a paint. Otherwise a delayed pointer tick would undo this key.
func (a *app) keyboardPlaceSelection() {
	if !a.singleSelectionPlace() {
		return
	}
	a.placePointer.suspended = true
	a.ptr.have, a.ptr.notches = false, 0
	a.clearPlaceRowHover()
}

func (a *app) clearPlaceRowHover() {
	if !a.singleSelectionPlace() {
		return
	}
	rowHot := a.hot.kind == hoverTaskSheet || a.hot.kind == hoverHop
	changed := a.home.hover >= 0 || a.mem.hover >= 0 || a.orders.hover >= 0 || a.spend.hover >= 0 || a.search.hover >= 0 || rowHot
	a.home.hover, a.mem.hover, a.orders.hover, a.spend.hover, a.search.hover = -1, -1, -1, -1, -1
	if rowHot {
		a.hot = hoverAt{}
	}
	if changed {
		a.touch()
	}
}

// placeMotionAllowed runs before coalescing, so even a queued position is
// remembered when keyboard navigation cancels it. Dragging still owns motion.
func (a *app) placeMotionAllowed(msg tea.MouseMotionMsg) bool {
	if !a.singleSelectionPlace() || msg.Mouse().Button != tea.MouseNone {
		return true
	}
	p := &a.placePointer
	x, y := msg.Mouse().X, msg.Mouse().Y
	if p.suspended && p.known && p.x == x && p.y == y {
		return false
	}
	*p = placePointer{x: x, y: y, known: true}
	return true
}

// selectPlaceRow gives hover and keyboard actions one row identity. Moving to
// another row also retires any menu captured for the previous one.
func (a *app) selectPlaceRow(cursor *int, at int) bool {
	if at < 0 {
		return false
	}
	if a.bar.on {
		a.bar.on = false
		a.touch()
	}
	if *cursor == at {
		return false
	}
	*cursor = at
	a.holdStrip()
	a.touch()
	return true
}
