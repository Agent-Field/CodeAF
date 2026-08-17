package orchestrate

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCancelCutsWhatIsInFlight is the whole of what a stop means: the node
// running when the key was pressed loses its context, the node queued behind it
// never starts, and the run settles as stopped without paying for a synthesis.
func TestCancelCutsWhatIsInFlight(t *testing.T) {
	var (
		started = make(chan struct{})
		cut     = make(chan struct{})
		ended   = make(chan Snapshot, 1)
	)
	plan := &script{steps: []Amendment{{Add: []Node{
		{ID: "a", Goal: "the long one"},
		{ID: "b", Goal: "after a", Needs: []string{"a"}},
		{ID: "c", Goal: "never launched"},
	}}}}
	run := New("goal", plan, execFunc(func(ctx context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		if n.ID != "a" {
			return "ran", 0, nil
		}
		close(started)
		<-ctx.Done()
		close(cut)
		// The half-answer a cut node is holding, handed back exactly as a real
		// executor would hand it back. Nothing downstream may ever see it.
		return "half of what I was going to say", 0.20, ctx.Err()
	}), Options{Cap: 5, Lanes: 1})

	go func() {
		snap, err := run.Run(testContext(t))
		if err != nil {
			t.Errorf("a stopped run is not a run that failed: %v", err)
		}
		ended <- snap
	}()

	waitFor(t, started, "the first node to start")
	run.Cancel()
	waitFor(t, cut, "the in-flight node's context to be cut")
	snap := waitForEnd(t, ended)

	if !snap.Stopped || !snap.Done {
		t.Fatalf("a cancelled run is stopped AND over: %+v", snap)
	}
	if snap.Answer != "" {
		t.Fatalf("a stopped run does not pay for a synthesis, got %q", snap.Answer)
	}
	if got := stateOf(snap, "a"); got != Cancelled {
		t.Fatalf("the in-flight node is %v, want cancelled", got)
	}
	for _, node := range snap.Nodes {
		if node.ID == "a" && node.Digest != "" {
			t.Fatalf("a cut node's partial output is discarded, kept %q", node.Digest)
		}
	}
	if got := stateOf(snap, "c"); got != Cancelled {
		t.Fatalf("the queued node is %v, want cancelled", got)
	}
	// What it spent is what it spent: the tank keeps its meter through a stop.
	if snap.Fuel.Spent < 0.20 {
		t.Fatalf("the tank kept %v, want what the cut node had already burned", snap.Fuel.Spent)
	}
	if !strings.Contains(strings.Join(snap.Notes, " | "), "stopped") {
		t.Fatalf("a stop said nothing: %+v", snap.Notes)
	}
}

// TestCancelDropsAQueuedNodeInstantly: a node that has not launched needs
// nothing to come home, so the snapshot says so before Run has done anything
// about it.
func TestCancelDropsAQueuedNodeInstantly(t *testing.T) {
	run := New("goal", &script{}, execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
		t.Fatalf("nothing may run")
		return "", 0, nil
	}), Options{})
	run.apply(Amendment{Add: []Node{{ID: "a", Goal: "waits"}, {ID: "b", Goal: "waits too"}}})

	run.Cancel()
	snap := run.Snapshot()
	for _, id := range []string{"a", "b"} {
		if got := stateOf(snap, id); got != Cancelled {
			t.Fatalf("%s is %v, want cancelled the instant the run was", id, got)
		}
	}
	if !snap.Stopped {
		t.Fatalf("the snapshot does not say the run was stopped: %+v", snap)
	}
	// AND THE NODES ARE STILL THERE. The planner's own cancel deletes a node it
	// no longer wants; a person's cancel keeps the trace they are looking at.
	if len(snap.Nodes) != 2 {
		t.Fatalf("a stop kept %d nodes, want the trace whole", len(snap.Nodes))
	}
}

// TestCancelIsIdempotent: pressing it twice, and pressing it on a run that has
// already settled, are both nothing.
func TestCancelIsIdempotent(t *testing.T) {
	run := New("goal", &script{}, execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
		return "ok", 0, nil
	}), Options{})
	run.Cancel()
	run.Cancel()
	run.Cancel()
	first := run.Snapshot()
	if !first.Stopped {
		t.Fatalf("the first cancel did not take")
	}
	if len(first.Notes) != 1 {
		t.Fatalf("a stop is said once, got %+v", first.Notes)
	}

	snap, err := run.Run(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Done || !snap.Stopped {
		t.Fatalf("a run cancelled before it began settles as stopped: %+v", snap)
	}
	run.Cancel()
	if got := run.Snapshot(); len(got.Notes) != 1 {
		t.Fatalf("cancelling a settled run says nothing more, got %+v", got.Notes)
	}
}

// TestCancelBeforeRunSpendsNothing: a run stopped between being built and being
// started never calls its planner.
func TestCancelBeforeRunSpendsNothing(t *testing.T) {
	var called bool
	planner := plannerFunc(func(context.Context, View) (Amendment, error) {
		called = true
		return Amendment{}, nil
	})
	run := New("goal", planner, execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
		t.Fatalf("nothing may run")
		return "", 0, nil
	}), Options{})
	run.Cancel()
	if _, err := run.Run(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatalf("a run stopped before it started still asked its planner")
	}
}

// TestGateStopIsTheSameStop: the fuel gate's own "stop" answer settles the run
// through Cancel, so a run ended at the gate leaves the trace a run ended from
// its page leaves.
func TestGateStopIsTheSameStop(t *testing.T) {
	run, _, ended := atTheGate(t)
	if err := run.Resolve(GateStop); err != nil {
		t.Fatal(err)
	}
	snap := waitForEnd(t, ended)
	if !snap.Stopped {
		t.Fatalf("stopping at the gate is stopping: %+v", snap)
	}
	if snap.Paused {
		t.Fatalf("an answered gate is not still up")
	}
	if snap.Answer != "" {
		t.Fatalf("stop is somebody declining to pay for one more call, got %q", snap.Answer)
	}
}

// TestCancelledRunTakesNoAmendment: a planner call that was already out when
// somebody stopped the run cannot grow the frontier under them.
func TestCancelledRunTakesNoAmendment(t *testing.T) {
	var (
		asked   = make(chan struct{})
		release = make(chan struct{})
		ended   = make(chan Snapshot, 1)
	)
	planner := plannerFunc(func(context.Context, View) (Amendment, error) {
		select {
		case <-asked:
		default:
			close(asked)
		}
		<-release
		return Amendment{Add: []Node{{ID: "late", Goal: "arrived after the stop"}}}, nil
	})
	run := New("goal", planner, execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
		return "ok", 0, nil
	}), Options{})

	go func() {
		snap, _ := run.Run(testContext(t))
		ended <- snap
	}()
	waitFor(t, asked, "the opening plan to be asked for")
	run.Cancel()
	close(release)

	snap := waitForEnd(t, ended)
	if stateOf(snap, "late") != State(-1) {
		t.Fatalf("an amendment landed on a stopped run: %+v", snap.Nodes)
	}
}

// TestCancelSettlesPromptly guards the one way this could hang: a stop with a
// planner call still in flight must not wait for a thought that will never be
// delivered.
func TestCancelSettlesPromptly(t *testing.T) {
	var (
		running = make(chan struct{})
		ended   = make(chan Snapshot, 1)
	)
	plan := &script{steps: []Amendment{{Add: []Node{{ID: "a", Goal: "work"}}}}}
	planner := plannerFunc(func(ctx context.Context, v View) (Amendment, error) {
		if len(v.Results) > 0 {
			// The completion's own planner call, parked until its context dies.
			<-ctx.Done()
			return Amendment{}, ctx.Err()
		}
		return plan.Plan(ctx, v)
	})
	run := New("goal", planner, execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		if n.ID == "a" {
			close(running)
		}
		return "ok", 0, nil
	}), Options{})

	go func() {
		snap, _ := run.Run(testContext(t))
		ended <- snap
	}()
	waitFor(t, running, "the node to run")
	// Give the completion's planner call a moment to be in flight, then stop.
	time.Sleep(20 * time.Millisecond)
	run.Cancel()

	select {
	case snap := <-ended:
		if !snap.Stopped {
			t.Fatalf("the run ended without saying it was stopped: %+v", snap)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("a stopped run waited on a planner call that will never land")
	}
}
