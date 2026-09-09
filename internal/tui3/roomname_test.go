package tui3

// ── THE NAME REACHES THE ROW WHOLE ──────────────────────────────────────────
//
// A task's identity used to be cut to three words the moment it arrived
// (taskident.go's [taskTitleOf]), which is [rowfit.go]'s first law broken at the
// earliest possible moment: a fitter cannot give back cells that were spent
// before it was asked. So a hundred-and-sixty-column room header named the work
// no better than a twenty-four-cell rail row did, and a family of six pieces
// that all begin with a verb and a plural noun came out as `Cut every list`,
// `Fold the settled`, `Move the tab` on every surface at every width.
//
// These pin the fix from both ends: the name arrives whole, and each row still
// cuts it to the space that row actually has.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The two names the demo home's own fixture carries, and they are the point:
// they share their first three words, so a surface that cuts to three words
// draws one string for two different pieces of work.
const (
	roomLongName    = "Cut every list on the task surface over to the shared row fitter so a name is never cut to nine cells"
	roomSiblingName = "Cut every list of files the record card keeps folded behind its own disclosure"
	roomSharedCut   = "Cut every list"
)

// roomNamed plants a node under a long name and opens its room on it, the way
// the rail's own door does ([app.openRoomFor] passes the node's title).
func roomNamed(t *testing.T, id uint64, name string) *app {
	t.Helper()
	a, _, _ := taskApp(t)
	a.taskUpdate(update(id, name, session.TaskUnverified, session.TaskNotice{
		CostUSD: 0.52, Elapsed: 12 * time.Minute,
	}))
	node := a.tasks[id]
	if node == nil {
		t.Fatalf("no node %d on the roster", id)
	}
	a.room = a.newRoom(id, node.title)
	return a
}

// A WIDE ROOM NAMES THE WORK BETTER THAN A NARROW RAIL ROW DOES, which is the
// whole claim: the identity arrives whole and the ROW decides what it can
// afford. The header at a hundred and sixty columns has the cells for the name
// and for three facts behind it; the column, twenty-four cells wide, does not,
// and it is the column's business to say so.
func TestAWideRoomNamesTheWorkBetterThanANarrowRail(t *testing.T) {
	a := roomNamed(t, 3, roomLongName)

	if got := a.tasks[3].title; got != roomLongName {
		t.Fatalf("the node's name reached the surface as\n\t%q\nand the engine's own title is\n\t%q\n— the surface is still cutting a name before it knows a width",
			got, roomLongName)
	}
	head := plain(roomHeadAll(a, 160))
	if !strings.Contains(head, roomLongName) {
		t.Fatalf("a 160-column room header reads\n\t%q\nand it should name the work whole:\n\t%q",
			head, roomLongName)
	}
	// AND THE FACTS ARE STILL BEHIND IT, in rank order, because a name that fits
	// leaves its remainder to the tail (rowfit.go's law 1).
	for _, want := range []string{tierYourCallWord, "$0.52"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the 160-column header lost %q behind the name it now draws whole:\n\t%q", want, head)
		}
	}
	// AND THE COLUMN IS STILL THE COLUMN. The cut did not vanish; it moved to the
	// row that actually has no cells, which is what makes the header worth more
	// than it was.
	row, ok := railRowFor(a, 40, roomSharedCut)
	if !ok {
		t.Fatalf("the roster has no row for the node:\n%s", strings.Join(railText(a, 40), "\n"))
	}
	if strings.Contains(row, roomLongName) {
		t.Fatalf("the column drew the whole name:\n\t%q\nit is meant to fit it to the space it has", row)
	}
	if !strings.Contains(row, glyphMore) {
		t.Fatalf("the column's row is\n\t%q\nand a name it could not hold should end in %q", row, glyphMore)
	}
	// AND THE HEADER SAYS STRICTLY MORE OF THE NAME THAN THE COLUMN DOES, which
	// is the sentence this test is named after.
	if ansi.StringWidth(head) <= ansi.StringWidth(row) {
		t.Fatalf("a 160-column header is %d cells and the column's row is %d:\n\t%q\n\t%q",
			ansi.StringWidth(head), ansi.StringWidth(row), head, row)
	}
}

// TWO PIECES OF WORK THAT SHARE THREE WORDS ARE TOLD APART IN THE ROOM. This is
// the defect said as a person meets it: `Cut every list` was the answer to "what
// is this page about" for two different nodes, and the room they opened to find
// out more told them exactly what the column already had.
func TestTwoTasksSharingTheirFirstWordsAreToldApartInTheirRooms(t *testing.T) {
	one := plain(roomNamed(t, 3, roomLongName).roomHeadWord(160))
	two := plain(roomNamed(t, 4, roomSiblingName).roomHeadWord(160))
	if one == two {
		t.Fatalf("two different pieces of work draw the same header:\n\t%q\nand they are called\n\t%q\n\t%q", one, roomLongName, roomSiblingName)
	}
	if !strings.Contains(one, roomLongName) || !strings.Contains(two, roomSiblingName) {
		t.Fatalf("the two headers are\n\t%q\n\t%q\nand each should carry its own whole name:\n\t%q\n\t%q",
			one, two, roomLongName, roomSiblingName)
	}
}

// A NAME THAT CANNOT FIT THE TRAIL ROW TAKES THE WHOLE TRAIL ROW, and the facts
// it used to compete with are on a row of their own now: law 1 is about the row
// the identity is drawn on, and no figure was ever going to buy back a cut name.
func TestANarrowRoomGivesTheTrailRowToTheNameAndKeepsTheFactsUnderIt(t *testing.T) {
	a := roomNamed(t, 3, roomLongName)
	trail := plain(a.roomHeadWord(60))
	if ansi.StringWidth(trail) > 60-roomHeadFurniture {
		t.Fatalf("the 60-column trail row is %d cells wide and has %d:\n\t%q",
			ansi.StringWidth(trail), 60-roomHeadFurniture, trail)
	}
	for _, never := range []string{"needs your look", "$0.52"} {
		if strings.Contains(trail, never) {
			t.Fatalf("the trail row drew the telemetry %q that belongs on the row under it:\n\t%q", never, trail)
		}
	}
	if !strings.HasPrefix(trail, a.chatCrumbWord()) {
		t.Fatalf("the 60-column trail row lost the trail it hangs off:\n\t%q", trail)
	}
	// AND THE FACTS ARE STILL THERE, on their own row, ranked.
	facts, _ := a.roomFactsWord(a.roomNode(), 60)
	if !strings.Contains(plain(facts), tierYourCallWord) {
		t.Fatalf("the 60-column facts row lost the state word:\n\t%q", plain(facts))
	}
}

// AND THE KIN BLOCK STAYS INSIDE ITS OWN ROW BUDGET. Those rows are dim
// telemetry charged to the transcript under them, capped at [roomKinRowCap], so
// a relative's name that arrives whole is cut where the width is known. Left
// uncut it spilled: at sixty columns the block wrapped to three rows and the
// last one ended on a bare `—`, with the state word cut off the bottom of it.
func TestTheKinBlockKeepsEveryRelativesStateInsideItsRowBudget(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.tasks[1].title, a.tasks[1].label = roomKinLongParent, roomKinLongParent
	a.tasks[4].title, a.tasks[4].label = roomLongName, roomLongName
	a.room = a.newRoom(3, a.tasks[3].title)

	for _, width := range []int{160, 120, 80, 60} {
		a.width, a.height = width, 40
		rows := kinRows(a)
		if len(rows) > roomKinRowCap {
			t.Fatalf("at %d columns the kin block is %d rows and the cap is %d:\n%q",
				width, len(rows), roomKinRowCap, rows)
		}
		// ONE LINE, AND IT IS THE HANDED-OUT ONE. The parent's line left this block
		// for the breadcrumb the moment the trail started carrying the whole chain
		// (roomcrumbs.go), so what is budgeted down here is the children.
		if len(rows) != 1 {
			t.Fatalf("at %d columns the kin block is\n%q\nand it should be the handed-out line alone", width, rows)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Fatalf("at %d columns a kin row is %d cells:\n\t%q", width, ansi.StringWidth(row), row)
			}
		}
		// THE STATE WORD IS THE LAST THING ON THE HANDED-OUT LINE, and it is the
		// half that was falling off the bottom of the block.
		if got := rows[0]; !strings.HasSuffix(got, roomQueuedWord) {
			t.Fatalf("at %d columns the handed-out line is\n\t%q\nand it should end in the child's state, %q", width, got, roomQueuedWord)
		}
		// AND THE PARENT IT USED TO DRAW IS ON THE TRAIL, CUT TO THE FRAME RATHER
		// THAN SPILLED DOWN IT.
		for _, row := range a.roomHeadRows(width) {
			if got := plain(row); ansi.StringWidth(got) > width {
				t.Fatalf("at %d columns a header row is %d cells:\n\t%q", width, ansi.StringWidth(got), got)
			}
		}
	}
}

// The parent name in the fixture above: long enough that an uncut one wraps a
// sixty-cell row on its own.
const roomKinLongParent = "Measure the frame budget in every package that draws a row"
