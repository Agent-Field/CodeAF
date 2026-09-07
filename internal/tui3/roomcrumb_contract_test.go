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
	a.roomHead(a.width)
}

// crumbSpanFor is where one crumb was drawn on the last laid-out header.
func crumbSpanFor(t *testing.T, a *app, word string) hudSpan {
	t.Helper()
	for _, hit := range a.crumbs {
		if hit.crumb.word == word {
			return hit.span
		}
	}
	t.Fatalf("the header drew no crumb saying %q:\n%q\n%+v", word, plain(a.roomHead(a.width)), a.crumbs)
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
	if head := plain(a.roomHead(a.width)); !strings.Contains(head, chain) {
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
	if head := plain(a.roomHead(a.width)); strings.Contains(head, "3") && strings.Contains(head, "▸ 3") {
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
	a.roomHead(a.width)
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
	// FOLDING IS FROM THE OUTSIDE IN, so at sixty columns the `…` stands for the
	// root of the family alone and the immediate parent is still spelled — and
	// the fold opens the nearest thing IT hid, which is that outer step.
	a := crumbApp(t)
	a.width = 60
	a.touch()
	head := plain(a.roomHead(a.width))
	if !strings.Contains(head, crumbFoldWord) {
		t.Fatalf("a sixty-column header spelled the whole chain:\n%q", head)
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
	narrow := plain(b.roomHead(b.width))
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
		head := plain(a.roomHead(width))
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
		line := plain(a.roomHead(width))
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("at %d columns the header is %d cells:\n%q", width, got, line)
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

// AND NOTHING ON THE TRAIL REACHES THE ✕. The two are on one row, and the mark
// is the only control on this surface that ends work with a pointer: a crumb
// overlapping it would be a walk up the family that stopped a task instead.
func TestTheStopMarkOutranksTheTrailItSharesARowWith(t *testing.T) {
	a := crumbApp(t)
	for _, width := range []int{200, 120, 80, 40} {
		a.width = width
		a.touch()
		a.roomHead(width)
		if !a.roomStop.pressable() {
			continue
		}
		for _, hit := range a.crumbs {
			for x := hit.span.from; x < hit.span.to; x++ {
				if a.roomStop.holds(x) {
					t.Fatalf("at %d columns the crumb %q covers the ✕ at column %d",
						width, hit.crumb.word, x)
				}
			}
		}
		// And the press order says the same thing: the mark's own column is the
		// stop's, so a press there is never a walk up the family and never the way
		// out either.
		if _, ok := a.crumbAt(a.roomStop.from, a.roomHeadRow()); ok {
			t.Fatalf("at %d columns the trail answers for the ✕'s own column", width)
		}
		if !a.stopMarkAt(a.roomStop.from, a.roomHeadRow()) {
			t.Fatalf("at %d columns the ✕ was drawn and does not answer for its cells", width)
		}
		clickHead(t, a, a.roomStop.from)
		if roomID(a) != 4 {
			t.Fatalf("at %d columns a press on the ✕ walked the trail to %d", width, roomID(a))
		}
	}
}

// AND A PRESS THAT MISSES EVERY CRUMB IS STILL THE WAY OUT. The row is the
// pointer's exit and the crumbs are cut into it; the cells between them belong
// to the row.
func TestAPressBetweenTheCrumbsIsStillTheWayOut(t *testing.T) {
	a := crumbApp(t)
	sep := crumbSpanFor(t, a, "Ship the port")
	clickHead(t, a, sep.to) // the ` ▸ ` after the crumb, which opens nothing
	if a.roomOpen() {
		t.Fatal("a press on the trail's own punctuation did not leave the page")
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
	head := plain(a.roomHead(a.width))
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
	a.roomHead(a.width)
	if len(a.crumbs) == 0 {
		t.Fatal("the node's page recorded no crumbs to go stale")
	}
	a.openOrchRoom("r1", "answer the retry question")
	a.touch()
	head := plain(a.roomHead(a.width))
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
	if got := a.roomHead(a.width); got != "" {
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
	if a.headHeight() != 1 || a.bodyTop() != 1 || a.roomHeadRow() != 1 {
		t.Fatalf("the strip is drawn but not budgeted: head=%d top=%d row=%d",
			a.headHeight(), a.bodyTop(), a.roomHeadRow())
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

// THE STATE WORD OUTRANKS THE MIDDLE OF THE CHAIN. A person checking on work is
// asking what it is doing first and whose piece of what second, so the trail is
// offered the line less the leading fact and folds to fit it. What it never
// gives up is the page's own name: a cut name takes the whole row and no fact is
// drawn beside it (rowfit.go's law 1).
func TestTheHeaderKeepsTheStateWordWhileTheChainCanFold(t *testing.T) {
	a := crumbApp(t)
	a.tasks[4].state = session.TaskRunning
	a.tasks[4].title = strings.TrimSpace(strings.Repeat("long name ", 9))
	openRoomThroughRail(t, a, 3) // out of node 4's page, so the new name is read
	openRoomThroughRail(t, a, 4)
	a.width = 120
	a.touch()
	head := plain(a.roomHead(a.width))
	if !strings.Contains(head, crumbFoldWord) {
		t.Fatalf("the chain did not fold to make room for the state:\n%q", head)
	}
	if !strings.Contains(head, stateWorking.String()) {
		t.Fatalf("the header dropped the state word it folded the chain for:\n%q", head)
	}
	// AND WHERE EVEN THE FOLDED TRAIL CANNOT HOLD THE NAME, the name takes the
	// row and nothing is spelled beside it.
	a.width = 40
	a.touch()
	narrow := plain(a.roomHead(a.width))
	if strings.Contains(narrow, stateWorking.String()) && strings.Contains(narrow, glyphMore) {
		t.Fatalf("a cut name still drew a fact beside it:\n%q", narrow)
	}
}
