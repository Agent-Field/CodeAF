package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE TASK PAGE ───────────────────────────────────────────────────────────
//
// The roster's column is THIS SESSION'S record and the project's record is on
// disk, and until this page the only door onto the second one was a completion
// somebody had to already be typing a message to reach. These are the whole of
// that claim: the page opens and closes, it draws the tree at the top and the
// flat record under it, the two fullscreen pages never disagree about which one
// owns the frame, and the column offers the door exactly when there is something
// behind it.

// ctrlDot is the page's own key (taskview.go's [taskSheetKey]).
func ctrlDot() tea.KeyPressMsg { return tea.KeyPressMsg{Code: '.', Mod: tea.ModCtrl} }

// pastTask is one row of the project's record as internal/session hands it over:
// work that landed, in a conversation that is not this one.
func pastTask(id, name, title string, ago time.Duration) session.TaskIndexEntry {
	return session.TaskIndexEntry{
		ID:        id,
		Name:      name,
		Label:     title,
		Title:     title,
		Status:    string(session.TaskDone),
		Outcome:   "it came home clean",
		EndedAt:   time.Now().Add(-ago),
		SessionID: "an-earlier-conversation",
	}
}

// taskSheetText is the page as a reader sees it.
func taskSheetText(a *app) string {
	width, height := a.size()
	lines, _, _, _ := a.taskSheetFrame(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = plain(line)
	}
	return strings.Join(out, "\n")
}

// THE KEY OPENS IT AND esc CLOSES IT, and while it is up it is the WHOLE frame:
// no conversation, no box, no status line. A page you read the conversation past
// is a page nobody finishes reading.
func TestTheTaskPageOpensOnItsKeyAndTakesTheWholeFrame(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)

	drive(t, a, ctrlDot())
	if !a.taskSheet.open {
		t.Fatal("ctrl+. did not open the task page")
	}
	frame, _, _ := a.frame()
	if !strings.Contains(plain(frame), taskSheetWord) {
		t.Fatalf("the frame is not the task page:\n%s", plain(frame))
	}
	// The conversation is not drawn under it, and neither is the box a person
	// types into: the page took the frame whole.
	if strings.Contains(plain(frame), "› ") {
		t.Fatalf("the message box is still on the frame under the page:\n%s", plain(frame))
	}

	drive(t, a, key("esc"))
	if a.taskSheet.open {
		t.Fatal("esc did not close the task page")
	}
}

// THE KEY FALLS THROUGH ON A PROJECT THAT HAS RUN NOTHING, and /tasks says so
// rather than raising a page with a title and nothing under it. The emptiness law
// reaches modals: a fullscreen page with no rows is the loudest way of saying
// nothing.
func TestTheTaskPageRefusesToOpenWithNoTasksAtAll(t *testing.T) {
	a, _, _ := taskApp(t)

	drive(t, a, ctrlDot())
	if a.taskSheet.open {
		t.Fatal("ctrl+. raised an empty task page")
	}

	a.slash("/tasks")
	if a.taskSheet.open {
		t.Fatal("/tasks raised an empty task page")
	}
	if text := taskText(a); !strings.Contains(text, taskSheetEmpty) {
		t.Fatalf("/tasks said nothing about why it opened nothing:\n%s", text)
	}
}

// THE TWO SECTIONS ANSWER DIFFERENT QUESTIONS: the tree at the top is what is
// happening, whole and never folded, and the flat list under it is what the
// project has done — including the work of conversations this one never saw.
func TestTheTaskPageDrawsTheRunningTreeAndTheFlatRecord(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("4", "sweep-the-call-sites", "Sweep the call sites", 3*time.Hour),
		pastTask("9", "port-the-parser", "Port the parser", 40*time.Hour),
	}

	if !a.openTaskSheet() {
		t.Fatal("the page refused to open on a session with work in it")
	}
	text := taskSheetText(a)

	// The tree: both section words, the family's root and its children, and the
	// connectors that say which hangs off which.
	for _, want := range []string{
		taskSheetNowHead, taskSheetPastHead,
		"Ship the port", "Write the tree", "Cut the goldens",
		treeBranch, treeLast,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the page is missing %q:\n%s", want, text)
		}
	}
	// AND THE FAMILY IS DRAWN WHOLE. The column folds a settled child away; this
	// page never does, because a tree with its finished branches taken out is a
	// tree whose connectors point at nothing.
	if !strings.Contains(text, "Read the law") {
		t.Fatalf("the settled member of the running family was folded away:\n%s", text)
	}

	// The record: work another conversation ran, which the column cannot show at
	// all, under the flat heading rather than in the tree.
	now := strings.Index(text, taskSheetNowHead)
	past := strings.Index(text, taskSheetPastHead)
	port := strings.Index(text, "Port the parser")
	if port < past {
		t.Fatalf("an earlier conversation's task is drawn above the %q rule:\n%s", taskSheetPastHead, text)
	}
	if now > past {
		t.Fatalf("the sections are in the wrong order:\n%s", text)
	}
	// A flat list and not a tree: nothing under the record wears a connector.
	for _, line := range strings.Split(text[past:], "\n") {
		if strings.Contains(line, treeBranch) || strings.Contains(line, treeLast) {
			t.Fatalf("the record is drawn as a tree:\n%s", line)
		}
	}
	// And the tally counts both sections, in the same two words they are headed
	// with, with neither of them written as a zero.
	if !strings.Contains(text, "5 "+taskSheetNowHead) || !strings.Contains(text, "2 "+taskSheetPastHead) {
		t.Fatalf("the foot does not count what is on the page:\n%s", text)
	}
}

// A ROW OF THIS SESSION'S IS NOT SAID TWICE. The index's live rows come off the
// very graph the tree is drawn from, so a node in both is one node.
func TestTheTaskPageDoesNotRepeatWorkTheTreeIsAlreadyShowing(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		{
			ID: "3", Name: "write-the-tree", Label: "Write the tree", Title: "Write the tree",
			Status: string(session.TaskRunning), SessionID: "this-one",
		},
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}

	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}
	text := taskSheetText(a)
	if n := strings.Count(text, "Write the tree"); n != 1 {
		t.Fatalf("the running node is drawn %d times, want once:\n%s", n, text)
	}
}

// ONLY ONE PAGE MAY BELIEVE IT OWNS THE FRAME. Opening either closes the other,
// in both directions, because view.go draws the settings panel first and a page
// opened under it would take the keyboard and never be seen.
func TestTheTwoFullscreenPagesAreNeverBothOpen(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)

	a.openSettings()
	if !a.openTaskSheet() {
		t.Fatal("the task page refused to open over the settings panel")
	}
	if a.sheet.open {
		t.Fatal("opening the task page left the settings panel open under it")
	}

	a.openSettings()
	if a.taskSheet.open {
		t.Fatal("opening the settings panel left the task page open under it")
	}
}

// THE PAGE WALKS ITS ROWS AND STEPS OVER THE SECTION WORDS. A cursor that could
// land on a rule is a cursor that answers enter with nothing.
func TestTheTaskPageCursorNeverLandsOnASectionWord(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}

	items := a.taskSheetItems()
	for step := 0; step < len(items)+4; step++ {
		item, ok := a.taskSheetCurrent()
		if !ok {
			t.Fatalf("the cursor fell off the page after %d steps down", step)
		}
		if item.heading() {
			t.Fatalf("the cursor landed on the %q rule", item.head)
		}
		drive(t, a, key("down"))
	}
	// It clamps at the end rather than wrapping, the way every other list here
	// walks, and end takes it there in one press.
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnd})
	if item, ok := a.taskSheetCurrent(); !ok || item.node != nil {
		t.Fatal("end did not land on the last row of the record")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyHome})
	if item, ok := a.taskSheetCurrent(); !ok || item.node == nil {
		t.Fatal("home did not land on the first row of the tree")
	}
}

// enter ON A NODE THIS SESSION HOLDS OPENS ITS ROOM, which is exactly what enter
// on the roster does. One door onto one task, reached from two lists.
func TestEnterOnTheTaskPageOpensThatTasksRoom(t *testing.T) {
	// roomApp's one node is 7, and it is the only family here, so the cursor opens
	// on it.
	a, _, _ := roomApp(t)
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}

	drive(t, a, key("enter"))
	if a.taskSheet.open {
		t.Fatal("opening a room left the page standing over it")
	}
	if a.room == nil || a.room.id != 7 {
		t.Fatalf("enter did not open the focused task's room: %+v", a.room)
	}
}

// enter ON WORK ANOTHER CONVERSATION RAN WRITES ITS NAME INTO THE MESSAGE BOX,
// because there is no room to open: a room is a live lane onto a node in THIS
// session's graph, and that session is closed. The mention is the door that
// already exists for reaching old work (taskmention.go).
func TestEnterOnAnEarlierConversationsTaskWritesTheMention(t *testing.T) {
	a, _, _ := taskApp(t)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open on a project with only a record")
	}
	// THE FOOT SAYS WHICH DOOR enter IS. A page that promised a room over work
	// that has none would be lying about its own key.
	if text := taskSheetText(a); !strings.Contains(text, taskSheetMentionKeys) {
		t.Fatalf("the foot promises a room over a task that has none:\n%s", text)
	}

	// A half-written sentence is KEPT: the name is appended to it, because the box
	// is where the person was part-way through saying what the name was for.
	a.input.setText("what happened in")
	drive(t, a, key("enter"))
	if a.taskSheet.open {
		t.Fatal("writing the mention left the page up")
	}
	if got := string(a.input.value); got != "what happened in @port-the-parser " {
		t.Fatalf("the draft reads %q", got)
	}
}

// ── the column's own door onto the page ─────────────────────────────────────

// THE LINE IS DRAWN WHEN THERE IS WORK BEHIND IT AND NOT OTHERWISE. A "view
// more" over a column that is already showing everything is a row that promises
// a page and delivers the list you were looking at.
func TestTheColumnOffersViewMoreOnlyWhenThereIsMore(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	// One node, nothing folded, and no record: the column is showing the whole of
	// what there is to show.
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	if rail := rosterText(a, a.viewHeight()); strings.Contains(rail, taskSheetMoreHint) {
		t.Fatalf("the column offered more with nothing behind it:\n%s", rail)
	}

	// A landed node of THIS session's, already on the column, still earns nothing:
	// it is the same row said twice.
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("1", "ship-the-port", "Ship the port", time.Minute),
	}
	if rail := rosterText(a, a.viewHeight()); strings.Contains(rail, taskSheetMoreHint) {
		t.Fatalf("the column offered more for a row it is already drawing:\n%s", rail)
	}

	// Work an EARLIER conversation ran is work this column cannot show at all, so
	// the line appears.
	a.comp.tasks = append(a.comp.tasks,
		pastTask("9", "port-the-parser", "Port the parser", 40*time.Hour))
	rail := rosterText(a, a.viewHeight())
	if !strings.Contains(rail, taskSheetMoreHint) {
		t.Fatalf("the column hid a record this session never ran:\n%s", rail)
	}
	// IT SITS ABOVE THE COLUMN'S OWN DOOR. The way out of anything is the last
	// line of it, and this one is a way further in.
	lines := strings.Split(strings.TrimRight(rail, "\n"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.Contains(last, railStowHint) {
		t.Fatalf("the column's own door is no longer its last line: %q", last)
	}
}

// A FOLDED FAMILY EARNS IT TOO, because a folded root is one row standing for
// work the column is deliberately not drawing.
func TestAFoldedFamilyEarnsTheViewMoreLine(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)
	if rail := rosterText(a, a.viewHeight()); strings.Contains(rail, taskSheetMoreHint) {
		t.Fatalf("an open column with no record offered more:\n%s", rail)
	}
	a.railSetOpen(a.tasks[1], false)
	if rail := rosterText(a, a.viewHeight()); !strings.Contains(rail, taskSheetMoreHint) {
		t.Fatalf("a folded family did not earn the line:\n%s", rail)
	}
}

// AND THE LINE IS A BUTTON AS WELL AS A KEY. A row that names a chord and cannot
// be pressed is an affordance for one of the two hands.
func TestPressingViewMoreOpensTheTaskPage(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)
	a.railSetOpen(a.tasks[1], false)

	height := a.viewHeight()
	view, _ := a.railView(height)
	at := -1
	for i, line := range view {
		if line.more {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the column drew no view-more line to press:\n%s", rosterText(a, height))
	}
	if _, took := a.railPress(a.bodyWidth()+4, at+a.topHeight()); !took {
		t.Fatal("the press fell through the column")
	}
	if !a.taskSheet.open {
		t.Fatal("pressing view more did not open the task page")
	}
	// The column is left exactly as it was: the page is somewhere you go and come
	// back from, not a state the column enters.
	if a.railAway {
		t.Fatal("opening the page put the column away")
	}
}

// ── the column keeps what is running ────────────────────────────────────────

// WORK THAT IS RUNNING IS NEVER SCROLLED OFF THE COLUMN. The families already
// sort so that everything moving leads; this is the other half of it — a cursor
// walked down into the record takes the record with it and leaves the running
// head where it is.
func TestRunningWorkStaysOnTheColumnHoweverFarTheCursorWalks(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	for i := 2; i <= 60; i++ {
		a.taskUpdate(update(uint64(i), "landed "+itoa(i), session.TaskDone,
			session.TaskNotice{Merge: mergeWordMerged}))
	}

	drive(t, a, ctrlT())
	for i := 0; i < 40; i++ {
		drive(t, a, key("down"))
	}
	rail := rosterText(a, a.viewHeight())
	if !strings.Contains(rail, "Ship the port") {
		t.Fatalf("the running task scrolled off the column:\n%s", rail)
	}
	// And the record under it did move, which is what the cursor was walking
	// through: the pin is the head alone.
	if strings.Contains(rail, "landed 2 ") {
		t.Fatalf("nothing scrolled at all:\n%s", rail)
	}
}
