package tui3

// ── THE BREADCRUMB BAR ──────────────────────────────────────────────────────
//
// The top row says WHERE YOU ARE, and until this wave it said half of it: `main
// ▸ <this page>`, whatever the work was actually a piece of. These hold the four
// claims the file makes (roomcrumbs.go) — the chain is the real one, a crumb is
// a door exactly where a door exists, the two ends survive the narrowest frame,
// and the fold opens what it hid — plus the two things that can quietly stop
// being true about any hit map: that the columns recorded are the columns drawn,
// and that nothing on this row reaches past the ✕ riding its end.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// crumbApp is a window standing inside `Cut the goldens`, which is three levels
// down the planted family: main ▸ Ship the port ▸ Write the tree ▸ Cut the
// goldens ([railRun] plants it).
func crumbApp(t *testing.T) *app {
	t.Helper()
	a, _, _ := roomApp(t)
	railRun(a)
	a.width, a.height = 200, 40
	openRoomThroughRail(t, a, 4)
	return a
}

// openRoomThroughRail walks into one node's page the way a person does, and
// fails loudly rather than leaving a test standing somewhere else.
func openRoomThroughRail(t *testing.T, a *app, id uint64) {
	t.Helper()
	a.openRailRoom(a.tasks[id])
	if a.room == nil || a.room.id != id {
		t.Fatalf("the rail did not open node %d: room=%d", id, roomID(a))
	}
	a.touch()
	strings.Join(a.roomHeadRows(a.width), "\n")
}

// crumbSpanFor is where one crumb was drawn on the last laid-out header.
func crumbSpanFor(t *testing.T, a *app, word string) hudSpan {
	t.Helper()
	for _, hit := range a.crumbs {
		if hit.crumb.word == word {
			return hit.span
		}
	}
	t.Fatalf("the header drew no crumb saying %q:\n%q\n%+v", word, plain(strings.Join(a.roomHeadRows(a.width), "\n")), a.crumbs)
	return hudSpan{}
}

// clickHead presses one column of the room's own header row, which is the row
// under the tab strip (chattabs.go's [app.roomHeadRow]).
func clickHead(t *testing.T, a *app, x int) {
	t.Helper()
	y := a.roomHeadRow()
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// ── THE CHAIN IS THE REAL ONE ───────────────────────────────────────────────

// LAW 1. Every step between the conversation and this page is on the trail, in
// order, out of the same tree the roster's column is grown from.
func TestTheTrailNamesEveryStepOfTheActualChain(t *testing.T) {
	a := crumbApp(t)
	const chain = "main ▸ Ship the port ▸ Write the tree ▸ Cut the goldens"
	if got := a.roomTrail(); got != chain {
		t.Fatalf("the trail is %q, want %q", got, chain)
	}
	if head := plain(strings.Join(a.roomHeadRows(a.width), "\n")); !strings.Contains(head, chain) {
		t.Fatalf("the header does not draw the chain:\n%q", head)
	}
	// AND THE PAGE ONE STEP UP IS THE CHAIN WITHOUT ITS LAST STEP: the trail is
	// the family read upwards and not a history of where this window has been.
	openRoomThroughRail(t, a, 3)
	if got, want := a.roomTrail(), "main ▸ Ship the port ▸ Write the tree"; got != want {
		t.Fatalf("from the parent's page the trail is %q, want %q", got, want)
	}
}

// AND NOTHING THIS WINDOW CANNOT NAME IS ON IT. A node whose parent the surface
// has had no update for ends the walk rather than drawing an id — the refusal
// the kin line already made about a parent it could not name, applied to every
// level.
func TestTheTrailInventsNoStepItCannotName(t *testing.T) {
	a := crumbApp(t)
	// The middle of the family is dropped from the graph, exactly as a restart
	// that never re-published it would leave things.
	delete(a.tasks, 3)
	a.touch()
	if got, want := a.roomTrail(), "main ▸ Cut the goldens"; got != want {
		t.Fatalf("the trail is %q, want %q — no id, no blank, no guess", got, want)
	}
	if head := plain(strings.Join(a.roomHeadRows(a.width), "\n")); strings.Contains(head, "3") && strings.Contains(head, "▸ 3") {
		t.Fatalf("the header named a missing ancestor by its id:\n%q", head)
	}
}

// AND A FAMILY THAT CLAIMS TO BE ITS OWN GRANDPARENT DOES NOT HANG THE WALK. A
// record and not a tree; the walk ends where the column's own does.
func TestACycleInTheFamilyEndsTheTrailRatherThanTheProgram(t *testing.T) {
	a := crumbApp(t)
	a.tasks[1].parent = itoa(4)
	a.touch()
	trail := a.roomTrail()
	if !strings.HasPrefix(trail, roomCrumbRoot) || !strings.HasSuffix(trail, "Cut the goldens") {
		t.Fatalf("the trail lost its two ends walking a cycle: %q", trail)
	}
	if n := strings.Count(trail, roomCrumbSep); n > crumbLayoutCap {
		t.Fatalf("the walk went round %d times: %q", n, trail)
	}
}

// ── THE DOORS ───────────────────────────────────────────────────────────────

// LAW 2, THE ORDINARY HALF. An ancestor's crumb opens that ancestor's page,
// through the roster's own door — and the page you are standing on is inert,
// because there is nowhere for it to go.
func TestAnAncestorCrumbOpensThatPageAndTheCurrentOneIsInert(t *testing.T) {
	a := crumbApp(t)
	here := crumbSpanFor(t, a, "Cut the goldens")
	clickHead(t, a, here.from+1)
	if roomID(a) != 4 {
		t.Fatalf("a press on the page's own crumb moved to %d", roomID(a))
	}

	up := crumbSpanFor(t, a, "Write the tree")
	clickHead(t, a, up.from+1)
	if roomID(a) != 3 {
		t.Fatalf("the parent's crumb opened %d, want node 3", roomID(a))
	}
	// AND IT IS THE ROSTER'S DOOR, WHICH IS IDEMPOTENT. A second press on the
	// crumb of the page you are already on does not close it — the page keeps its
	// scroll, its subscription and the draft written for it.
	strings.Join(a.roomHeadRows(a.width), "\n")
	again := crumbSpanFor(t, a, "Write the tree")
	clickHead(t, a, again.from+1)
	if roomID(a) != 3 {
		t.Fatalf("pressing the crumb of the open page closed it: room=%d", roomID(a))
	}
	// AND THE ROOT IS THE WAY BACK OUT, which is the door `esc` and the header's
	// right end already are.
	root := crumbSpanFor(t, a, roomCrumbRoot)
	clickHead(t, a, root.from+1)
	if a.roomOpen() {
		t.Fatal("the root crumb did not come back out to the conversation")
	}
}

// AND THE DRAFT GOES WITH THE PAGE. Walking up the trail is walking into another
// node's room, so the box is re-pointed exactly as the roster's own row
// re-points it: what was written for one node is stashed under it, not carried.
func TestWalkingUpTheTrailKeepsEachPagesOwnDraft(t *testing.T) {
	a := crumbApp(t)
	a.input.setText("this line is for the goldens")
	up := crumbSpanFor(t, a, "Write the tree")
	clickHead(t, a, up.from+1)
	if got := string(a.input.value); got != "" {
		t.Fatalf("the parent's page opened holding the child's draft: %q", got)
	}
	if trail := a.roomTrail(); strings.Contains(trail, "Cut the goldens") {
		t.Fatalf("the child is still on the trail of its own parent: %q", trail)
	}
	openRoomThroughRail(t, a, 4)
	if got := string(a.input.value); got != "this line is for the goldens" {
		t.Fatalf("coming back to the page did not restore its own draft: %q", got)
	}
}

// ── THE NARROW FRAME ────────────────────────────────────────────────────────

// LAW 3 AND LAW 4. The middle folds into one `…`, the two ends stay, and the
// fold opens the nearest ancestor it hid — which is the immediate parent, and
// the crumb that would come back first if the terminal grew.
func TestTheTrailFoldsItsMiddleAndTheFoldOpensTheParent(t *testing.T) {
	// FOLDING IS FROM THE OUTSIDE IN, so at fifty columns the `…` stands for the
	// root of the family alone and the immediate parent is still spelled — and
	// the fold opens the nearest thing IT hid, which is that outer step.
	a := crumbApp(t)
	a.width = 50
	a.touch()
	head := plain(strings.Join(a.roomHeadRows(a.width), "\n"))
	if !strings.Contains(head, crumbFoldWord) {
		t.Fatalf("a fifty-column header spelled the whole chain:\n%q", head)
	}
	for _, want := range []string{roomCrumbRoot, "Write the tree", "Cut the goldens"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the folded header lost %q:\n%q", want, head)
		}
	}
	if strings.Contains(head, "Ship the port") {
		t.Fatalf("the fold kept the step it claims to have hidden:\n%q", head)
	}
	fold := crumbSpanFor(t, a, crumbFoldWord)
	clickHead(t, a, fold.from)
	if roomID(a) != 1 {
		t.Fatalf("the fold opened %d, want the step it hid, node 1", roomID(a))
	}

	// NARROWER STILL, the `…` stands for the whole middle — and now it opens the
	// IMMEDIATE PARENT, which is the nearest of the two it is hiding and the crumb
	// that would come back first if the terminal grew.
	b := crumbApp(t)
	b.width = 40
	b.touch()
	narrow := plain(strings.Join(b.roomHeadRows(b.width), "\n"))
	for _, gone := range []string{"Ship the port", "Write the tree"} {
		if strings.Contains(narrow, gone) {
			t.Fatalf("at forty columns the trail still spells %q:\n%q", gone, narrow)
		}
	}
	if !strings.Contains(narrow, roomCrumbRoot) || !strings.Contains(narrow, "Cut the goldens") {
		t.Fatalf("at forty columns the trail lost one of its two ends:\n%q", narrow)
	}
	deep := crumbSpanFor(t, b, crumbFoldWord)
	clickHead(t, b, deep.from)
	if roomID(b) != 3 {
		t.Fatalf("the fold opened %d, want the immediate parent, node 3", roomID(b))
	}
}

// AND THE FOLD IS NEVER TRADED FOR SILENCE. Whatever the width, a page with work
// above it says so — the mark is what keeps `main ▸ this` from claiming the
// conversation asked for this directly.
func TestANarrowTrailNeverHidesThatThereIsMoreChain(t *testing.T) {
	a := crumbApp(t)
	for width := roomHeadFloor; width <= 90; width++ {
		a.width = width
		a.touch()
		head := plain(strings.Join(a.roomHeadRows(width), "\n"))
		if !strings.Contains(head, roomCrumbRoot) {
			// Below the two ends the root itself is given up; that is the one rung
			// where the fold goes too, and the page's own name is all that is left.
			continue
		}
		if strings.Contains(head, "Write the tree") {
			continue // the chain is spelled out; there is nothing hidden to mark
		}
		if !strings.Contains(head, crumbFoldWord) {
			t.Fatalf("at %d columns the trail dropped its middle in silence:\n%q", width, head)
		}
	}
}

// ── THE GEOMETRY ────────────────────────────────────────────────────────────

// WHAT WAS RECORDED IS WHAT WAS DRAWN, at every width and with a title made of
// wide glyphs. The spans are measured in DISPLAY CELLS — a map built in bytes or
// runes would put every crumb after a Japanese title several columns from where
// a person sees it.
func TestEveryCrumbIsRecordedOnTheCellsItWasDrawnOn(t *testing.T) {
	a := crumbApp(t)
	a.tasks[3].title = "日本語のタイトル"
	a.tasks[4].title = "Cut the goldens 🚀"
	openRoomThroughRail(t, a, 4)
	for _, width := range []int{200, 160, 120, 100, 80, 60, 40, 24, roomHeadFloor} {
		a.width = width
		a.touch()
		// THE TRAIL ROW ALONE, because that is the row the crumbs are cut into: the
		// facts under it are a row of their own and answer for none of these spans
		// (room.go).
		line := plain(a.roomHeadRows(width)[0])
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("at %d columns the trail row is %d cells:\n%q", width, got, line)
		}
		for _, row := range a.roomHeadRows(width) {
			if got := ansi.StringWidth(plain(row)); got > width {
				t.Fatalf("at %d columns a header row is %d cells:\n%q", width, got, plain(row))
			}
		}
		for _, hit := range a.crumbs {
			if hit.span.to > ansi.StringWidth(line) {
				t.Fatalf("at %d columns a crumb was recorded past the end of the row: %+v\n%q",
					width, hit.span, line)
			}
			if got := ansi.Cut(line, hit.span.from, hit.span.to); got != hit.crumb.word {
				t.Fatalf("at %d columns the cells %d..%d hold %q and the map says %q\n%q",
					width, hit.span.from, hit.span.to, got, hit.crumb.word, line)
			}
		}
	}
}

// AND `Stop` IS ON A ROW OF ITS OWN, WHICH THE TRAIL NEVER REACHES. The two used
// to share a line and the mark had to outrank the crumbs on it; they are two
// rows now, and the separation is the whole point — ending work and walking up
// the family are opposite gestures, and the expensive one no longer sits one
// cell from a breadcrumb (room.go, chattabs.go).
func TestTheStopWordIsOnItsOwnRowAndTheTrailNeverReachesIt(t *testing.T) {
	a := crumbApp(t)
	for _, width := range []int{200, 120, 80, 40} {
		a.width = width
		a.touch()
		strings.Join(a.roomHeadRows(width), "\n")
		if !a.roomStop.pressable() {
			continue
		}
		// The trail answers for no column of the row `Stop` is on, because the
		// trail is not on that row at all.
		if _, ok := a.crumbAt(a.roomStop.from, a.roomFactsRow()); ok {
			t.Fatalf("at %d columns the trail answers on the facts row", width)
		}
		if !a.stopMarkAt(a.roomStop.from, a.roomFactsRow()) {
			t.Fatalf("at %d columns Stop was drawn and does not answer for its cells", width)
		}
		// And nothing on the trail's own row is the stop: a press up there is the
		// walk or the way out, never the end of a task.
		if a.stopMarkAt(a.roomStop.from, a.roomHeadRow()) {
			t.Fatalf("at %d columns Stop answers on the trail's row", width)
		}
		if !a.stopMarkPress(a.roomStop.from, a.roomFactsRow()) {
			t.Fatalf("at %d columns a press on Stop was not taken by it", width)
		}
		a.stop = nil
		if roomID(a) != 4 {
			t.Fatalf("at %d columns a press on Stop walked the trail to %d", width, roomID(a))
		}
	}
}

// Punctuation describes the path; it is not a destination of its own.
func TestAPressBetweenTheCrumbsDoesNotNavigate(t *testing.T) {
	a := crumbApp(t)
	before := a.room.id
	sep := crumbSpanFor(t, a, "Ship the port")
	clickHead(t, a, sep.to)
	if !a.roomOpen() || a.room.id != before {
		t.Fatal("breadcrumb punctuation navigated away from the page")
	}
}

// ── THE PAGE READ THROUGH SOMEBODY ELSE'S CONVERSATION ──────────────────────

// LAW 2, THE DANGEROUS HALF. A guest page draws the chain the record showed
// under THAT conversation, and not one crumb of it is a door: opening one would
// mean resolving another conversation's ids against this window's graph, which
// is the same-number crossover the whole guest lane exists to prevent.
func TestAGuestPagesChainIsDrawnAndOpensNothing(t *testing.T) {
	a, _ := guestLab(t)
	enterAway(t, a)
	if !a.roomIsGuest() {
		t.Fatal("the row opened no reading page")
	}
	a.room.guest.trail = []string{"Ship their port", "Write their tree"}
	a.width, a.height = 200, 40
	a.touch()
	head := plain(strings.Join(a.roomHeadRows(a.width), "\n"))
	for _, want := range []string{roomGuestOwnerWord, "Ship their port", "Write their tree", "Port the parser"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the guest header is missing %q:\n%q", want, head)
		}
	}
	if strings.Contains(head, roomCrumbRoot+roomCrumbSep) {
		t.Fatalf("the guest page hangs its chain off this conversation:\n%q", head)
	}
	for _, hit := range a.crumbs {
		if hit.crumb.door() {
			t.Fatalf("a crumb of somebody else's chain offers a door: %+v", hit.crumb)
		}
	}
	// A PRESS ON ONE STAYS WHERE IT IS AND TOUCHES NOTHING. It does not fall
	// through to the way out either: the crumb is drawn because it is true, and a
	// press on it that left the page would be the trail acting on a promise it
	// never made.
	up := crumbSpanFor(t, a, "Write their tree")
	clickHead(t, a, up.from+1)
	if !a.roomIsGuest() {
		t.Fatal("a press on a guest crumb left the page")
	}
	if node := a.tasks[7]; node == nil || node.state != session.TaskRunning || node.model != "mine/model" {
		t.Fatalf("a guest crumb reached this window's own task 7: %+v", node)
	}
}

// AND THE CHAIN IS READ OFF THE RECORD THE ROW CAME FROM, inside one
// conversation. Ids restart with every conversation, so a walk that matched a
// parent id across the machine would hang this page under a stranger's work.
func TestTheGuestChainIsWalkedInsideOneConversationOnly(t *testing.T) {
	a, _, _ := roomApp(t)
	mine := session.TaskIndexEntry{SessionID: "theirs", ID: "9", Parent: "3", Title: "the piece"}
	a.taskSheet.reading = tasksReading{items: []tasksItem{
		{entry: mine},
		{entry: session.TaskIndexEntry{SessionID: "theirs", ID: "3", Parent: "1", Title: "the middle"}},
		{entry: session.TaskIndexEntry{SessionID: "theirs", ID: "1", Title: "the root"}},
		// A task of ANOTHER conversation wearing a number this walk passes through.
		{entry: session.TaskIndexEntry{SessionID: "mine", ID: "1", Title: "not this one"}},
	}}
	trail := a.taskGuestTrail(tasksItem{entry: mine})
	if got, want := strings.Join(trail, " ▸ "), "the root ▸ the middle"; got != want {
		t.Fatalf("the guest chain is %q, want %q", got, want)
	}
}

// ── AN ADAPTIVE RUN KEEPS ITS OWN HEADER ────────────────────────────────────

// A RUN'S PAGE ANSWERS FOR ITS OWN LINE (roomorch.go), and the crumb map stays
// out of it: its trail is the run's chain of goals and its own card, none of
// which is a node this window can open. What must not happen is a stale map —
// crumbs recorded on the page before it, answering for cells a run is drawing
// something else on.
func TestARunsPageKeepsItsOwnHeaderAndRecordsNoCrumbs(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	// A node's page first, so there are crumbs on the map to go stale.
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	roomOn(a, 1, "Ship the port")
	strings.Join(a.roomHeadRows(a.width), "\n")
	if len(a.crumbs) == 0 {
		t.Fatal("the node's page recorded no crumbs to go stale")
	}
	a.openOrchRoom("r1", "answer the retry question")
	a.touch()
	head := plain(strings.Join(a.roomHeadRows(a.width), "\n"))
	if !strings.Contains(head, roomCrumbRoot) || !strings.Contains(head, "answer the retry question") {
		t.Fatalf("the run's page lost its own trail:\n%q", head)
	}
	if len(a.crumbs) != 0 {
		t.Fatalf("the run's page is answering with the last page's crumbs: %+v", a.crumbs)
	}
	if _, ok := a.crumbAt(headLabelAt+2, a.roomHeadRow()); ok {
		t.Fatal("a press on a run's trail resolved against a node's chain")
	}
}

// ── THE CONVERSATION ITSELF ─────────────────────────────────────────────────

// A CONVERSATION HAS NO TRAIL ROW. The row over it is the tab strip, which says
// the same name and the names of the other conversations beside it
// (chattabs.go); a second row reading `main` and nothing else would be that fact
// said twice, and the emptiness law is exactly that.
func TestTheConversationDrawsNoTrailRowOfItsOwn(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width, a.height = 120, 40
	a.touch()
	if got := strings.Join(a.roomHeadRows(a.width), "\n"); got != "" {
		t.Fatalf("the conversation drew a header of its own: %q", plain(got))
	}
	if len(a.crumbs) != 0 {
		t.Fatalf("the conversation recorded crumbs nothing drew: %+v", a.crumbs)
	}
	// The strip is the one pinned row, and the geometry is charged for exactly it.
	strip := plain(tabsRowOf(a))
	if !strings.Contains(strip, roomCrumbRoot) {
		t.Fatalf("the tab strip does not name the conversation: %q", strip)
	}
	if strings.Contains(strip, "─") {
		t.Fatalf("the tab strip drew a border across the top of the page: %q", strip)
	}
	// The strip and the seam under it are the two pinned rows out here, and both
	// are charged for (chattabs.go's [app.chatRuleHeight]).
	want := 1 + a.chatRuleHeight(a.width)
	if a.headHeight() != want || a.bodyTop() != want || a.roomHeadRow() != 1 {
		t.Fatalf("the strip is drawn but not budgeted: head=%d top=%d row=%d want=%d",
			a.headHeight(), a.bodyTop(), a.roomHeadRow(), want)
	}
	// AND IT STANDS DOWN WHERE THE ROOM'S HEADER WOULD: a terminal with no
	// breathing room has no row to spare for a fact that is true all day.
	a.height = airyFloor - 1
	a.touch()
	if a.headHeight() != 0 {
		t.Fatalf("a short terminal paid a row for the strip: head=%d", a.headHeight())
	}
	a.height, a.width = 40, roomHeadFloor-1
	a.touch()
	if a.headHeight() != 0 {
		t.Fatalf("a narrow terminal paid a row for the strip: head=%d", a.headHeight())
	}
}

// ── WHAT THE HEADER STILL OWES ──────────────────────────────────────────────

// THE STATE WORD NO LONGER COMPETES WITH THE CHAIN FOR CELLS, and this is the
// law that replaced the negotiation between them. The trail has a row and the
// facts have a row, so a narrow frame folds the chain's middle because the
// CHAIN does not fit — never because a figure wanted the space — and the state
// word survives on its own row either way. Navigation and telemetry degrade
// independently, which is what "navigation first when narrow" actually means.
func TestTheTrailAndTheFactsDegradeOnTheirOwnRows(t *testing.T) {
	a := crumbApp(t)
	a.tasks[4].state = session.TaskRunning
	a.tasks[4].title = strings.TrimSpace(strings.Repeat("long name ", 9))
	openRoomThroughRail(t, a, 3) // out of node 4's page, so the new name is read
	openRoomThroughRail(t, a, 4)
	for _, width := range []int{120, 60, 40} {
		a.width = width
		a.touch()
		rows := a.roomHeadRows(width)
		if len(rows) != roomHeadRowCount {
			t.Fatalf("at %d columns the header is %d rows", width, len(rows))
		}
		trail, facts := plain(rows[0]), plain(rows[1])
		// THE TRAIL ROW CARRIES NO TELEMETRY AT ANY WIDTH. That is the whole of the
		// separation: a path with a state word threaded into it is a path nobody
		// reads as a path.
		if strings.Contains(trail, stateWorking.String()) {
			t.Fatalf("at %d columns the trail row carries the state word:\n%q", width, trail)
		}
		// AND THE STATE WORD IS ON ITS OWN ROW WHATEVER THE TRAIL DID.
		if !strings.Contains(facts, stateWorking.String()) {
			t.Fatalf("at %d columns the facts row lost the state word:\n%q", width, facts)
		}
		// The page's own name survives on the trail row, cut where it must be.
		if !strings.Contains(trail, "long name") {
			t.Fatalf("at %d columns the trail lost the page's own name:\n%q", width, trail)
		}
	}
	// AND THE MIDDLE OF THE CHAIN FOLDS WHERE THE CHAIN ITSELF CANNOT FIT.
	a.width = 60
	a.touch()
	if trail := plain(a.roomHeadRows(60)[0]); !strings.Contains(trail, crumbFoldWord) {
		t.Fatalf("a sixty-column trail did not fold its middle:\n%q", trail)
	}
}
