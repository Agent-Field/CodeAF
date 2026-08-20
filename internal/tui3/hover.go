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
// the "… N more lines" foot, a thinking block, the consent choices, the model's
// name at the foot of the frame, and the rows of whichever list is open. That
// set is not a taste — it is exactly the
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
	// hoverSettle is one CHIP of a landed card's answers row (tasksettle.go);
	// entry is the card and index is which of its chips. It is a kind of its own
	// rather than a hoverEntry because a hoverEntry brightens the whole card,
	// which is right for "click to expand" and wrong for a row where four
	// different presses do four different things — the law at the top of this
	// file, read the other way round.
	hoverSettle
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
	// hoverRailGrip is the CLOSED column's edge — the two columns down the right
	// of the frame that a stowed roster leaves behind (task.go's [railGripCols]).
	// It is its own kind and not a [hoverRailArea] because there is no roster
	// there to be an area of: everything that asks about the rail's rows would
	// answer about a column that is not on the frame.
	hoverRailGrip
	// hoverRailDoor is the STANDING column's own door — the footer line carrying
	// the `❯` and `ctrl+g — hide` (task.go's [railStowHint]). It is the other half
	// of [hoverRailGrip]: one control in two states, so the right edge lights the
	// same way whether the column is up or away.
	hoverRailDoor
	// hoverRailMore is the footer's OTHER door — the one line that leaves the
	// column for the task page (taskview.go's [taskSheetPastHint]). It is a kind
	// of its own for [hoverRailDoor]'s reason: it belongs to no node, and it does
	// something different from every other line of the footer.
	hoverRailMore
	// hoverTaskSheet is one row of the task page; index is its item
	// (taskview.go). It is a kind of its own rather than another [hoverSheet]
	// because the two pages number their rows out of different lists, and a
	// pointer that left the settings panel with a hover on item nine would light
	// the ninth task the moment this page opened.
	hoverTaskSheet
	// hoverStatusModel is the MODEL SEGMENT of the status row — the name of what
	// is answering, at the foot of the frame, which is a control as well as a
	// label (render.go's [app.identityParts]). It is the third target on this
	// surface narrower than the row it is drawn on, and it is asked about the
	// column for the jump chip's reason.
	//
	// IT COVERS BOTH SUBJECTS AND NEEDS NO SECOND KIND. Out in the conversation
	// the segment is the session's model and a press opens the picker; inside a
	// room it is the node's and a press retargets that node. What lights is the
	// same span in both, because what lights is what [app.press] acts on — and
	// where the press would do nothing, the render records no span and this
	// answers nothing (room.go's [app.roomModelMovable]).
	hoverStatusModel
	// hoverTable is the foot under a markdown table that was cut (mdtable.go);
	// entry is the answer it belongs to and index is which of that answer's
	// tables. It is the other narrow target, and it is a kind of its own rather
	// than a hoverEntry because a hoverEntry brightens the WHOLE block — which is
	// right for a tool call and wrong for a paragraph, where the pressable thing
	// is three words at the end of a table.
	hoverTable
	// hoverRewindSheet is one row of the rewind timeline; index is its row in that
	// page's list (rewindsheet.go). It is a kind of its own rather than another
	// [hoverRewind] because that one is an index into the POINTS the inline mode
	// is walking and this is an index into a list of transcript rows, and the two
	// are numbered out of different things — a pointer that left one surface with
	// a hover on item nine would light somebody else's row the moment the other
	// opened.
	hoverRewindSheet
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
	// A CARD'S ANSWERS ROW IS MARKED THE SAME WAY, and for the same reason: the
	// chip under the pointer is painted by the card's own render (tasksettle.go),
	// which is cached beside every other row of that card.
	if a.hot.kind == hoverEntry || a.hot.kind == hoverTable || a.hot.kind == hoverSettle {
		a.markStale(a.hot.entry)
	}
	if next.kind == hoverEntry || next.kind == hoverTable || next.kind == hoverSettle {
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
	// AND THE CLOSED COLUMN'S EDGE IS ASKED FIRST OF ALL OF THEM, because when it
	// is on the frame none of the others can be: the roster is away, so every
	// question below about a row of it answers about nothing (task.go's
	// [app.railGripAt]).
	if a.railGripAt(x, y) {
		return hoverAt{kind: hoverRailGrip}
	}
	if a.railSeamAt(x, y) {
		return hoverAt{kind: hoverRailSeam}
	}
	// AND THE STANDING COLUMN'S OWN DOOR, which is asked before the rows for the
	// reason the seam is: it is a line of the footer and belongs to no node, so a
	// question about which node is under the pointer would answer about the empty
	// space beside it (task.go's [app.railDoorAt]).
	if a.railDoorAt(x, y) {
		return hoverAt{kind: hoverRailDoor}
	}
	if node := a.railHoverNode(x, y); node != nil {
		return hoverAt{kind: hoverRail, id: node.id}
	}
	// AND THE FOOTER'S DOOR ONTO THE TASK PAGE, which is asked on the same terms
	// as the two above: it is a line of the footer, it belongs to no node, and it
	// answers to a click (task.go's [app.railMoreAt]).
	if a.railMoreAt(x, y) {
		return hoverAt{kind: hoverRailMore}
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
		case r.hit == hitSettle:
			// THE THIRD TARGET IN THE TRANSCRIPT THAT IS NARROWER THAN ITS ROW, and
			// the only one with four of them on one line: which chip the pointer is
			// on is a question about the column, and a row that lit as a whole would
			// promise that pressing anywhere on it did something (tasksettle.go).
			if card := a.doneCardAt(r.entry); card != nil {
				for i, chip := range card.chips {
					if chip.span.holds(x) {
						return hoverAt{kind: hoverSettle, entry: r.entry, index: i}
					}
				}
			}
			return hoverAt{}
		case r.hit == hitFold || r.hit == hitWorkFold:
			return hoverAt{kind: hoverFold, turn: r.turn}
		case r.hit == hitTool, r.hit == hitMore, r.hit == hitTask, r.hit == hitDone,
			r.hit == hitHarness, r.hit == hitChoice, r.hit == hitModel:
			// THE THREE THAT WERE MISSING FROM THIS LIST, and every one of them is
			// a row [app.press] already acts on. A sub-harness card opens the same
			// way a landed task's does (harnesscard.go), and a proposal's answers
			// and models rows are pressable along their whole width
			// (app.go's [app.choicePress]) — so a card that lit up and then went
			// dark the moment the pointer reached the row a person was aiming for
			// was the surface withdrawing the affordance at the exact cell where it
			// mattered. The whole block lights, because the block is what the press
			// belongs to.
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
		case chromeStatus:
			// THE SAME THREE QUESTIONS [app.statusPress] ASKS, IN THE SAME ORDER,
			// because this file's law is that the set which lights is the set the
			// press acts on: the overlays that swallow the press first, then the
			// identity's own row, then the columns the render recorded for the model.
			// Any of them answering differently here would be a name that brightens
			// and then does nothing.
			if a.copy.on || a.sheet.open || a.pick.open {
				return hoverAt{}
			}
			if mark.index == 0 && a.modelSpan.holds(x) {
				return hoverAt{kind: hoverStatusModel}
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

// hoveringRailMore reports whether the pointer is over the footer's door onto
// the task page.
func (a *app) hoveringRailMore() bool { return a.hot.kind == hoverRailMore }

// hoveringRailArea reports whether the pointer is anywhere over the roster.
func (a *app) hoveringRailArea() bool {
	switch a.hot.kind {
	case hoverRail, hoverRailArea, hoverRailSeam, hoverRailMore:
		return true
	}
	return false
}

// hoveringRailSeam reports whether the pointer is over the resize handle.
func (a *app) hoveringRailSeam() bool { return a.hot.kind == hoverRailSeam }

// hoveringRailGrip reports whether the pointer is over the closed column's edge.
func (a *app) hoveringRailGrip() bool { return a.hot.kind == hoverRailGrip }

// hoveringRailDoor reports whether the pointer is over the standing column's own
// door line, which is the same control in its other state.
func (a *app) hoveringRailDoor() bool { return a.hot.kind == hoverRailDoor }

// hoveringStatusModel reports whether the pointer is on the status row's model
// segment (render.go's [app.paintIdentity] is what it changes).
func (a *app) hoveringStatusModel() bool { return a.hot.kind == hoverStatusModel }

// hoveringOverlay reports whether the pointer is on this row of the open list.
func (a *app) hoveringOverlay(index int) bool {
	return a.hot.kind == hoverOverlay && a.hot.index == index
}

// hoverRow is the background step, applied to a row that is already painted.
// It is the LAST thing done to a row, in one place (render.go's layout pass),
// so no renderer has to remember the pointer exists.
func (a *app) hoverRow(text string, width int) string { return a.pal.hover(text, width) }
