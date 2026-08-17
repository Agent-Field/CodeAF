package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// stripText is the strip as a reader sees it, laid out at the frame's width.
func stripText(a *app) string {
	width, _ := a.size()
	return plain(a.stripRow(width))
}

// THE STRIP IS THE DOOR THAT SURVIVES A NARROW FRAME. The rail is thirty columns
// and the first thing a hundred-column terminal gives up; the strip is one row
// and it is drawn at every width there is a running node, which is the whole
// point — under [railSlimFloor] it is the only thing on screen that leads into
// running work.
func TestTheTaskStripStandsAtEveryWidthWhileWorkRuns(t *testing.T) {
	a, _, _ := taskApp(t)
	if stripText(a) != "" {
		t.Fatalf("an empty session drew a strip:\n%q", stripText(a))
	}

	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(8, "Write the auth tests", session.TaskQueued, session.TaskNotice{})},
	)
	// WIDE: the rail is up as well, and the strip is still the frame's first row
	// — the two answer different questions, and a person watching one node does
	// not want to read a column to find it.
	for _, width := range []int{200, 80} {
		a.width = width
		a.touch()
		if !a.stripShowing() || a.stripHeight() != 1 {
			t.Fatalf("at %d columns nothing raised the strip over a running node", width)
		}
		text := stripText(a)
		for _, want := range []string{"Fix the nil-map", "Write the auth"} {
			if !strings.Contains(text, want) {
				t.Fatalf("at %d columns the strip is missing %q:\n%q", width, want, text)
			}
		}
		// THE CHIP IS A GLYPH AND A NAME: the state's glyph while it runs, the
		// node's own identity cell where the state has nothing to say (taskstrip.go).
		if !strings.Contains(text, plain(a.taskMark(identFor(8)))+" Write the auth") {
			t.Fatalf("at %d columns the queued chip lost its identity cell:\n%q", width, text)
		}
		if w := ansi.StringWidth(text); w > width {
			t.Fatalf("the strip is %d cells wide on a %d-column frame:\n%q", w, width, text)
		}
		// It is the frame's first row, and the body starts under it.
		if got := plain(strings.Split(frame(a), "\n")[0]); !strings.Contains(got, "Fix the nil-map") {
			t.Fatalf("at %d columns the strip is not the frame's first row:\n%q", width, got)
		}
		if a.bodyTop() != 1 {
			t.Fatalf("at %d columns the strip is drawn but not budgeted: top=%d", width, a.bodyTop())
		}
	}

	// AND IT GOES WHEN THE WORK DOES. The roster keeps the record; a permanent
	// row saying nothing is running is a row of chrome bought with a row of
	// conversation.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskDone,
		session.TaskNotice{Merge: mergeWordMerged})})
	if a.stripShowing() || stripText(a) != "" {
		t.Fatalf("the strip outlived the running work:\n%q", stripText(a))
	}
	if a.bodyTop() != 0 {
		t.Fatalf("the strip kept its row after it stopped drawing: top=%d", a.bodyTop())
	}
}

// WHAT THE ROW CANNOT HOLD IT COUNTS. The overflow mark is budgeted for BEFORE
// the chips are laid down — a row that filled itself and then had no room to say
// how many it dropped would be hiding exactly the work a person came looking for
// — and pressing it opens the roster.
func TestTheStripCountsWhatItCannotHoldAndOpensTheRoster(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 44
	for i := 1; i <= 5; i++ {
		a.taskUpdate(update(uint64(i), "node number "+itoa(i), session.TaskRunning, session.TaskNotice{}))
	}
	text := stripText(a)
	if !strings.Contains(text, "+") {
		t.Fatalf("five nodes in forty-four columns dropped none of them:\n%q", text)
	}
	if w := ansi.StringWidth(text); w > a.width {
		t.Fatalf("the strip is %d cells wide on a %d-column frame:\n%q", w, a.width, text)
	}
	if len(a.stripSpans) == 0 || !a.stripMore.pressable() {
		t.Fatalf("the strip recorded no columns to press: chips=%d more=%+v", len(a.stripSpans), a.stripMore)
	}
	if want := stripMoreWord(5 - len(a.stripSpans)); !strings.Contains(text, want) {
		t.Fatalf("the overflow mark does not say %q:\n%q", want, text)
	}

	// THE +N IS THE DOOR TO THE WHOLE ROSTER, which on this frame is the roster
	// over the body (task.go's [app.railFull]).
	drive(t, a, tea.MouseClickMsg{X: a.stripMore.from, Y: 0, Button: tea.MouseLeft})
	if !a.railHold || !a.railFull() {
		t.Fatalf("the overflow mark did not open the roster: hold=%v full=%v", a.railHold, a.railFull())
	}
}

// A CHIP IS A DOOR. Pressing one is the pointer's whole path into a running node
// on a frame with no rail on it.
func TestAStripChipOpensThatNodesRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width = 80 // no rail at all: the strip is the only way in
	a.touch()
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Write the auth tests", session.TaskRunning,
		session.TaskNotice{})})
	if a.railShowing() {
		t.Fatal("eighty columns lent the rail its column")
	}

	// The row is laid out first, because laying it out is what records where the
	// chips landed (taskstrip.go's [app.stripPress] says why).
	_ = stripText(a)
	at, found := 0, false
	for _, chip := range a.stripSpans {
		if chip.id == 9 {
			at, found = chip.span.from, true
		}
	}
	if !found {
		t.Fatalf("the second node has no chip on the strip:\n%q", stripText(a))
	}
	drive(t, a, tea.MouseClickMsg{X: at, Y: a.headHeight(), Button: tea.MouseLeft})
	if !a.roomOpen() || a.room.id != 9 {
		t.Fatalf("the chip did not open its node's room: open=%v", a.roomOpen())
	}
	// AND THE ROW SAYS WHICH DOOR YOU WENT THROUGH: the open room's chip wears
	// the accent, which is what makes this a tab row rather than a list. The row
	// itself has moved down one — the room pins its header above it.
	if !strings.Contains(a.stripRow(a.width), sgr256(hueAccent)) {
		t.Fatalf("the open room's chip is not picked out:\n%q", a.stripRow(a.width))
	}
	// A press in the gap after the chips is still the strip's: falling through to
	// the page under it would act on a row the pointer was not over.
	drive(t, a, tea.MouseClickMsg{X: a.width - 1, Y: a.headHeight(), Button: tea.MouseLeft})
	if !a.roomOpen() {
		t.Fatal("a press on the strip's empty end fell through and closed the room")
	}
}

// UNDER THE BREAKPOINT THE ROSTER OPENS OVER THE BODY. Same entries, same folds,
// same footer, same keys — laid out at the width the frame actually has instead
// of squeezed into thirty columns it does not.
func TestTheRosterOpensOverTheBodyOnANarrowFrame(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 80
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(8, "Write the auth tests", session.TaskQueued, session.TaskNotice{})},
	)
	if a.railShowing() || a.railFull() {
		t.Fatal("a narrow frame drew a roster nobody asked for")
	}

	drive(t, a, ctrlT())
	if !a.railFull() {
		t.Fatal("ctrl+t did not raise the roster on a frame with no column for it")
	}
	// The strip stands down under it: the overlay is the strip's destination, and
	// an index of the list drawn on top of the list is a row spent twice.
	if a.stripShowing() {
		t.Fatal("the strip drew over the roster it opens")
	}
	lines := strings.Split(plain(frame(a)), "\n")
	if len(lines) != a.height {
		t.Fatalf("the overlay changed the frame's height: %d rows, want %d", len(lines), a.height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > a.width {
			t.Fatalf("overlay row %d is %d cells wide on an %d-column frame:\n%q", i, w, a.width, line)
		}
	}
	body := strings.Join(lines[a.bodyTop():a.bodyTop()+a.viewHeight()], "\n")
	for _, want := range []string{railGroupWords[railRunning], "Fix the nil-map", "Write the auth"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the roster over the body is missing %q:\n%s", want, body)
		}
	}
	// THE SEAM IS A SEAM AND NOT A BORDER, so it is not drawn where there is
	// nothing on the other side of it (task.go's [app.railRows]).
	if strings.Contains(body, railSeam) {
		t.Fatalf("the fullscreen roster drew the rail's seam down its left edge:\n%s", body)
	}
	// The keys are the column's keys, and the pointer's half works the same: a
	// press on a node row is that node's door, wherever on the row it lands.
	if _, ok := a.railEntryAt(a.bodyTop()); !ok {
		t.Fatal("the overlay's first row answers to no entry")
	}

	drive(t, a, key("esc"))
	if a.railFull() || a.railHold {
		t.Fatal("esc did not put the roster away")
	}
	if !a.stripShowing() {
		t.Fatal("the strip did not come back when the roster went away")
	}
}

// ── THE ROSTER TREE ─────────────────────────────────────────────────────────

// stripKin hands a node to a parent, in the alphabet the seam speaks
// (taskstrip.go's [taskNode.ParentID]).
func stripKin(a *app, parent uint64, kids ...uint64) {
	for _, id := range kids {
		a.tasks[id].parent = itoa(int(parent))
	}
}

// stripRun plants one adaptive run: a root the person started, and the tree its
// planner spawned under it.
//
//	1 Ship the port        running
//	├── 2 Read the law     done
//	├── 3 Write the tree   running
//	│   └── 4 Cut goldens  queued
//	└── 5 Wire the seam    queued
func stripRun(a *app) {
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "Read the law", session.TaskDone, session.TaskNotice{}))
	a.taskUpdate(update(3, "Write the tree", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(4, "Cut the goldens", session.TaskQueued, session.TaskNotice{}))
	a.taskUpdate(update(5, "Wire the seam", session.TaskQueued, session.TaskNotice{}))
	stripKin(a, 1, 2, 3, 5)
	stripKin(a, 3, 4)
	// The spinner is on the frame clock (tokens.Spinner), so a golden has to say
	// which frame it was taken on.
	a.paints = 0
}

// stripLines is the strip as a reader sees it, row by row.
func stripLines(a *app) []string {
	width, _ := a.size()
	rows := a.stripRows(width)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = plain(row)
	}
	return out
}

// A FAMILY IS A TREE AND THE CONNECTORS ARE IN THE CHIP ROW. One row per task,
// children indented under the thing that spawned them, and the connectors drawn
// where the eye already is rather than in a gutter beside it.
//
// This is the WIDE golden. It is a byte comparison on purpose: a tree is
// alignment, and a test that only asked "does it contain ├──" would pass on a
// tree whose second level had drifted a cell.
func TestTheStripDrawsAFamilyAsATreeAtTheWideTier(t *testing.T) {
	a, _, _ := taskApp(t)
	stripRun(a)
	if layoutTier(a.width) != tierWide {
		t.Fatalf("%d columns is not the wide tier", a.width)
	}
	want := []string{
		" ⠋ Ship the port ",
		"├── ✓ Read the law ",
		"├── ⠋ Write the tree ",
		"│   └── ◌ Cut the goldens ",
		"└── ◌ Wire the seam ",
	}
	got := stripLines(a)
	if len(got) != len(want) {
		t.Fatalf("the tree is %d rows, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d is\n\t%q\nwant\n\t%q\nwhole tree:\n%s", i, got[i], want[i], strings.Join(got, "\n"))
		}
	}
	// AND THE STRIP IS AS TALL AS THE TREE IT DREW. A frame that budgeted one row
	// for five puts the conversation's last line under the input box (view.go).
	if a.stripHeight() != len(want) || a.bodyTop() != len(want) {
		t.Fatalf("the tree drew %d rows and the frame budgeted %d (bodyTop %d)",
			len(want), a.stripHeight(), a.bodyTop())
	}
	frameRows := strings.Split(plain(frame(a)), "\n")
	for i := range want {
		if frameRows[i] != want[i] {
			t.Fatalf("the frame's row %d is %q, want %q", i, frameRows[i], want[i])
		}
	}
	// THE GLYPH COLUMN READS DOWN, which is what the four-cell grid buys: every
	// chip on a level opens at the same column as its siblings.
	at := map[uint64]int{}
	for _, chip := range a.stripSpans {
		at[chip.id] = chip.span.from
	}
	if at[1] != 0 || at[2] != stripElbowCols || at[3] != stripElbowCols || at[5] != stripElbowCols {
		t.Fatalf("the first level is not one grid step in: %v", at)
	}
	if want := stripIndentCols + stripElbowCols; at[4] != want {
		t.Fatalf("the grandchild opens at column %d, want %d", at[4], want)
	}
}

// A TREE ROW IS A DOOR LIKE EVERY OTHER CHIP, and the row is now half the
// answer: a press has two coordinates where it used to have one.
func TestPressingATreeRowOpensThatNodesRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width = 80 // no rail: the strip is the only way in
	a.touch()
	stripRun(a)

	rows := stripLines(a)
	var chip stripSpan
	for _, span := range a.stripSpans {
		if span.id == 4 {
			chip = span
		}
	}
	// This session has a loose node of its own, so the live row leads and the
	// family hangs under it — which is exactly the geometry the press has to
	// resolve through.
	if chip.row >= len(rows) || !strings.Contains(rows[chip.row], "Cut the goldens") {
		t.Fatalf("the grandchild's chip says row %d:\n%s", chip.row, strings.Join(rows, "\n"))
	}
	drive(t, a, tea.MouseClickMsg{X: chip.span.from + 1, Y: a.headHeight() + chip.row, Button: tea.MouseLeft})
	if !a.roomOpen() || a.room.id != 4 {
		t.Fatalf("a press on a tree row did not open its node's room: open=%v", a.roomOpen())
	}
	// AND THE SAME COLUMN ON A DIFFERENT ROW IS A DIFFERENT DOOR. A strip that
	// resolved a press by its column alone would open the grandchild from
	// anywhere down the tree.
	drive(t, a, key("esc"))
	var sibling stripSpan
	for _, span := range a.stripSpans {
		if span.id == 5 {
			sibling = span
		}
	}
	drive(t, a, tea.MouseClickMsg{X: sibling.span.from + 1, Y: a.headHeight() + sibling.row, Button: tea.MouseLeft})
	if !a.roomOpen() || a.room.id != 5 {
		t.Fatalf("the press landed on room %v rather than the sibling it was over", a.room)
	}
	// The open room's chip wears the band, down a tree exactly as along the row.
	if !strings.Contains(a.stripRow(a.width), sgr256(hueAccent)) {
		t.Fatalf("the open room's chip is not picked out:\n%q", a.stripRow(a.width))
	}
}

// THE PHONE KEEPS EVERY STEM AND SPENDS ON THE NAMES. The indent is the only
// thing carrying the shape and it costs four cells; a name is expensive and it
// is one keystroke away in the room.
func TestTheStripDrawsTheTreeAtThePhoneTier(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 44
	stripRun(a)
	if layoutTier(a.width) != tierPhone {
		t.Fatalf("%d columns is not the phone tier", a.width)
	}
	// The phone's row budget is four, so the deepest leaf folds into its parent.
	want := []string{
		" ⠋ Ship the po… ",
		"├── ✓ Read the law ",
		"├── ⠋ Write the t…  ▸ +1",
		"└── ◌ Wire the se… ",
	}
	got := stripLines(a)
	if len(got) != len(want) {
		t.Fatalf("the phone tree is %d rows, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("phone row %d is\n\t%q\nwant\n\t%q\nwhole tree:\n%s", i, got[i], want[i], strings.Join(got, "\n"))
		}
	}
	for i, row := range got {
		if w := ansi.StringWidth(row); w > a.width {
			t.Fatalf("row %d is %d cells wide on a %d-column frame:\n%q", i, w, a.width, row)
		}
	}
	// THE FOLD MARK IS A DOOR TO THE ROSTER, which is where every row is.
	if len(a.stripFolds) != 1 {
		t.Fatalf("the fold recorded %d chips to press", len(a.stripFolds))
	}
	fold := a.stripFolds[0]
	drive(t, a, tea.MouseClickMsg{X: fold.span.from, Y: a.headHeight() + fold.row, Button: tea.MouseLeft})
	if !a.railHold || !a.railFull() {
		t.Fatalf("the fold mark did not open the roster: hold=%v full=%v", a.railHold, a.railFull())
	}
}

// THE FOLD NEVER HIDES WHAT IS MOVING. It gives up the deepest leaf it is
// allowed to, and a node that is running, that failed, or that is held at the
// fuel gate is never one of them — nor is any ancestor it hangs from, because a
// row hanging from nothing is not a tree.
func TestTheFoldGivesUpDepthAndNeverLiveWork(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 200, 24
	// A root with six children, and a seventh generation under one of them, so
	// the tree is well over the six-row budget however it is folded.
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	for i := uint64(2); i <= 9; i++ {
		a.taskUpdate(update(i, "node number "+itoa(int(i)), session.TaskDone, session.TaskNotice{}))
	}
	stripKin(a, 1, 2, 3, 4, 5, 6, 7, 8)
	stripKin(a, 8, 9)
	// Three of them are work nobody may hide: one running, one failed, one held
	// at the gate.
	a.taskUpdate(update(3, "node number 3", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(5, "node number 5", session.TaskFailed, session.TaskNotice{}))
	a.tasks[7].paused = true
	a.paints = 0

	rows := strings.Join(stripLines(a), "\n")
	if got := len(stripLines(a)); got > a.stripBudget() {
		t.Fatalf("the tree folded to %d rows on a budget of %d:\n%s", got, a.stripBudget(), rows)
	}
	for _, want := range []string{
		tokens.Spinner(0) + " node number 3", // running
		glyphBad + " node number 5",          // failed
		glyphPaused + " node number 7",       // held at the gate
	} {
		if !strings.Contains(rows, want) {
			t.Fatalf("the fold hid live work — %q is gone:\n%s", want, rows)
		}
	}
	// The deepest row went first: node 9 hung under node 8, and it is the finest
	// work on the tree.
	if strings.Contains(rows, "node number 9") {
		t.Fatalf("the fold took a shallow row before the deepest one:\n%s", rows)
	}
	// And what it took is counted where it was taken from.
	if !strings.Contains(rows, stripFoldMark+"+") {
		t.Fatalf("the fold took rows and counted none of them:\n%s", rows)
	}
	if len(a.stripFolds) == 0 {
		t.Fatalf("the fold chips recorded no columns to press")
	}
}

// A SESSION WITH NO FAMILIES IS THE ROW IT HAS ALWAYS BEEN. The tree costs a
// flat session nothing — not a row, not a cell, not a byte — which is the whole
// reason it could be added to a pinned surface at all.
func TestAFlatSessionIsStillOneRow(t *testing.T) {
	a, _, _ := taskApp(t)
	for i := uint64(1); i <= 3; i++ {
		a.taskUpdate(update(i, "node number "+itoa(int(i)), session.TaskRunning, session.TaskNotice{}))
	}
	rows := stripLines(a)
	if len(rows) != 1 || a.stripHeight() != 1 {
		t.Fatalf("a flat session drew %d rows:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	for _, chip := range a.stripSpans {
		if chip.row != 0 {
			t.Fatalf("a flat session's chip landed on row %d: %+v", chip.row, chip)
		}
	}
	// AND A NODE IN A FAMILY LEAVES THE FLAT ROW. One chip up top and the same
	// chip down the tree is one node claiming to be two.
	stripKin(a, 1, 2)
	rows = stripLines(a)
	if len(rows) != 3 {
		t.Fatalf("a family of two under one loose node is %d rows:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], "node number 3") || strings.Contains(rows[0], "node number 2") {
		t.Fatalf("the live row still carries a node the tree drew:\n%q", rows[0])
	}
	if !strings.Contains(rows[1], "node number 1") || !strings.Contains(rows[2], "node number 2") {
		t.Fatalf("the family is not under its root:\n%s", strings.Join(rows, "\n"))
	}
}

// THE SPAWN CARD IS A DOOR ONCE THERE IS SOMETHING BEHIND IT. A click used to
// open the brief — the card's own text, one fold down — and the question a person
// has when they press a card about work that has started is what it is DOING.
func TestASpawnCardClickOpensTheNodesRoom(t *testing.T) {
	a, agent, _ := roomApp(t)
	// A frame with no rail on it, so the card is laid out at the width the click
	// is resolved through and the strip is the only other door on screen.
	a.width = 80
	a.touch()
	agent.pending = []uint64{12}
	// Approved with the row's own default, so the card is settled — and running,
	// so there is a node behind it to open.
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 12, 0)}, key("enter"))
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(12, "Write the auth tests", session.TaskRunning,
		session.TaskNotice{})})

	clickHit(t, a, hitTask)
	if !a.roomOpen() || a.room.id != 12 {
		t.Fatalf("a click on the spawn card did not open its node's room: open=%v", a.roomOpen())
	}

	// AND THE BRIEF KEEPS A KEY. ctrl+o is what this surface already means "show
	// me the rest of this" by, and the selected card spends it on the fold the
	// click gave up (task.go's [app.openCard]).
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc did not leave the room")
	}
	drive(t, a, key("up"))
	if a.sel < 0 || a.entries[a.sel].kind != entryTask {
		t.Fatalf("the walk did not reach the spawn card (sel %d)", a.sel)
	}
	open := a.entries[a.sel].card.open
	drive(t, a, key("ctrl+o"))
	if a.entries[a.sel].card.open == open {
		t.Fatal("ctrl+o on the selected card did not open its brief")
	}
}

// ── THE FAMILY ARRIVES ON THE WIRE ──────────────────────────────────────────

// THE PARENT SEAM IS FILLED BY THE ENGINE, not by this surface. An adaptive run
// registers itself with the tasker — one row for the run, one per node, each
// carrying the run's id as its parent (internal/session's orchestrate.go) — and
// the tree above is drawn from exactly that. The goldens plant kinship by hand
// because they are about the DRAWING; this is the test that the drawing is
// reachable from a real session at all.
func TestARunsNodesReachTheTreeThroughTheirNotices(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "Read the law", session.TaskRunning, session.TaskNotice{Parent: 1}))
	a.taskUpdate(update(3, "Write the tree", session.TaskQueued, session.TaskNotice{Parent: 1}))

	if got := a.tasks[2].ParentID(); got != stripKey(a.tasks[1]) {
		t.Fatalf("the node's parent is %q, want the run's own key %q", got, stripKey(a.tasks[1]))
	}
	if got := a.tasks[1].ParentID(); got != "" {
		t.Fatalf("the run's own row hangs off %q, want a root", got)
	}
	rows := stripLines(a)
	want := []string{
		" ⠋ Ship the port ",
		"├── ⠋ Read the law ",
		"└── ◌ Write the tree ",
	}
	if len(rows) != len(want) {
		t.Fatalf("the run drew %d rows, want %d:\n%s", len(rows), len(want), strings.Join(rows, "\n"))
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d is\n\t%q\nwant\n\t%q\nwhole tree:\n%s", i, rows[i], want[i], strings.Join(rows, "\n"))
		}
	}
	// AND KINSHIP IS KEPT. A later update that says nothing about the parent has
	// not changed who spawned the work.
	a.taskUpdate(update(2, "Read the law", session.TaskDone, session.TaskNotice{}))
	if got := a.tasks[2].ParentID(); got != stripKey(a.tasks[1]) {
		t.Fatalf("a quiet update orphaned the node: parent %q", got)
	}
}

// A CHILD LANDS ON THE ROSTER AND NOT IN THE CONVERSATION. One card per decision
// a person made: the run is that decision, and the dozen nodes its planner cut
// are its internals — a card each would bury the conversation under the workings
// of one answer.
func TestAFamilysNodesLandOnTheRosterAndNotInTheConversation(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "Read the law", session.TaskRunning, session.TaskNotice{Parent: 1}))

	a.taskUpdate(update(2, "Read the law", session.TaskDone, session.TaskNotice{
		Parent: 1, Report: "the law is in section four",
	}))
	for _, e := range a.entries {
		if e.kind == entryDone {
			t.Fatalf("a run's node wrote a card into the conversation: %+v", e.done)
		}
	}
	// The run itself still does: it is the work somebody asked for.
	a.taskUpdate(update(1, "Ship the port", session.TaskDone, session.TaskNotice{Report: "ported"}))
	cards := 0
	for _, e := range a.entries {
		if e.kind == entryDone {
			cards++
		}
	}
	if cards != 1 {
		t.Fatalf("the run's own landing wrote %d cards, want one", cards)
	}
}
