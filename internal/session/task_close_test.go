package session

import (
	"os"
	"testing"
	"time"
)

// ── the quit reaches the graph ──────────────────────────────────────────────

// A CLOSED SESSION HAS NO RUNNING NODES.
//
// The quit used to reach nodes through one door, the jobs round, which walks a
// registry a node only puts itself in from INSIDE its own goroutine. A node
// admitted in the last moments of a session was therefore invisible to the quit
// for as long as that goroutine took to be scheduled, and what it did next was
// run — a log opened under a session that had left, two model calls of real
// spend, and nothing a person could stop or find (issue #381).
//
// So this admits a node whose body does nothing but wait to be cut, closes the
// session, and asks the two questions the law is made of: was the node cut, and
// had its goroutine returned before Close did.
func TestCloseCutsEveryNodeAndWaitsForIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	running := make(chan struct{}, 1)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		// The context is the node's own, made by the frontier before this
		// goroutine existed: nothing here mints one, which is the whole fix.
		ctx, _ := node.runContext()
		select {
		case running <- struct{}{}:
		default:
		}
		<-ctx.Done()
	})

	graph.admit(graph.reserve(), taskSpec{
		title: "wait to be cut", brief: "block until the session ends", acceptance: "cut",
	})
	select {
	case <-running:
	case <-time.After(5 * time.Second):
		t.Fatal("the node never started")
	}

	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The count the quit waited on is back to zero. A node that was never cut
	// would still be sitting in its select, and this is the assertion that
	// catches it — the body above returns for no other reason.
	drained := make(chan struct{})
	go func() {
		graph.runners.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("Close returned while a node's goroutine was still running")
	}
}

// AND A NODE CUT BEFORE ITS BODY RAN OPENS NOTHING AT ALL.
//
// This is the window the defect lived in, put under the microscope: a node that
// is running, whose context is already cut, being handed to the body a moment
// later. It must register no job, open no log, and put no jobs folder back into
// a session that is being taken away — and it must give its lane back, because
// the goroutine holding it is returning.
func TestANodeCutBeforeItsBodyRanOpensNothing(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	handed := make(chan *TaskNode, 1)
	graph := stubbedGraph(agent, func(node *TaskNode) { handed <- node })

	graph.admit(graph.reserve(), taskSpec{
		title: "cut at the door", brief: "never gets to run", acceptance: "nothing",
	})
	var node *TaskNode
	select {
	case node = <-handed:
	case <-time.After(5 * time.Second):
		t.Fatal("the node never started")
	}

	_, cancel := node.runContext()
	cancel()
	agent.runTaskNode(node)

	if jobs := agent.jobs.all(); len(jobs) != 0 {
		t.Fatalf("a node cut before it ran registered %d job(s)", len(jobs))
	}
	directory := droppingsDir(agent.jobs.place, workspace, droppingJobs)
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("a node cut before it ran made %s (stat error %v)", directory, err)
	}
	if parked, _ := node.parkStanding(); !parked {
		t.Fatal("the node kept its lane after returning without running")
	}
}

// AND NOTHING NEW REGISTERS INTO A SESSION THAT HAS LEFT.
//
// The graph's stop is what makes the case above rare; this is the door itself
// refusing, so that a straggler past the grace cannot put a folder back either.
func TestNoJobStartsAfterTheJobsHaveShutDown(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.jobs.shutdown(0)

	if _, err := agent.jobs.startTask(1, "too late", func() {}); err == nil {
		t.Fatal("a task registered as a job after the session had closed")
	}
	if _, err := agent.jobs.start("true"); err == nil {
		t.Fatal("a command started as a job after the session had closed")
	}
	directory := droppingsDir(agent.jobs.place, workspace, droppingJobs)
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("a refused job still made %s (stat error %v)", directory, err)
	}
}
