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
//
// AND "CHEAP" IS A CEILING NOW RATHER THAN A CLAIM. A motion that changed
// nothing leaves no stale entry, no dirty flag and no command behind it, and a
// motion that crossed a boundary leaves exactly two — the entry that lost the
// highlight and the one that gained it. Both are stated as tests
// (inputsmooth_test.go), because the cost of a message that arrives per CELL is
// the sort of thing that grows a little at a time until a pointer moved over a
// link is a surface that stutters.

import "github.com/Agent-Field/aforge-v2/internal/session"

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
	// hoverConnectAsk is the connect offer's answers line (connect.go). It is a
	// kind of its own rather than another hoverChoices because the two blocks can
	// be on screen together, and a pointer over one must not brighten the other.
	hoverConnectAsk
	// hoverHarnessAsk is the sub-harness offer's row (harness.go). One row and
	// one target, like the offer above it.
	hoverHarnessAsk
	// hoverRoomApproval is the answers row of a design room's approval block
	// (roomapproval.go), and a kind of its own for the reason above it: it can be
	// on screen at the same time as any of the three offers, because it is not a
	// question the session is blocked on.
	hoverRoomApproval
	// hoverOverlay is one row of the open list; index is its row in that list.
	hoverOverlay
	// hoverSheet is one row of the settings panel; index is its item
	// (settings.go). The panel is fullscreen, so while it is up this is the
	// only kind the pointer can produce.
	hoverSheet
	// hoverWelcome is one recent-session row of the welcome box; index is its
	// slot (welcome.go).
	hoverWelcome
	// hoverRewind is a CUT the pointer is offering while the rewind mode is up
	// (rewind.go); index is the point a click would choose. It is a kind of its
	// own rather than a hoverEntry because the row that brightens is not the row
	// under the pointer — it is the block the cut would land above, which is
	// wherever the nearest point at or above the pointer happens to be.
	hoverRewind
	// hoverJump is the jump-to-latest chip floating in the frame's breathing gap
	// (jumpchip.go). It is one of the two hover targets on this surface that are
	// narrower than the row they are drawn on, which is why [app.hoverTarget] is
	// asked the COLUMN as well as the row.
	hoverJump
	// hoverRail is a node row of the roster, and the identity it carries is the
	// NODE's (task.go). Every row of that column is a door into a node's room, so
	// every row of it reacts — which is this file's own law read the other way
	// round: the set that lights is the set [app.press] acts on, and in the
	// roster that is all of it. What the hover buys beyond the background step is
	// the disclosure triangle a family root reveals in its glyph cell, which is
	// the whole of the column's fold affordance at rest.
	hoverRail
	// hoverRailArea is the roster's non-node space. The rail remains one
	// pointer target even between rows, because its footer offer follows the
	// hand across the whole column.
	hoverRailArea
	// hoverRailSeam is the two-cell resize handle at the rail's left edge.
	// It is separate from the row behind it so the handle can light without
	// painting that node as a door.
	hoverRailSeam
	// hoverRailPast is one row of the PROJECT'S RECORD at the foot of the column
	// (taskview.go's [app.railRecordLines]), and index is its place in the rows
	// the layout drew. It is a kind of its own rather than a [hoverRail] because
	// those rows carry no node — the id they have belongs to a conversation that
	// is closed, and ids restart with every one of them, so an id is not a name
	// this file could hold them by.
	hoverRailPast
	// hoverTaskSheet is one row of the task page; index is its item
	// (taskview.go). It is a kind of its own rather than another [hoverSheet]
	// because the two pages number their rows out of different lists, and a
	// pointer that left the settings panel with a hover on item nine would light
	// the ninth task the moment this page opened.
	hoverTaskSheet
	// hoverTable is the foot under a markdown table that was cut (mdtable.go);
	// entry is the answer it belongs to and index is which of that answer's
	// tables. It is the other narrow target, and it is a kind of its own rather
	// than a hoverEntry because a hoverEntry brightens the WHOLE block — which is
	// right for a tool call and wrong for a paragraph, where the pressable thing
	// is three words at the end of a table.
	hoverTable
)

// hoverAt is what the pointer is over, as an identity rather than as a screen
// row. It is an identity because rows are rebuilt every frame: a hover stored
// as "screen line 14" would follow the scroll instead of following the thing.
type hoverAt struct {
	kind  hoverKind
	entry int
	turn  int
	index int
	// id is the roster node the pointer is over, and it is a field of its own
	// because a node id is not an index into anything this file can renumber: the
	// column re-sorts its families as work moves, and a hover stored as a row of
	// it would follow the sort instead of following the work.
	id uint64
}

// setHover takes one pointer position and records what is under it.
//
// Nothing repaints unless the answer changed. The two entries that gained or
// lost the hover are marked stale by hand because their rows are CACHED
// (render.go's entryRows): a thinking block that was drawn dim yesterday would
// otherwise stay dim under the pointer.
//
// IT TAKES THE COLUMN NOW AS WELL AS THE ROW. Every target this file started
// with was a whole row wide, and the jump chip is not: it is three words at the
// right edge of a row that is otherwise empty, and a chip that brightened
// because the pointer was forty columns away from it would be claiming to be
// something you could press there (jumpchip.go). Every other kind ignores x, as
// it always did.
func (a *app) setHover(x, y int) {
	next := a.hoverTarget(x, y)
	if next == a.hot {
		return
	}
	// A TABLE'S FOOT IS MARKED THE SAME WAY, and for the same reason: it is drawn
	// dim or accent by the block's own render (mdtable.go), and that render is
	// cached beside every other row of the answer.
	if a.hot.kind == hoverEntry || a.hot.kind == hoverTable {
		a.markStale(a.hot.entry)
	}
	if next.kind == hoverEntry || next.kind == hoverTable {
		a.markStale(next.entry)
	}
	a.hot = next
	// A page's rows are cached as a LIST rather than per entry (room.go), so the
	// entry-level staleness above cannot reach them: the room is dropped whole,
	// which is what makes a rail brighten under the pointer on a node that has
	// finished and stopped asking for frames.
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}

// hoverTarget resolves a screen row to what a click on it would act on. The
// conversation is asked first and the chrome after it, in the order the frame
// draws them.
func (a *app) hoverTarget(x, y int) hoverAt {
	// THE REWIND MODE ANSWERS FOR THE WHOLE TRANSCRIPT while it is up: every row
	// is a cut point, and what the pointer is over is WHICH CUT (rewind.go). It is
	// asked first because none of the ordinary targets below mean anything in a
	// mode where a click cannot open a call — brightening a tool row a person is
	// about to drop would be the surface offering a door it has closed.
	if a.rew.on {
		if r, ok := a.rowAt(y); ok {
			switch {
			case r.hit == hitRewind:
				return hoverAt{kind: hoverRewind, index: a.rew.at}
			case r.entry >= 0:
				if at := a.rewindPointAtEntry(r.entry); at >= 0 {
					return hoverAt{kind: hoverRewind, index: at}
				}
			}
		}
		return hoverAt{}
	}
	// THE ROSTER IS ASKED BEFORE THE CONVERSATION, for the reason [app.press]
	// resolves it first: the two are drawn side by side, so which one the pointer
	// is over is a question about x — and every row of the transcript answers to
	// the same y as the roster row beside it (room.go's [app.railPress]).
	if a.railSeamAt(x, y) {
		return hoverAt{kind: hoverRailSeam}
	}
	if node := a.railHoverNode(x, y); node != nil {
		return hoverAt{kind: hoverRail, id: node.id}
	}
	// AND THE RECORD ROWS UNDER THEM, which answer to a click for the mention
	// (taskview.go). They are asked here, between the nodes and the column's own
	// empty space, because that is where they are drawn.
	if at := a.railHoverPast(x, y); at >= 0 {
		return hoverAt{kind: hoverRailPast, index: at}
	}
	if a.railAt(x, y) {
		return hoverAt{kind: hoverRailArea}
	}
	if r, ok := a.rowAt(y); ok {
		switch {
		case r.foot.span.holds(x):
			// THE ONE TARGET IN THE TRANSCRIPT THAT IS NARROWER THAN ITS ROW, so it
			// is asked about the column the way the jump chip is: the rest of the
			// row is the margin a table ended in, and a foot that brightened because
			// the pointer was forty cells away from it would be claiming to be
			// something you could press there (mdtable.go).
			return hoverAt{kind: hoverTable, entry: r.entry, index: r.foot.table}
		case r.hit == hitFold || r.hit == hitWorkFold:
			return hoverAt{kind: hoverFold, turn: r.turn}
		case r.hit == hitTool, r.hit == hitMore, r.hit == hitTask, r.hit == hitDone:
			return hoverAt{kind: hoverEntry, entry: r.entry}
		case r.entry >= 0 && r.entry < len(a.bodyDeck().entries) &&
			a.bodyDeck().entries[r.entry].kind == entryThinking:
			// A thinking block is clickable over its whole height (thinking.go
			// says why), so it is hoverable over its whole height too.
			return hoverAt{kind: hoverEntry, entry: r.entry}
		}
		return hoverAt{}
	}
	if mark, ok := a.chromeAt(y); ok {
		switch mark.kind {
		case chromeChoices:
			// The index is the row WITHIN the block, which the one-line offer
			// never needed and the phone sheet does: its answers are a row each
			// (consent.go's [app.hoveringChoice]).
			return hoverAt{kind: hoverChoices, index: mark.index}
		case chromeHarnessAsk:
			// One row again, and the same reason: the offer is the only
			// pressable row that block has (harness.go).
			return hoverAt{kind: hoverHarnessAsk}
		case chromeConnectAsk:
			// One row, so there is no index to carry: the offer is the only
			// pressable row that block has (connect.go).
			return hoverAt{kind: hoverConnectAsk}
		case chromeRoomApproval:
			// One row again, and the same reason: the answers row is the only
			// pressable row a design room's approval block has (roomapproval.go).
			return hoverAt{kind: hoverRoomApproval}
		case chromeOverlay:
			return hoverAt{kind: hoverOverlay, index: mark.index}
		case chromeWelcome:
			if slot := a.welcomeSlotAt(mark.index); slot >= 0 {
				return hoverAt{kind: hoverWelcome, index: slot}
			}
		case chromeJump:
			// The row was laid out to answer, and laying it out is what wrote the
			// span — the same order [app.jumpPress] and [app.statusPress] keep.
			if a.jumpSpan.holds(x) {
				return hoverAt{kind: hoverJump}
			}
		}
	}
	return hoverAt{}
}

// markStale drops one entry's cached rows, in whichever list is on screen: the
// pointer is over the BODY REGION, and while a room is open the body region is
// that node's page (render.go's [app.bodyDeck]).
func (a *app) markStale(i int) {
	es := a.bodyDeck().entries
	if i >= 0 && i < len(es) {
		es[i].stale = true
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

// hoveringRail reports whether the pointer is on this node's roster row.
func (a *app) hoveringRail(node *taskNode) bool {
	return node != nil && a.hot.kind == hoverRail && a.hot.id == node.id
}

// hoveringRailPast reports whether the pointer is on this row of the project's
// record at the foot of the column. It is asked by IDENTITY and answered against
// the drawn list, so a record that has shifted under a stale hover lights
// nothing rather than lighting the row that took its place.
func (a *app) hoveringRailPast(entry *session.TaskIndexEntry) bool {
	if entry == nil || a.hot.kind != hoverRailPast {
		return false
	}
	return a.railPastAt(a.hot.index) == entry
}

// hoveringRailArea reports whether the pointer is anywhere over the roster.
func (a *app) hoveringRailArea() bool {
	switch a.hot.kind {
	case hoverRail, hoverRailArea, hoverRailSeam, hoverRailPast:
		return true
	}
	return false
}

// hoveringRailSeam reports whether the pointer is over the resize handle.
func (a *app) hoveringRailSeam() bool { return a.hot.kind == hoverRailSeam }

// hoveringOverlay reports whether the pointer is on this row of the open list.
func (a *app) hoveringOverlay(index int) bool {
	return a.hot.kind == hoverOverlay && a.hot.index == index
}

// hoverRow is the background step, applied to a row that is already painted.
// It is the LAST thing done to a row, in one place (render.go's layout pass),
// so no renderer has to remember the pointer exists.
func (a *app) hoverRow(text string, width int) string { return a.pal.hover(text, width) }
