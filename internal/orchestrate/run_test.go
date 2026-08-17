package orchestrate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── the stand-ins ───────────────────────────────────────────────────────────

// plannerFunc is a planner written as one function: these tests are about what
// the scheduler does with an amendment, never about how a model produced one.
type plannerFunc func(ctx context.Context, v View) (Amendment, error)

func (f plannerFunc) Plan(ctx context.Context, v View) (Amendment, error) { return f(ctx, v) }

// execFunc is one node's work.
type execFunc func(ctx context.Context, n Node, deps []NodeStatus) (string, float64, error)

func (f execFunc) Exec(ctx context.Context, n Node, deps []NodeStatus) (string, float64, error) {
	return f(ctx, n, deps)
}

// script is the planner these tests mostly use: an opening amendment, then a
// reply per completion, then done. It records every view it was shown.
type script struct {
	mu    sync.Mutex
	steps []Amendment
	seen  []View
	at    int
}

func (s *script) Plan(_ context.Context, v View) (Amendment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, v)
	if s.at >= len(s.steps) {
		return Amendment{}, nil
	}
	step := s.steps[s.at]
	s.at++
	return step, nil
}

func (s *script) views() []View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]View(nil), s.seen...)
}

func done(brief string) Amendment { return Amendment{Done: &DonePlan{Brief: brief}} }

func waitFor(t *testing.T, signal <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", why)
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ── the scheduler ───────────────────────────────────────────────────────────

// TestSchedulerUnlocksOnCompletion is the no-barrier law: a node whose needs
// are met starts the moment they are met, with a sibling still running. A wave
// scheduler would hold c until b finished, and this test would time out.
func TestSchedulerUnlocksOnCompletion(t *testing.T) {
	var (
		hold     = make(chan struct{})
		cStarted = make(chan struct{})
	)
	plan := &script{steps: []Amendment{{Add: []Node{
		{ID: "a", Goal: "first"},
		{ID: "b", Goal: "long"},
		{ID: "c", Goal: "after a", Needs: []string{"a"}},
	}}}}
	var finished sync.WaitGroup
	finished.Add(1)
	planner := plannerFunc(func(ctx context.Context, v View) (Amendment, error) {
		amendment, err := plan.Plan(ctx, v)
		if len(v.Results) == 3 {
			return done("say what happened"), nil
		}
		return amendment, err
	})
	exec := execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		switch n.ID {
		case "b":
			<-hold
		case "c":
			close(cStarted)
		}
		return n.ID + " is done", 0, nil
	})

	run := New("the goal", planner, exec, Options{})
	var snap Snapshot
	go func() {
		defer finished.Done()
		snap, _ = run.Run(testContext(t))
	}()

	waitFor(t, cStarted, "c to start while b is still running")
	close(hold)
	finished.Wait()

	if !snap.Done {
		t.Fatalf("run did not finish: %+v", snap)
	}
	if snap.Answer == "" {
		t.Fatalf("a finished run has a synthesis")
	}
	for _, node := range snap.Nodes {
		if node.State != Done {
			t.Fatalf("node %q ended %v", node.ID, node.State)
		}
		if node.Digest == "" {
			t.Fatalf("node %q kept no digest", node.ID)
		}
	}
}

// TestLanesBoundConcurrency: the frontier may be wide and the run is not.
func TestLanesBoundConcurrency(t *testing.T) {
	var (
		mu     sync.Mutex
		now    int
		most   int
		inside = make(chan struct{}, 8)
	)
	plan := &script{steps: []Amendment{{Add: []Node{
		{ID: "n1", Goal: "one"}, {ID: "n2", Goal: "two"},
		{ID: "n3", Goal: "three"}, {ID: "n4", Goal: "four"},
	}}}}
	planner := plannerFunc(func(ctx context.Context, v View) (Amendment, error) {
		if len(v.Results) == 4 {
			return done("done"), nil
		}
		return plan.Plan(ctx, v)
	})
	exec := execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		if n.ID == SynthesisID {
			return "answer", 0, nil
		}
		mu.Lock()
		now++
		if now > most {
			most = now
		}
		mu.Unlock()
		inside <- struct{}{}
		time.Sleep(20 * time.Millisecond)
		<-inside
		mu.Lock()
		now--
		mu.Unlock()
		return "ok", 0, nil
	})

	run := New("goal", planner, exec, Options{Lanes: 2})
	if _, err := run.Run(testContext(t)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if most > 2 {
		t.Fatalf("two lanes ran %d nodes at once", most)
	}
}

// TestFailedNodeReachesThePlanner: a node that broke is a FACT the next view
// carries, and it never becomes a met need.
func TestFailedNodeReachesThePlanner(t *testing.T) {
	plan := &script{steps: []Amendment{{Add: []Node{
		{ID: "a", Goal: "breaks"},
		{ID: "b", Goal: "waits on a", Needs: []string{"a"}},
	}}}}
	var sawFailure bool
	planner := plannerFunc(func(ctx context.Context, v View) (Amendment, error) {
		for _, result := range v.Results {
			if result.ID == "a" && result.State == Failed && strings.Contains(result.Err, "the disk is gone") {
				sawFailure = true
			}
		}
		if sawFailure {
			return done("say what broke"), nil
		}
		return plan.Plan(ctx, v)
	})
	var ranB bool
	exec := execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		switch n.ID {
		case "a":
			return "", 0, errors.New("the disk is gone")
		case "b":
			ranB = true
		}
		return "ok", 0, nil
	})

	snap, err := New("goal", planner, exec, Options{}).Run(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if !sawFailure {
		t.Fatalf("the planner was never shown the failure")
	}
	if ranB {
		t.Fatalf("b ran behind a need that failed")
	}
	if state := stateOf(snap, "b"); state != Queued {
		t.Fatalf("b ended %v, want queued behind its dead need", state)
	}
}

// TestRunSettlesWhenNothingIsLaunchable: no DonePlan, a frontier that cannot
// move — the run still ends, and it ends with a synthesis over what happened.
func TestRunSettlesWhenNothingIsLaunchable(t *testing.T) {
	plan := &script{steps: []Amendment{{Add: []Node{
		{ID: "a", Goal: "runs"},
		{ID: "b", Goal: "never", Needs: []string{"a"}},
	}}}}
	exec := execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		if n.ID == "a" {
			return "", 0, errors.New("no")
		}
		return "the answer", 0, nil
	})
	out, err := New("goal", plan, exec, Options{}).Run(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Done {
		t.Fatalf("the run never settled: %+v", out)
	}
	if stateOf(out, "b") != Queued {
		t.Fatalf("b is %v, want left standing on the frontier", stateOf(out, "b"))
	}
}

// TestSteerLandsInTheNextView: a line typed at a run is on every later view,
// and on the snapshot.
func TestSteerLandsInTheNextView(t *testing.T) {
	const line = "do the other one first"
	var (
		running = make(chan struct{})
		steered = make(chan struct{})
		plan    = &script{steps: []Amendment{{Add: []Node{{ID: "a", Goal: "work"}}}}}
	)
	planner := plannerFunc(func(ctx context.Context, v View) (Amendment, error) {
		amendment, err := plan.Plan(ctx, v)
		if len(v.Results) == 1 {
			return done("done"), nil
		}
		return amendment, err
	})
	run := New("goal", planner, execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		if n.ID == "a" {
			// The steer lands while the node is running, which is the only
			// moment a person actually types one.
			close(running)
			<-steered
		}
		return "ok", 0, nil
	}), Options{})

	go func() {
		<-running
		run.Steer(line)
		close(steered)
	}()

	snap, err := run.Run(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Steer) != 1 || snap.Steer[0] != line {
		t.Fatalf("steering is not on the snapshot: %+v", snap.Steer)
	}
	views := plan.views()
	if len(views) < 2 {
		t.Fatalf("the planner was called %d times, want the opening call and the completion", len(views))
	}
	for _, carried := range views[1].Steer {
		if carried == line {
			return
		}
	}
	t.Fatalf("the view after the steer did not carry it: %+v", views[1].Steer)
}

// TestOpeningPlanFailureIsTheOneFatalOne.
func TestOpeningPlanFailureIsTheOneFatalOne(t *testing.T) {
	planner := plannerFunc(func(context.Context, View) (Amendment, error) {
		return Amendment{}, errors.New("the model is down")
	})
	exec := execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
		t.Fatalf("nothing may run without a plan")
		return "", 0, nil
	})
	if _, err := New("goal", planner, exec, Options{}).Run(testContext(t)); err == nil {
		t.Fatalf("an opening plan that failed is the run failing")
	}
}

// TestLaterPlanFailureIsANote: a planner that dies mid-run does not take the
// frontier with it.
func TestLaterPlanFailureIsANote(t *testing.T) {
	var calls int
	var mu sync.Mutex
	planner := plannerFunc(func(_ context.Context, v View) (Amendment, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return Amendment{Add: []Node{{ID: "a", Goal: "work"}}}, nil
		}
		return Amendment{}, errors.New("the model is down")
	})
	exec := execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		return "ok", 0, nil
	})
	snap, err := New("goal", planner, exec, Options{}).Run(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if stateOf(snap, "a") != Done {
		t.Fatalf("the node did not run: %+v", snap.Nodes)
	}
	var noted bool
	for _, note := range snap.Notes {
		if strings.Contains(note, "the planner did not answer") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("a planner that died said nothing: %+v", snap.Notes)
	}
}

// TestSnapshotEvolves walks the states one run passes through.
func TestSnapshotEvolves(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	plan := &script{steps: []Amendment{{
		Add:  []Node{{ID: "a", Goal: "work"}},
		Note: "starting with one node",
	}}}
	planner := plannerFunc(func(ctx context.Context, v View) (Amendment, error) {
		if len(v.Results) == 1 {
			return done("wrap up"), nil
		}
		return plan.Plan(ctx, v)
	})
	run := New("the goal", planner, execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		if n.ID == "a" {
			close(started)
			<-release
			return "what a found", 0.25, nil
		}
		return "the write-up", 0.05, nil
	}), Options{Cap: 10})

	go func() { run.Run(testContext(t)) }()
	waitFor(t, started, "the node to start")
	mid := run.Snapshot()
	if stateOf(mid, "a") != Running {
		t.Fatalf("a is %v mid-run", stateOf(mid, "a"))
	}
	if mid.Done {
		t.Fatalf("a run with work in flight is not done")
	}
	close(release)

	deadline := time.After(5 * time.Second)
	for {
		snap := run.Snapshot()
		if snap.Done {
			if snap.Answer != "the write-up" {
				t.Fatalf("the synthesis is the answer, got %q", snap.Answer)
			}
			if snap.Fuel.Spent < 0.29 {
				t.Fatalf("every call bills the tank, got %v", snap.Fuel.Spent)
			}
			if len(snap.Notes) == 0 || snap.Notes[0] != "starting with one node" {
				t.Fatalf("the planner's note is not on the snapshot: %+v", snap.Notes)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("the run never finished: %+v", snap)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func stateOf(snap Snapshot, id string) State {
	for _, node := range snap.Nodes {
		if node.ID == id {
			return node.State
		}
	}
	return State(-1)
}
