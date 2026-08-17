package session

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// A RUN IS A FAMILY ON THE ROSTER: one row for the run, one per node under it,
// and every node's state in the tasker's own words. These tests drive the seam
// directly rather than through a whole run, because what is worth pinning is the
// mapping and the de-dup — the engine's own tests own the scheduling.

// familyNotices drains what a session published about its tasks until it goes
// quiet.
func familyNotices(t *testing.T, updates <-chan Event) []TaskNotice {
	t.Helper()
	var out []TaskNotice
	for {
		select {
		case event, open := <-updates:
			if !open {
				return out
			}
			if event.Kind == EventTaskUpdate && event.Task != nil {
				out = append(out, *event.Task)
			}
		case <-time.After(250 * time.Millisecond):
			return out
		}
	}
}

// familyAgent is a conversation with nothing wired but the tasker.
func familyAgent(t *testing.T) (*Agent, <-chan Event) {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	return agent, agent.TaskUpdates()
}

func node(id, goal string, state orchestrate.State) orchestrate.NodeStatus {
	return orchestrate.NodeStatus{Node: orchestrate.Node{ID: id, Goal: goal}, State: state}
}

// THE SHAPE: the run is a root, every node is a child of it, and the states are
// the tasker's.
func TestARunRegistersItsNodesAsAFamily(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "cheap/model")

	family.upsert([]orchestrate.NodeStatus{
		node("n1", "read the tariff table", orchestrate.Running),
		node("n2", "read the invoice writer", orchestrate.Queued),
	})

	notices := familyNotices(t, updates)
	if len(notices) != 3 {
		t.Fatalf("%d rows published, want the run and its two nodes: %+v", len(notices), notices)
	}
	root := notices[0]
	if root.Parent != 0 {
		t.Fatalf("the run's own row has parent %d, want a root", root.Parent)
	}
	if root.Title != "audit the pricing code" || root.State != TaskRunning || root.Model != "cheap/model" {
		t.Fatalf("the run's row is %+v", root)
	}
	for _, kid := range notices[1:] {
		if kid.Parent != root.ID {
			t.Fatalf("node row %d hangs off %d, want the run %d", kid.ID, kid.Parent, root.ID)
		}
		if kid.ID == root.ID {
			t.Fatal("a node took the run's own id")
		}
	}
	if notices[1].State != TaskRunning || notices[2].State != TaskQueued {
		t.Fatalf("the states published are %s / %s", notices[1].State, notices[2].State)
	}
	if notices[1].Title != "read the tariff table" {
		t.Fatalf("the node's row is titled %q", notices[1].Title)
	}
}

// ONE UPSERT PER STATE CHANGE. A run publishes on every launch, landing, note
// and steer; a roster redrawing every row for a note is a roster nobody reads.
func TestAFamilyPublishesOnlyWhatMoved(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "")
	frontier := []orchestrate.NodeStatus{
		node("n1", "read the tariff table", orchestrate.Running),
		node("n2", "read the invoice writer", orchestrate.Queued),
	}
	family.upsert(frontier)
	familyNotices(t, updates)

	// The same graph, published again: nothing moved, so nothing is said.
	family.upsert(frontier)
	if quiet := familyNotices(t, updates); len(quiet) != 0 {
		t.Fatalf("an unchanged graph published %+v", quiet)
	}

	// n1 lands. One row, carrying its digest and its spend.
	landed := []orchestrate.NodeStatus{
		{Node: orchestrate.Node{ID: "n1", Goal: "read the tariff table"},
			State: orchestrate.Done, Digest: "the table is keyed by region", Cost: 0.12},
		frontier[1],
	}
	family.upsert(landed)
	notices := familyNotices(t, updates)
	if len(notices) != 1 {
		t.Fatalf("%d rows published for one landing: %+v", len(notices), notices)
	}
	if notices[0].State != TaskDone || notices[0].Report != "the table is keyed by region" || notices[0].CostUSD != 0.12 {
		t.Fatalf("the landing row is %+v", notices[0])
	}
}

// THE STATE MAPPING, including the two joins that are not one-to-one: ready is
// queued, and a cancelled node is a failed row that says a person stopped it.
func TestTheNodeStatesMapOntoTheTaskersOwn(t *testing.T) {
	for _, want := range []struct {
		state   orchestrate.State
		task    TaskState
		stopped bool
	}{
		{orchestrate.Queued, TaskQueued, false},
		{orchestrate.Ready, TaskQueued, false},
		{orchestrate.Running, TaskRunning, false},
		{orchestrate.Done, TaskDone, false},
		{orchestrate.Failed, TaskFailed, false},
		{orchestrate.Cancelled, TaskFailed, true},
	} {
		state, stopped := orchestrateTaskState(want.state)
		if state != want.task || stopped != want.stopped {
			t.Fatalf("node state %v → %s (stopped=%v), want %s (stopped=%v)",
				want.state, state, stopped, want.task, want.stopped)
		}
	}
}

// A FAILED NODE CARRIES ITS ERROR and a running one carries nothing at all —
// an unknown is nothing, never a placeholder.
func TestAFailedNodesRowSaysWhatStoppedIt(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "")
	family.upsert([]orchestrate.NodeStatus{
		{Node: orchestrate.Node{ID: "n1", Goal: "read the tariff table"},
			State: orchestrate.Failed, Err: "the file is not there\nand nothing else is either"},
	})
	notices := familyNotices(t, updates)
	kid := notices[len(notices)-1]
	if kid.State != TaskFailed || kid.Report != "the file is not there" {
		t.Fatalf("the failed row is %+v", kid)
	}
}

// A NODE THE PLANNER TOOK BACK LEAVES THE ROSTER SETTLED. An amendment deletes
// pending work outright, and a row left saying "queued" for it would be a queue
// that no longer exists.
func TestAPlannerCancelSettlesTheRowItLeftBehind(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "")
	family.upsert([]orchestrate.NodeStatus{
		node("n1", "read the tariff table", orchestrate.Running),
		node("n2", "read the invoice writer", orchestrate.Queued),
	})
	familyNotices(t, updates)

	// n2 is gone from the graph entirely.
	family.upsert([]orchestrate.NodeStatus{node("n1", "read the tariff table", orchestrate.Running)})
	notices := familyNotices(t, updates)
	if len(notices) != 1 {
		t.Fatalf("%d rows published for one cancellation: %+v", len(notices), notices)
	}
	if notices[0].State != TaskFailed || !notices[0].Stopped {
		t.Fatalf("the cancelled row is %+v, want a stopped one", notices[0])
	}
	// And it is said once: the node stays gone, and the row stays settled.
	family.upsert([]orchestrate.NodeStatus{node("n1", "read the tariff table", orchestrate.Running)})
	if quiet := familyNotices(t, updates); len(quiet) != 0 {
		t.Fatalf("a settled row was published again: %+v", quiet)
	}
}

// THE THREE ENDINGS a run can have, on the run's own row.
func TestTheRunsRowSettlesTheWayTheRunDid(t *testing.T) {
	for _, want := range []struct {
		name    string
		snap    orchestrate.Snapshot
		err     error
		state   TaskState
		stopped bool
		report  string
	}{
		{
			name:   "landed",
			snap:   orchestrate.Snapshot{Done: true, Answer: "the tariff table is keyed by region (n1)"},
			state:  TaskDone,
			report: "the tariff table is keyed by region (n1)",
		},
		{
			name:    "stopped",
			snap:    orchestrate.Snapshot{Done: true, Stopped: true},
			state:   TaskFailed,
			stopped: true,
		},
		{
			name:   "broke",
			err:    errors.New("the opening plan failed"),
			state:  TaskFailed,
			report: "the opening plan failed",
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			agent, updates := familyAgent(t)
			family := agent.newOrchestrateFamily("audit the pricing code", "")
			familyNotices(t, updates)

			family.settle(want.snap, want.err)
			notices := familyNotices(t, updates)
			if len(notices) != 1 {
				t.Fatalf("%d rows published for one ending: %+v", len(notices), notices)
			}
			row := notices[0]
			if row.ID != family.root || row.Parent != 0 {
				t.Fatalf("the ending landed on row %d (parent %d)", row.ID, row.Parent)
			}
			if row.State != want.state || row.Stopped != want.stopped || row.Report != want.report {
				t.Fatalf("the run's last row is %+v", row)
			}
		})
	}
}

// AND THE IDS ARE THE TASKER'S, so a run's rows can never collide with the
// tasks a person proposed beside them.
func TestAFamilyMintsItsIdsFromTheTaskGraph(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "")
	family.upsert([]orchestrate.NodeStatus{node("n1", "read the tariff table", orchestrate.Running)})
	familyNotices(t, updates)

	// The next proposal a person answers takes the id after the run's.
	next := agent.graph().reserve()
	if next <= family.root {
		t.Fatalf("a task would be minted %d beside a run rooted at %d", next, family.root)
	}
	family.mu.Lock()
	kid := family.ids["n1"]
	family.mu.Unlock()
	if kid == family.root || kid >= next {
		t.Fatalf("the node's id is %d, the run's %d, the next task's %d", kid, family.root, next)
	}
}
