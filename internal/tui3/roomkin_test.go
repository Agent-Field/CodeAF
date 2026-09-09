package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE KIN BLOCK UNDER THE FOCUS HEADER ────────────────────────────────────
//
// A room used to say nothing at all about where its node sat in the family the
// engine had modelled all along, so a person standing inside a piece of a
// recursive task could not tell it was a piece of anything. These are the whole
// of that claim: the two sentences, their words, the emptiness, and the rows
// being budgeted rather than merely drawn.

// roomOn puts a room over a node without a door, the way [taskwords_test] does:
// what is under test is the header, and the journal and the lane are not part of
// it.
func roomOn(a *app, id uint64, title string) {
	a.room = a.newRoom(id, title)
}

// kinRows is the kin block as a reader sees it.
func kinRows(a *app) []string {
	rows := a.roomKinRows(a.width)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = plain(row)
	}
	return out
}

// THE ROOM NAMES WHAT THE WORK HANDED OUT, AND WHO ASKED FOR IT IS ON THE TRAIL.
// Node 3 of the planted run is the interesting one: it has a parent above it and
// a piece of its own below it, which is exactly the case the rail's tree shape
// carried and this page had no shape to carry.
//
// THE `part of:` ROW IS GONE ON PURPOSE (roomcrumbs.go). The parent is a PLACE
// and is on the breadcrumb with the rest of the chain, where it can be pressed;
// saying it again down here would be the header spending a row of the transcript
// to repeat the row above it.
func TestARoomsHeaderNamesWhatItHandedOutAndLeavesTheParentToTheTrail(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	roomOn(a, 3, "Write the tree")

	rows := kinRows(a)
	if len(rows) != 1 {
		t.Fatalf("the kin block is %d rows:\n%q", len(rows), rows)
	}
	// The piece is queued behind nothing, so it wears the plain queued word.
	if got, want := rows[0], roomKinIndent+roomKinSpawnedWord+"Cut the goldens"+roomKinStateSep+roomQueuedWord; got != want {
		t.Fatalf("the spawned row is %q, want %q", got, want)
	}
	if block := strings.Join(rows, "\n"); strings.Contains(block, "part of") {
		t.Fatalf("the kin block still says the parent the trail is naming:\n%s", block)
	}
	if trail := a.roomTrail(); trail != "Untitled ▸ Ship the port ▸ Write the tree" {
		t.Fatalf("the trail does not carry the parent: %q", trail)
	}
}

// THE ROOT OF A FAMILY HAS NO PARENT LINE AND EVERY PIECE ON ONE. It is the same
// data read from the other end, and the emptiness law is the interesting half:
// a task nobody spawned draws no parent row at all rather than an empty one.
func TestARootsRoomListsEveryPieceAndClaimsNoParent(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	roomOn(a, 1, "Ship the port")

	rows := kinRows(a)
	if len(rows) != 1 {
		t.Fatalf("the kin block is %d rows:\n%q", len(rows), rows)
	}
	row := rows[0]
	if strings.Contains(row, "part of") {
		t.Fatalf("a task nobody spawned claims a parent: %q", row)
	}
	// AND ITS TRAIL CLAIMS NONE EITHER: a root's chain is the conversation and the
	// page, which is the trail this surface has always drawn.
	if trail := a.roomTrail(); trail != "Untitled ▸ Ship the port" {
		t.Fatalf("a root's trail invented a step: %q", trail)
	}
	for _, want := range []string{
		"Read the law" + roomKinStateSep + roomDoneWord,
		"Write the tree" + roomKinStateSep + stateWorking.String(),
		"Wire the seam" + roomKinStateSep + roomQueuedWord,
	} {
		if !strings.Contains(row, want) {
			t.Fatalf("the spawned row is missing %q:\n%s", want, row)
		}
	}
	// THE ORDER IS THE ROSTER'S, AND THE ROSTER'S ORDER CHANGED. It was arrival
	// order alone, which put every settled piece of a family in front of the ones
	// still going — a run that hands four errands out and finishes them one at a
	// time read `done, done, running, running`, with the only rows anybody was
	// watching at the bottom. Both surfaces now rank a family's members the way
	// the column already ranked whole families: what needs a person, then what is
	// running, then what is waiting, then what is over (task.go's [app.railKin]).
	//
	// So `Write the tree` (running) leads, `Wire the seam` (queued) follows, and
	// `Read the law` (done) is last — and arrival order still decides between two
	// pieces in the same state, which is what keeps the block still under somebody
	// reading it.
	live, queued, over := strings.Index(row, "Write the tree"),
		strings.Index(row, "Wire the seam"), strings.Index(row, "Read the law")
	if live > queued || queued > over {
		t.Fatalf("the family's pieces are not ranked by what they need (%d/%d/%d):\n%s",
			live, queued, over, row)
	}
}

// A CHILD HELD BEHIND ANOTHER SAYS "waiting" AND NOT THE WHOLE DEPENDENCY. The
// prerequisite's name belongs to the page a person would open to act on it; a
// header row carrying three tasks' business says least about the one it is for.
//
// AND THE WORD IS THE COLUMN'S. This row used to read `queued`, which promises
// that a scheduler will get to the child — while what is actually true is that
// it is behind the piece of work whose room this is, which is itself waiting on
// the person reading the page. The column called the same child `parked` the
// whole time, and two surfaces one keypress apart may not have two words for one
// fact.
func TestAHandedOutPieceWaitingOnAnotherSaysParkedInTheColumnsWord(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.tasks[4].dependsOn = []uint64{5}
	roomOn(a, 3, "Write the tree")

	// The handed-out line is the block's FIRST row now: the parent moved to the
	// trail and took its own row with it (roomcrumbs.go).
	row := kinRows(a)[0]
	if !strings.Contains(row, "Cut the goldens"+roomKinStateSep+railGroupWords[railParked]) {
		t.Fatalf("the held piece reads %q and should say %q, which is what the column calls it",
			row, railGroupWords[railParked])
	}
	if strings.Contains(row, roomQueuedWord) {
		t.Fatalf("the held piece still says %q, which promises a scheduler that is not coming:\n%s", roomQueuedWord, row)
	}
	// AND THE LINE'S OWN LEAD IS NOT THE MACHINERY'S VERB. `spawned:` is what one
	// process does to another; this work handed some of itself out.
	if strings.Contains(strings.ToLower(strings.Join(kinRows(a), "\n")), "spawned") {
		t.Fatalf("the kin block still says `spawned`:\n%q", kinRows(a))
	}
	if strings.Contains(row, "waits:") || strings.Contains(row, "Wire the seam") {
		t.Fatalf("the piece's own prerequisite is spelled out on the parent's header:\n%s", row)
	}
	// And the node's OWN prerequisites are still the accent line's business, in
	// the one place they have always been said.
	a.tasks[3].state, a.tasks[3].dependsOn = session.TaskQueued, []uint64{5}
	if got := a.roomStateWord(a.tasks[3]); got != "waits: Wire the seam" {
		t.Fatalf("the header's state word is %q", got)
	}
}

// WORK THAT NEEDS A PERSON SAYS SO IN THE SURFACE'S OWN WORDS, and never in the
// machinery's: this row is read by a person, so "unverified" and the checking
// apparatus behind it are not its vocabulary (tasktier.go).
func TestASpawnedPieceThatNeedsALookSaysSoInPlainWords(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.tasks[4].state = session.TaskUnverified
	roomOn(a, 3, "Write the tree")

	// The handed-out line is the block's FIRST row now: the parent moved to the
	// trail and took its own row with it (roomcrumbs.go).
	row := kinRows(a)[0]
	if !strings.Contains(row, "Cut the goldens"+roomKinStateSep+tierYourCallWord) {
		t.Fatalf("the piece does not ask for a look:\n%s", row)
	}
	for _, banned := range []string{"unverified", "auditor", "verdict", "refuted"} {
		if strings.Contains(strings.ToLower(row), banned) {
			t.Fatalf("the kin row speaks the machinery's %q:\n%s", banned, row)
		}
	}
}

// A TASK WITH NO FAMILY IS THE ONE PINNED ROW THIS SURFACE HAS ALWAYS DRAWN. The
// header does not grow an empty shelf to hold a fact nobody has.
func TestAFlatTasksRoomGrowsNoKinBlock(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	if rows := kinRows(a); len(rows) != 0 {
		t.Fatalf("a task with no family drew a kin block:\n%q", rows)
	}
	// The tab strip and room header include their own breathing room. None
	// of that space is a shelf for a family this task does not have.
	if a.headHeight() != a.tabsHeight(a.width)+a.roomHeadHeight(a.width) {
		t.Fatalf("the pinned region is %d rows over a flat task", a.headHeight())
	}
}

// THE ROWS ARE BUDGETED, NOT MERELY DRAWN. They are pinned above the body, so
// the number every geometric question resolves through has to know about them —
// rows the frame draws and the scrolling has not subtracted push the room's last
// row under the input box.
func TestTheKinRowsAreChargedToTheBodyRegion(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	roomOn(a, 3, "Write the tree")
	a.touch()

	kin := kinRows(a)
	// The tab strip, the room header including its padding, and the kin block
	// under them are all charged before the transcript begins.
	if a.headHeight() != a.roomHeadRow()+a.roomHeadHeight(a.width)+len(kin) {
		t.Fatalf("the pinned region is %d rows over a %d-row kin block", a.headHeight(), len(kin))
	}
	if a.bodyTop() != a.headHeight()+a.stripHeight() {
		t.Fatalf("the body starts at %d under a %d-row header", a.bodyTop(), a.headHeight())
	}
	// And they are where the geometry says they are: under the accent line, in
	// the order the block builds them.
	rows := strings.Split(frame(a), "\n")
	for i, want := range kin {
		at := a.roomHeadRow() + a.roomHeadHeight(a.width) + i
		if got := plain(rows[at]); got != want {
			t.Fatalf("frame row %d is %q, want %q", at, got, want)
		}
	}
}

// A TERMINAL TOO SHORT FOR A SECOND BLANK IS TOO SHORT FOR THESE ROWS. They cost
// the page its rows, so they are spent only where there is page to spend them
// from — the same ladder the breathing room stands on (view.go).
func TestTheKinRowsStandDownOnAShortTerminal(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	roomOn(a, 3, "Write the tree")

	a.height = airyFloor - 1
	if rows := kinRows(a); len(rows) != 0 {
		t.Fatalf("a short terminal still drew the kin block:\n%q", rows)
	}
	// The strip has stood down with the kin block at this height, and what is
	// left is the breadcrumb row; secondary task facts also give way.
	if a.headHeight() != 1 {
		t.Fatalf("the pinned region is %d rows on a short terminal", a.headHeight())
	}
	a.height = airyFloor
	if len(kinRows(a)) == 0 {
		t.Fatal("a terminal with the height to spare drew no kin block")
	}
}

// EVERY ROW FITS THE FRAME, AT EVERY WIDTH, AND THE BLOCK NEVER OUTGROWS ITS CAP.
// A header that wrapped with the family would take the transcript the person
// opened the room to read.
func TestTheKinBlockFitsAndIsCapped(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	roomOn(a, 1, "Ship the port")

	for _, width := range []int{roomHeadFloor, 30, 60, 200} {
		a.width = width
		rows := a.roomKinRows(width)
		if len(rows) > roomKinRowCap {
			t.Fatalf("at %d cells the kin block is %d rows", width, len(rows))
		}
		for _, row := range rows {
			if got := ansi.StringWidth(plain(row)); got > width {
				t.Fatalf("at %d cells a kin row is %d cells wide: %q", width, got, plain(row))
			}
		}
	}
	// Under the header's own floor there is no header, so there is nothing to
	// hang under: both halves of the pinned region stand on one number.
	a.width = roomHeadFloor - 1
	if rows := a.roomKinRows(a.width); len(rows) != 0 {
		t.Fatalf("the kin block outlived the header it hangs under:\n%q", rows)
	}
}
