package tui3

// planrow_identity_test.go pins WHICH HALF OF ONE PIECE OF WORK THE TASKS PLACE
// DRAWS when the store and a row of this conversation are both talking about it.
//
// THERE ARE TWO ROADS AND THE ANSWER IS NOT THE SAME ON BOTH. A plan-born node
// is a real node of this session's tree that happens to have a store task
// beside it, and the node is the half with a room behind it. A row the run's
// door published is not a node at all — the door seeds a store, names the
// store's task with the number the person was answered with, and publishes a
// row under it (internal/session's task_run_belt.go) — so the store is the only
// half with a state that moves and a page that opens.
//
// THE DEFECT (#1355's two tmux failures) WAS ONE HEURISTIC ANSWERING BOTH. The
// place matched the halves on the TITLE they share, which cannot tell a run's
// row from a node wearing the same words, and so drew the run as a node row: it
// wore `working`, the engine's word for a node nobody is driving, while the
// store said `running`, and Enter over it opened a room the engine holds no
// node for. The row says which store task it is now
// ([session.TaskNotice.PlanTask]), and these two tests are the two roads.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// planRowFixtureNow is the one clock both roads are read on.
func planRowFixtureNow() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }

// planRowChat is the conversation both halves of the work belong to. The dedupe
// is restricted to this window's own rows, so both the node row and the plan
// row have to wear it or the test would be proving nothing.
const planRowChat = "a-conversation"

// planRowReading reads one reading for a run whose row and whose store task are
// both in front of the place: `storeID` is what the store answered under and
// `planTask` is what the row says it is ("" for a node of this session's own
// tree). BOTH ARE SPELLED THE WAY A STORE ID CROSSES THE SEAM — `t-<id>`,
// which is what [session.PlanTaskRow.ID] carries — because an id spelled two
// ways is an identity that joins nothing, and that is the second half of what
// this defect was.
func planRowReading(t *testing.T, planTask string, storeID string) tasksReading {
	t.Helper()
	const title = "write HELLO.md containing the word hello"
	// The row as the surface holds it: running, because the row the run's door
	// published says so, and `working` is the word that reading comes out as.
	live := session.ProjectTask(session.TaskFacts{State: session.TaskRunning, Liveness: session.TaskLivenessHeld})
	node := tasksMineRow{
		entry: session.TaskIndexEntry{
			ID: "7", Label: title, Title: title,
			Status: string(session.TaskRunning), SessionID: planRowChat,
			StartedAt: planRowFixtureNow().Add(-2 * time.Minute),
		},
		runs:     true,
		live:     &live,
		planTask: planTask,
	}
	// And the store's own read of the same work: `claimed`, which is a worker
	// holding the task, and the word that maps onto `running`.
	store := session.PlanTaskRow{
		ID: storeID, Title: title, Status: "claimed",
		Started: planRowFixtureNow().Add(-2 * time.Minute),
	}
	mine := tasksMine{
		row:  session.SessionRow{ID: planRowChat, Title: "the conversation", At: planRowFixtureNow()},
		rows: []tasksMineRow{node},
		plan: []session.PlanTaskRow{store},
	}
	return readTasks(session.World{}, mine, session.LastDays(planRowFixtureNow(), 10), tasksSort{},
		time.Time{}, planRowFixtureNow())
}

// planRowItemOf is the one item of a reading whose title carries these words, or
// the zero item and false when the place read none.
func planRowItemOf(reading tasksReading, words string) (tasksItem, int) {
	var found tasksItem
	count := 0
	for _, item := range reading.items {
		if strings.Contains(item.entry.Title, words) {
			found, count = item, count+1
		}
	}
	return found, count
}

// THE RUN'S ROW IS DRAWN AS THE STORE'S TASK, because the store is the half
// that answers for it: the state word it wears is the store's, and the page
// Enter opens is the store's own — the description its worker was given and the
// trajectory of every command it ran.
//
// THE ROW WEARING `working` IS THE WHOLE DEFECT. It is the reading of a
// [session.TaskNode] nobody is driving, on a row the graph holds no node for,
// and it is what the tmux drive read off the real screen.
func TestARunsRowIsDrawnAsTheStoreTaskItSaysItIs(t *testing.T) {
	reading := planRowReading(t, "t-7", "t-7")
	item, drawn := planRowItemOf(reading, "HELLO.md")
	if drawn != 1 {
		t.Fatalf("the place draws the run %d times, want once: the row and its store task are one "+
			"piece of work read from two ends", drawn)
	}
	if item.plan == nil {
		t.Fatalf("the run's row is the node row and not the store's plan row, so enter over it opens a "+
			"room the engine holds no node for. The row drawn is %+v", item.entry)
	}
	if got := item.status().Word; got != "running" {
		t.Fatalf("the run's row wears %q, want %q — the word its store status maps to (planStateWord); "+
			"`working` is the engine's reading of a node nobody is driving", got, "running")
	}
	// AND THE DRAWN LINE SAYS IT, because a word on a reading nothing paints is
	// not a word a person reads.
	row := tasksDrawnRow(tasksPage(reading, 140), "HELLO.md")
	if !strings.Contains(row, "running") {
		t.Fatalf("the run's drawn row reads\n  %s\nand it must wear `running`", row)
	}
}

// AND A PLAN-BORN NODE IS STILL DRAWN AS THE NODE, which is the dedupe this
// change must not break. Its row says no store task — the node road's link
// lives on the node's own spec and never on the row — so the place matches the
// two halves on the title they share and keeps the half with a room behind it.
func TestAPlanBornNodeIsStillDrawnAsItsNodeRow(t *testing.T) {
	reading := planRowReading(t, "", "t-kq3f7a")
	item, drawn := planRowItemOf(reading, "HELLO.md")
	if drawn != 1 {
		t.Fatalf("the place draws the plan-born node %d times, want once", drawn)
	}
	if item.plan != nil {
		t.Fatalf("the plan-born node is drawn as its store task, and the node is the half with a room " +
			"behind it: the title dedupe (planRowShown) is what keeps the place drawing it once")
	}
}

// A ROW WHOSE STORE TASK THE PLAN READ DOES NOT HOLD IS STILL DRAWN. The read
// may not have landed, and a finished plan is archived out from under its own
// rows — dropping the row on the strength of an identity nothing answers for
// would take the run off the page altogether.
func TestARunsRowSurvivesAPlanReadThatDoesNotHoldIt(t *testing.T) {
	reading := planRowReading(t, "t-7", "t-someother")
	item, drawn := planRowItemOf(reading, "HELLO.md")
	if drawn != 1 {
		t.Fatalf("the place draws the run %d times, want once", drawn)
	}
	if item.plan != nil {
		t.Fatalf("the run's row was replaced by a store task that is not the one it named")
	}
}

// AND THE IDENTITY SURVIVES THE WHOLE SURFACE, which is the half a reading built
// by hand cannot prove: the notice the run's door publishes reaches
// [app.taskUpdate], the node keeps the store task it named, [app.taskSheetMine]
// carries it onto the row, and the place reads it there. Every one of those four
// is a place the fact can be dropped, and the tmux drive that found this defect
// is the only other thing that walks all four.
func TestTheRunsRowCarriesItsStoreTaskThroughTheWholeSurface(t *testing.T) {
	const title = "write HELLO.md containing the word hello"
	root := session.PlanTaskRow{ID: "t-1", Title: title, Status: "claimed"}
	a, _ := planAppWith(t, []session.PlanTaskRow{root}, nil)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, title, session.TaskRunning,
		session.TaskNotice{PlanTask: "t-1"})})
	if node := a.tasks[1]; node == nil || node.planTask != "t-1" {
		t.Fatalf("the node kept planTask=%q, want \"t-1\": the run's row said which store task it is",
			func() string {
				if a.tasks[1] == nil {
					return "<no node>"
				}
				return a.tasks[1].planTask
			}())
	}
	mine := a.taskSheetMine()
	found := false
	for _, row := range mine.rows {
		if strings.Contains(row.entry.Title, "HELLO.md") {
			found = true
			if row.planTask != "t-1" {
				t.Fatalf("the place's own row carries planTask=%q, want \"t-1\"", row.planTask)
			}
		}
	}
	if !found {
		t.Fatalf("the place holds no row for the run at all; it holds %d rows and %d plan rows",
			len(mine.rows), len(mine.plan))
	}
	if len(mine.plan) != 1 {
		t.Fatalf("the place holds %d plan rows, want the one the store answered", len(mine.plan))
	}
	reading := readTasks(session.World{}, mine, session.LastDays(a.now(), 10), tasksSort{}, time.Time{}, a.now())
	item, drawn := planRowItemOf(reading, "HELLO.md")
	if drawn != 1 || item.plan == nil {
		t.Fatalf("the place draws the run %d times and item.plan=%v, want once as the store's own row",
			drawn, item.plan != nil)
	}
	if got := item.status().Word; got != "running" {
		t.Fatalf("the run's row wears %q, want %q", got, "running")
	}
}
