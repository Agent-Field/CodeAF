package tui3

// THE COLUMN OF WORK IS A COLUMN OF DOORS, AND THREE THINGS WERE EATING THE
// PRESSES.
//
// Reported as "the tasks on the right side — I am unable to open it, and it
// looks a bit not organized". Three separate defects in one gesture, all of them
// cells that answered a click and changed nothing a person could see:
//
//  1. THE SEAM CLAIMED TWO CELLS AT A WIDTH WHERE IT IS NOT A HANDLE. The resize
//     handle is the column's two leftmost cells at every width the column
//     stands at — but the third tier only exists from [railFloor] up
//     (task.go's [app.railColumns]), so between [railSlimFloor] and [railFloor]
//     the press was swallowed and nothing moved. Two dead columns of a
//     twenty-four-column list, down the edge a hand crossing from the
//     conversation reaches first.
//
//  2. THE STATE CELL FOLDED INSTEAD OF OPENING. The press asked whether the row
//     COULD disclose — a family root, or a landed row with a block tucked under
//     it — and the cell only DRAWS the ▾/▸ while the pointer is on that row. So
//     on every frame this surface was not holding a hover, the leading cell of a
//     root and of a finished row folded the list while the screen showed a
//     spinner or a tick. Opening any room drops the hover ([app.dropHover]) and
//     a pointer that has not moved sends no motion to put it back, so the click
//     straight after opening anything landed in exactly that gap.
//
//  3. THE COLUMN ANSWERED FOR ROWS IT DOES NOT DRAW. [app.railAt] had no
//     vertical bound outside the full-frame roster, so every press in the
//     frame's last thirty columns — the composer, the legend, the status line —
//     was taken by [app.railPress] and dropped.
//
// These hold all three ends, at every width tier and in every shape the column
// takes, and they press through [app.Update] rather than calling the hit-test:
// the ordering of the presses ahead of the roster's is part of what was being
// tested (app.go's mouse switch), and one of them goes the whole way through
// Bubble Tea's own SGR decoder.

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// railFamily is the shape this column is actually met in: a root with four
// children, one of them with a child of its own, mixed running, queued, waiting
// and finished, with names long enough that the narrow tiers have to cut them —
// and one flat node beside the family, which is the other row grammar.
//
// IT GIVES THE FRAME THE HEIGHT TO DRAW ALL OF IT, because a test that pressed
// a row the window had scrolled away would be testing the window. The one test
// that wants a short frame takes the height back afterwards.
func railFamily(a *app) {
	a.height = 40
	a.taskUpdate(update(1, "Ship the streaming parser rewrite", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "Read the parser law", session.TaskDone, session.TaskNotice{
		Elapsed: 42 * time.Second, Merge: mergeWordMerged,
	}))
	a.taskUpdate(update(3, "Write the tree walker", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(4, "Cut the goldens for the tree walker", session.TaskQueued, session.TaskNotice{}))
	a.taskUpdate(update(5, "Wire the seam onto the narrow tier", session.TaskQueued, session.TaskNotice{}))
	a.taskUpdate(update(6, "Port the key table", session.TaskUnverified,
		unverifiedNotice("finished, but the key table still drops the last key")))
	railKinship(a, 1, 2, 3, 5, 6)
	railKinship(a, 3, 4)
	// AND ONE FINISHED ROW WITH NO FAMILY AROUND IT. It is the second row that
	// could fold — a landed flat row tucks its own block away
	// ([app.railTucks]) — so it is the second row whose leading cell used to
	// swallow the press.
	a.taskUpdate(update(9, "Collect the fixture sources", session.TaskDone, session.TaskNotice{
		Elapsed: 8 * time.Second, Merge: mergeWordMerged,
	}))
	a.paints = 0
}

// railRowY is the screen row a node is drawn on, and it fails rather than
// returning a sentinel: a test that pressed row −1 would pass for the wrong
// reason.
func railRowY(t *testing.T, a *app, id uint64) int {
	t.Helper()
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		if node := a.railNodeAt(y); node != nil && node.id == id {
			return y
		}
	}
	t.Fatalf("node %d is not on the column:\n%s", id,
		strings.Join(railText(a, a.viewHeight()), "\n"))
	return -1
}

// railClick is one whole press: down and up, through the surface's own update,
// so everything the mouse switch asks ahead of the roster gets its turn first
// (app.go).
func railClick(t *testing.T, a *app, x, y int) {
	t.Helper()
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// ── DEFECT 2: THE STATE CELL IS NOT A CONTROL ───────────────────────────────

// THE DEFECT ITSELF, ON BOTH ROWS THAT COULD FOLD. With no pointer registered on
// the row — which is every frame after a room is opened, and every frame on a
// terminal that is not reporting motion — the leading cell draws the node's
// STATE, and pressing a state opens the work it is the state of.
func TestTheStateCellOpensTheTaskWhenNoDisclosureIsDrawnOnIt(t *testing.T) {
	a, _, _ := roomApp(t)
	railFamily(a)
	glyph := a.bodyWidth() + ansi.StringWidth(railSeam)

	// Nothing on the column is drawn as a disclosure, which is the precondition
	// the whole test rests on: the pointer is nowhere near it.
	if drawn := strings.Join(railText(a, a.viewHeight()), "\n"); strings.Contains(drawn, glyphOpen) ||
		strings.Contains(drawn, glyphShut) {
		t.Fatalf("the column drew a disclosure with no pointer on it:\n%s", drawn)
	}

	// A FAMILY ROOT. Its leading cell is the spinner, so the press is a press on
	// a running task and it opens that task.
	rootY := railRowY(t, a, 1)
	railClick(t, a, glyph, rootY)
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("the root's state cell did not open its room: open=%v id=%d",
			a.roomOpen(), roomID(a))
	}
	if _, ok := railRowFor(a, a.viewHeight(), "Read the parser law"); !ok {
		t.Fatalf("the press folded the family instead of opening it:\n%s",
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}

	// A FINISHED FLAT ROW. It has a block tucked under it, so it was the other
	// row whose leading cell folded rather than opening ([app.railTucks]).
	if e, ok := railEntryFor(a, 9); !ok || !a.railTucks(e) {
		t.Fatalf("the fixture's finished row has nothing tucked under it, so it "+
			"is not the row this test is about: found=%v", ok)
	}
	doneY := railRowY(t, a, 9)
	railClick(t, a, glyph, doneY)
	if !a.roomOpen() || a.room.id != 9 {
		t.Fatalf("the finished row's state cell did not open its room: open=%v id=%d",
			a.roomOpen(), roomID(a))
	}
}

// AND THE FOLD IS STILL THERE, on the cell that is drawn as one. The affordance
// did not move: it appeared under the pointer before this change and it appears
// under the pointer now, and now it is the ONLY thing the press reads.
func TestTheDisclosureUnderThePointerStillFoldsAndTheRestOfTheRowStillOpens(t *testing.T) {
	a, _, _ := roomApp(t)
	railFamily(a)
	glyph := a.bodyWidth() + ansi.StringWidth(railSeam)
	rootY := railRowY(t, a, 1)

	a.setHover(glyph, rootY)
	if !strings.Contains(mustRailRow(t, a, "Ship the streaming"), glyphOpen) {
		t.Fatalf("the pointer on the root revealed no disclosure:\n%q",
			mustRailRow(t, a, "Ship the streaming"))
	}
	railClick(t, a, glyph, rootY)
	if a.roomOpen() {
		t.Fatalf("the disclosure walked into the room behind it: id=%d", roomID(a))
	}
	if _, ok := railRowFor(a, a.viewHeight(), "Read the parser law"); ok {
		t.Fatalf("the disclosure did not fold the family:\n%s",
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}

	// ONE CELL WIDE AND NO WIDER. The cell after it is the row's air, and the row
	// is the node's door — a fold that ate the space beside it would be the same
	// defect one column over.
	a.setHover(glyph+1, rootY)
	railClick(t, a, glyph+1, rootY)
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("the cell beside the disclosure did not open the room: open=%v id=%d",
			a.roomOpen(), roomID(a))
	}
}

// ── DEFECT 1: THE SEAM IS A HANDLE ONLY WHERE IT CAN PULL ───────────────────

// THE TWO LEFTMOST CELLS BELONG TO THE HANDLE WHERE THERE IS A TIER TO PULL TO,
// AND TO THE ROW EVERYWHERE ELSE. This is the width table, and it is the whole
// of the first defect: at 100 and 119 columns the press used to vanish.
func TestTheRailsLeftEdgeOpensTheRowAtEveryWidthTheHandleCannotAct(t *testing.T) {
	for _, tc := range []struct {
		width  int
		handle bool
		cols   int
	}{
		{100, false, railSlimCols},
		{119, false, railSlimCols},
		{120, true, railCols},
		{160, true, railCols},
	} {
		a, _, _ := roomApp(t)
		a.width = tc.width
		railFamily(a)
		if got := a.railWidth(); got != tc.cols {
			t.Fatalf("at %d columns the rail is %d wide, want %d", tc.width, got, tc.cols)
		}
		if got := a.railCanWiden(); got != tc.handle {
			t.Fatalf("at %d columns the handle claims can-widen=%v, want %v",
				tc.width, got, tc.handle)
		}
		rootY := railRowY(t, a, 1)
		for _, x := range []int{a.bodyWidth(), a.bodyWidth() + 1} {
			a.closeRoom()
			wide := a.railWide
			railClick(t, a, x, rootY)
			switch {
			case tc.handle:
				// The handle acts, and it does not open anything behind it.
				if a.railWide == wide {
					t.Fatalf("at %d columns the seam cell %d did not take the tier",
						tc.width, x-a.bodyWidth())
				}
				if a.roomOpen() {
					t.Fatalf("at %d columns the seam opened a room as well: id=%d",
						tc.width, roomID(a))
				}
				a.railWiden(wide)
			default:
				// THERE IS NO HANDLE HERE, so the cell is the row's like every
				// other cell on it. This is the press that used to do nothing at
				// all.
				if a.railWide != wide {
					t.Fatalf("at %d columns the seam took a tier the frame does not lend",
						tc.width)
				}
				if !a.roomOpen() || a.room.id != 1 {
					t.Fatalf("at %d columns the rail's cell %d opened nothing: open=%v id=%d",
						tc.width, x-a.bodyWidth(), a.roomOpen(), roomID(a))
				}
			}
		}
	}
}

// AND THE FOOTER DOES NOT NAME A HANDLE THAT IS NOT THERE, which is the same
// question asked of the other hand: [app.railOffersResize] and [app.railSeamAt]
// read one answer now, so the column cannot advertise a chord it will refuse.
func TestTheNarrowColumnNeitherOffersNorAnswersTheHandle(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = railFloor - 1
	railFamily(a)
	a.railHold = true
	if a.railOffersResize() {
		t.Fatalf("a column with no wider tier offered one:\n%s",
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	if drawn := strings.Join(railText(a, a.viewHeight()), "\n"); strings.Contains(drawn, railWideHint) {
		t.Fatalf("the narrow column drew the widen hint:\n%s", drawn)
	}
	if a.railSeamAt(a.bodyWidth(), railRowY(t, a, 1)) {
		t.Fatal("the narrow column still claims its left edge as a handle")
	}
	// AND THE POINTER AGREES WITH THE PRESS, which is [app.railAt]'s own law: the
	// edge does not light as a handle either.
	a.setHover(a.bodyWidth(), railRowY(t, a, 1))
	if a.hoveringRailSeam() {
		t.Fatalf("the narrow column lights an edge it will not act on: %+v", a.hot)
	}
}

// ── DEFECT 3: THE COLUMN IS AS TALL AS THE BODY ─────────────────────────────

// A PRESS UNDER THE ROSTER IS SOMEBODY ELSE'S. The column draws exactly
// [app.viewHeight] rows and no more, so the rows under it — the blank, the box,
// the legend, the status line — are not the roster's to swallow.
func TestAPressBelowTheRosterIsNotTheRostersToSwallow(t *testing.T) {
	a, _, _ := taskApp(t)
	railFamily(a)
	x := a.bodyWidth() + 4
	bottom := a.bodyTop() + a.viewHeight()
	if bottom >= a.height {
		t.Fatalf("the frame has no chrome under the body to test: bottom=%d height=%d",
			bottom, a.height)
	}
	for y := bottom; y < a.height; y++ {
		if a.railAt(x, y) {
			t.Fatalf("row %d is under the roster's last row (%d) and the column claims it",
				y, bottom-1)
		}
		if _, took := a.railPress(x, y); took {
			t.Fatalf("the roster swallowed the press on row %d", y)
		}
		a.setHover(x, y)
		if a.hoveringRailArea() {
			t.Fatalf("row %d lights as the roster: %+v", y, a.hot)
		}
	}
	// AND THE LAST DRAWN ROW IS STILL THE ROSTER'S, so the bound is where the
	// column ends and not one row short of it.
	if !a.railAt(x, bottom-1) {
		t.Fatalf("the roster's own last row (%d) is not the roster's", bottom-1)
	}

	// THE VISIBLE CONSEQUENCE. A pointer parked on the status line used to resolve
	// to [hoverRailArea], which is one of the three things that make the footer
	// offer the wide tier ([app.railOffersResize]) — a column advertising a chord
	// because somebody's pointer was resting under it.
	a.setHover(a.bodyWidth()+4, a.height-1)
	if a.hoveringRailArea() {
		t.Fatalf("a pointer on the status line reads as the roster: %+v", a.hot)
	}
}

// ── THE COLUMN AS A MAP: SWITCHING, SCROLLING, AND THE SHAPES ───────────────

// EVERY ROW IS A DOOR AND THE DOOR IS THE ROW UNDER THE POINTER. Root, child,
// grandchild, a row waiting on a person, a finished row, and a flat row — pressed
// on the state cell, on the title, and out in the row's trailing air.
func TestEveryVisibleRowOfTheColumnOpensTheTaskItDraws(t *testing.T) {
	a, _, _ := roomApp(t)
	railFamily(a)
	seam := a.bodyWidth() + ansi.StringWidth(railSeam)
	for _, id := range []uint64{1, 3, 4, 5, 6, 2, 9} {
		for _, at := range []int{0, 4, a.railRoom() - 1} {
			a.closeRoom()
			y := railRowY(t, a, id)
			railClick(t, a, seam+at, y)
			if !a.roomOpen() || a.room.id != id {
				t.Fatalf("column %d of node %d's row opened %d, want %d:\n%s",
					at, id, roomID(a), id, strings.Join(railText(a, a.viewHeight()), "\n"))
			}
		}
	}
}

// SWITCHING IS ONE PRESS, and the roster says where you are while you do it. The
// room a person is standing in does not make its neighbours inert.
func TestPressingAnotherRowWhileARoomIsOpenSwitchesToIt(t *testing.T) {
	a, _, _ := roomApp(t)
	railFamily(a)
	seam := a.bodyWidth() + ansi.StringWidth(railSeam)

	railClick(t, a, seam+4, railRowY(t, a, 1))
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("the first press opened %d, want 1", roomID(a))
	}
	// THE ROW WEARS THE BAND WHILE YOU ARE BEHIND IT (task.go's [app.railRows]).
	if !a.roomStandingOn(a.tasks[1]) {
		t.Fatal("the roster does not mark the row the reader walked through")
	}
	for _, next := range []uint64{3, 9, 6, 1} {
		// THE ROW IS FOUND WITH THE PAGE STILL OPEN, which is the case this is
		// about: the room pins a header above the body, so the roster's rows are
		// not where they were before anybody walked in (view.go's
		// [app.topHeight]).
		was := roomID(a)
		y := railRowY(t, a, next)
		railClick(t, a, seam+4, y)
		if !a.roomOpen() || a.room.id != next {
			t.Fatalf("pressing node %d's row while node %d's page was open landed on %d",
				next, was, roomID(a))
		}
		if !a.roomStandingOn(a.tasks[next]) {
			t.Fatalf("the band did not follow the reader onto node %d", next)
		}
	}
}

// A SCROLLED COLUMN OPENS WHAT IS UNDER THE POINTER AND NOT WHAT USED TO BE. The
// window's offset is [app.railTop], and the press resolves through the same
// [app.railView] the frame drew — so a column scrolled off its top still hands a
// press the row a person is looking at.
func TestAScrolledColumnOpensTheRowThatIsDrawnThere(t *testing.T) {
	a, _, _ := roomApp(t)
	railFamily(a)
	a.height = 12 // a window too short for the whole roster
	seam := a.bodyWidth() + ansi.StringWidth(railSeam)
	top := a.bodyTop()
	if top < 0 || a.viewHeight() <= 0 {
		t.Fatalf("the short frame has no body region: top=%d height=%d", top, a.viewHeight())
	}
	for _, offset := range []int{0, 1, 2, 40} {
		a.closeRoom()
		a.railTop = offset
		// The row the frame draws at this y, asked of the layout rather than
		// guessed: this is the fact the press has to agree with.
		want := a.railNodeAt(top)
		if want == nil {
			continue
		}
		railClick(t, a, seam+3, top)
		if !a.roomOpen() || a.room.id != want.id {
			t.Fatalf("at offset %d the column's first row draws node %d and opened %d",
				offset, want.id, roomID(a))
		}
	}
}

// THE FULL-FRAME ROSTER IS THE SAME COLUMN AND THE SAME DOORS. Under
// [railSlimFloor] there is no column to stand beside the conversation, so the
// roster opens OVER it — with no seam to claim anything, since there is nothing
// on the other side of it to widen.
func TestTheFullFrameRosterOpensRowsFromItsFirstCell(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width = railSlimFloor - 20
	railFamily(a)
	drive(t, a, ctrlT())
	if !a.railFull() {
		t.Fatalf("the narrow frame did not raise the roster over the body: showing=%v",
			a.railShowing())
	}
	if a.railCanWiden() {
		t.Fatal("the full-frame roster claims a column it does not have")
	}
	for _, x := range []int{0, 1, ansi.StringWidth(railSeam), ansi.StringWidth(railSeam) + 5} {
		a.closeRoom()
		// Walking into a page hands the keyboard back, and the full-frame roster
		// IS that keyboard ([app.railFull] is the same state as [app.railHold]),
		// so it is asked for again before each press.
		if !a.railFull() {
			drive(t, a, ctrlT())
		}
		y := railRowY(t, a, 3)
		railClick(t, a, x, y)
		if !a.roomOpen() || a.room.id != 3 {
			t.Fatalf("cell %d of the full-frame roster opened %d, want 3", x, roomID(a))
		}
	}
}

// AND A STOWED COLUMN HAS NO ROWS TO PRESS, only its edge — which brings it back.
// This is the third shape, and it is here so the press's first question keeps
// answering before [app.railAt] does.
func TestTheStowedColumnsEdgeBringsItBackAndOpensNothing(t *testing.T) {
	a, _, _ := roomApp(t)
	railFamily(a)
	// Put it away through the door itself rather than through ctrl+g, which is
	// also what backgrounds a running command (steer.go): this test is about the
	// press on the edge, not about which key won the chord.
	a.railStow(true)
	if !a.railStowed() {
		t.Fatalf("the column did not go away: showing=%v away=%v",
			a.railShowing(), a.railAway)
	}
	railClick(t, a, a.width-1, a.bodyTop()+1)
	if a.railStowed() {
		t.Fatal("a press on the closed column's edge did not bring it back")
	}
	if a.roomOpen() {
		t.Fatalf("the edge opened a room as well: id=%d", roomID(a))
	}
}

// ── THE ROW ITSELF ──────────────────────────────────────────────────────────

// ONE GLYPH LEADS EVERY ROW OF THIS COLUMN AND IT IS THE STATE. The identity ◆
// is furniture that says "this row is a task" (taskident.go) — which is nothing
// this column has to tell apart, since it holds nothing but tasks — and the two
// cells buy name on the narrowest surface here.
func TestTheColumnLeadsWithStateAndSpendsNoCellOnIdentity(t *testing.T) {
	a, _, _ := taskApp(t)
	railFamily(a)
	rows := railText(a, a.viewHeight())
	mark := plain(a.taskMark(identFor(1)))
	for i, row := range rows {
		if strings.Contains(row, mark+" ") {
			t.Fatalf("row %d spends a cell on the identity mark:\n%q", i, row)
		}
	}
	// A FLAT ROW AND A FAMILY ROOT NOW LEAD THE SAME WAY, and in the same number
	// of cells: the seam, one state glyph, and the air after it. That is what
	// lets the column be read downward as one column of states.
	flat, root := mustRailRow(t, a, "Collect the fixture"), mustRailRow(t, a, "Ship the streaming")
	want := ansi.StringWidth(railSeam) + 2
	for _, probe := range []struct{ row, title string }{
		{flat, "Collect the fixture"}, {root, "Ship the streaming"},
	} {
		if got := leadCells(t, probe.row, probe.title); got != want {
			t.Fatalf("the row leads in %d cells, want %d:\n%q", got, want, probe.row)
		}
	}

	// AND THE UNDER-BLOCK IS SQUARE UNDER THE TITLE IT BELONGS TO. It is indented
	// [app.railUnderCols] — two cells for a flat row — which is exactly the lead
	// now that the ◆ is gone. It was two cells to the left of the title while the
	// marker was there.
	a.taskUpdate(update(11, "Fix the loader nil-map", session.TaskRunning, session.TaskNotice{}))
	a.tasks[11].tool, a.tasks[11].toolBegan = "bash go test ./...", a.now().Add(-30*time.Second)
	rows = railText(a, a.viewHeight())
	at := -1
	for i, row := range rows {
		if strings.Contains(row, "Fix the loader") {
			at = i
		}
	}
	if at < 0 || at+1 >= len(rows) {
		t.Fatalf("the running flat row has no under-row:\n%s", strings.Join(rows, "\n"))
	}
	head, under := rows[at], rows[at+1]
	if !strings.Contains(under, "go test") {
		t.Fatalf("the row under the title is not the call it is in:\n%q", under)
	}
	body := strings.TrimPrefix(under, railSeam)
	underLead := ansi.StringWidth(railSeam) + len(body) - len(strings.TrimLeft(body, " "))
	if got := leadCells(t, head, "Fix the loader"); got != underLead {
		t.Fatalf("the title starts at cell %d and its call at cell %d:\nhead  %q\nunder %q",
			got, underLead, head, under)
	}
}

// leadCells is how many cells a drawn row spends before its name — the seam and
// whatever [app.railLead] put in front of the title.
func leadCells(t *testing.T, row, title string) int {
	t.Helper()
	at := strings.Index(row, title)
	if at < 0 {
		t.Fatalf("the row does not carry %q: %q", title, row)
	}
	return ansi.StringWidth(row[:at])
}

// ── THE WHOLE WAY THROUGH THE TERMINAL ──────────────────────────────────────

// AND ONE PRESS AS A TERMINAL ACTUALLY SENDS IT: the SGR bytes, through Bubble
// Tea's decoder and its program loop, onto the leading cell of a family root.
// That cell is the one defect 2 was about, and nothing between the wire and
// [app.railPress] gets a chance to make it look right.
func TestRawTerminalBytesOnARootsStateCellOpenThatTask(t *testing.T) {
	a, _, _ := roomApp(t)
	railFamily(a)
	x, y := a.bodyWidth()+ansi.StringWidth(railSeam), railRowY(t, a, 1)

	input, keyboard := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	model := &localRoomTerminal{app: a, snapshots: make(chan string, 128)}
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(input),
		tea.WithOutput(io.Discard), tea.WithWindowSize(a.width, a.height), tea.WithoutSignalHandler())
	finished := make(chan error, 1)
	go func() { _, err := program.Run(); finished <- err }()
	t.Cleanup(func() {
		cancel()
		_ = keyboard.Close()
		_ = input.Close()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("terminal program did not stop")
		}
	})
	// The wire is one-based in both axes, and a press is a down and an up.
	if _, err := io.WriteString(keyboard,
		fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1)); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case state := <-model.snapshots:
			if strings.HasPrefix(state, "TASK") {
				return
			}
		case err := <-finished:
			t.Fatalf("the terminal exited before the press landed: %v", err)
		case <-deadline.C:
			t.Fatal("a press on the root's leading cell never opened a page")
		}
	}
}

// ── small readers ───────────────────────────────────────────────────────────

// railEntryFor is the roster's entry for one node, which is what the questions
// about a row's shape ([app.railTucks]) are asked of.
func railEntryFor(a *app, id uint64) (railEntry, bool) {
	for _, e := range a.railEntries() {
		if e.node != nil && e.node.id == id {
			return e, true
		}
	}
	return railEntry{}, false
}
