package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE ROSTER'S GROUPS ─────────────────────────────────────────────────────
//
// The column files every task under what it is doing (task.go's
// [app.railEntries]): the running work always open, and the queued, waiting
// and finished work folded to one heading until the person opens it, and then
// remembered open for the session. Every task is one line; what a row used to
// say under it is the hint line's. These are the whole of that claim: the
// shape, the order, the fold and its memory, the two hands that reach it.

// railKinship hands a node to a parent, in the alphabet the seam speaks
// (taskstrip.go's [taskNode.ParentID]).
func railKinship(a *app, parent uint64, kids ...uint64) {
	for _, id := range kids {
		a.tasks[id].parent = itoa(int(parent))
	}
}

// railRun plants one adaptive run: a root the person started, and the tree its
// planner spawned under it. The column does not draw the tree; it draws what
// each node is doing, newest first in each group:
//
//	1 Ship the port        running
//	├─ 2 Read the law      done
//	├─ 3 Write the tree    running
//	│  └─ 4 Cut goldens    queued
//	└─ 5 Wire the seam     queued
func railRun(a *app) {
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "Read the law", session.TaskDone, session.TaskNotice{}))
	a.taskUpdate(update(3, "Write the tree", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(4, "Cut the goldens", session.TaskQueued, session.TaskNotice{}))
	a.taskUpdate(update(5, "Wire the seam", session.TaskQueued, session.TaskNotice{}))
	railKinship(a, 1, 2, 3, 5)
	railKinship(a, 3, 4)
	// The spinner is on the frame clock (tokens.Spinner), so a golden has to say
	// which frame it was taken on.
	a.paints = 0
}

// railText is the roster as a reader sees it, row by row.
func railText(a *app, height int) []string {
	rows := a.railRows(height)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = plain(row)
	}
	return out
}

// mustRailRow is [railRowFor] over the whole visible column, for the tests that
// would be lying if the row were missing.
func mustRailRow(t *testing.T, a *app, title string) string {
	t.Helper()
	row, ok := railRowFor(a, a.viewHeight(), title)
	if !ok {
		t.Fatalf("the column has no row for %q:\n%s", title,
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	return row
}

// railRowFor is the drawn row a node's title is on, and whether there is one.
func railRowFor(a *app, height int, title string) (string, bool) {
	for _, row := range railText(a, height) {
		if strings.Contains(row, title) {
			return row, true
		}
	}
	return "", false
}

// railFocusOn walks the roster's cursor onto a node, however far down it is.
func railFocusOn(t *testing.T, a *app, id uint64) {
	t.Helper()
	railOpenAll(a)
	for i := 0; i < 40 && a.railWhere.id != id; i++ {
		a.railMove(1)
	}
	if a.railWhere.id != id {
		t.Fatalf("the cursor never reached node %d", id)
	}
}

// railOpenAll opens every group that folds, so each row the tests press is
// drawn.
func railOpenAll(a *app) {
	for g := range a.side.open {
		a.side.open[g] = true
	}
}

// railHeadY is the screen row group g's heading is drawn on, failing the test
// when the column draws none.
func railHeadY(t *testing.T, a *app, g railGroup) int {
	t.Helper()
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		if e, ok := a.railEntryAt(y); ok && e.head && e.group == g {
			return y
		}
	}
	t.Fatalf("group %s has no heading on the column:\n%s", railHeadWords[g],
		strings.Join(railText(a, a.viewHeight()), "\n"))
	return -1
}

// ONE LINE PER TASK, UNDER THE WORD FOR WHAT IT IS DOING. The header, then
// the running work open, newest first, then the groups that fold, each one
// line with its count and its ▸. No blank row, no connectors, no id.
func TestTheRosterIsOneLinePerTaskUnderItsGroups(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	spin := a.railTreeGlyph(a.tasks[3])
	rows := railText(a, 12)
	idle := a.railGroupOf(a.tasks[4])
	want := []string{
		"│ Running 2",
		"│   " + plain(spin) + " Write the tree",
		"│   " + plain(spin) + " Ship the port",
		"│ " + railHeadWords[idle] + " 2 " + glyphShut,
		"│ Done 1 " + glyphShut,
	}
	if len(rows) < len(want)+1 {
		t.Fatalf("the roster drew %d rows:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[0], "│ "+sideTasksWord+" 5") || !strings.HasSuffix(strings.TrimRight(rows[0], " "), sideHideKey) {
		t.Fatalf("the header is %q", rows[0])
	}
	for i, w := range want {
		// A running row ends in its age, in the muted ink at the right.
		got := strings.TrimSuffix(strings.TrimRight(rows[i+1], " "), " 0s")
		got = strings.TrimRight(got, " ")
		if got != w {
			t.Fatalf("row %d is\n\t%q\nwant\n\t%q\nwhole column:\n%s", i+1, got, w, strings.Join(rows, "\n"))
		}
	}
	// EVERY ROW IS INSIDE THE COLUMN, and none of them is a tree.
	for i, row := range rows {
		if w := ansi.StringWidth(row); w > a.railWidth() {
			t.Fatalf("row %d is %d cells wide, want at most %d:\n%q", i, w, a.railWidth(), row)
		}
		for _, stem := range []string{"├─", "└─", "#1", "#3"} {
			if strings.Contains(row, stem) {
				t.Fatalf("row %d still draws the forest's %q:\n%q", i, stem, row)
			}
		}
	}
}

// THE FOLD IS THE PERSON'S AND IT IS REMEMBERED. A press on a folded heading
// opens it, the next update does not close it, a new task does not close it,
// and a second press folds it again. The running work has no fold at all.
func TestAFoldIsRememberedForTheSession(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	a.height = 30
	if _, ok := railRowFor(a, a.viewHeight(), "Read the law"); ok {
		t.Fatalf("the finished work came up open:\n%s", strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	x := a.bodyWidth() + ansi.StringWidth(railSeam) + 2
	railClick(t, a, x, railHeadY(t, a, railDone))
	if a.roomOpen() {
		t.Fatalf("a press on a heading opened a room: id=%d", roomID(a))
	}
	if _, ok := railRowFor(a, a.viewHeight(), "Read the law"); !ok {
		t.Fatalf("a press on the heading did not open the group:\n%s", strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	if !strings.Contains(mustRailRow(t, a, "Done 1"), glyphOpen) {
		t.Fatalf("the open group does not wear ▾: %q", mustRailRow(t, a, "Done 1"))
	}
	a.taskUpdate(update(2, "Read the law", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	a.taskUpdate(update(6, "Port the loader", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	for _, title := range []string{"Read the law", "Port the loader"} {
		if _, ok := railRowFor(a, a.viewHeight(), title); !ok {
			t.Fatalf("the group the person opened closed itself on an update:\n%s", strings.Join(railText(a, a.viewHeight()), "\n"))
		}
	}
	railClick(t, a, x, railHeadY(t, a, railDone))
	if _, ok := railRowFor(a, a.viewHeight(), "Read the law"); ok {
		t.Fatalf("a second press did not fold the group:\n%s", strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	// THE RUNNING WORK DOES NOT FOLD, and its heading says so by wearing no
	// mark and taking no press.
	run := mustRailRow(t, a, "Running ")
	if strings.Contains(run, glyphOpen) || strings.Contains(run, glyphShut) {
		t.Fatalf("the running heading wears a fold it does not have: %q", run)
	}
	railClick(t, a, x, railHeadY(t, a, railRunning))
	if _, ok := railRowFor(a, a.viewHeight(), "Ship the port"); !ok {
		t.Fatalf("a press folded the running work:\n%s", strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	// AND THE KEYBOARD REACHES THE SAME FOLD: enter on a heading.
	drive(t, a, altT())
	a.railWhere = railSpot{group: int(railDone) + 1}
	drive(t, a, key("enter"))
	if _, ok := railRowFor(a, a.viewHeight(), "Read the law"); !ok {
		t.Fatalf("enter on the heading did not open the group:\n%s", strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	if a.roomOpen() {
		t.Fatal("enter on a heading opened a room")
	}
}

// THE HEADING'S HOVER GROUND IS ITS PRESS. The pointer on a folded heading
// lights it and names what a press does; the pointer on the running heading
// lights nothing, because a press there does nothing.
func TestAHeadingLightsOnlyWhereItFolds(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	x := a.bodyWidth() + ansi.StringWidth(railSeam) + 2
	a.setHover(x, railHeadY(t, a, railDone))
	if a.hot.kind != hoverRailGroup || railGroup(a.hot.index) != railDone {
		t.Fatalf("the pointer on the Done heading resolved to %+v", a.hot)
	}
	if got := a.sideHoverWords(); !strings.Contains(got, "Show what is done") {
		t.Fatalf("the hint over the folded heading reads %q", got)
	}
	a.setHover(x, railHeadY(t, a, railRunning))
	if a.hot.kind == hoverRailGroup {
		t.Fatalf("the running heading lit as a fold: %+v", a.hot)
	}
}

// A ROW IS ONE LINE WHATEVER THE WORK IS DOING. The call a running task is in
// used to hang under its row; it is the hint line's now, with the whole name.
func TestARunningRowIsOneLineAndItsCallIsOnTheHint(t *testing.T) {
	a, _, advance := taskApp(t)
	railRun(a)
	a.tasks[3].tool, a.tasks[3].toolBegan = "bash go test ./...", a.now().Add(-24*time.Second)
	advance(0)
	rows := railText(a, 14)
	for _, row := range rows {
		if strings.Contains(row, "go test") {
			t.Fatalf("the call is drawn on the column:\n%s", strings.Join(rows, "\n"))
		}
	}
	if hint := railHint(a, 3); !strings.Contains(hint, "Write the tree") || !strings.Contains(hint, "go test") {
		t.Fatalf("the hint over the running row reads %q", hint)
	}
}

// THE COLUMN WIDENS ON DEMAND. alt+w takes the wide tier while the column
// holds the keyboard, the column is charged against the conversation, the
// footer's hint is a button that does the same, and /new gives it back.
func TestTheWidenChordAndHintToggleTheWideTier(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	cols := sideColsFor(a.width)
	drive(t, a, altT())
	if !strings.Contains(strings.Join(railText(a, 14), "\n"), railWideHint) {
		t.Fatalf("the held column did not offer the wide tier:\n%s", strings.Join(railText(a, 14), "\n"))
	}
	drive(t, a, key(railWidenChord))
	if !a.railWide || a.railWidth() != cols+railWideGain {
		t.Fatalf("%s did not widen the column: wide=%v width=%d", railWidenChord, a.railWide, a.railWidth())
	}
	if a.bodyWidth() != a.width-cols-railWideGain {
		t.Fatalf("the wide column is not charged against the conversation: body=%d", a.bodyWidth())
	}
	drive(t, a, key(railWidenChord))
	if a.railWide || a.railWidth() != cols {
		t.Fatalf("%s did not give the columns back: wide=%v width=%d", railWidenChord, a.railWide, a.railWidth())
	}
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		if line, ok := a.railLineAt(y); ok && line.hint {
			drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + 4, Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{X: a.bodyWidth() + 4, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if !a.railWide {
		t.Fatalf("a press on the hint did not widen the column")
	}
	a.dropTasks()
	if a.railWide {
		t.Fatal("/new kept the wide column")
	}
}

// THE FULLSCREEN ROSTER IS THE SAME COLUMN. Under a hundred columns there is
// no column to lend, so alt+t draws it over the body: the same header, the
// same groups and the same folds, at the width the frame has.
func TestTheFullscreenRosterDrawsTheSameGroups(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 80
	a.touch()
	railRun(a)
	drive(t, a, altT())
	if !a.railFull() {
		t.Fatal("alt+t did not raise the roster over the body")
	}
	rows := railText(a, a.viewHeight())
	body := strings.Join(rows, "\n")
	for _, want := range []string{sideTasksWord + " 5", "Running 2", "Ship the port", "Done 1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the fullscreen roster is missing %q:\n%s", want, body)
		}
	}
	for i, row := range rows {
		if strings.HasPrefix(row, railSeam) {
			t.Fatalf("fullscreen row %d opens with the rail's seam:\n%q", i, row)
		}
		if w := ansi.StringWidth(row); w > a.width {
			t.Fatalf("row %d is %d cells wide on an %d-column frame:\n%q", i, w, a.width, row)
		}
	}
	a.railWhere = railSpot{group: int(railDone) + 1}
	drive(t, a, key("enter"))
	if !strings.Contains(strings.Join(railText(a, a.viewHeight()), "\n"), "Read the law") {
		t.Fatalf("enter on the heading did not open the group over the body:\n%s",
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}
}
