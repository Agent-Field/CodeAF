package tui3

// WHAT A RUNNING NODE HAS BURNED, AS THIS SURFACE LEARNS IT. The standing lane
// prices a node twice in its whole life — when it starts and when it lands — and
// the pilot prices it at every step boundary in between (task.go's [taskPilot]).
// These tests are about the seam between the two: that the live figure grows,
// that the engine's figure wins, and that the two are never added together.

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// turnDone is one EventTurnDone as a child agent ends a step with it: the turn's
// own totals, sealed (session's loop.go).
func turnDone(input, output int, cost float64) session.Event {
	return session.Event{
		Kind:  session.EventTurnDone,
		Usage: session.Usage{Input: input, Output: output, CostUSD: cost},
	}
}

// flyTurn hands one event to the node's pilot the way its lane does.
func flyTurn(t *testing.T, a *app, id uint64, ev session.Event) {
	t.Helper()
	pilot := a.pilots[id]
	if pilot == nil {
		t.Fatalf("node %d has no pilot to fly", id)
	}
	drive(t, a, taskPilotMsg{gen: pilot.gen, id: id, ev: ev})
}

// near reports whether two dollar figures are the same money. The sums are
// floating point and the assertions are about accounting, not about bits.
func near(got, want float64) bool {
	d := got - want
	return d < 1e-9 && d > -1e-9
}

// THE PILOT IS THE ONLY LIVE METER THERE IS. Every turn the child finishes says
// what that turn burned, and the node's totals are the sum of them — which is
// the whole reason the lane folds this kind at all.
func TestTheNodesTokensAndPriceGrowWithEveryTurnItFinishes(t *testing.T) {
	a, _, _ := roomApp(t)
	node := a.tasks[7]
	if node == nil {
		t.Fatal("the running node is not on the roster")
	}
	if node.tokens != 0 || node.spent() != 0 {
		t.Fatalf("a node nobody has counted starts at %d tokens / %v", node.tokens, node.spent())
	}

	flyTurn(t, a, 7, turnDone(1200, 300, 0.04))
	if node.tokens != 1500 || !near(node.spent(), 0.04) {
		t.Fatalf("after one turn the node holds %d tokens / %v", node.tokens, node.spent())
	}

	flyTurn(t, a, 7, turnDone(900, 100, 0.02))
	if node.tokens != 2500 {
		t.Fatalf("the second turn's tokens did not land: %d", node.tokens)
	}
	if !near(node.spent(), 0.06) {
		t.Fatalf("the second turn's price did not land: %v", node.spent())
	}
}

// AND IT REACHES THE ROSTER THROUGH THE DOOR, not only through a message a test
// wrote: the turn's end is on the child's own stream, beside the tool calls the
// pilot was opened for.
func TestATurnEndingOnTheChildsStreamReachesTheNode(t *testing.T) {
	a, fake, _ := taskApp(t)
	agent := &roomFake{taskFake: fake, lanes: map[uint64]chan session.Event{}}
	a.agent = agent
	// SEEDED BEFORE THE NODE STARTS, because the door hands a watcher what is on
	// the lane when it opens (bundle_test.go's [roomFake.WatchTask]) and a test
	// that pushed afterwards would be a test that waited.
	agent.lane(7) <- turnDone(800, 200, 0.05)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{})})

	node := a.tasks[7]
	if node == nil || node.tokens != 1000 || !near(node.spent(), 0.05) {
		t.Fatalf("the child's own turn did not reach the node: %+v", node)
	}
}

// THE ENGINE'S FIGURE IS THE AUTHORITY. It is the cumulative bill and so is the
// pilot's sum — the same money counted from the other end (session's task_run.go
// spend()) — so the larger of the two is the more recent reading, and the two are
// NEVER ADDED. A rail that summed them would charge for every turn twice.
func TestAnUpdatesPriceOutranksThePilotsSumAndIsNeverAddedToIt(t *testing.T) {
	a, _, _ := roomApp(t)
	node := a.tasks[7]

	flyTurn(t, a, 7, turnDone(1000, 200, 0.03))
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{CostUSD: 0.42})})

	if !near(node.spent(), 0.42) {
		t.Fatalf("the node is priced at %v, want the engine's own 0.42", node.spent())
	}
	// The pilot's own count is kept — it is the only token figure there is, and no
	// notice carries one.
	if node.tokens != 1200 {
		t.Fatalf("the update took the pilot's tokens: %d", node.tokens)
	}

	// A turn that lands after it adds to the live sum and NOT to the bill, which
	// still has further to go before it overtakes what the engine published.
	flyTurn(t, a, 7, turnDone(500, 100, 0.05))
	if !near(node.spent(), 0.42) {
		t.Fatalf("a turn was added to the engine's figure: %v, want 0.42", node.spent())
	}
	if node.tokens != 1800 {
		t.Fatalf("the later turn's tokens did not land: %d", node.tokens)
	}

	// Once the sum passes it, the sum is the fresher reading of the same number
	// and the row moves — forwards, which is the only direction a bill goes.
	flyTurn(t, a, 7, turnDone(0, 0, 0.40))
	if !near(node.spent(), 0.48) {
		t.Fatalf("the pilot's sum did not overtake a stale figure: %v", node.spent())
	}
}

// A LANDED NODE'S PRICE IS FROZEN AT THE ENGINE'S. The final notice is the books
// closed (session's foldTaskUsage), and a surface holding a larger guess after
// the work is over would be disputing a bill rather than reporting one.
func TestALandedNodeKeepsTheEnginesFinalPriceAndNotThePilotsGuess(t *testing.T) {
	a, _, advance := roomApp(t)
	node := a.tasks[7]

	flyTurn(t, a, 7, turnDone(4000, 1000, 0.90))
	if !near(node.spent(), 0.90) {
		t.Fatalf("the running node is priced at %v", node.spent())
	}

	advance(2 * time.Minute)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{Report: "the guard is in", Merge: "merged", CostUSD: 0.75})})

	if !near(node.spent(), 0.75) {
		t.Fatalf("the landed node is priced at %v, want the frozen 0.75", node.spent())
	}
	if node.tokens != 5000 {
		t.Fatalf("landing lost the tokens the pilot counted: %d", node.tokens)
	}
	// THE WATCHER IS GONE WITH THE WORK. Nothing can move a settled figure,
	// including an event that was already in flight on the lane when it landed.
	if a.pilots[7] != nil {
		t.Fatal("the pilot is still flying a node that has landed")
	}
	drive(t, a, taskPilotMsg{gen: 1, id: 7, ev: turnDone(9000, 9000, 9.00)})
	if node.tokens != 5000 || !near(node.spent(), 0.75) {
		t.Fatalf("a late turn moved a settled node: %d tokens / %v", node.tokens, node.spent())
	}
}

// A NODE NOBODY PRICED STAYS AT NOTHING, and that is the emptiness law rather
// than an accident: an unpriced model and a provider that reported no usage both
// arrive as zeroes, and zero is what a surface draws nothing for. It must not
// become a $0.00, and it must not spend a repaint either.
func TestATurnThatReportedNoUsageLeavesTheNodeAtNothing(t *testing.T) {
	a, _, _ := roomApp(t)
	node := a.tasks[7]

	a.dirty = false
	flyTurn(t, a, 7, turnDone(0, 0, 0))
	if node.tokens != 0 || node.spent() != 0 {
		t.Fatalf("an empty turn wrote %d tokens / %v", node.tokens, node.spent())
	}
	if a.dirty {
		t.Fatal("a turn with nothing in it asked for a repaint")
	}
}
