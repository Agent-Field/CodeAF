package session

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// ── which kind of work an id names ──────────────────────────────────────────

// THE PREFIX IS THE WHOLE ROUTING, and a bare number is a task — the one id
// space every surface on this program already had.
func TestCancelRefusesAnIdItCannotPlace(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, id := range []string{
		"7",          // a task in a session with no graph
		"task:7",     // the same, spelled out
		"run:7",      // a run nobody started
		"harness:2",  // a harness run that is not in flight
		"design:2",   // a design that is not in flight
		"widget:2",   // a kind this session does not have
		"task:",      // a number that is not one
		"task:zero",  // nor is that
		"task:0",     // and nor is zero
		"design:one", // nor is that
	} {
		if line, err := agent.Cancel(id); err == nil {
			t.Errorf("%q was stopped anyway: %q", id, line)
		}
	}
}

// ── a task node ─────────────────────────────────────────────────────────────

// TestCancelCutsARunningTask: the node's context dies, the node lands stopped,
// and the model is told it was STOPPED rather than that it failed.
func TestCancelCutsARunningTask(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	var (
		started = make(chan struct{})
		cut     = make(chan struct{})
	)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ctx, stop := context.WithCancel(context.Background())
		node.setCancel(stop)
		defer stop()
		close(started)
		<-ctx.Done()
		close(cut)
		node.finish("stopped before it finished", nil, "task/one", mergeAborted)
		node.graph.complete(node, TaskFailed)
	})
	updates := agent.TaskUpdates()

	id := graph.reserve()
	graph.admit(id, taskSpec{title: "the long one", brief: "work", acceptance: "done"})
	waitSignal(t, started, "the node to start")

	line, err := agent.Cancel("task:" + itoa64(id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "stopping") || !strings.Contains(line, "branch is kept") {
		t.Fatalf("the line a person is shown reads %q", line)
	}
	waitSignal(t, cut, "the node's context to be cut")

	notice := awaitNotice(t, updates, id, func(n TaskNotice) bool { return n.State == TaskFailed })
	if !notice.Stopped {
		t.Fatalf("the landing does not say a person stopped it: %+v", notice)
	}
	if note := taskNote(notice, "", TaskSettleAsk, landingAddress{person: true}); !strings.Contains(note, "stopped") || strings.Contains(note, "failed") {
		t.Fatalf("the model is told %q — a stop is not a failure", note)
	}
}

// THE STOP IS PUBLISHED WHEN IT IS TAKEN, and not whenever the accounting next
// says something. A running node stays RUNNING for as long as its child takes to
// wind up, which is why the line above promises "stopping" — so the news arrives
// on a notice that carries no state change at all, and a surface with nothing to
// go on drew "working" over work a person had just ended for the whole of that
// window.
func TestARunningNodeSaysItIsStoppingBeforeItLands(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	var (
		started = make(chan struct{})
		hold    = make(chan struct{})
	)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ctx, stop := context.WithCancel(context.Background())
		node.setCancel(stop)
		defer stop()
		close(started)
		<-ctx.Done()
		// THE CHILD WINDS UP SLOWLY, which is the whole state this test is about:
		// a `bash` holding a leaked pipe or a `jobs` kill spends seconds here.
		<-hold
		node.finish("stopped before it finished", nil, "task/one", mergeAborted)
		node.graph.complete(node, TaskFailed)
	})
	updates := agent.TaskUpdates()

	id := graph.reserve()
	graph.admit(id, taskSpec{title: "the long one", brief: "work", acceptance: "done"})
	waitSignal(t, started, "the node to start")

	if _, err := agent.Cancel("task:" + itoa64(id)); err != nil {
		t.Fatal(err)
	}
	notice := awaitNotice(t, updates, id, func(n TaskNotice) bool { return n.Stopped })
	if notice.State != TaskRunning {
		t.Fatalf("the stop was only published once the node had moved to %v", notice.State)
	}
	// AND A SECOND PRESS IS A PERSON LEANING ON A KEY. The answer is what is
	// already happening, in the same word.
	line, err := agent.Cancel("task:" + itoa64(id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "already stopping") {
		t.Fatalf("the second stop answers %q", line)
	}
	close(hold)
	awaitNotice(t, updates, id, func(n TaskNotice) bool { return n.State == TaskFailed })
}

// TestCancelDropsAQueuedTaskInstantly: nothing is running, so nothing has to
// come home first — and the slot the running node holds is not handed back
// under it.
func TestCancelDropsAQueuedTaskInstantly(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	release := make(chan struct{})
	started := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
		node.finish("did it", nil, "", "")
		node.graph.complete(node, TaskDone)
	})
	graph.mu.Lock()
	graph.limit = 1 // one lane, so the second node is genuinely queued
	graph.mu.Unlock()

	first := graph.reserve()
	second := graph.reserve()
	graph.admit(first, taskSpec{title: "the running one", brief: "work", acceptance: "done"})
	graph.admit(second, taskSpec{title: "the queued one", brief: "work", acceptance: "done"})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatalf("the first node never started")
	}

	line, err := agent.Cancel("task:" + itoa64(second))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "before it started") {
		t.Fatalf("a queued node's stop reads %q", line)
	}
	node := graph.node(second)
	if got := node.stateNow(); got != TaskFailed {
		t.Fatalf("the queued node is %q, want settled the moment it was stopped", got)
	}
	if notice := node.notice(); !notice.Stopped || notice.Report != taskStoppedQueuedWord {
		t.Fatalf("the queued node's landing reads %+v", notice)
	}
	// The lane the running node holds is still its own: a node that never ran
	// never took one, and a decrement here would raise the cap for everybody.
	graph.mu.Lock()
	running := graph.running
	graph.mu.Unlock()
	if running != 1 {
		t.Fatalf("%d lanes are in use, want the one the running node took", running)
	}
	close(release)
}

// TestCancelIsIdempotentOverATask: leaning on the key, and pressing it on work
// that has already landed, are both a sentence and nothing else.
func TestCancelIsIdempotentOverATask(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	var cuts int
	var mu sync.Mutex
	started := make(chan struct{})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.setCancel(func() {
			mu.Lock()
			cuts++
			mu.Unlock()
		})
		close(started)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "one", brief: "work", acceptance: "done"})
	waitSignal(t, started, "the node to start")

	for i := 0; i < 3; i++ {
		if _, err := agent.Cancel("task:" + itoa64(id)); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	got := cuts
	mu.Unlock()
	if got != 1 {
		t.Fatalf("the node was cut %d times, want once", got)
	}

	// And once it has settled there is nothing to stop, said plainly.
	graph.complete(graph.node(id), TaskDone)
	line, err := agent.Cancel("task:" + itoa64(id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "already finished") {
		t.Fatalf("stopping settled work reads %q", line)
	}
}

// ── an adaptive run ─────────────────────────────────────────────────────────

// TestCancelStopsAnAdaptiveRun: the session's door reaches the orchestrator's
// own Cancel, and a run that has already settled is a no-op with a note.
func TestCancelStopsAnAdaptiveRun(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	run := orchestrate.New("goal", stoppablePlanner{}, stoppableExec{}, orchestrate.Options{})
	agent.mu.Lock()
	agent.orchestrations = map[string]*orchestration{"3": {run: run, cancel: func() {}}}
	agent.mu.Unlock()

	line, err := agent.Cancel("run:3")
	if err != nil {
		t.Fatal(err)
	}
	if line != orchestrate.StoppedWord {
		t.Fatalf("the line a person is shown reads %q", line)
	}
	if snap, ok := agent.OrchestrateSnapshot("3"); !ok || !snap.Stopped {
		t.Fatalf("the run was not stopped: %+v", snap)
	}

	// Settle it, and the second press says so rather than stopping it again.
	if _, err := run.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	line, err = agent.Cancel("run:3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "already finished") {
		t.Fatalf("stopping a settled run reads %q", line)
	}
}

// THE GATE'S OWN STOP IS THE SAME STOP. One decision, one path, one sentence.
func TestTheGateStopRoutesThroughCancel(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	run := orchestrate.New("goal", stoppablePlanner{}, stoppableExec{}, orchestrate.Options{})
	agent.mu.Lock()
	agent.orchestrations = map[string]*orchestration{"4": {run: run, cancel: func() {}}}
	agent.mu.Unlock()

	line, err := agent.ResolveOrchestrate("4", orchestrate.GateStop)
	if err != nil {
		t.Fatal(err)
	}
	if line != orchestrate.StoppedWord {
		t.Fatalf("the gate's stop said %q", line)
	}
	if snap, _ := agent.OrchestrateSnapshot("4"); !snap.Stopped {
		t.Fatalf("the gate's stop did not stop the run: %+v", snap)
	}
}

// THE NOTE A STOPPED RUN LEAVES IN THE CONVERSATION: what it spent, and how
// much of the graph it got through.
func TestAStoppedRunSaysWhatItSpentAndHowFarItGot(t *testing.T) {
	snap := orchestrate.Snapshot{
		Stopped: true,
		Fuel:    orchestrate.Fuel{Cap: 2, Spent: 0.42},
		Nodes: []orchestrate.NodeStatus{
			{Node: orchestrate.Node{ID: "n1"}, State: orchestrate.Done},
			{Node: orchestrate.Node{ID: "n2"}, State: orchestrate.Done},
			{Node: orchestrate.Node{ID: "n3"}, State: orchestrate.Done},
			{Node: orchestrate.Node{ID: "n4"}, State: orchestrate.Done},
			{Node: orchestrate.Node{ID: "n5"}, State: orchestrate.Done},
			{Node: orchestrate.Node{ID: "n6"}, State: orchestrate.Cancelled},
			{Node: orchestrate.Node{ID: "n7"}, State: orchestrate.Cancelled},
			{Node: orchestrate.Node{ID: "n8"}, State: orchestrate.Cancelled},
			{Node: orchestrate.Node{ID: "n9"}, State: orchestrate.Cancelled},
		},
	}
	if got, want := stoppedRunNote(snap), "stopped — $0.42 spent, 5 of 9 nodes done"; got != want {
		t.Fatalf("the note reads %q, want %q", got, want)
	}
}

// ── a sub-harness run, and a design ─────────────────────────────────────────

// A RUN IS ONLY ON THE REGISTER WHILE IT IS RUNNING, and while it is there the
// stop reaches the context it rides.
func TestCancelEndsAHarnessRunWhileItIsInFlight(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	runCtx, id, ended := agent.beginHarnessRun(context.Background())
	if _, err := agent.Cancel("harness:" + itoa64(id)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("the harness run's context was never cut")
	}
	ended()
	if _, err := agent.Cancel("harness:" + itoa64(id)); err == nil {
		t.Fatalf("a run that has ended is not on the register")
	}
}

// A DESIGN IS NOT A KIND OF ITS OWN ANY MORE, and the id space it used to have
// answers to nothing: a harness being designed is a TASK (harness_task.go), so
// `task:4` is the one way to end one and `design:4` names nothing at all.
func TestADesignIsStoppedAsTheTaskItIs(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := agent.Cancel("design:5"); err == nil {
		t.Fatal("design: still names a kind of work this session can stop")
	}
}

// ── the stand-ins ───────────────────────────────────────────────────────────

// stoppablePlanner and stoppableExec are a run with nothing behind it: these
// tests are about the session's door, and a run that did any work would be a
// test about the orchestrator (its own cancel_test.go has those).
type stoppablePlanner struct{}

func (stoppablePlanner) Plan(context.Context, orchestrate.View) (orchestrate.Amendment, error) {
	return orchestrate.Amendment{}, nil
}

type stoppableExec struct{}

func (stoppableExec) Exec(context.Context, orchestrate.Node, []orchestrate.NodeStatus) (string, float64, error) {
	return "", 0, nil
}

func waitSignal(t *testing.T, signal <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", why)
	}
}

// awaitNotice reads the task lane until one node's update satisfies want.
func awaitNotice(t *testing.T, lane <-chan Event, id uint64, want func(TaskNotice) bool) TaskNotice {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, open := <-lane:
			if !open {
				t.Fatalf("the task lane closed before task %d said anything expected", id)
			}
			if event.Task == nil || event.Task.ID != id || !want(*event.Task) {
				continue
			}
			return *event.Task
		case <-deadline:
			t.Fatalf("task %d never sent the update this test is about", id)
			return TaskNotice{}
		}
	}
}

func itoa64(n uint64) string { return strconv.FormatUint(n, 10) }
