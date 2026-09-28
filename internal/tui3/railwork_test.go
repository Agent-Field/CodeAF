package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE LIVE WORK TREE, AND WHAT THIS COLUMN TAKES FROM IT ──────────────────
//
// The engine has one door that answers "who is working right now"
// ([session.Agent.WorkingNow]), and it spells a worker the way cancel.go spells
// one: `task:7` for a node of the task graph, `run:2` for an adaptive run,
// `run:2/plan` for one planned node inside one.
//
// This column takes ONE NUMBER from that door — how many hands are moving — and
// draws no row out of it. Every test here is that claim from a different side,
// and every one of them scripts the ids the engine actually mints, because the
// surface this file guards was once wired to a key nobody publishes ("7") and
// the tests that were supposed to catch it scripted the same bare key.

// workRailApp is one task with a family under it, and the engine's live tree
// saying the same thing in its own spelling: the ids are production's, so a
// column that hung the engine's children under the row would draw this family
// twice.
func workRailApp(t *testing.T, kids ...session.WorkNode) (*app, *taskFake) {
	t.Helper()
	a, agent, _ := taskApp(t)
	a.taskUpdate(update(7, "Build the rail", session.TaskRunning, session.TaskNotice{}))
	agent.work = []session.WorkNode{{
		ID: session.CancelTask + ":7", Title: "Build the rail",
		State: session.WorkRunning, Children: kids,
	}}
	a.paints = 0
	return a, agent
}

func TestANilWorkTreeLeavesTheRailByteIdentical(t *testing.T) {
	a, agent, _ := taskApp(t)
	a.taskUpdate(update(7, "Build the rail", session.TaskRunning, session.TaskNotice{}))
	withDoor := strings.Join(a.railRows(12), "\n")
	a.agent = agent.fakeAgent
	withoutDoor := strings.Join(a.railRows(12), "\n")
	if withDoor != withoutDoor {
		t.Fatalf("a nil work tree changed the rail:\nwith door:\n%s\nwithout door:\n%s", withDoor, withoutDoor)
	}
}

// A WORKER IS DRAWN ONCE, BY THE FAMILY THAT OWNS IT.
//
// A task that divided itself has a row for the parent and a row for each part,
// because every node of the graph announces itself with the id of whatever
// spawned it and the column grows its families out of exactly those notices
// (task.go's [app.railForest]). The engine's live tree reports the same three
// hands under `task:7`. Hanging those under the row would be the family drawn
// twice — a full row with its state, its mark and its id, and under it a second,
// poorer copy of the same two names.
//
// This is the test the old file could not have been: it scripted `ID: "7"`,
// which nothing in the engine publishes, so the preview it was pinning never
// attached in a running codeaf and the double draw was invisible.
func TestAWorkerIsDrawnOnceByTheFamilyThatOwnsIt(t *testing.T) {
	a, _ := workRailApp(t,
		session.WorkNode{ID: session.CancelTask + ":8", Title: "Read the law", State: session.WorkWaiting},
		session.WorkNode{ID: session.CancelTask + ":9", Title: "Write the tests", State: session.WorkRunning},
	)
	// The same two hands, as the engine announced them: a row each, under 7.
	a.taskUpdate(update(8, "Read the law", session.TaskQueued, session.TaskNotice{Parent: 7}))
	a.taskUpdate(update(9, "Write the tests", session.TaskRunning, session.TaskNotice{Parent: 7}))
	railOpenAll(a)

	text := strings.Join(railText(a, a.viewHeight()), "\n")
	for _, title := range []string{"Build the rail", "Read the law", "Write the tests"} {
		if got := strings.Count(text, title); got != 1 {
			t.Fatalf("%q is drawn %d times, want once:\n%s", title, got, text)
		}
	}
}

// AND A RUN'S PLANNED NODES ARE THE SAME CLAIM IN THE OTHER SPELLING. A run
// registers a row for itself and one per planned node under it (session's
// orchestrate.go family seam), so `run:1` and `run:1/plan` name work this column
// has already been told about by name.
func TestARunsPlannedWorkersAreDrawnOnceEach(t *testing.T) {
	a, agent, _ := taskApp(t)
	a.taskUpdate(update(1, "port the parser", session.TaskRunning, session.TaskNotice{Run: "1"}))
	a.taskUpdate(update(2, "token bucket", session.TaskRunning, session.TaskNotice{Run: "1", Node: "plan", Parent: 1}))
	a.taskUpdate(update(3, "feature matrix", session.TaskRunning, session.TaskNotice{Run: "1", Node: "matrix", Parent: 1}))
	agent.work = []session.WorkNode{{
		ID: session.CancelRun + ":1", Title: "port the parser", State: session.WorkRunning,
		Children: []session.WorkNode{
			{ID: session.CancelRun + ":1/plan", Title: "token bucket", State: session.WorkRunning},
			{ID: session.CancelRun + ":1/matrix", Title: "feature matrix", State: session.WorkRunning},
		},
	}}
	a.paints = 0

	text := strings.Join(railText(a, a.viewHeight()), "\n")
	for _, title := range []string{"port the parser", "token bucket", "feature matrix"} {
		if got := strings.Count(text, title); got != 1 {
			t.Fatalf("%q is drawn %d times, want once:\n%s", title, got, text)
		}
	}
}

// NOTHING IS DRAWN FOR A WORKER THIS COLUMN WAS NEVER TOLD ABOUT. The engine's
// tree is not a second source of rows: a hand it reports under a task whose own
// notice has not arrived leaves the column exactly as it was. There is no such
// worker in a running codeaf — a node cannot be in that tree without having
// announced itself first — and this pins that the column does not invent one
// if the two ever disagree.
func TestTheLiveTreeAddsNoRowOfItsOwn(t *testing.T) {
	a, agent := workRailApp(t)
	before := len(a.railEntries())

	agent.work[0].Children = []session.WorkNode{
		{ID: session.CancelTask + ":8", Title: "a hand with no row", State: session.WorkRunning},
	}
	a.paints = 0
	if after := len(a.railEntries()); after != before {
		t.Fatalf("the live tree grew the column from %d rows to %d", before, after)
	}
	// A live-work count does not replace the blank spacer.
	drawn := strings.Join(railText(a, a.viewHeight()), "\n")
	if strings.Contains(drawn, "a hand with no row") {
		t.Fatalf("the live tree drew a row of its own:\n%s", drawn)
	}
	if strings.Contains(drawn, marginTasksWord+" · 2 working") {
		t.Fatalf("the task heading returned:\n%s", drawn)
	}
}

// The spacer stays blank regardless of the number of working tasks.
func TestTheTasksSpacerStaysBlankWhileWorkRuns(t *testing.T) {
	a, agent := workRailApp(t)
	for n := 0; n <= 3; n++ {
		kids := make([]session.WorkNode, n)
		for i := range kids {
			kids[i] = session.WorkNode{
				ID: session.CancelTask + ":" + itoa(i+8), Title: "hand", State: session.WorkRunning,
			}
		}
		agent.work[0].Children = kids
		head := plain(a.marginHead(28, true)[0].text)
		if head != "" {
			t.Fatalf("%d working filled the spacer: %q", n+1, head)
		}
	}
}
