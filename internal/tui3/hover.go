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
// the "… N more lines" foot, a thinking block, a sign-in still waiting, a task
// reference inside somebody's paragraph, the consent choices, the two answers on
// the stop card, a message parked above the box, the chips on the task strip and
// on the tray, the room's pinned header and the ✕ riding it, the targets on an
// adaptive run's page, the model's name at the foot of the frame, and the rows of
// whichever list, page or card is open. That set is not a taste — it is exactly
// the set [app.press] and the overlays already act on, read from the same row map
// the click hit-testing reads (render.go's row.hit, and view.go's chrome
// marks). Two definitions of "interactive" is how a surface ends up glowing at
// a row that does nothing.
//
// AND IT REACTS AT THE SIZE OF THE THING, not at the size of the row it is drawn
// on. Half of what is listed above shares its line with something else — four
// chips on a strip, two answers on a card, three references in one sentence — so
// a background band across the row would be the surface promising a door at every
// cell of it. Whatever is under the pointer lights; its neighbours do not. Where
// a target really is the whole row (a parked message, a deck row, the header) the
// whole row lights, and that is the same rule and not an exception to it.
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
	hoverPaste
	// hoverEntry is a conversation row that belongs to an entry — a tool call,
	// its expansion, its "more" foot, a thinking block.
	hoverEntry
	// hoverFold is the "N earlier tool calls" line, which belongs to a turn
	// rather than to an entry.
	hoverFold
	// hoverBrief is the door under a node's folded instruction (brieffold.go).
	// It is a kind of its own rather than a hoverFold because that one is keyed
	// by TURN and the instruction shares its turn with the node's first calls —
	// so a pointer on one door would light the other. And it is not a hoverEntry
	// because that brightens the WHOLE block, which is right for "click to
	// expand" and wrong here: the three lines above the door are the person's own
	// words and a press on them does nothing.
	hoverBrief
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
	// (jumpchip.go). It was the FIRST hover target on this surface narrower than
	// the row it is drawn on, which is why [app.hoverTarget] is asked the COLUMN as
	// well as the row; most of the kinds under it are narrow now too.
	hoverJump
	// hoverRail is a node row of the roster, and the identity it carries is the
	// NODE's (task.go). Every row of that column is a door into a node's room, so
	// every row of it reacts — which is this file's own law read the other way
	// round: the set that lights is the set [app.press] acts on, and in the
	// roster that is all of it. What the hover buys beyond the background step is
	// the disclosure triangle a family root reveals in its glyph cell, which is
	// the whole of the column's fold affordance at rest.
	// hoverForming is a row of the forming block at the transcript tail
	// (formingblock.go); index is the wait it belongs to. It is a kind of its own
	// rather than a hoverEntry because that one is keyed by ENTRY and this block
	// belongs to no entry — it is what stands where a block is about to be.
	hoverForming
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
	// the `❯` and `ctrl+g hide` (task.go's [railStowHint]). It is the other half
	// of [hoverRailGrip]: one control in two states, so the right edge lights the
	// same way whether the column is up or away.
	hoverRailDoor
	// hoverMarginDoor is one of the margin's two `+` rows, held by the SLASH WORD
	// it types (margin.go): there are two of them and they type two different
	// things, so the word is what tells them apart — and it is what the paint
	// asks by, which keeps the row that lights and the row that answers one row.
	hoverMarginDoor
	// hoverMarginStand is one standing order's row in that same margin, held by
	// the order's own id for [hoverOrch]'s reason: an order is named by a string
	// and the column is rebuilt every frame, so a hover stored as a row of it
	// would follow the scroll instead of following the order.
	hoverMarginStand
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
	// label (render.go's [app.identityParts]). It is asked about the column for the
	// jump chip's reason: the telemetry beside it is figures rather than controls,
	// and a name that brightened from forty cells away would be claiming the whole
	// row is a door.
	//
	// IT COVERS BOTH SUBJECTS AND NEEDS NO SECOND KIND. Out in the conversation
	// the segment is the session's model and a press opens the picker; inside a
	// room it is the node's and a press retargets that node. What lights is the
	// same span in both, because what lights is what [app.press] acts on — and
	// where the press would do nothing, the render records no span and this
	// answers nothing (room.go's [app.roomModelMovable]).
	hoverStatusModel
	// hoverKeeping is the `keeping an eye on N` segment of the status row, which
	// is a door onto /standing (standdoor.go). It is a kind of its own rather
	// than a second reading of [hoverStatusModel] for the reason that one covers
	// both of ITS subjects with one kind: what lights has to be what the press
	// acts on, and these two segments open two different things.
	hoverKeeping
	// hoverMoney is the money segment of the status row, which is a door onto
	// the Spending tab (moneydoor.go). It is a kind of its own for
	// [hoverKeeping]'s reason: three doors on one row that open three different
	// things, and what lights has to be what the press acts on.
	hoverMoney
	// hoverTable is the foot under a markdown table that was cut (mdtable.go);
	// entry is the answer it belongs to and index is which of that answer's
	// tables. It is a kind of its own rather
	// than a hoverEntry because a hoverEntry brightens the WHOLE block — which is
	// right for a tool call and wrong for a paragraph, where the pressable thing
	// is three words at the end of a table.
	hoverTable
	// hoverRoomBack is the top bar's way out for the pointer — the `esc/← back`
	// word and the chat name's own step of the crumb, which climb one level
	// (topbar.go's [app.topBarPress]). The two light as one kind because the
	// press they answer is one climb; they light separately because they are
	// separate spans, and what lights is what the press acts on.
	hoverRoomBack
	// hoverRoomStop is the ✕ riding the right end of the top bar (stop.go's
	// [app.stopMarkPress]). It is a kind of its own and not part of the back
	// word beside it because ending work and leaving the page you were watching
	// it on are opposite gestures — so the two never light together, and the
	// expensive one wins the cells it is drawn on.
	hoverRoomStop
	// hoverHome is the crumb's project step, which is space space parity: the
	// same door the legend's advertisement always opened, now on the bar where
	// the project is named (topbar.go). It is a kind of its own rather than a
	// second [hoverRoomBack] because the two doors open different pages, and
	// what lights has to be what the press acts on.
	hoverHome
	// hoverYolo is the top bar's YOLO term, which opens the Settings page's
	// Safety tab (topbar.go). It is a kind of its own for [hoverHome]'s reason:
	// a door of its own, opening a page of its own.
	hoverYolo
	// hoverCtx is the status row's context percent, which prints /status into
	// the transcript (rowdoors.go). A kind of its own for [hoverHome]'s reason:
	// a door of its own, opening a note of its own.
	hoverCtx
	// hoverOpen is the status row's `N open · M want you` clause, which opens
	// the conversations list (rowdoors.go) — tab parity, said in the pointer's
	// own grammar.
	hoverOpen
	// hoverStopAnswer is one of the stop card's two answers; index is which
	// (stop.go). Two presses share that row, so it is a chip and not a row for
	// [hoverSettle]'s reason.
	hoverStopAnswer
	// hoverParked is one MESSAGE waiting for the answer to finish; index is its
	// place in the queue (park.go). Every row that message wrapped over lights,
	// because the press pulls the whole message back into the box — and the dim
	// line under the block belongs to no message and lights not at all.
	hoverParked
	// hoverChip is one thing on the tray above the box; index is the picture it
	// names, or [trayHarnessChip] for the picked harness's own cell (attach.go,
	// harnesspick.go). A press takes that one thing off, so that one thing lights.
	hoverChip
	// hoverOrch is one target on an adaptive run's page, held by KEY rather than
	// by row (roomorch.go's [orchSpot.key]): the page is re-laid every poll, so a
	// hover stored as a row of it would follow the redraw instead of the chip. One
	// key can cover three rows on a phone, which is right — a chip drawn as a
	// stack of rows is one object.
	hoverOrch
	// hoverTaskCard is one region of the task record card; index is
	// [taskCardHead] or [taskCardFoot] (taskrecord.go). The card's edges are the
	// way back and its body is read, so the edges are exactly what lights.
	hoverTaskCard
	// hoverLink is one inline task reference inside a block of the model's prose;
	// entry is the block and index is which of its references (markdown.go's
	// [linkifyTasks]). It is the third narrow target in the transcript and the
	// most crowded one — a paragraph can name four nodes — so the words under the
	// pointer brighten and the sentence around them does not.
	hoverLink
	// hoverDeck is one of the phone status deck's two rows; index is which
	// (statusdeck.go). The whole row lights because [app.deckPress] takes every
	// press that lands on either of them.
	hoverDeck
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
	// key is the same bargain for a target an adaptive run names in ITS OWN
	// alphabet: an orchestrate node is "n3" and not a number (roomorch.go's
	// [orchSpot.key]), so a uint64 here would be a conversion in the adapter and a
	// lie in the type.
	key string
}

// setHover takes one pointer position and records what is under it.
//
// Nothing repaints unless the answer changed. The two entries that gained or
// lost the hover are marked stale by hand because their rows are CACHED
// (render.go's entryRows): a thinking block that was drawn dim yesterday would
// otherwise stay dim under the pointer.
//
// IT TAKES THE COLUMN AS WELL AS THE ROW, and most of what it resolves now needs
// both. Every target this file started with was a whole row wide, and the jump
// chip was the first that was not: three words at the right edge of a row that is
// otherwise empty, and a chip that brightened because the pointer was forty
// columns away from it would be claiming to be something you could press there
// (jumpchip.go). The strip's chips, the tray's, a card's answers, a run's layers
// and the references inside a paragraph are all read the same way. The kinds that
// still ignore x are the ones whose target really is the whole row.
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

// hoverTarget resolves a pointer to what a click on it would act on, ASKING THE
// SAME QUESTIONS [app.Update] ASKS OF A PRESS, IN THE SAME ORDER: the rows the
// frame pins above the body, then the roster's column, then whichever page or
// transcript fills the body, then the chrome below it. The order is the answer to
// overlap — three regions can be true of one screen row — so a hover resolved
// differently from a press is a surface that lights one thing and does another.
func (a *app) hoverTarget(x, y int) hoverAt {
	if a.pasteEdit.open {
		return hoverAt{}
	}
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
	// THE TOP BAR IS ASKED BEFORE THE CONVERSATION, for the reason [app.press]
	// resolves it first: the bar spans the whole window above the body, so a
	// question asked the other way round would answer about a transcript row
	// that is not on those lines. The ✕ is asked first of all of them, because
	// its hit box is three rows tall on a phone and deliberately overlaps the
	// top of the body (stop.go).
	if a.stopMarkAt(x, y) {
		return hoverAt{kind: hoverRoomStop}
	}
	if at, ok := a.topBarHover(x, y); ok {
		return at
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
	// AND THE MARGIN'S OWN LINES, asked on the same terms as the footer's above
	// them: a `+` row and a standing order's row belong to no node, and both
	// answer to a click (margin.go).
	if word, ok := a.marginDoorAt(x, y); ok {
		return hoverAt{kind: hoverMarginDoor, key: word}
	}
	if id, ok := a.marginStandAt(x, y); ok {
		return hoverAt{kind: hoverMarginStand, key: id}
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
	// A RUN'S PAGE ANSWERS FOR ITS OWN ROWS, before the transcript's hit-testing
	// is asked anything, which is the order [app.press] keeps: its rows are chips
	// and links and a gate rather than blocks, so what the pointer is over is
	// resolved by column against the targets the layout recorded (roomorch.go).
	if key, ok := a.orchHoverAt(x, y); ok {
		return hoverAt{kind: hoverOrch, key: key}
	}
	if r, ok := a.rowAt(y); ok {
		// A TASK REFERENCE IS THE ONE TARGET INSIDE A SENTENCE, so it is asked
		// before the row's own answer for the reason [app.linkPress] is resolved
		// before it: the prose it sits in has no gesture of its own, and a paragraph
		// that lit as a whole would promise a door on every word of it (markdown.go's
		// [linkifyTasks]).
		if at := a.linkHoverAt(x, r); at >= 0 {
			return hoverAt{kind: hoverLink, entry: r.entry, index: at}
		}
		switch {
		case r.foot.span.holds(x):
			// NARROWER THAN ITS ROW, so it is asked about the column the way the jump
			// chip is: the rest of the
			// row is the margin a table ended in, and a foot that brightened because
			// the pointer was forty cells away from it would be claiming to be
			// something you could press there (mdtable.go).
			return hoverAt{kind: hoverTable, entry: r.entry, index: r.foot.table}
		case r.hit == hitSettle:
			// NARROWER THAN ITS ROW, with four of them on one line: which chip the
			// pointer is
			// on is a question about the column, and a row that lit as a whole would
			// promise that pressing anywhere on it did something (tasksettle.go).
			//
			// A ROOM'S FOOT IS THE SAME ROW WITH NO ENTRY UNDER IT, so the card is
			// asked for through the seam that knows which of the two it is
			// (tasksettle.go's [app.settleCardOf]).
			if card := a.settleCardOf(r.entry); card != nil {
				for i, chip := range card.chips {
					if chip.span.holds(x) {
						return hoverAt{kind: hoverSettle, entry: r.entry, index: i}
					}
				}
			}
			return hoverAt{}
		case r.hit == hitFold || r.hit == hitWorkFold:
			return hoverAt{kind: hoverFold, turn: r.turn}
		case r.hit == hitBrief:
			return hoverAt{kind: hoverBrief, entry: r.entry}
		case r.hit == hitForming:
			// THE WAIT AND NOT THE ROW. A wait's compact row and the preview under
			// it are one thing to press, so they light together (formingblock.go's
			// [app.formingHot]) — and the wait is named by its place in the list,
			// which is what the row carries in place of an entry.
			return hoverAt{kind: hoverForming, index: r.turn}
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
		case a.connectLinkable(r.entry):
			// AND A SIGN-IN THAT IS STILL WAITING IS THE OTHER WHOLE-BLOCK TARGET, on
			// exactly the same terms: the card has one thing to do — copy the address
			// — and it does it wherever it is pressed (connect.go's
			// [app.connectLinkPress]), so the whole card is what lights.
			return hoverAt{kind: hoverEntry, entry: r.entry}
		}
		return hoverAt{}
	}
	// THE TRAY ABOVE THE BOX, asked where the chrome is asked and for the chrome's
	// own cost: resolving it lays the block out, and the question is worth asking
	// only once the pointer has left the conversation (attach.go's
	// [app.chipTrayTarget] rejects in a field test on the frames that carry no
	// tray, which is nearly all of them).
	if at, ok := a.chipTrayTarget(x, y); ok {
		return hoverAt{kind: hoverChip, index: at}
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
		case chromeStop:
			// THE CARD'S TWO ANSWERS, which share one row — so which of them the
			// pointer is on is a question about the column, and a row that lit as a
			// whole would say "you can press here" about the answer nobody is aiming
			// at (stop.go). The question above them is a sentence and lights not at
			// all.
			if a.stop == nil || mark.index != 1 {
				return hoverAt{}
			}
			for at, span := range a.stop.spans {
				if span.holds(x) {
					return hoverAt{kind: hoverStopAnswer, index: at}
				}
			}
		case chromeParked:
			// One waiting message, whichever of its rows the pointer is on. The dim
			// line under the block carries no mark and answers to nothing, which is
			// what [app.parkedMark] already says (park.go).
			return hoverAt{kind: hoverParked, index: mark.index}
		case chromeDraft:
			if n := a.pastePointerAt(x, mark.index); n > 0 {
				return hoverAt{kind: hoverPaste, index: n}
			}
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
			// THE SAME QUESTIONS [app.statusPress] ASKS, IN THE SAME ORDER, because
			// this file's law is that the set which lights is the set the press acts
			// on: the overlays that swallow the press first, then the columns the
			// render recorded for the row's doors. Any of them answering differently
			// here would be a name that brightens and then does nothing.
			if a.copy.on || a.at(pageSettings) || a.pick.open {
				return hoverAt{}
			}
			// AT PHONE WIDTH THE ROW IS A DECK AND THE DECK ANSWERS FOR BOTH OF ITS
			// ROWS, which is [app.statusPress]'s own branch read the other way round:
			// every cell of those two rows opens something — the chip its picker, the
			// rest of them the sheet — so the row under the pointer lights whole
			// (statusdeck.go's [app.deckPress]).
			if width, _ := a.size(); layoutTier(width) == tierPhone {
				return hoverAt{kind: hoverDeck, index: mark.index}
			}
			// The row's doors, in the order [app.statusPress] reads them: the
			// keeping segment onto /standing, the bill onto the Spending tab, the
			// meter onto /status, and the count onto the conversations list
			// (standdoor.go, moneydoor.go, rowdoors.go).
			if mark.index == a.keepRow && a.keepSpan.holds(x) {
				return hoverAt{kind: hoverKeeping}
			}
			if mark.index == a.moneyRow && a.moneySpan.holds(x) {
				return hoverAt{kind: hoverMoney}
			}
			if mark.index == a.ctxRow && a.ctxSpan.holds(x) {
				return hoverAt{kind: hoverCtx}
			}
			if mark.index == a.openRow && a.openSpan.holds(x) {
				return hoverAt{kind: hoverOpen}
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

// hoveringRoomBack reports whether the pointer is on the top bar's way out —
// the back word or the chat name's own step of the crumb.
func (a *app) hoveringRoomBack() bool { return a.hot.kind == hoverRoomBack }

// hoveringRoomStop reports whether the pointer is on that ✕.
func (a *app) hoveringRoomStop() bool { return a.hot.kind == hoverRoomStop }

// hoveringStopAnswer reports whether the pointer is on this answer of the stop
// card.
func (a *app) hoveringStopAnswer(at int) bool {
	return a.hot.kind == hoverStopAnswer && a.hot.index == at
}

// hoveringParked reports whether the pointer is on this waiting message.
func (a *app) hoveringParked(at int) bool {
	return a.hot.kind == hoverParked && a.hot.index == at
}

// hoveringChip reports whether the pointer is on this thing on the tray —
// [trayHarnessChip] for the picked harness, an index into [app.chips] otherwise.
func (a *app) hoveringChip(at int) bool {
	return a.hot.kind == hoverChip && a.hot.index == at
}

// hoveringOrch reports whether the pointer is on the run-page target this key
// names (roomorch.go's [orchSpot.key]).
func (a *app) hoveringOrch(key string) bool {
	return a.hot.kind == hoverOrch && a.hot.key == key
}

// hoveringTaskCard reports whether the pointer is on this region of the task
// record card.
func (a *app) hoveringTaskCard(at int) bool {
	return a.hot.kind == hoverTaskCard && a.hot.index == at
}

// hoveringLink is which task reference of this block the pointer is on, and -1
// for none. It is asked with the ordinal the LAYOUT counted, because a block's
// references are numbered across all the rows it wrapped over (render.go's
// [app.deckRows]).
func (a *app) hoveringLink(entry int) int {
	if a.hot.kind != hoverLink || a.hot.entry != entry {
		return -1
	}
	return a.hot.index
}

// hoveringDeck reports whether the pointer is on this row of the phone status
// deck.
func (a *app) hoveringDeck(row int) bool {
	return a.hot.kind == hoverDeck && a.hot.index == row
}

// hoveringOverlay reports whether the pointer is on this row of the open list.
func (a *app) hoveringOverlay(index int) bool {
	return a.hot.kind == hoverOverlay && a.hot.index == index
}

// hoverRow is the background step, applied to a row that is already painted.
// It is the LAST thing done to a row, in one place (render.go's layout pass),
// so no renderer has to remember the pointer exists.
func (a *app) hoverRow(text string, width int) string { return a.pal.cursor(text, width) }
