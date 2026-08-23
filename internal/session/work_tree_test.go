package session

// THE WORK TREE, AS TESTS: what "working right now" answers, and what it must
// never answer.
//
// Two surfaces read this door and nothing else, so the thing worth pinning is
// not the shape of the struct — that is the contract, and it is frozen — but
// the three claims a reader is entitled to make about what comes out of it:
// every row is a worker that exists, a row's state is what that worker is
// actually doing, and an idle session answers nothing at all.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

func TestAnIdleSessionIsWorkingOnNothing(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if nodes := agent.WorkingNow(); nodes != nil {
		t.Fatalf("an idle session says it is working on %d things", len(nodes))
	}
	if n := CountWorking(agent.WorkingNow()); n != 0 {
		t.Fatalf("an idle session counts %d workers", n)
	}
}

func TestTheSessionsTasksStandAtTheTopAndTheirPartsBeneath(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 3))

	tree := nest.session.WorkingNow()
	if len(tree) != 1 {
		t.Fatalf("the tree has %d roots, want the one task this session has", len(tree))
	}
	root := tree[0]
	if root.Title != "the whole job" {
		t.Fatalf("the root is called %q", root.Title)
	}
	if len(root.Children) != 3 {
		t.Fatalf("the task shows %d workers beneath it, want the 3 parts it split into", len(root.Children))
	}
	// EVERY ROW IS SOMETHING A SURFACE CAN ACT ON: the id is cancel.go's own
	// spelling, so a reader can hand it straight to [Agent.Cancel].
	for _, row := range append([]WorkNode{root}, root.Children...) {
		kind, rest := WorkPath(row.ID)
		if kind != CancelTask || rest == "" {
			t.Fatalf("row %q is not an id anything can be done with", row.ID)
		}
	}
}

func TestABornChildIsInTheTreeTheMomentItExists(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	if kids := nest.session.WorkingNow()[0].Children; len(kids) != 0 {
		t.Fatalf("a task that has divided nothing already shows %d workers under it", len(kids))
	}
	nest.divide(t, divideArgs(wideEvidence, 2))
	if kids := nest.session.WorkingNow()[0].Children; len(kids) != 2 {
		t.Fatalf("the tree shows %d parts straight after the division, want 2", len(kids))
	}
}

func TestAWorkerWaitingForAFreeHandSaysItIsWaiting(t *testing.T) {
	// Two lanes, the parent standing in one and two parts wanting the other: one
	// part starts and one is held on the frontier, which is exactly the wait
	// WorkWaiting is for.
	nest := newDivideNest(t, wideBrief, 2)
	nest.divide(t, divideArgs(wideEvidence, 2))

	kids := nest.session.WorkingNow()[0].Children
	if len(kids) != 2 {
		t.Fatalf("the tree shows %d parts, want 2", len(kids))
	}
	waiting := 0
	for _, kid := range kids {
		if kid.State == WorkWaiting {
			waiting++
		}
	}
	if waiting == 0 {
		t.Fatalf("with one free lane and %d parts, nothing says it is waiting", len(kids))
	}
	// AND THE COUNT IS THE RUNNING ONES ONLY. A pulse line quoting this must
	// never say three hands are at work when one of them is queued.
	if got := CountWorking(nest.session.WorkingNow()); got > 2 {
		t.Fatalf("the count says %d hands are at work with only 2 lanes", got)
	}
}

func TestAParkedWorkerIsWaitingRatherThanRunning(t *testing.T) {
	// A parent that has handed its lane back is waiting on an answer — its
	// parts' reports — and a rail that drew it as running would be saying a hand
	// is at work when the hand is empty.
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 2))
	nest.graph.mu.Lock()
	nest.parent.state = TaskRunning
	nest.graph.mu.Unlock()
	if state := nest.session.WorkingNow()[0].State; state != WorkRunning {
		t.Fatalf("a running task reads %q", state)
	}
	nest.parent.park()
	if state := nest.session.WorkingNow()[0].State; state != WorkWaiting {
		t.Fatalf("a task waiting on its parts reads %q, want waiting", state)
	}
}

func TestLandedWorkLeavesTheTreeAndItsPartsGoWithIt(t *testing.T) {
	// The tree is LIVE work. A session accumulates finished tasks for as long as
	// it runs, and a door that kept them would say "working right now" about
	// work that ended an hour ago — the roster is where history is read.
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 2))
	kids := nest.graph.children(nest.parent.id)

	// One part home, the rest of the family still going: the finished part stays
	// on the board, because a division with two of three home is a board with
	// three rows on it.
	nest.graph.mu.Lock()
	kids[0].state = TaskDone
	nest.graph.mu.Unlock()
	tree := nest.session.WorkingNow()
	if len(tree) != 1 || len(tree[0].Children) != 2 {
		t.Fatal("a part that landed was dropped from its family's board")
	}
	done := 0
	for _, kid := range tree[0].Children {
		if kid.State == WorkDone {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("%d parts read as done, want the one that landed", done)
	}

	// The whole family home: nothing is live, so nothing is drawn.
	nest.graph.mu.Lock()
	kids[1].state = TaskDone
	nest.parent.state = TaskDone
	nest.graph.mu.Unlock()
	if tree := nest.session.WorkingNow(); tree != nil {
		t.Fatalf("a session whose work is all finished says it is working on %d things", len(tree))
	}
}

func TestTheTreeNeverReportsAWorkerThatDoesNotExist(t *testing.T) {
	// NEVER FAKE LIVENESS. A division that was refused invents no rows, and a
	// task that has divided nothing has no children — the two ways this door
	// could lie about work being under way.
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(narrowEvidence, 4))
	tree := nest.session.WorkingNow()
	if len(tree) != 1 {
		t.Fatalf("the tree has %d roots after a refused division", len(tree))
	}
	if len(tree[0].Children) != 0 {
		t.Fatalf("a refused division put %d workers in the tree", len(tree[0].Children))
	}
}

func TestAnAdaptiveRunStandsAtTheTopWithItsPlannedNodesBeneath(t *testing.T) {
	// A run is the other kind of work this session holds, and it is drawn at the
	// same two depths a task and its parts are: the run, and the workers under
	// it. Its nodes live on the run's own snapshot and never in the task graph,
	// so this is the half of the tree that would silently go missing.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	run := orchestrate.New("migrate every adapter", nil, nil, orchestrate.Options{Cap: 1})
	agent.mu.Lock()
	agent.orchestrations = map[string]*orchestration{"2": {run: run, cancel: func() {}, born: time.Now()}}
	agent.mu.Unlock()

	tree := agent.WorkingNow()
	if len(tree) != 1 {
		t.Fatalf("the tree has %d roots, want the one run this session has", len(tree))
	}
	if tree[0].ID != CancelRun+":2" {
		t.Fatalf("the run's row is %q, want an id Cancel answers to", tree[0].ID)
	}
	if tree[0].Title != "migrate every adapter" {
		t.Fatalf("the run's row is called %q", tree[0].Title)
	}
	if tree[0].Born.IsZero() {
		t.Fatal("a run that started has no age to draw")
	}
	// OUT OF FUEL IS WAITING ON AN ANSWER, not running: the run is standing at
	// the gate for the person to top it up or finish on what is done.
	run.Charge(2)
	if state := agent.WorkingNow()[0].State; state != WorkWaiting {
		t.Fatalf("a run standing at its fuel gate reads %q, want waiting", state)
	}
}

func TestAWorkPathIsTheSpellingTheRestOfTheEngineAnswersTo(t *testing.T) {
	for _, probe := range []struct{ id, kind, rest string }{
		{"task:4", CancelTask, "4"},
		{"run:2", CancelRun, "2"},
		{"run:2/node-a", CancelRun, "2/node-a"},
		{"4", "", "4"},
	} {
		kind, rest := WorkPath(probe.id)
		if kind != probe.kind || rest != probe.rest {
			t.Errorf("WorkPath(%q) = %q, %q; want %q, %q", probe.id, kind, rest, probe.kind, probe.rest)
		}
	}
}

func TestATitleTooLongForARowIsCutRatherThanDrawnWhole(t *testing.T) {
	// A run's title is a goal, and a goal is a paragraph. The rail draws a row.
	long := strings.Repeat("a very long goal ", 40)
	if cut := clip(firstLine(long), titleLimit); len(cut) > titleLimit+3 {
		t.Fatalf("a %d-character title reached a row", len(cut))
	}
}
