package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE TOP BAR ─────────────────────────────────────────────────────────────
//
//	· af-chrome › fix the parser          glm-4.6:high · main* · devbox · YOLO
//	──────────────────────────────────────────────────────────────────────────
//
// One row above the conversation, plus the legend's own hairline rule under it,
// carrying the SLOW facts at both ends: where you are (the crumb, left) and
// what you are talking to (the terms, right). The fast facts stay on the status
// row at the bottom (render.go's [app.statusRows]) — a number in a static bar
// becomes wallpaper, and a number attached to a thing that moves is a dial.
//
// The room header this replaces was a task-only surface; the top bar is the
// same facts worn by the chat too, which is the whole of the change: the
// conversation has a head now, and it says what a task's does.
//
// THE LEFT CLUSTER IS ONE LIT ELEMENT while a room is open — the room-header
// accent law, moved up. One hue for the glyph, the crumb, the state, the clock
// and the spend, because they are one sentence about one thing; the handle
// `#N` rides dim inside it, a label on the lit thing rather than a second thing
// lit. A consent question up drops the whole cluster to dim, the same fall
// [app.paintPart] gives a finished fact. The chat at rest wears ink and dim and
// never accent: nothing about a conversation at rest is lit.

// topBarPress is the pointer's share of the bar's doors, read before the
// rail and the body for the reason the strip's press was: the bar spans the
// WHOLE window while the rail claims every press in its own columns whether or
// not a row was under it, so a bar read after the rail would be dead at
// exactly the end where the words are printed.
//
// The doors, in the order the bar draws them: the crumb's two steps (home,
// then the chat's own name — the precise way out of a task, one level up, the
// same climb esc makes), the model (which retargets the node in a room, and
// opens the picker in a chat), YOLO (the Settings page's Safety tab — seeing
// the gate open has to offer the way to close it), the back word (the wider
// way out), and the ✕ (stop.go's own press, which the bar's span feeds).
func (a *app) topBarPress(x, y int) (tea.Cmd, bool) {
	// The bar is the frame's first row; a press anywhere below it is not the
	// bar's to answer.
	if y != 0 || a.headHeight() == 0 {
		return nil, false
	}
	if a.crumbHomeSpan.holds(x) {
		return a.openHome(), true
	}
	if a.crumbChatSpan.holds(x) {
		// One crumb level up, with the scroll restored — the same climb esc
		// makes (room.go's [app.closeRoom]).
		a.closeRoom()
		return nil, true
	}
	if a.modelSpan.holds(x) {
		// A ROOM POINTS THE SAME DOOR AT THE NODE THE BAR NAMES, exactly as the
		// status row's identity cluster did before it (app.go's
		// [app.statusPress]): the segment in there is the task's model, so the
		// picker it opens moves the task's model and nothing else. Which nodes may
		// be moved at all is settled by the render, in the columns it recorded —
		// a node past being moved has no span, so this never sees the press
		// (room.go's [app.roomModelMovable]). One esc puts the door back on the
		// conversation.
		if a.roomOpen() {
			a.openTaskPicker(a.room.id)
		} else {
			a.openPicker()
		}
		return nil, true
	}
	if a.topYoloSpan.holds(x) {
		return a.openSettingsTab(tabSafety), true
	}
	if a.backSpan.holds(x) {
		// The back word climbs exactly one crumb level, like esc and ←
		// (room.go's [app.roomStepUp]).
		a.roomStepUp()
		return nil, true
	}
	if a.roomStop.holds(x) {
		return nil, a.stopMarkPress(x, y)
	}
	return nil, false
}

// topBarHover is the bar's answer to the pointer standing on it, asked by
// [app.hoverTarget] before the body for the same reason the press is: the bar
// is above the conversation, and a hover that the body answered first would
// light a row under the bar rather than the bar itself. The hover lights
// exactly what presses — every door the press reads, and nothing else.
func (a *app) topBarHover(x, y int) (hoverAt, bool) {
	if y != 0 || a.headHeight() == 0 {
		return hoverAt{}, false
	}
	if a.crumbHomeSpan.holds(x) {
		return hoverAt{kind: hoverHome}, true
	}
	if a.crumbChatSpan.holds(x) {
		return hoverAt{kind: hoverCrumbChat}, true
	}
	if a.modelSpan.holds(x) {
		return hoverAt{kind: hoverStatusModel}, true
	}
	if a.topYoloSpan.holds(x) {
		return hoverAt{kind: hoverYolo}, true
	}
	if a.backSpan.holds(x) {
		return hoverAt{kind: hoverRoomBack}, true
	}
	if a.roomStop.holds(x) {
		return hoverAt{kind: hoverRoomStop}, true
	}
	return hoverAt{}, false
}

// topBarShowing reports whether the top bar is on this frame at all. It stands
// on the breathing law ([app.breathingRows]) — a terminal too short for the
// blank above the draft is too short for a head — and on the phone tier's own
// rule: below [hudTight] the frame is a deck (statusdeck.go), and the deck's
// top row takes the crumb instead.
func (a *app) topBarShowing(width int) bool {
	if a.breathingRows() == 0 {
		return false
	}
	if layoutTier(width) == tierPhone {
		return false
	}
	// AND ON A BRUTALLY SHORT WINDOW THE TRANSCRIPT WINS. The bar is the FIRST
	// chrome to go and the bottom two rows are the last — so it stands only
	// where the frame has its own rows and one of the conversation's left over.
	// Without this a sixty-by-twelve frame under the greeting was charged
	// thirteen rows for twelve, and the tear came out of the body.
	//
	// It asks [app.chromeHeight] and never [app.viewHeight], which is that
	// function's own standing rule: the body is what is LEFT after this answer,
	// so a body that helped decide it would be a circle.
	_, height := a.size()
	return height-a.chromeHeight() > topBarHeight
}

// topBarHeight is what the bar costs the body: the row and its rule.
const topBarHeight = 2

// topBarRows is the bar and its rule, or nothing at all. The rule is the
// legend's own hairline ([app.rule]) — the same one line this surface has
// always drawn under its head — with nothing written into it, because the bar
// above it is where the words go and a rule that repeated them would be the
// frame saying everything twice.
func (a *app) topBarRows(width int) []string {
	if !a.topBarShowing(width) {
		// A BAR THAT IS NOT DRAWN LEAVES NO DOORS BEHIND IT. The spans are this
		// surface's whole answer to "was it on the screen" (standdoor.go's law,
		// and the reason [app.topBarWord] clears them before it writes them), and
		// an early return that skipped the clearing left the LAST frame's columns
		// live: open a room wide, drag the window under sixty columns, and a click
		// on the transcript landed on a ✕ that was three frames gone. Every path
		// out of the bar clears them, this one included.
		a.clearTopSpans()
		return nil
	}
	// The rule paints itself dim ([app.rule]); a second coat over the top of it
	// is a hue nested inside a hue, and it ends at the inner one's reset.
	return []string{a.topBarWord(width), a.rule(width)}
}

// clearTopSpans forgets every door the bar records. It is one function because
// the list has to be one list: a span cleared on one path out and not on another
// is exactly the defect above, wearing a different width.
func (a *app) clearTopSpans() {
	a.crumbHomeSpan = hudSpan{}
	a.crumbChatSpan = hudSpan{}
	a.backSpan = hudSpan{}
	a.topYoloSpan = hudSpan{}
	a.roomStop = hudSpan{}
	a.modelSpan = hudSpan{}
}

// topBarWord is the bar's one row, plain-spaced: the left cluster from the
// frame's left edge, the right cluster against its right, and at least
// [hudGap] columns of quiet between them.
//
// THE SPANS ARE WRITTEN HERE, as the row is laid out, for the reason
// [app.statusPress] gives about the model segment: a column read from anywhere
// else is a column from the frame before this one. A term that was dropped by
// the fitting records nothing, which is what makes the span its own answer to
// "was it drawn" — a door that is not on the screen cannot be pressed.
func (a *app) topBarWord(width int) string {
	a.clearTopSpans()

	// THE WELCOME BOX QUIETS THE BAR TO ITS CRUMB (render.go's
	// [app.statusQuiet]): a greeting is the one moment the surface is about
	// nothing yet, and a bar that named a model nobody has chosen over a box
	// that is asking for the first sentence would be answering a question
	// nobody asked.
	quiet := a.statusQuiet()

	// The run page answers for its own header (roomorch.go): its trail is the
	// engine's own vocabulary — main ▸ goal ▸ n8 — and its facts are a tank's
	// rather than a clock's. The top bar wears that word as its left cluster
	// and keeps the room's right cluster, which is the same deal the room
	// header gave it.
	if a.orchOpen() {
		return a.topBarOrchWord(width, quiet)
	}

	// ── the left cluster ────────────────────────────────────────────────────
	//
	// The crumb, then the room's three live facts. Each fact is dropped when
	// nobody has published it — an empty segment is not a dim one — and the
	// whole cluster is one hue (see the file's head).
	room := a.roomOpen()
	node := a.roomNode()
	accent := a.pal.accent
	if a.roomApprovalHeight() > 0 {
		accent = a.pal.dim
	}

	showGlyph, showState, showClock, showSpend := true, true, true, true
	if room {
		if a.roomStateWord(node) == "" {
			showState = false
		}
		if a.roomClock(node) == "" {
			showClock = false
		}
		if a.roomSpend(node) == "" {
			showSpend = false
		}
	}
	rung := trailFull
	left := a.topBarLeft(rung, showGlyph, showState, showClock, showSpend, accent)
	right := a.topBarRight(quiet, accent)

	fits := func() bool {
		return cellsWidth(left)+hudGap+cellsWidth(right) <= width
	}

	// ── the give-way, in the one order the spec states three times ──────────
	//
	// The trimmings go first ($, then clock, then state, then the glyph — the
	// crumb outranks its own trimmings), then the crumb walks its ladder
	// ([fitTrail]), and only then does the right cluster give a term up. The
	// current segment of the crumb yields last of all: it is the thing the bar
	// exists to confirm, so it is truncated to [roomHeadFloor] columns and
	// never dropped.
	for !fits() && (showSpend || showClock || showState || showGlyph) {
		switch {
		case showSpend:
			showSpend = false
		case showClock:
			showClock = false
		case showState:
			showState = false
		case showGlyph:
			showGlyph = false
		}
		left = a.topBarLeft(rung, showGlyph, showState, showClock, showSpend, accent)
	}
	for !fits() && rung < trailTitle {
		rung++
		left = a.topBarLeft(rung, showGlyph, showState, showClock, showSpend, accent)
	}
	// ── the right cluster's own ladder ───────────────────────────────────────
	//
	// The host's latency detail goes first (the name survives), then the
	// branch, then the :effort rider, then the model, then the back word. YOLO
	// and a reconnect note are never dropped, and the ✕ outlives the back
	// word's microcopy at narrow widths exactly as it did on the room header.
	hostDetail, showBranch, showEffort, showModel, showBack := true, true, true, true, true
	for !fits() && (hostDetail || showBranch || showEffort || showModel || showBack) {
		switch {
		case hostDetail:
			hostDetail = false
		case showBranch:
			showBranch = false
		case showEffort:
			showEffort = false
		case showModel:
			showModel = false
		case showBack:
			showBack = false
		}
		right = a.topBarRightFit(quiet, accent, hostDetail, showBranch, showEffort, showModel, showBack)
	}

	// ── assembly ────────────────────────────────────────────────────────────
	//
	// The left cluster from column zero, the right against the frame's right
	// edge, quiet between. The spans are recorded from the plain cells BEFORE
	// the painting, because the model's own paint reads the span it is about
	// to be given — the lift and the record have to come from the same walk.
	left = clampCells(left, width-cellsWidth(right)-1)
	a.recordTopSpans(left, right, width)
	gap := width - cellsWidth(left) - cellsWidth(right)
	if gap < 0 {
		gap = 0
	}
	row := paintCells(left)
	row += strings.Repeat(" ", gap)
	row += paintCells(right)
	return fit(row, width)
}

// clampCells cuts a cluster down to a column budget, keeping whole cells while
// they fit, truncating the one that straddles the edge, and dropping the rest.
//
// IT IS WHAT KEEPS THE TWO CLUSTERS FROM STANDING ON EACH OTHER once both
// ladders have given everything they have. The row used to be assembled at
// whatever width the pieces came to and handed to [fit], which cuts from the
// RIGHT — so the last resort took the ✕ and the YOLO term off a bar whose crumb
// had already refused to shorten, which is the exact inversion of the give-way
// this file states three times. The crumb yields last among the things that
// yield; it does not outrank the safety posture or the way to stop the work.
//
// A budget below zero is a right cluster wider than the frame — a reconnect
// sentence at sixty columns — and the left cluster is then nothing at all,
// which is that sentence's own stated right to evict everything beside it.
func clampCells(cells []topBarCell, budget int) []topBarCell {
	if budget < 0 {
		budget = 0
	}
	if cellsWidth(cells) <= budget {
		return cells
	}
	kept := make([]topBarCell, 0, len(cells))
	at := 0
	for _, c := range cells {
		w := ansi.StringWidth(c.text)
		if at+w <= budget {
			kept = append(kept, c)
			at += w
			continue
		}
		if room := budget - at; room > 0 {
			c.text = fit(c.text, room)
			kept = append(kept, c)
		}
		break
	}
	return kept
}

// topBarOrchWord is the bar while a run page is open: the run's own word as
// the left cluster (its trail and its tank, roomorch.go's
// [app.orchHeadWord]), the room's right cluster beside it.
func (a *app) topBarOrchWord(width int, quiet bool) string {
	accent := a.pal.accent
	if a.roomApprovalHeight() > 0 {
		accent = a.pal.dim
	}
	right := a.topBarRight(quiet, accent)
	// THE RUN'S WORD IS ASKED FOR THE COLUMNS IT WILL ACTUALLY GET, and not for
	// the frame's: its own ladder shortens the trail properly ([fitTrail] again,
	// wearing the run page's separator), and a word built at the full width would
	// have been CUT by the clamp below instead — which is the one thing this
	// file's give-way exists to avoid. The clamp stays as the floor under it.
	room := width - cellsWidth(right) - hudGap
	if room < roomHeadFloor {
		room = roomHeadFloor
	}
	left := []topBarCell{{text: a.orchHeadWord(room), paint: accent}}
	left = clampCells(left, width-cellsWidth(right)-1)
	a.recordTopSpans(left, right, width)
	gap := width - cellsWidth(left) - cellsWidth(right)
	if gap < 0 {
		gap = 0
	}
	row := paintCells(left) + strings.Repeat(" ", gap) + paintCells(right)
	return fit(row, width)
}

// recordTopSpans walks the fitted cells once and writes every door's columns:
// the left cluster from column zero, the right cluster back from the frame's
// right edge. One walk for the record and the paint is the whole guarantee
// that a span and its word cannot disagree about which of them was dropped.
func (a *app) recordTopSpans(left, right []topBarCell, width int) {
	at := 0
	for _, c := range left {
		w := ansi.StringWidth(c.text)
		a.recordTopSpan(c.door, at, w)
		at += w
	}
	at = width - cellsWidth(right)
	for _, c := range right {
		w := ansi.StringWidth(c.text)
		a.recordTopSpan(c.door, at, w)
		at += w
	}
}

func (a *app) recordTopSpan(door topBarDoor, at, w int) {
	if w == 0 {
		return
	}
	span := hudSpan{from: at, to: at + w}
	switch door {
	case topDoorModel:
		a.modelSpan = span
	case topDoorYolo:
		a.topYoloSpan = span
	case topDoorBack:
		a.backSpan = span
	case topDoorStop:
		a.roomStop = span
	case topDoorCrumbHome:
		a.crumbHomeSpan = span
	case topDoorCrumbChat:
		a.crumbChatSpan = span
	}
}

// topBarLeft is the left cluster at one rung of the crumb's ladder with the
// given trimmings. It is a function of those two facts — and nothing else —
// because the give-way loop has to be able to rebuild the cluster one rung at
// a time without any other state drifting between the rungs.
func (a *app) topBarLeft(rung trailRung, showGlyph, showState, showClock, showSpend bool, accent func(string) string) []topBarCell {
	room := a.roomOpen()
	var cells []topBarCell
	// THE LEAD GLYPH COLUMN, [topBarLead] cells wide: the conversation's own `·`
	// at rest, and the node's state glyph in a room — the one mark on the bar
	// that moves while you watch, which is why it is first: the eye lands on the
	// thing that changes and reads the still things around it.
	//
	// The trailing cell is the column's and not a separator's. Every mark that
	// goes in here is one cell wide (a spinner frame, a ✓, a ✗, the queued ring,
	// the resting dot), so the crumb after it starts in the same column whichever
	// of them is drawn — and it is one cell of quiet rather than a ` · `, because
	// the glyph is not a step of the trail.
	if showGlyph {
		if room {
			if node := a.roomNode(); node != nil {
				cells = append(cells, topBarCell{text: a.roomMark(node) + " ", paint: accent})
			}
		} else {
			cells = append(cells, topBarCell{text: topBarRestMark + " ", paint: a.pal.dim})
		}
	}
	// THE CRUMB. Parents dim, current ink — in a room the whole cluster is the
	// one accent and the handle rides dim inside it (see the file's head).
	cells = append(cells, a.crumbCells(fitTrail(a.crumbSegments(), rung), room, accent)...)
	// THE TRIMMINGS, each dropped when nobody published it, joined with the
	// dotted ` · ` the status row has always used.
	if room {
		node := a.roomNode()
		for _, part := range []struct {
			show bool
			text string
		}{
			{showState, a.roomStateWord(node)},
			{showClock, a.roomClock(node)},
			{showSpend, a.roomSpend(node)},
		} {
			if part.show && part.text != "" {
				cells = append(cells,
					topBarCell{text: legendJoin, paint: accent},
					topBarCell{text: part.text, paint: accent})
			}
		}
	}
	return cells
}

// crumbCells paints the fitted trail: every parent dim, the current segment
// ink in a chat and accent in a room, and the handle `#N` dim in both — a
// label on the lit thing rather than a second thing lit. The door tags ride
// with the cells so the assembly can record the spans without a second walk
// over the trail's own logic.
//
// THE CURRENT STEP IS THE ONE BOLD THING ON THE BAR. Weight is spent on exactly
// three things across this whole surface — where you are, a region's heading,
// and a thing that wants you — and this is the first of them: the crumb is read
// by finding the end of it, and hue alone (ink against dim) is a step a person
// scanning a peripheral row does not reliably make. It is bold IN ITS OWN HUE
// rather than in a fourth one, so the accent budget is unchanged and a room's
// cluster stays one lit element.
func (a *app) crumbCells(segs []crumbSeg, room bool, accent func(string) string) []topBarCell {
	var cells []topBarCell
	for i, seg := range segs {
		if i > 0 {
			cells = append(cells, topBarCell{text: topBarSep, paint: a.pal.dim})
		}
		last := i == len(segs)-1
		if seg.text == trailEllipsis {
			cells = append(cells, topBarCell{text: seg.text, paint: a.pal.dim})
			continue
		}
		paint := a.pal.dim
		if last && !room {
			paint = a.pal.ink
		}
		if room {
			paint = accent
		}
		if last {
			paint = boldIn(a.pal, paint)
		}
		cells = append(cells, topBarCell{text: seg.text, paint: a.topBarPaint(seg.door, paint), door: seg.door})
		if seg.handle != "" {
			cells = append(cells, topBarCell{text: seg.handle, paint: a.pal.dim})
		}
	}
	return cells
}

// topBarSep is the crumb's separator: the › the crumb is spelled with, padded
// to read as a step rather than a slash.
const topBarSep = " › "

// boldIn is one hue worn at weight: the hue's own paint with [palette.bold]
// around it. The order matters and it is this one — the weight is the OUTER
// wrapper, so the hue's reset does not end the bold halfway through the word,
// which is the same nesting rule every other paint on this surface follows.
func boldIn(pal palette, hue func(string) string) func(string) string {
	return func(text string) string { return pal.bold(hue(text)) }
}

const (
	// topBarLead is the lead glyph column: the mark and one cell of quiet after
	// it, so the crumb starts in the same column at rest and in a room.
	topBarLead = 2
	// topBarRestMark is what stands in that column when nothing is running —
	// the smallest mark this surface has, which is what a conversation at rest
	// is owed: the column is held so the crumb does not shift sideways the
	// moment work starts, and nothing is claimed by holding it.
	topBarRestMark = "·"
)

// ── the crumb ───────────────────────────────────────────────────────────────
//
// project › chat name › parent task › this task #2.1
//
// The trail is the room header's own fact — where you are — widened to carry
// the conversation's two names under it. The current segment is never
// sacrificed for a parent; the handles stay attached to their titles through
// every rung.

// crumbSeg is one step of the trail.
type crumbSeg struct {
	text   string
	handle string // " #N" on the current step, empty on the rest
	door   topBarDoor
}

// crumbSegments is the full trail, outermost first: the project, the chat's
// own name, the parent tasks between the conversation and this one, and this
// task's title with its handle. A chat's trail is the first two alone.
//
// The doors: the project's step is space space parity (home); the chat's own
// name, in a task, is the precise way out — one crumb level up, the same climb
// esc makes. The current step has no door — a press on where you already are
// is a press on nothing.
func (a *app) crumbSegments() []crumbSeg {
	segs := []crumbSeg{{text: a.place, door: topDoorCrumbHome}}
	// THE CHAT'S STEP EXISTS ONLY WHERE THE CHAT HAS A NAME OF ITS OWN. A
	// conversation nobody has titled yet, and one whose title IS the folder it
	// stands in, would put the same word on the crumb twice — which is a step
	// that says nothing and a door that goes where its neighbour goes.
	if name := a.sessionName(); name != "" && name != a.place {
		segs = append(segs, crumbSeg{text: name})
	}
	if !a.roomOpen() {
		return segs
	}
	node := a.roomNode()
	if node == nil {
		return segs
	}
	// AND THE ROOM'S OWN STEP IS ADDED WHATEVER IS ABOVE IT. An unnamed
	// conversation used to collapse the whole trail to the project's one step and
	// take the task's title down with it — the bar in a room said `lab` and
	// nothing about the task the person was standing in, which is the one thing a
	// crumb exists to say.
	if len(segs) > 1 {
		// The way out is the chat's step where there is one. Where there is not,
		// the project step keeps its own door — home — and the back word is the
		// only way out the pointer has, which is what the back word is for.
		segs[1].door = topDoorCrumbChat
	}
	// THE PARENTS, outermost first, walked up the engine's own parent seam
	// (taskchip.go's [taskNode.ParentID], indexed once by [app.nodesByKey]).
	//
	// The walk is guarded twice over, because a crumb is drawn on every frame and
	// neither failure would be survivable there. A key nobody admitted stops the
	// climb rather than dereferencing nothing; a key already seen stops it rather
	// than walking a cycle for ever — and a roster that could name its own
	// ancestor is a roster the rail could not draw either.
	byKey := a.nodesByKey()
	var parents []string
	seen := map[string]bool{}
	for key := node.ParentID(); key != "" && !seen[key]; {
		seen[key] = true
		parent := byKey[key]
		if parent == nil {
			break
		}
		parents = append([]string{parent.title}, parents...)
		key = parent.ParentID()
	}
	for _, title := range parents {
		segs = append(segs, crumbSeg{text: title})
	}
	segs = append(segs, crumbSeg{
		text:   a.room.title,
		handle: " #" + itoa(int(a.room.id)),
	})
	return segs
}

// trailRung is one step of the crumb's give-way ladder.
type trailRung int

const (
	trailFull      trailRung = iota // the whole trail
	trailMiddles                    // the middles folded to one …
	trailNoProject                  // the project gone, … in its place
	trailNoParent                   // the parent gone too
	trailTitle                      // the current title truncated, floor roomHeadFloor
)

// trailEllipsis is the elision mark a dropped rung leaves behind: one …
// standing for everything the trail no longer says, so a shortened crumb
// still says that it was shortened.
const trailEllipsis = "…"

// crumbWord is a fitted trail as one plain string, handles attached, for a
// surface that paints its left cluster in one hue and has no use for cells
// (statusdeck.go's [app.deckTitle]). It is beside [trailWidth] because the two
// have to agree: what this writes is what that measures.
func crumbWord(segs []crumbSeg, sep string) string {
	var b strings.Builder
	for i, seg := range segs {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(seg.text)
		b.WriteString(seg.handle)
	}
	return b.String()
}

// trailWidth is what a fitted trail measures, joined with its separator. The
// ladder's caller needs it to know when to stop walking, and it lives beside
// the ladder so the two cannot disagree about what a rung costs.
func trailWidth(segs []crumbSeg, sep string) int {
	n := 0
	for i, seg := range segs {
		if i > 0 {
			n += ansi.StringWidth(sep)
		}
		n += ansi.StringWidth(seg.text) + ansi.StringWidth(seg.handle)
	}
	return n
}

// fitTrail is the give-way ladder every crumb on this surface walks, taken
// one rung at a time by the caller ([app.topBarWord]) or to the floor at once
// (roomorch.go's [app.orchHeadWord]):
//
//	full → middles folded to … (first and last two kept) → project dropped →
//	parent dropped → title truncated to [roomHeadFloor] columns
//
// The current segment is never sacrificed for a parent — it is truncated at
// the last rung and never dropped — and its handle stays attached through
// every rung, because a title the handle has left is a title that names
// nothing. It is one function for the top bar's crumb and the run page's own
// trail because the two are the same design wearing two separators, and a
// ladder each would be two places the same width could be spent two ways.
func fitTrail(segs []crumbSeg, rung trailRung) []crumbSeg {
	if len(segs) == 0 {
		return segs
	}
	// The rungs are taken in order, each from the trail the last one left.
	if rung >= trailMiddles && len(segs) > 3 {
		// Keep the first and the last two; the middle is one ….
		segs = append([]crumbSeg{segs[0], {text: trailEllipsis}}, segs[len(segs)-2:]...)
	}
	if rung >= trailNoProject && len(segs) > 2 {
		// The project goes; the … it leaves behind is the marker of the drop.
		segs = append([]crumbSeg{{text: trailEllipsis}}, segs[len(segs)-2:]...)
	}
	if rung >= trailNoParent && len(segs) > 2 {
		// The parent goes; the … stands for it too.
		segs = append([]crumbSeg{{text: trailEllipsis}}, segs[len(segs)-1:]...)
	}
	if rung >= trailTitle {
		// The title truncates to its floor with the handle still attached.
		last := &segs[len(segs)-1]
		if w := ansi.StringWidth(last.text); w > roomHeadFloor {
			last.text = fit(last.text, roomHeadFloor)
		}
	}
	return segs
}

// ── the right cluster ──────────────────────────────────────────────────────
//
// model[:effort] · branch* · host · YOLO [· esc/← back · ✕]
//
// The terms the legend's left end used to carry (host · branch) and the ones
// the status row's identity cluster did (the model), gathered where the slow
// facts live. A reconnect note outranks everything in here and may evict all
// of it — the wire being down is the one fact that rewrites the rest.

// topBarRight is the full right cluster. The model segment is lent its
// suffixed form for the length of the call, exactly as the status row's
// identity cluster always lent it (render.go's [app.statusRows] draws what is
// left of that row): the level is how this model is being run, not a thing
// beside it.
func (a *app) topBarRight(quiet bool, accent func(string) string) []topBarCell {
	return a.topBarRightFit(quiet, accent, true, true, true, true, true)
}

func (a *app) topBarRightFit(quiet bool, accent func(string) string, hostDetail, showBranch, showEffort, showModel, showBack bool) []topBarCell {
	if quiet {
		// The welcome box quiets the bar to its crumb; a right cluster with no
		// terms records no spans, and a door that is not drawn is a door that
		// cannot be pressed.
		return nil
	}
	room := a.roomOpen()
	var cells []topBarCell
	dim := a.pal.dim
	if room {
		dim = accent
	}
	add := func(text string, paint func(string) string) {
		if text != "" {
			cells = append(cells, topBarCell{text: text, paint: paint})
		}
	}
	sep := func() {
		if len(cells) > 0 {
			cells = append(cells, topBarCell{text: legendJoin, paint: dim})
		}
	}
	// THE MODEL, with the reasoning level spliced onto it where one has been
	// dialled. In a room it is the task's own model — `task glm-4.6` — and the
	// door it opens retargets the node; which nodes may be moved at all is
	// settled by the render, in the columns it recorded (room.go's
	// [app.roomModelMovable]): a node past being moved has no span, so the
	// press never sees it.
	//
	// IT WEARS THE CLUSTER'S OWN HUE AT REST, which is [palette.dim] in a chat
	// and the accent in a room — the same `dim` this function paints every other
	// term with.
	//
	// THE RIGHT CLUSTER IS THE QUIET "ABOUT" VOICE, and the model is not an
	// exception to it. It used to rest in ink, which made the one word on the bar
	// that is a CONTROL also the loudest thing on a row whose whole job is to be
	// peripheral — and loudness is not how this surface says pressable anyway.
	// The pointer is: the term lifts a step under it ([app.topBarPaint]), which
	// is exactly what the model's name did at the foot of the frame before it
	// moved up here. The two terms that stay loud stay loud because of what they
	// MEAN and not because of what they do — YOLO in [palette.bad], a reconnect
	// note in the accent.
	//
	// An ink model amid an accent cluster would also be invisible under the
	// pointer, because ink is what a room's lift raises a door TO: a lift only
	// reads as a lift from the hue beside it.
	modelRest := dim
	if showModel {
		if room {
			if node := a.roomNode(); node != nil {
				word := a.roomModelWord()
				if showEffort {
					if clause := a.taskEffortClause(node); clause != "" {
						word += legendJoin + clause
					}
				}
				sep()
				if a.roomModelMovable() {
					cells = append(cells, topBarCell{text: word, paint: a.topBarPaint(topDoorModel, modelRest), door: topDoorModel})
				} else {
					add(word, dim)
				}
			}
		} else if a.model != "" {
			id := a.model
			if showEffort {
				// The level is spliced onto the id for the length of the call, exactly
				// as the status row always lent it: the level is how this model is
				// being run, not a thing beside it.
				if level := a.reasoningFor(a.model); level != "" {
					id = id + ":" + level
				}
			}
			sep()
			cells = append(cells, topBarCell{text: modelBase(id), paint: a.topBarPaint(topDoorModel, modelRest), door: topDoorModel})
		}
	}
	// THE BRANCH, dirty star and all, as the legend carried it.
	if showBranch {
		if branch := a.branchWord(); branch != "" {
			sep()
			add(branch, dim)
		}
	}
	// THE HOST, with its latency detail while the link is healthy — the detail
	// is the first thing the cluster gives up, the name the last-but-three —
	// and as the reconnect note itself while the wire is down, which is the
	// one fact that outranks the terms around it.
	if note := a.linkNote(); note != "" {
		sep()
		// ACCENT AND NOT [palette.bad], which is the status row's own ruling about
		// this sentence carried up here with it (render.go's [app.paintPart]): a
		// redial is expected, bounded and usually resolves itself, and the words
		// rather than the paint are what distinguish it from a healthy reading.
		add(note, a.pal.accent)
	} else if a.hosted() {
		sep()
		if hostDetail {
			if seg := a.linkSegment(); seg != "" {
				add(seg, dim)
			}
		} else {
			add(a.host, dim)
		}
	}
	// YOLO, painted bad, never dropped: seeing the gate open has to offer the
	// way to close it, at every width the bar is drawn at. The word and the
	// condition are [app.yoloSegment]'s, said once (render.go's negative-space
	// safety law) — the bar is where it is DRAWN, not where it is decided.
	if yolo := a.yoloSegment(); yolo != "" {
		sep()
		cells = append(cells, topBarCell{text: yolo, paint: a.topBarPaint(topDoorYolo, a.pal.bad), door: topDoorYolo})
	}
	// THE ROOM'S WAY OUT, the back word and the ✕, in the order a hand
	// reaches for them. The ✕ outlives the back word's microcopy at narrow
	// widths exactly as it did on the room header (stop.go).
	if room {
		if showBack {
			sep()
			cells = append(cells, topBarCell{text: roomBackWord, paint: a.topBarPaint(topDoorBack, accent), door: topDoorBack})
		}
		if stop := a.roomStopWord(); stop != "" {
			sep()
			cells = append(cells, topBarCell{text: stop, paint: a.topBarPaint(topDoorStop, accent), door: topDoorStop})
		}
	}
	return cells
}

// ── THE BAR'S HOVER ────────────────────────────────────────────────────────
//
// EVERY DOOR ON THIS BAR LIGHTS, AND NOTHING ELSE DOES — hover.go's law, which
// is that the set which lights is exactly the set the press acts on, each at the
// size of its own span rather than of the row it rides. The bar is one row and a
// band across it would offer to go home over cells that stop the work.
//
// THE STEP IS THE ROOM HEADER'S OWN, moved up with everything else. A cell in an
// accent cluster lifts to [palette.ink] — which is what the ✕ has always done
// inside that line — and a cell in a chat at rest, where the bar is ink and dim,
// lifts to [palette.accent]. One step in both cases, and no new hue in either.
//
// YOLO IS THE EXCEPTION, and it is the only one: it wears [palette.bad] because
// of what it MEANS, and both lifts above would paint the open gate in the hue of
// something safe. So it takes the band instead — [palette.cursor] round exactly
// its own four cells, which is the tray's answer for a chip whose own hue has to
// survive the pointer (attach.go, effortchip.go) — and the word stays red under
// it.

// topBarPaint wraps a cell's resting hue in its door's hover step. A cell with
// no door is returned untouched: a span that answers to nothing does not react.
func (a *app) topBarPaint(door topBarDoor, rest func(string) string) func(string) string {
	if door == topDoorNone {
		return rest
	}
	return func(text string) string {
		if !a.topBarHot(door) {
			return rest(text)
		}
		if door == topDoorYolo {
			return a.pal.cursor(rest(text), 0)
		}
		if a.roomOpen() {
			return a.pal.ink(text)
		}
		return a.pal.accent(text)
	}
}

// topBarHot reports whether the pointer is on this door — AND whether the door
// was drawn at all. The span is the second half of that question and it is asked
// here rather than trusted: the spans are written by the layout before the paint
// runs ([app.topBarWord] says why the two are one walk), so a term the fitting
// dropped has no columns, cannot be pressed, and must not light.
func (a *app) topBarHot(door topBarDoor) bool {
	switch door {
	case topDoorModel:
		return a.modelSpan.pressable() && a.hoveringStatusModel()
	case topDoorYolo:
		return a.topYoloSpan.pressable() && a.hoveringYolo()
	case topDoorBack:
		return a.backSpan.pressable() && a.hoveringRoomBack()
	case topDoorStop:
		return a.roomStop.pressable() && a.hoveringRoomStop()
	case topDoorCrumbHome:
		return a.crumbHomeSpan.pressable() && a.hoveringHome()
	case topDoorCrumbChat:
		return a.crumbChatSpan.pressable() && a.hoveringCrumbChat()
	}
	return false
}

// ── cells ──────────────────────────────────────────────────────────────────

// topBarDoor is what a press on one cell of the bar does.
type topBarDoor uint8

const (
	topDoorNone topBarDoor = iota
	topDoorModel
	topDoorYolo
	topDoorBack
	topDoorStop
	topDoorCrumbHome
	topDoorCrumbChat
)

// topBarCell is one piece of the bar: its plain text, the paint it wears, and
// — where the piece is a door — which door. The width math, the drawing and
// the span recording read the same three fields, which is the whole trick to
// a row whose pieces can be dropped one at a time without the columns, the
// paint and the pointer ever disagreeing about what is left.
type topBarCell struct {
	text  string
	paint func(string) string
	door  topBarDoor
}

func cellsWidth(cells []topBarCell) int {
	n := 0
	for _, c := range cells {
		n += ansi.StringWidth(c.text)
	}
	return n
}

func paintCells(cells []topBarCell) string {
	var b strings.Builder
	for _, c := range cells {
		b.WriteString(c.paint(c.text))
	}
	return b.String()
}
