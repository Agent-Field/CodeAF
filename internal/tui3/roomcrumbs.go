package tui3

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── THE BREADCRUMB BAR: WHERE YOU ARE, THE WHOLE WAY DOWN ───────────────────
//
// The top row of the frame answers one question and it is the first question a
// person has when they look up: WHICH PAGE IS THIS. Until this file it answered
// half of it. The trail was built over [app.roomPath], which was the open room's
// own title and nothing else, so a task three levels inside a family drew the
// same two crumbs as a task the conversation had asked for directly —
//
//	main ▸ Cut the goldens
//
// — while the ancestry the engine had modelled all along sat one row underneath
// in the kin block, spelled as a fact (`part of: Write the tree`) rather than as
// a place, and only ever ONE level of it. The page knew who its parent was and
// said so in a sentence you could not press.
//
// So the trail is now the ACTUAL CHAIN, root first:
//
//	main ▸ Ship the port ▸ Write the tree ▸ Cut the goldens
//
// and every step of it but the last is a DOOR. The last one is the page you are
// standing on and is inert — a crumb that reopened the page you are already on
// is a control that does nothing, and the surface does not draw those.
//
// ── THE FOUR LAWS THIS FILE HOLDS ───────────────────────────────────────────
//
//  1. NO CRUMB IS INVENTED. A step is drawn only where this window can name the
//     work behind it. An ancestor whose node this surface has had no update for
//     ends the walk — the same refusal the kin line already made about a parent
//     it could not name ("part of: 7" has told a person nothing), applied to
//     every level rather than to one.
//
//  2. A CRUMB IS A DOOR ONLY WHERE A DOOR EXISTS THAT LANDS EXACTLY ON IT. The
//     root of a task page is `main` and its door is [app.closeRoom], which is
//     the conversation this window is sitting in. An ancestor's door is
//     [app.openRailRoom], which is the same door the roster's own row is. A page
//     read through ANOTHER conversation has neither: its ancestors belong to a
//     graph this window may not touch, and its root is somebody else's
//     conversation that `esc` does not lead to. Those crumbs are drawn and do
//     nothing, because a capability that cannot work is absent, not broken.
//
//  3. THE ROOT AND THE PAGE YOU ARE ON SURVIVE THE NARROWEST FRAME. What gives
//     way as the terminal narrows is the middle of the chain, and it gives way
//     to `…` — never to silence. A trail that quietly dropped two ancestors
//     would be a trail that says this work hangs off the conversation, which is
//     the one thing it exists to deny.
//
//  4. THE `…` IS A DOOR TOO, AND IT OPENS THE NEAREST THING IT HID. Pressing it
//     opens the IMMEDIATE PARENT — one step up, the step a person reaching for
//     a folded crumb almost always wants, and the crumb that would reappear
//     first if the terminal grew. An ellipsis standing for ancestors that are
//     all inert (a guest page) is inert with them.

// The crumb bar's own alphabet. The separator and the root word are room.go's
// ([roomCrumbSep], [roomCrumbRoot]) because this is the same trail it has always
// drawn, with the middle filled in.
const (
	// crumbFoldWord stands for the ancestors a narrow frame could not spell. It
	// is [glyphMore] and not three periods, so it is the same one-cell mark the
	// rest of this surface cuts with.
	crumbFoldWord = glyphMore
	// crumbHereFloor is the fewest cells the page's own crumb is worth drawing
	// in. Under it the name has stopped identifying anything and the row is an
	// ellipsis with a rule after it.
	crumbHereFloor = 6
	// crumbLayoutCap bounds the fitting ladder, not the semantic ancestry.
	// Deeper ancestors remain represented by a fold with an exact door.
	crumbLayoutCap = 8
)

// crumbKind says what one step of the trail IS, which is what decides whether it
// is a door and what that door does.
type crumbKind int

const (
	// crumbRoot is what this page hangs off: the conversation on screen, or —
	// on a page read through another one — whose conversation it is.
	crumbRoot crumbKind = iota
	// crumbOwner names another conversation without borrowing a local door.
	crumbOwner
	// crumbAncestor is one piece of work between the root and this page.
	crumbAncestor
	// crumbFold is the `…` standing for ancestors a narrow frame could not
	// spell (law 3).
	crumbFold
	// crumbHere is the page you are standing on. It is never a door (law 2 has
	// nothing to say about it: there is nowhere to go).
	crumbHere
)

// roomCrumb is one step of the trail: what it says, what it is, and the node its
// door opens where it has one.
//
// THE NODE IS THE DOOR AND NOT AN ID. Task ids restart with every conversation
// (session's task_index.go says so on [session.TaskIndexEntry.ID]), so a crumb
// carrying `7` and resolving it at press time against [app.tasks] is exactly the
// same-number crossover [app.roomNode] exists to prevent. A crumb built out of
// another conversation's graph carries no node at all, and therefore no door.
type roomCrumb struct {
	word string
	kind crumbKind
	node *taskNode
}

// door reports whether pressing this crumb would take a person anywhere.
func (c roomCrumb) door() bool {
	switch c.kind {
	case crumbRoot:
		// The root's door is "come back out to the conversation", which exists
		// exactly when there is a room to come out of and it is this window's own.
		return true
	case crumbAncestor, crumbFold:
		return c.node != nil
	}
	return false
}

// crumbHit is where one crumb was drawn and what pressing it opens. It is the
// bargain every pointer target on this surface makes (render.go's [hudSpan]): the
// render writes where the words landed, and the press resolves against that
// rather than against a second computation of the same layout.
type crumbHit struct {
	span  hudSpan
	crumb roomCrumb
}

// roomCrumbs is the trail as a model, whole and unfitted — the root, every
// ancestor this window can name, and the page you are standing on.
//
// IT IS THE ONE ANSWER, and [app.roomTrail] is this read out as a sentence. The
// header, the hit map and the hover all resolve through here, so a trail that
// draws one way cannot be pressed another.
func (a *app) roomCrumbs() []roomCrumb {
	// THE CONVERSATION ITSELF IS ONE STEP AND IT IS THE PAGE YOU ARE ON, so it is
	// inert. There is no trail row over a conversation any more — the tab strip
	// above says which conversation this is, and the picker `ctrl+k` opens is the
	// control at that strip's own right end (chattabs.go) — but the model still
	// answers here, because [app.roomTrail] is read by pages that ask what the
	// trail SAYS without drawing one.
	if a.room == nil {
		return []roomCrumb{{word: a.chatCrumbWord(), kind: crumbHere}}
	}
	// A PAGE READ THROUGH ANOTHER CONVERSATION HANGS OFF THAT CONVERSATION AND
	// NOT OFF THIS ONE (taskowner.go). `main` is this surface's one name for the
	// conversation on screen, and a trail reading `main ▸ Port the parser` would
	// say this window started that work. So the root is whose it is, its
	// ancestors are the ones the record showed under THAT conversation, and none
	// of them is a door: this window's graph is not theirs.
	if guest := a.roomGuest(); guest != nil {
		crumbs := []roomCrumb{{word: roomGuestOwnerWord + guestOwnerName(guest.owner), kind: crumbOwner}}
		for _, up := range guest.trail {
			crumbs = append(crumbs, roomCrumb{word: up, kind: crumbAncestor})
		}
		return crumbHereOn(crumbs, a.roomHereWord())
	}
	crumbs := []roomCrumb{{word: a.chatCrumbWord(), kind: crumbRoot}}
	for _, up := range a.roomAncestors() {
		crumbs = append(crumbs, roomCrumb{word: up.title, kind: crumbAncestor, node: up})
	}
	return crumbHereOn(crumbs, a.roomHereWord())
}

// crumbHereOn omits an unnamed final step rather than ending the trail in a
// separator. Known ancestry remains useful even before the current title arrives.
func crumbHereOn(crumbs []roomCrumb, word string) []roomCrumb {
	if strings.TrimSpace(word) == "" {
		return crumbs
	}
	return append(crumbs, roomCrumb{word: word, kind: crumbHere})
}

// roomHereWord is what the page you are standing on is called: the title the
// room was opened with, and the node's own where the room has none.
func (a *app) roomHereWord() string {
	if a.room == nil {
		return a.chatCrumbWord()
	}
	if a.room.title != "" {
		return a.room.title
	}
	if node := a.roomNode(); node != nil {
		return node.title
	}
	return a.room.title
}

// chatCrumbWord names the actual conversation, including its unnamed state.
func (a *app) chatCrumbWord() string {
	if name := a.sessionName(); name != "" {
		return name
	}
	return roomCrumbRoot
}

// roomAncestors follows only this room's parent links, avoiding a sorted roster
// rebuild on every frame. The seen set stops malformed cycles. Fitting folds
// deep chains later so their existence is never silently omitted here.
func (a *app) roomAncestors() []*taskNode {
	node := a.roomNode()
	if node == nil || a.roomIsGuest() {
		return nil
	}
	seen := map[uint64]bool{node.id: true}
	var up []*taskNode
	for parent := node.ParentID(); parent != ""; {
		id, err := strconv.ParseUint(parent, 10, 64)
		if err != nil || seen[id] {
			break
		}
		at := a.tasks[id]
		if at == nil {
			break
		}
		seen[id] = true
		up = append(up, at)
		parent = at.ParentID()
	}
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up
}

// roomCrumbLine lays the trail out in room cells and says where every crumb
// landed, in columns measured from the line's own start. The third answer is
// whether THE PAGE'S OWN CRUMB SURVIVED WHOLE, which is what the header spends
// its remaining cells on (room.go's [app.roomHeadWord] and law 1 of rowfit.go).
//
// THE LADDER IS LAW 3 WRITTEN DOWN. Each rung says the same true things as the
// one above it with less of the middle, and the two ends — what this hangs off,
// and what it is — are on every rung until the frame cannot hold them:
//
//	main ▸ Ship the port ▸ Write the tree ▸ Cut the goldens
//	main ▸ … ▸ Write the tree ▸ Cut the goldens
//	main ▸ … ▸ Cut the goldens
//	main ▸ … ▸ Cut the gold…
//	Cut the goldens
//
// The `…` is one crumb rather than an ellipsis inside a word, which is what
// makes it pressable at all (law 4) and what keeps it honest: a fold that hid
// two ancestors and a fold that hid five are the same mark, and both say the
// same true thing — there is more chain here than the row can spell.
func (a *app) roomCrumbLine(room int) (string, []crumbHit, bool) {
	crumbs := a.roomCrumbs()
	if room <= 0 || len(crumbs) == 0 {
		return "", nil, false
	}
	if len(crumbs) > 1 {
		crumbs[0].word = fit(crumbs[0].word, max(4, min(32, room/3)))
	}
	for _, rung := range crumbRungs(crumbs) {
		if line, hits, ok := crumbDraw(rung, room, false); ok {
			return line, hits, true
		}
	}
	// THE TWO ENDS WITH THE PAGE'S NAME CUT, THEN THE TWO ENDS ALONE, THEN THE
	// NAME BY ITSELF. All three are below law 1's line — the identity has an
	// ellipsis in it now — so all three report that the trail did not survive
	// whole, and the header spends nothing beside them.
	//
	// THE ROOT OUTLIVES THE FOLD AND THE FOLD OUTLIVES THE ANCESTORS, which is
	// law 3 at the bottom of the ladder: `main ▸ Cut the gold…` on a frame that
	// cannot hold the `…` still says the two things a person is asking for.
	root, here := crumbs[0], crumbs[len(crumbs)-1]
	rungs := [][]roomCrumb{}
	if len(crumbs) > 2 {
		rungs = append(rungs, []roomCrumb{root, crumbFoldOf(crumbs[1 : len(crumbs)-1]), here})
	}
	if len(crumbs) > 1 {
		rungs = append(rungs, []roomCrumb{root, here})
	}
	rungs = append(rungs, []roomCrumb{here})
	for _, rung := range rungs {
		if line, hits, ok := crumbDraw(rung, room, true); ok {
			return line, hits, false
		}
	}
	return "", nil, false
}

// crumbRungs is the ladder itself: the whole chain, then the chain with more and
// more of its middle folded into one `…`, then the two ends alone.
//
// FOLDING IS FROM THE OUTSIDE IN, so the crumb that survives longest is the
// IMMEDIATE PARENT. It is the one a person is most likely to be reaching for, it
// is the one whose door the `…` inherits (law 4), and it is the step that says
// most about where this page sits — `Cut the goldens` under `Write the tree`
// tells you more than `Cut the goldens` under `Ship the port` does.
func crumbRungs(crumbs []roomCrumb) [][]roomCrumb {
	if len(crumbs) > crumbLayoutCap+2 {
		tail := len(crumbs) - crumbLayoutCap - 1
		bounded := []roomCrumb{crumbs[0], crumbFoldOf(crumbs[1:tail])}
		crumbs = append(bounded, crumbs[tail:]...)
	}
	if len(crumbs) < 3 {
		return [][]roomCrumb{crumbs}
	}
	root, here := crumbs[0], crumbs[len(crumbs)-1]
	middle := crumbs[1 : len(crumbs)-1]
	rungs := [][]roomCrumb{crumbs}
	for keep := len(middle) - 1; keep >= 0; keep-- {
		rung := []roomCrumb{root, crumbFoldOf(middle[:len(middle)-keep])}
		rung = append(rung, middle[len(middle)-keep:]...)
		rungs = append(rungs, append(rung, here))
	}
	// AND THE LAST RUNG BEFORE THE FRAME GIVES UP IS THE TWO ENDS WITH THE PAGE'S
	// NAME CUT. It is a rung of its own rather than [app.roomCrumbLine]'s fallback
	// because the root and the fold are still true at this width and are worth
	// more than the last few cells of a title.
	return append(rungs, []roomCrumb{root, crumbFoldOf(middle), here})
}

// crumbFoldOf is the `…` standing for a run of ancestors: it inherits the door of
// the NEAREST one it hides, which is the last in the run (law 4).
func crumbFoldOf(hidden []roomCrumb) roomCrumb {
	fold := roomCrumb{word: crumbFoldWord, kind: crumbFold}
	if len(hidden) > 0 {
		fold.node = hidden[len(hidden)-1].node
	}
	return fold
}

// crumbDraw lays one rung out and reports whether it fitted. cut allows the last
// crumb to be truncated, which is the only rung that is ever allowed to.
func crumbDraw(crumbs []roomCrumb, room int, cut bool) (string, []crumbHit, bool) {
	line, at := "", 0
	hits := make([]crumbHit, 0, len(crumbs))
	sep := ansi.StringWidth(roomCrumbSep)
	for i, crumb := range crumbs {
		word := crumb.word
		lead := 0
		if i > 0 {
			lead = sep
		}
		if cut && i == len(crumbs)-1 {
			// THE CUT IS MEASURED IN DISPLAY CELLS, not in bytes or runes: a title
			// with a wide glyph or a combining mark in it is fitted by the same
			// grapheme walk every other row on this surface uses ([fit]).
			word = fit(word, room-at-lead)
			// A NAME CUT BELOW [crumbHereFloor] HAS STOPPED NAMING ANYTHING, so the
			// rung fails and a shorter one is tried — one with fewer crumbs in front
			// of the name, which is where those cells come from. The rung that is
			// only the name has nothing left to give up and draws whatever fits.
			if len(crumbs) > 1 && ansi.StringWidth(word) < crumbHereFloor &&
				ansi.StringWidth(crumb.word) >= crumbHereFloor {
				return "", nil, false
			}
		}
		width := ansi.StringWidth(word)
		if width == 0 || at+lead+width > room {
			return "", nil, false
		}
		if i > 0 {
			line += roomCrumbSep
			at += lead
		}
		line += word
		drawn := crumb
		drawn.word = word
		hits = append(hits, crumbHit{span: hudSpan{from: at, to: at + width}, crumb: drawn})
		at += width
	}
	return line, hits, true
}

// roomTrail is the trail as ONE STRING, which is what the header's own fitter and
// every reader that does not care about columns wants. It is [app.roomCrumbs]
// read out rather than a second walk, so the sentence and the doors can never
// disagree about what the trail says.
func (a *app) roomTrail() string {
	crumbs := a.roomCrumbs()
	trail := ""
	for i, crumb := range crumbs {
		if i > 0 {
			trail += roomCrumbSep
		}
		trail += crumb.word
	}
	return trail
}

// ── THE ROW THE CONVERSATION'S OWN BAR USED TO BE ───────────────────────────
//
// It drew one dim crumb — the conversation's name, with a `▾` after it where the
// switcher had somewhere to go — over the transcript, and it is gone. THE TAB
// STRIP IS THAT ROW NOW (chattabs.go): it says the same name, it says the names
// of the other conversations this window has been in beside it, and the `▾` that
// opened the picker is the control at its right end. What is NOT drawn over a
// conversation is a trail: `main` with nothing after it is the tab above it said
// twice, and the emptiness law is exactly that.

// headLabelAt is the column a pinned label starts on. It is two cells for both
// bars and for one reason each: [app.legendLine] opens the room's header with the
// border's own `─ `, and the conversation's bar is indented to the transcript's
// own left margin so the crumb stands over the words it is about.
//
// It is stated once because the crumb spans are measured from the label's start
// and pressed in the terminal's own columns, and a bar that recorded one and read
// the other would light a crumb two cells from the one a click opens.
const headLabelAt = 2

// crumbsAt moves a set of hits from the label's own columns into the frame's, so
// the press and the hover can ask about them in the coordinates a mouse arrives
// in.
func crumbsAt(hits []crumbHit, from int) []crumbHit {
	if len(hits) == 0 {
		return nil
	}
	out := make([]crumbHit, 0, len(hits))
	for _, hit := range hits {
		hit.span = hudSpan{from: hit.span.from + from, to: hit.span.to + from}
		out = append(out, hit)
	}
	return out
}

// paintCrumbs paints one header label, brightening the crumb the pointer is on
// where that crumb is a door. at is the column the label was drawn at, because
// the spans are kept in the terminal's own columns and the cut has to happen in
// the label's.
//
// IT IS A BRIGHTENING AND NOT A BAND, and the pieces are painted separately
// rather than nested — both are render.go's [app.paintIdentity], whose comment
// holds the whole of the reasoning. One step up from wherever the row already
// is: ink from a room's accent, accent from the conversation's dim.
//
// A CRUMB THAT IS NOT A DOOR NEVER LIGHTS. What lights is what a press acts on,
// so the page you are standing on and every crumb of somebody else's chain stay
// exactly as loud as the words around them.
func (a *app) paintCrumbs(label string, at int, paint func(string) string) string {
	hot, ok := a.hotCrumb()
	if !ok {
		return paint(label)
	}
	from, to := hot.span.from-at, hot.span.to-at
	if from < 0 || to > ansi.StringWidth(label) {
		return paint(label)
	}
	lift := a.pal.accent
	if a.roomOpen() {
		lift = a.pal.ink
	}
	return paint(ansi.Cut(label, 0, from)) + lift(ansi.Cut(label, from, to)) +
		paint(ansi.Cut(label, to, ansi.StringWidth(label)))
}

// hotCrumb is the crumb the pointer is on, when it is on one that would do
// something.
func (a *app) hotCrumb() (crumbHit, bool) {
	if a.hot.kind != hoverCrumb {
		return crumbHit{}, false
	}
	for _, hit := range a.crumbs {
		if hit.span.from == a.hot.index && hit.crumb.door() {
			return hit, true
		}
	}
	return crumbHit{}, false
}

// ── THE POINTER ─────────────────────────────────────────────────────────────

// crumbHoverAt is the hover this row answers with, and whether it answers at
// all. A crumb that is not a door is not a hover: the row it rides is the way
// out and keeps its own band under those cells (room.go's [app.roomBackAt]).
//
// THE CRUMB IS HELD BY THE COLUMN IT STARTS ON rather than by its place in the
// trail, for the reason the roster's rows are held by node (task.go's
// [hoverRail]): the trail re-folds as the terminal is resized and as a title
// arrives, so a hover stored as "the second crumb" would follow the fold instead
// of following the words.
func (a *app) crumbHoverAt(x, y int) (hoverAt, bool) {
	crumb, span, ok := a.crumbSpanAt(x, y)
	if !ok || !crumb.door() {
		return hoverAt{}, false
	}
	return hoverAt{kind: hoverCrumb, index: span.from}, true
}

// crumbAt is the crumb under a pointer, and whether there is one.
//
// It answers only for the header's own row, which is the row under the tab strip
// wherever there is one — [app.view] draws them in that order and the geometry
// charges [app.headHeight] for both (chattabs.go's [app.roomHeadRow] is where
// that row number is stated once, and room.go's [app.roomBackAt] reads it too).
func (a *app) crumbAt(x, y int) (roomCrumb, bool) {
	crumb, _, ok := a.crumbSpanAt(x, y)
	return crumb, ok
}

// crumbSpanAt is that, with the columns the crumb was drawn on.
func (a *app) crumbSpanAt(x, y int) (roomCrumb, hudSpan, bool) {
	if y != a.roomHeadRow() || a.headHeight() == 0 {
		return roomCrumb{}, hudSpan{}, false
	}
	for _, hit := range a.crumbs {
		if hit.span.holds(x) {
			return hit.crumb, hit.span, true
		}
	}
	return roomCrumb{}, hudSpan{}, false
}

// crumbPress answers a press on the trail and reports whether it took it.
//
// IT IS READ UNDER THE ✕ AND OVER THE WAY OUT (app.go's press order). The ✕ ends
// work and outranks everything on the row; the rest of the row is the way back to
// the conversation, and a crumb is a way back to somewhere more particular — so
// a press that lands on a crumb's own cells is that crumb's, and every other cell
// of the row is still the exit it has always been.
//
// A CRUMB THAT IS NOT A DOOR TAKES THE PRESS AND DOES NOTHING WITH IT, rather
// than falling through to the way out. The page you are standing on, and every
// crumb of a page read through another conversation, are drawn because they are
// true; a press on them that quietly left the room would be the trail acting on a
// promise it never made.
func (a *app) crumbPress(x, y int) bool {
	crumb, ok := a.crumbAt(x, y)
	if !ok {
		return false
	}
	if !crumb.door() {
		return true
	}
	if crumb.node == nil {
		// The root of this window's own page: back out to the conversation, through
		// the door `esc` and the header's right end already use.
		a.closeRoom()
		return true
	}
	// AND AN ANCESTOR GOES THROUGH THE ROSTER'S OWN DOOR. [app.openRailRoom] is
	// what a row of the column does — idempotent, so a second press on the crumb
	// of the page you just opened does not close it, and it keeps that page's
	// draft, scroll and subscription rather than replacing them. A run's node
	// lands on the run's page there, which is where its row lands too.
	a.openRailRoom(crumb.node)
	return true
}
