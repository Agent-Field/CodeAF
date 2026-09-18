package session

// THE LANDING HOOK: what a door outside this package reads when a task lands.
//
// A landed node hands its own record to Config.TaskLanded exactly once, on a
// goroutine of its own; a node still queued or running hands over nothing, and
// a door that wired no hook at all costs nothing. Every test here drives the
// real graph — admit, the frontier, [Agent.reportTaskNode] — and scripts only
// the work itself, which is the part a test may not have.

import (
	"testing"
	"time"
)

// TestALandedTaskCallsTheLandingHookOnceWithTheRecordsFields pins the whole
// shape of one landing: the hook is called once, with the node's own record —
// the brief and the deliverable it was admitted under, the report it landed
// with, what it wrote and how many paths it changed, the checks it declared —
// beside the worker's model and the model its check ran on.
func TestALandedTaskCallsTheLandingHookOnceWithTheRecordsFields(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	landings := make(chan TaskLanding, 4)
	agent.config.TaskLanded = func(landing TaskLanding) { landings <- landing }

	graph := agent.graph()
	release := make(chan struct{})
	graph.run = func(node *TaskNode) {
		<-release
		node.graph.mu.Lock()
		node.wrote = []string{"alpha.md"}
		node.changed = []string{"alpha.md"}
		node.Checks = []string{"go test ./..."}
		node.checkedOn = "test/high"
		node.graph.mu.Unlock()
		node.finish("the report the node landed with", []string{"alpha.md"}, "", "")
		node.graph.complete(node, TaskDone)
	}
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title:       "read the law",
		brief:       "read ALPHA and say what it holds",
		deliverable: "the reading",
		acceptance:  "a reader can check it",
		model:       "test/worker",
	})
	node := graph.node(id)

	// THE NODE IS RUNNING FIRST, and a running node has no record to hand
	// over: the start announcement travels the same reporting path and must
	// reach the hook with nothing.
	deadline := time.Now().Add(5 * time.Second)
	for node.stateNow() != TaskRunning && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if node.stateNow() != TaskRunning {
		t.Fatalf("the node never started running; it is %s", node.stateNow())
	}
	select {
	case landing := <-landings:
		t.Fatalf("a running node called the landing hook with %+v", landing)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	waitDoneNode(t, node)

	select {
	case landing := <-landings:
		if landing.ID != id {
			t.Fatalf("the landing names node %d, want %d", landing.ID, id)
		}
		if landing.State != TaskDone {
			t.Fatalf("the landing's state is %s, want done", landing.State)
		}
		if landing.Brief != "read ALPHA and say what it holds" {
			t.Fatalf("the landing's brief is %q", landing.Brief)
		}
		if landing.Deliverable != "the reading" {
			t.Fatalf("the landing's deliverable is %q", landing.Deliverable)
		}
		if landing.Report != "the report the node landed with" {
			t.Fatalf("the landing's report is %q", landing.Report)
		}
		if len(landing.Wrote) != 1 || landing.Wrote[0] != "alpha.md" {
			t.Fatalf("the landing's wrote is %v", landing.Wrote)
		}
		if landing.Changed != 1 {
			t.Fatalf("the landing's changed is %d, want 1", landing.Changed)
		}
		if len(landing.Checks) != 1 || landing.Checks[0] != "go test ./..." {
			t.Fatalf("the landing's checks are %v", landing.Checks)
		}
		if landing.Worker != "test/worker" {
			t.Fatalf("the landing's worker is %q, want test/worker", landing.Worker)
		}
		if landing.High != "test/high" {
			t.Fatalf("the landing's high is %q, want test/high", landing.High)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a landed task never called the landing hook")
	}
}

// TestANilLandingHookIsFine is the ordinary door: a session that wired no
// reader lands its nodes exactly as before, and nothing anywhere notices.
func TestANilLandingHookIsFine(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if agent.config.TaskLanded != nil {
		t.Fatal("a session built with no landing hook wired one anyway")
	}

	graph := agent.graph()
	graph.run = func(node *TaskNode) {
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "a node with no reader", brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(id))
}
