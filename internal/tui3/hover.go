package tui3

// HOVER: the surface answers the pointer before it is clicked.
//
// Every interactive thing on this screen was already clickable and none of it
// LOOKED clickable, which on a terminal is worse than on a page: there is no
// underline, no cursor change, no affordance at all except the one the surface
// draws. So the row under the pointer takes one step up in background and its
// own marker brightens, and a row that answers to nothing does not react —
// which is the useful half. A hover style everything wears is a hover style
// that says nothing about what can be pressed.
//
// What reacts: a tool row and its expansion, the "N earlier tool calls" fold,
// the "… N more lines" foot, a thinking block, the consent choices, and the
// rows of whichever list is open. That set is not a taste — it is exactly the
// set [app.press] and the overlays already act on, read from the same row map
// the click hit-testing reads (render.go's row.hit, and view.go's chrome
// marks). Two definitions of "interactive" is how a surface ends up glowing at
// a row that does nothing.
//
// The tracking is deliberately cheap. A pointer crossing the window sends one
// motion message per cell, and each one asks where it landed and repaints only
// if the answer CHANGED — moving along a five-row tool expansion is one
// repaint, not five.

// hoverKind is what the pointer is over.
type hoverKind uint8

const (
	hoverNothing hoverKind = iota
	// hoverEntry is a conversation row that belongs to an entry — a tool call,
	// its expansion, its "more" foot, a thinking block.
	hoverEntry
	// hoverFold is the "N earlier tool calls" line, which belongs to a turn
	// rather than to an entry.
	hoverFold
	// hoverChoices is the consent block's offer line.
	hoverChoices
	// hoverOverlay is one row of the open list; index is its row in that list.
	hoverOverlay
)

// hoverAt is what the pointer is over, as an identity rather than as a screen
// row. It is an identity because rows are rebuilt every frame: a hover stored
// as "screen line 14" would follow the scroll instead of following the thing.
type hoverAt struct {
	kind  hoverKind
	entry int
	turn  int
	index int
}

// setHover takes one pointer position and records what is under it.
//
// Nothing repaints unless the answer changed. The two entries that gained or
// lost the hover are marked stale by hand because their rows are CACHED
// (render.go's entryRows): a thinking block that was drawn dim yesterday would
// otherwise stay dim under the pointer.
func (a *app) setHover(y int) {
	next := a.hoverTarget(y)
	if next == a.hot {
		return
	}
	if a.hot.kind == hoverEntry {
		a.markStale(a.hot.entry)
	}
	if next.kind == hoverEntry {
		a.markStale(next.entry)
	}
	a.hot = next
	a.touch()
}

// hoverTarget resolves a screen row to what a click on it would act on. The
// conversation is asked first and the chrome after it, in the order the frame
// draws them.
func (a *app) hoverTarget(y int) hoverAt {
	if r, ok := a.rowAt(y); ok {
		switch {
		case r.hit == hitFold:
			return hoverAt{kind: hoverFold, turn: r.turn}
		case r.hit == hitTool, r.hit == hitMore:
			return hoverAt{kind: hoverEntry, entry: r.entry}
		case r.entry >= 0 && r.entry < len(a.entries) &&
			a.entries[r.entry].kind == entryThinking:
			// A thinking block is clickable over its whole height (thinking.go
			// says why), so it is hoverable over its whole height too.
			return hoverAt{kind: hoverEntry, entry: r.entry}
		}
		return hoverAt{}
	}
	if mark, ok := a.chromeAt(y); ok {
		switch mark.kind {
		case chromeChoices:
			return hoverAt{kind: hoverChoices}
		case chromeOverlay:
			return hoverAt{kind: hoverOverlay, index: mark.index}
		}
	}
	return hoverAt{}
}

// markStale drops one entry's cached rows.
func (a *app) markStale(i int) {
	if i >= 0 && i < len(a.entries) {
		a.entries[i].stale = true
	}
}

// dropHover forgets where the pointer was. It runs where the entries are
// replaced wholesale (/new), because an index into a conversation that no
// longer exists is a highlight on somebody else's row.
func (a *app) dropHover() { a.hot = hoverAt{} }

// The four questions the renderers ask.

// hoveringEntry reports whether the pointer is on this entry's rows.
func (a *app) hoveringEntry(i int) bool {
	return a.hot.kind == hoverEntry && a.hot.entry == i
}

// hoveringFold reports whether the pointer is on this turn's fold line.
func (a *app) hoveringFold(turn int) bool {
	return a.hot.kind == hoverFold && a.hot.turn == turn
}

// hoveringChoices reports whether the pointer is on the consent offer.
func (a *app) hoveringChoices() bool { return a.hot.kind == hoverChoices }

// hoveringOverlay reports whether the pointer is on this row of the open list.
func (a *app) hoveringOverlay(index int) bool {
	return a.hot.kind == hoverOverlay && a.hot.index == index
}

// hoverRow is the background step, applied to a row that is already painted.
// It is the LAST thing done to a row, in one place (render.go's layout pass),
// so no renderer has to remember the pointer exists.
func (a *app) hoverRow(text string, width int) string { return a.pal.hover(text, width) }
