package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── WHAT A COLUMN OWES SOMEBODY GLANCING AT IT ──────────────────────────────
//
// The report this file is written from is a screenshot: thirteen background jobs
// that had all landed hours ago, each of them spending three lines of a thirty
// cell column on history nobody was reading, the `standing` section squeezed to
// one cut-off row underneath them, and a wheel turned over the whole thing
// scrolling the conversation beside it.
//
// Three laws come out of that, and these are them:
//
//   - A ROW THAT HAS LANDED IS ONE LINE, and what it had to say is on the hint
//     line when the pointer is on it rather than thrown away.
//   - THE SECTIONS UNDER THE ROSTER ARE RESERVED, not stacked. A list that can
//     grow without limit starves anything below it, every time.
//   - THE WHEEL MOVES THE LIST UNDER THE POINTER, which is the oldest thing a
//     pointer does.

// railLandedTask is one piece of work that has come home with something to say:
// the row every session ends the day with a dozen of.
//
// THE SCREENSHOT WAS FULL OF BACKGROUND JOBS AND THESE ARE TASKS, and the law is
// the same law. Jobs left this column entirely — they have a section of their
// own now, where history is a count rather than rows (jobsview.go) — so the
// roster's own crowding is pinned with the rows the roster still holds. What was
// being proved was never about jobs; it was about a list that grows all day
// above two sections that cannot give way.
func railLandedTask(id uint64, title string) session.Event {
	return update(id, title, session.TaskDone, session.TaskNotice{
		Merge:   mergeWordMerged,
		CostUSD: railCostOf(int(id)),
	})
}

// railCostOf and railReportOf are what one landed row is holding behind its
// fold: the merge word and what the work cost. The prices start above a dime so
// that every one of them is two decimals and no row's block is a prefix of
// another's — `$0.1` inside `$0.11` would be a case that passed on a row it
// never drew, which is [railBuild]'s own rule about names.
func railCostOf(i int) float64 { return float64(10+i) / 100 }

func railReportOf(i int) string { return mergeWordMerged + " · " + dollars(railCostOf(i)) }

// railBuild is one landed job's name, wide enough to read on a thirty-cell
// column and numbered so that no name is a prefix of another — `build-1` inside
// `build-12` would be a test that passed on a row it never drew.
func railBuild(i int) string {
	if i < 10 {
		return "build-0" + itoa(i)
	}
	return "build-" + itoa(i)
}

// railLanded fills a session with landed jobs, which is the shape the column was
// drowning in, with the finished group opened so that every one is drawn.
func railLanded(a *app, n int) {
	for i := 1; i <= n; i++ {
		a.taskUpdate(railLandedTask(uint64(i), railBuild(i)))
	}
	railOpenAll(a)
}

// ── 1. a landed row is one line ─────────────────────────────────────────────

// THE WHOLE DEFECT, AND THE WHOLE FIX. Thirteen landed jobs used to spend
// thirteen titles and thirteen log paths; they spend thirteen rows now, and the
// column has room left for the sections under them.
func TestALandedRowSpendsOneLineOnTheColumn(t *testing.T) {
	a, _, _ := taskApp(t)
	railLanded(a, 13)
	text := strings.Join(railText(a, 20), "\n")
	for i := 1; i <= 13; i++ {
		if strings.Contains(text, railBuild(i)) {
			t.Fatalf("completed task occupies sidebar: %s", text)
		}
		card := a.doneCardAt(a.doneEntryFor(uint64(i)))
		if card == nil || card.cost != railCostOf(i) {
			t.Fatalf("result facts lost for %d", i)
		}
	}
}

// WORK THAT IS STILL GOING IS ONE LINE TOO, and the row is its door: the
// column draws one row for one running node under its heading, and the hint
// line over it names it whole.
//
// It used to be said about a running background JOB, whose second line was the
// path its output was going to. That line is gone from this column with the jobs
// themselves (a job's log is on the job's own page now, jobpage.go), so what is
// left to pin is the rule the job row was only ever an example of.
func TestARunningRowKeepsWhatItIsDoing(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(4, "Collect the sources", session.TaskRunning, session.TaskNotice{
		Report: railReportOf(4),
	}))

	entries := a.railEntries()
	if len(entries) != 2 || !entries[0].head || entries[1].node == nil || entries[1].node.id != 4 {
		t.Fatalf("the column drew %d rows for one running node: %+v", len(entries), entries)
	}
	if hint := railHint(a, 4); !strings.Contains(hint, "Collect the sources") {
		t.Fatalf("the hint over the running row reads %q", hint)
	}
}

// AND A ROW THAT HAS NOT STARTED KEEPS ITS SENTENCE. `waits:` is the only
// place this surface says what is in the way of a flat node, and it is on the
// hint line over the row.
func TestAQueuedRowStillSaysWhatItWaitsOn(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "Collect sources", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "Mix audio", session.TaskQueued, session.TaskNotice{DependsOn: []uint64{1}}))

	if hint := railHint(a, 2); !strings.Contains(hint, "waits: Collect sources") {
		t.Fatalf("a queued row lost the sentence saying what is in its way: %q", hint)
	}
}

// NOTHING IS THROWN AWAY. What a landed row used to spend its second line on
// is on the hint line when the pointer is on it, and only that row's.
func TestALandedRowGivesItsBlockToTheHintLine(t *testing.T) {
	a, _, _ := taskApp(t)
	railLanded(a, 3)
	if _, ok := railRowFor(a, 20, railBuild(2)); ok {
		t.Fatal("completed task remains on sidebar")
	}
	card := a.doneCardAt(a.doneEntryFor(2))
	if card == nil || card.cost != railCostOf(2) || card.merge != mergeWordMerged {
		t.Fatalf("completed task details lost: %+v", card)
	}
}

// ── 2. the sections do not fight for space ──────────────────────────────────

// THE HEADER STAYS WHILE THE TASKS SCROLL, and the first row of the list is
// right under it with no spacer between.
func TestTheHeaderStaysWhileTheRosterScrolls(t *testing.T) {
	a, _, _ := taskApp(t)
	railActive(a, 40)
	a.railScroll(12)

	rows := railText(a, 20)
	if len(rows) < 2 || !strings.Contains(rows[0], sideTasksWord) ||
		strings.TrimSpace(strings.TrimPrefix(rows[1], "│")) == "" {
		t.Fatalf("the header moved while scrolling:\n%s", strings.Join(rows, "\n"))
	}
}

// ── 3. the wheel ────────────────────────────────────────────────────────────

// THE WHEEL OVER THE COLUMN MOVES THE COLUMN. It used to move the conversation
// beside it, which is the pointer landing on one list and acting on another.
func TestTheWheelOverTheColumnScrollsTheColumn(t *testing.T) {
	a, _, _ := taskApp(t)
	railActive(a, 60)
	drive(t, a, frameMsg{})
	was, wasOffset := a.railTop, a.offset

	y := a.topHeight() + 3
	drive(t, a, tea.MouseWheelMsg{X: a.railLeft() + 3, Y: y, Button: tea.MouseWheelDown})
	if a.railTop <= was {
		t.Fatalf("the wheel over the column left it at row %d", a.railTop)
	}
	if a.offset != wasOffset {
		t.Fatalf("the wheel over the column scrolled the conversation to %d", a.offset)
	}
	// AND IT COMES BACK. Up is up, and the top of the list is the end of it.
	drive(t, a,
		tea.MouseWheelMsg{X: a.railLeft() + 3, Y: y, Button: tea.MouseWheelUp},
		tea.MouseWheelMsg{X: a.railLeft() + 3, Y: y, Button: tea.MouseWheelUp},
	)
	if a.railTop != 0 {
		t.Fatalf("the wheel walked the column past its own top: %d", a.railTop)
	}
}

// AND THE CONVERSATION IS STILL THE CONVERSATION'S. A column that claimed the
// whole frame's wheel would have replaced one wrong answer with another.
func TestTheWheelBesideTheColumnStillScrollsTheConversation(t *testing.T) {
	a, _, _ := taskApp(t)
	railActive(a, 60)
	for i := 0; i < 30; i++ {
		typeLine(t, a, "line "+itoa(i))
	}
	drive(t, a, frameMsg{})
	was := a.railTop

	drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.topHeight() + 3, Button: tea.MouseWheelUp})
	if a.railTop != was {
		t.Fatalf("a wheel over the transcript moved the column to %d", a.railTop)
	}
	if a.stick {
		t.Fatal("a wheel over the transcript did not move the transcript")
	}
}

// WHILE THE ROSTER HOLDS THE KEYBOARD THE WHEEL WALKS THE CURSOR, which is the
// bargain the full-frame roster already makes: the window follows the focus, so
// an offset nudged out from under it would be undone by the next layout.
func TestTheWheelWalksTheCursorWhileTheColumnHoldsTheKeyboard(t *testing.T) {
	a, _, _ := taskApp(t)
	railActive(a, 60)
	a.railTake(true)
	was := a.railWhere.id

	drive(t, a, tea.MouseWheelMsg{X: a.railLeft() + 3, Y: a.topHeight() + 3, Button: tea.MouseWheelDown})
	if a.railWhere.id == was {
		t.Fatalf("the wheel left the cursor on node %d", was)
	}
}

func railActive(a *app, n int) {
	for i := 1; i <= n; i++ {
		a.taskUpdate(update(uint64(i), railBuild(i), session.TaskRunning, session.TaskNotice{}))
	}
}
