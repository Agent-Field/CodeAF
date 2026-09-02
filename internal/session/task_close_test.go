package session

import (
	"os"
	"sync"
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
	exited := make(chan struct{})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		// The context is the node's own, made by the frontier before this
		// goroutine existed: nothing here mints one, which is the whole fix.
		ctx, _ := node.runContext()
		select {
		case running <- struct{}{}:
		default:
		}
		<-ctx.Done()
		// The LAST act of the body, so that a close observed after Close
		// returned is proof the body had finished first.
		close(exited)
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

	// THE ASSERTION IS ABOUT ORDER, AND SO IT DOES NOT WAIT. Waiting here —
	// on the runners count, or on this channel with a timeout — would pass just
	// as happily for a Close that returned first and a node that landed a
	// millisecond later, which is the bug. What the law says is that the
	// goroutine had ALREADY returned when Close did, so the only honest test is
	// to look once, now, and refuse to block.
	select {
	case <-exited:
	default:
		t.Fatal("Close returned while a node's goroutine was still running")
	}

	// And the count the quit waited on is back to zero, which is the same fact
	// from the graph's side.
	drained := make(chan struct{})
	go func() {
		graph.runners.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("a node's goroutine was still running after Close")
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

// AND A STOP THAT LANDS IN THE SAME WINDOW GIVES THE LANE BACK.
//
// The window the fix above opened is real for a person's stop too, and it is
// the one road out of a running node that no runner is coming to land: the
// frontier marked the node running and took its slot, and the goroutine on its
// way to it will find the node already settled and return without ever having
// claimed it. Nothing else can hand that slot back — [TaskGraph.complete] never
// runs for this node, and [TaskGraph.handBackLane] parks only a node that is
// still RUNNING, which this one no longer is.
//
// So the cap would quietly shrink by one for the rest of the session: with
// task.parallel at 1, the next task would wait forever on a lane held by a node
// nobody was running. That is what this measures — not the counter for its own
// sake, but the only thing the counter is for, which is whether the next task
// starts.
func TestAStopBeforeTheRunClaimedItGivesTheLaneBack(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		// One lane, so a leaked slot is not a statistic — it is the next task
		// never starting.
		config.TaskParallel = 1
	})
	started := make(chan uint64, 2)
	release := make(chan struct{})
	// The body never claims the node: no [TaskNode.setCancel], no
	// [Agent.runTaskNode]. That IS the window — the graph says running, and
	// nobody has taken the node up.
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	defer close(release)

	first := graph.reserve()
	graph.admit(first, taskSpec{
		title: "holds the only lane", brief: "runs until stopped", acceptance: "stopped",
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first node never started")
	}

	graph.mu.Lock()
	held := graph.running
	graph.mu.Unlock()
	if held != 1 {
		t.Fatalf("the running node did not take a lane: running = %d, want 1", held)
	}

	if _, err := graph.stop(first); err != nil {
		t.Fatalf("stop: %v", err)
	}

	graph.mu.Lock()
	free := graph.running
	graph.mu.Unlock()
	if free != 0 {
		t.Fatalf("the stop kept the lane: running = %d, want 0", free)
	}

	// The assertion that matters: the cap is really free, not merely counted
	// free. A second task admitted now must actually start.
	second := graph.reserve()
	graph.admit(second, taskSpec{
		title: "needs the lane back", brief: "starts only if the lane came back", acceptance: "starts",
	})
	select {
	case id := <-started:
		if id != second {
			t.Fatalf("the wrong node started: %d, want %d", id, second)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the next task never started, so the stop took a lane out of the session for good")
	}
}

// AND NOTHING JOINS THE REGISTRY BEHIND THE ROUND THAT WALKS IT.
//
// TestNoJobStartsAfterTheJobsHaveShutDown asks the sequential question — a
// registry already closed refuses — and that question cannot see this one.
// [jobRegistry.newJob] reads `closed`, PUTS THE LOCK DOWN to make the log, and
// its caller appends only afterwards; a shutdown landing in that gap sets the
// flag, walks the slice and returns before the job ever arrives. The job is
// then running in a session that has left, behind the one round that would ever
// have killed it — which is issue #381 with the flag fitted and still open.
//
// THE INTERLEAVING IS WRITTEN OUT RATHER THAN RACED FOR. Driving concurrent
// starts at a shutdown does not reach this: the early refusal in newJob turns
// almost all of them away, and what is left is a window a few instructions
// wide that a scheduler will not land in on demand — a version of this test
// that spun four goroutines at the quit passed perfectly well against the bug.
// So the three acts are simply put in the order the defect needs, which is also
// the order that says what the law IS: a job made while the door was open may
// still not join after the room has been walked.
func TestNoJobJoinsBehindTheShutdownWalk(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)

	// Act one: the job is made while the registry is open, exactly as a task
	// node admitted a moment before the quit makes one.
	started, err := agent.jobs.newJob("made before the quit", jobKindTask)
	if err != nil {
		t.Fatalf("newJob before the quit: %v", err)
	}
	if _, err := os.Stat(started.logPath); err != nil {
		t.Fatalf("the job did not claim a log: %v", err)
	}

	// Act two: the quit closes the door and walks the room. The job is not in
	// it yet, so the round has nothing to find.
	agent.jobs.shutdown(0)

	// Act three: the job tries to take its place, behind the walk.
	if err := agent.jobs.add(started); err == nil {
		t.Fatal("a job made before the quit joined the registry after the round had already walked it")
	}
	if jobs := agent.jobs.all(); len(jobs) != 0 {
		t.Fatalf("the closed registry holds %d job(s)", len(jobs))
	}

	// AND IT TOOK ITS LOG WITH IT. Nothing will ever read that file — no row,
	// no id anybody was given, no round that will settle it — and leaving it
	// would put the jobs folder back a moment after the quit removed it.
	if _, err := os.Stat(started.logPath); !os.IsNotExist(err) {
		t.Fatalf("the refused job left %s behind (stat error %v)", started.logPath, err)
	}
	directory := droppingsDir(agent.jobs.place, workspace, droppingJobs)
	entries, err := os.ReadDir(directory)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read the jobs directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the jobs folder outlived the quit with %d file(s) in it", len(entries))
	}
}

// AND THE SAME SEAM UNDER THE RACE DETECTOR.
//
// The test above is the law; this is the lock discipline behind it. Starts are
// driven AT a shutdown from several goroutines at once, which is what the
// detector needs to see the check and the append and the walk touching the same
// state. It asserts the one thing that must hold however the scheduler
// interleaves them: the registry did not grow after the round returned.
func TestStartsRacingTheShutdownNeverGrowTheClosedRegistry(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for hand := 0; hand < 4; hand++ {
		wg.Add(1)
		go func(hand int) {
			defer wg.Done()
			for id := uint64(1); ; id++ {
				select {
				case <-stop:
					return
				default:
				}
				// Whether this one got in IS the race, so the error is
				// deliberately ignored; the assertion is about the registry.
				if job, err := agent.jobs.startTask(id, "racing the quit", func() {}); err == nil {
					// Settled at once, so a job that did get in before the walk
					// is a finished one and the round has nothing to wait for.
					job.settle(0)
				}
			}
		}(hand)
	}

	agent.jobs.shutdown(0)
	// Taken the instant the round returns: from here on the registry is shut,
	// and anything appearing in it appeared behind the walk.
	atTheWalk := len(agent.jobs.all())

	// The starts keep coming for a moment AFTER the round returned, which is
	// the window a sequential test cannot reach at all.
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	if after := len(agent.jobs.all()); after != atTheWalk {
		t.Fatalf("%d job(s) joined the registry after shutdown had walked it (%d at the walk, %d after)",
			after-atTheWalk, atTheWalk, after)
	}
}
