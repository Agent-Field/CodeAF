package exec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// blockingExecutor completes the nodes in fast immediately and holds every
// other node until its context is cancelled, reporting when the first slow
// node has actually started.
type blockingExecutor struct {
	fast    map[int]bool
	started chan int

	mutex sync.Mutex
	ran   []int
}

func (b *blockingExecutor) Subharness() string { return "linear" }

func (b *blockingExecutor) Run(ctx context.Context, task Task) (*Outcome, error) {
	b.mutex.Lock()
	b.ran = append(b.ran, task.NodeID)
	b.mutex.Unlock()
	if b.fast[task.NodeID] {
		return &Outcome{
			Text:  "done " + task.Title,
			Turns: 1,
			Stop:  StopDone,
			Usage: Usage{Calls: 1, PromptTokens: 100, CompletionTokens: 10},
		}, nil
	}
	if b.started != nil {
		b.started <- task.NodeID
	}
	<-ctx.Done()
	// A real leaf lands on cancellation: partial outcome plus the error.
	return &Outcome{Stop: StopDeadline, Turns: 2, Usage: Usage{Calls: 2, PromptTokens: 50}}, ctx.Err()
}

// TestCancelledRunLandsWithOutcomesAndStopReason is the "never die silently"
// contract. A run whose context is cancelled mid-flight must still record what
// finished, what was in flight, and what never started — and report why it
// stopped — rather than returning a bare context error over a half-annotated
// graph.
func TestCancelledRunLandsWithOutcomesAndStopReason(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	finished := graph.Add(plan.Node{Stage: 1, Title: "Finished"})
	wedged := graph.Add(plan.Node{Stage: 1, Title: "Wedged"})
	dependent := graph.Add(plan.Node{Stage: 1, Title: "Dependent"})
	if err := graph.AddNeed(dependent, wedged); err != nil {
		t.Fatal(err)
	}

	fake := &blockingExecutor{fast: map[int]bool{finished: true}, started: make(chan int, 1)}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 4).WithGovernor(calmGovernor())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-fake.started // the slow node is genuinely in flight
		cancel()
	}()
	err := scheduler.Run(ctx, graph)

	if err == nil {
		t.Fatal("a cancelled run returned nil; the stop must be reported")
	}
	if !strings.Contains(err.Error(), "run stopped") || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error %q does not state the stop reason", err)
	}
	if state := graph.Node(finished).State; state != plan.StateDone {
		t.Errorf("finished node = %s, want done — completed work was lost", state)
	}
	wedgedNode := graph.Node(wedged)
	if wedgedNode.State != plan.StateFailed {
		t.Errorf("in-flight node = %s, want failed with an explanation", wedgedNode.State)
	}
	if wedgedNode.Turns != 2 {
		t.Errorf("in-flight node's partial outcome was discarded: turns = %d, want 2", wedgedNode.Turns)
	}
	dependentNode := graph.Node(dependent)
	if dependentNode.State == plan.StatePending || dependentNode.State == plan.StateRunning {
		t.Errorf("never-started node = %s, want a terminal state naming why", dependentNode.State)
	}
	if dependentNode.Failure == "" {
		t.Error("never-started node carries no explanation")
	}
	if got := scheduler.Usage().PromptTokens; got != 150 {
		t.Errorf("usage = %d prompt tokens, want 150 — spend from landed nodes must survive a cancel", got)
	}
}

// TestGlobalBudgetStopsLaunchingAndLands guards the run-wide spend limit. Once
// cumulative spend passes the budget nothing new may launch; what is in flight
// lands normally; the stop reason names the budget; nodes that never ran say
// so.
func TestGlobalBudgetStopsLaunchingAndLands(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	var ids []int
	for _, title := range []string{"A", "B", "C", "D"} {
		ids = append(ids, graph.Add(plan.Node{Stage: 1, Title: title}))
	}

	fake := &blockingExecutor{fast: map[int]bool{ids[0]: true, ids[1]: true, ids[2]: true, ids[3]: true}}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 1).WithGovernor(calmGovernor())
	scheduler.Budget = 150 // two nodes at 110 tokens each cross it

	err := scheduler.Run(context.Background(), graph)
	if err == nil {
		t.Fatal("a budget-stopped run returned nil; the stop must be reported")
	}
	if !strings.Contains(err.Error(), "global budget exhausted") {
		t.Errorf("error %q does not name the global budget", err)
	}

	var done, neverRan int
	for _, id := range ids {
		switch node := graph.Node(id); node.State {
		case plan.StateDone:
			done++
		case plan.StateBlocked:
			neverRan++
			if !strings.Contains(node.Failure, "never started") {
				t.Errorf("stopped node's failure %q does not say it never started", node.Failure)
			}
		default:
			t.Errorf("node %d = %s, want done or blocked", id, node.State)
		}
	}
	if done != 2 {
		t.Errorf("%d nodes ran, want exactly 2 before the budget tripped", done)
	}
	if neverRan != 2 {
		t.Errorf("%d nodes marked never started, want 2", neverRan)
	}
	fake.mutex.Lock()
	launched := len(fake.ran)
	fake.mutex.Unlock()
	if launched != 2 {
		t.Errorf("executor was invoked %d times, want 2 — the budget must stop launches, not just mark nodes", launched)
	}
}

type landingExecutor struct {
	first, second int
	secondStarted chan struct{}
	releaseSecond chan struct{}

	mutex sync.Mutex
	ran   []int
}

func (e *landingExecutor) Subharness() string { return "linear" }

func (e *landingExecutor) Run(_ context.Context, task Task) (*Outcome, error) {
	e.mutex.Lock()
	e.ran = append(e.ran, task.NodeID)
	e.mutex.Unlock()
	if task.NodeID == e.first {
		<-e.secondStarted
	}
	if task.NodeID == e.second {
		close(e.secondStarted)
		<-e.releaseSecond
	}
	return &Outcome{Text: "landed", Turns: 1, Stop: StopDone, Usage: Usage{Calls: 1}}, nil
}

func TestBeforeLaunchStopsClaimsAndLandsInflightWork(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	first := graph.Add(plan.Node{Stage: 1, Title: "First"})
	second := graph.Add(plan.Node{Stage: 1, Title: "Second"})
	third := graph.Add(plan.Node{Stage: 1, Title: "Third"})
	fake := &landingExecutor{first: first, second: second, secondStarted: make(chan struct{}), releaseSecond: make(chan struct{})}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 2).WithGovernor(calmGovernor())
	stop := errors.New("daily rail pause")
	checks := 0
	scheduler.BeforeLaunch = func(context.Context) error {
		checks++
		if checks == 3 {
			close(fake.releaseSecond)
			return stop
		}
		return nil
	}
	err := scheduler.Run(context.Background(), graph)
	if !errors.Is(err, stop) {
		t.Fatalf("run error = %v, want wrapped rail pause", err)
	}
	if graph.Node(first).State != plan.StateDone || graph.Node(second).State != plan.StateDone {
		t.Fatalf("in-flight states = %s/%s, want done/done", graph.Node(first).State, graph.Node(second).State)
	}
	if graph.Node(third).State != plan.StateBlocked {
		t.Fatalf("unclaimed node = %s, want blocked", graph.Node(third).State)
	}
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if len(fake.ran) != 2 {
		t.Fatalf("executor ran %d leaves, want two in flight only", len(fake.ran))
	}
}

// TestWatchdogAbandonsWedgedExecutor covers the hang that once froze a real
// run forever: an executor stuck past every deadline it was given. The
// scheduler must record the node as failed and finish the run rather than
// waiting silently for a completion that never comes.
func TestWatchdogAbandonsWedgedExecutor(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	stuck := graph.Add(plan.Node{Stage: 1, Title: "Stuck"})
	healthy := graph.Add(plan.Node{Stage: 1, Title: "Healthy"})

	release := make(chan struct{})
	defer close(release)
	fake := &wedgedExecutor{healthy: healthy, release: release}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 4).WithGovernor(calmGovernor())
	scheduler.NodeTimeout = 100 * time.Millisecond

	finished := make(chan error, 1)
	go func() { finished <- scheduler.Run(context.Background(), graph) }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the scheduler never returned; a wedged executor froze the run")
	}

	stuckNode := graph.Node(stuck)
	if stuckNode.State != plan.StateFailed {
		t.Errorf("wedged node = %s, want failed", stuckNode.State)
	}
	if !strings.Contains(stuckNode.Failure, "did not return") {
		t.Errorf("wedged node's failure %q does not explain the abandonment", stuckNode.Failure)
	}
	if state := graph.Node(healthy).State; state != plan.StateDone {
		t.Errorf("healthy node = %s, want done — one wedged node must not stop the rest", state)
	}
}

// wedgedExecutor ignores its context entirely for every node but the healthy
// one — the shape of a goroutine stuck in an uninterruptible wait.
type wedgedExecutor struct {
	healthy int
	release chan struct{}
}

func (w *wedgedExecutor) Subharness() string { return "linear" }

func (w *wedgedExecutor) Run(ctx context.Context, task Task) (*Outcome, error) {
	if task.NodeID == w.healthy {
		return &Outcome{Text: "ok", Turns: 1, Stop: StopDone, Usage: Usage{Calls: 1}}, nil
	}
	<-w.release // held far past NodeTimeout, released only at test teardown
	return &Outcome{Stop: StopError}, context.Canceled
}

// TestExecutorPanicIsARecordedFailure keeps a programming error in one
// executor from killing the whole process or stranding the scheduler.
func TestExecutorPanicIsARecordedFailure(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	doomed := graph.Add(plan.Node{Stage: 1, Title: "Doomed"})

	scheduler := NewScheduler(NewRegistry(panickyExecutor{}), workspace(t), 1).WithGovernor(calmGovernor())
	if err := scheduler.Run(context.Background(), graph); err != nil {
		t.Fatalf("Run: %v", err)
	}
	node := graph.Node(doomed)
	if node.State != plan.StateFailed {
		t.Fatalf("node = %s, want failed", node.State)
	}
	if !strings.Contains(node.Failure, "panicked") {
		t.Errorf("failure %q does not say the executor panicked", node.Failure)
	}
}

type panickyExecutor struct{}

func (panickyExecutor) Subharness() string { return "linear" }
func (panickyExecutor) Run(ctx context.Context, task Task) (*Outcome, error) {
	panic("boom")
}

// TestLeafShapeSeparatesTheKindsOfLeaf is defect 2's other half: the router can
// only key a leaf rating on a population the scheduler names, and the naming has
// to fall along the axis the failures actually fell on.
//
// Arm B had one class for every leaf, so five budget stops on t1 — the task
// whose leaves carry 2.2M prompt tokens into a 300k budget — rerouted t2's
// document reading and t3's small repairs, where the demoted model had never
// failed once. Size is that axis and Kind is the one it cannot see: a synthesis
// node has no size at all, and is a roll-up over many long inputs rather than a
// job, which is a different population again.
func TestLeafShapeSeparatesTheKindsOfLeaf(t *testing.T) {
	cases := []struct {
		node plan.Node
		want string
	}{
		{plan.Node{Kind: plan.KindWork, Size: plan.SizeAtomic}, "atomic"},
		{plan.Node{Kind: plan.KindWork, Size: plan.SizeUnknown}, "atomic"},
		// Borderline sits with oversized because the risk it names is the same
		// risk, and erring that way keeps a lesson learned on a doubtful leaf
		// away from the leaves nobody doubted.
		{plan.Node{Kind: plan.KindWork, Size: plan.SizeBorderline}, "oversized"},
		{plan.Node{Kind: plan.KindWork, Size: plan.SizeOversized}, "oversized"},
		{plan.Node{Kind: plan.KindSynthesis, Size: plan.SizeUnknown}, "synthesis"},
	}
	buckets := map[string]bool{}
	for _, item := range cases {
		got := leafShape(&item.node)
		if got != item.want {
			t.Errorf("leafShape(%s/%s) = %q, want %q", item.node.Kind, item.node.Size, got, item.want)
		}
		buckets[got] = true
	}
	// Three, and the count is the design. A key fine enough to name every node
	// is a key no bucket ever fills, and a rating that never reaches
	// router.MinGraded has learned nothing at all — expensively.
	if len(buckets) != 3 {
		t.Fatalf("leaves fall into %d buckets, want three", len(buckets))
	}
}

// countingExecutor records how many leaves the scheduler had running at once,
// and can hold every leaf until a barrier of them has arrived.
type countingExecutor struct {
	mutex   sync.Mutex
	running int
	peak    int
	arrived chan struct{}
	release chan struct{}
}

func (c *countingExecutor) Subharness() string { return "linear" }

func (c *countingExecutor) Run(ctx context.Context, task Task) (*Outcome, error) {
	c.mutex.Lock()
	c.running++
	if c.running > c.peak {
		c.peak = c.running
	}
	c.mutex.Unlock()
	if c.arrived != nil {
		c.arrived <- struct{}{}
		select {
		case <-c.release:
		case <-ctx.Done():
		}
	}
	c.mutex.Lock()
	c.running--
	c.mutex.Unlock()
	return &Outcome{Text: "done", Turns: 1, Stop: StopDone, Usage: Usage{Calls: 1}}, nil
}

// TestSchedulerLaunchesEveryReadyAPIBoundLeafOnASaturatedHost is the headless
// half of the admission doctrine. The gate used to be the host's load average
// for every leaf, and a leaf here is a goroutine parked on a socket waiting for
// a model — so `aforge run --concurrency 8` on a machine somebody else was
// compiling on launched three leaves and then waited, forever, for a reading
// that its own idle sockets could never bring down. Concurrency belongs to the
// graph: a node runs when the nodes it needs have landed. What the panel asked
// for is the bound; the host is not consulted about this class at all.
func TestSchedulerLaunchesEveryReadyAPIBoundLeafOnASaturatedHost(t *testing.T) {
	leaves := GovernorLocalFloor + 3
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	for index := range leaves {
		graph.Add(plan.Node{Stage: 1, Title: fmt.Sprintf("leaf-%d", index)})
	}
	fake := &countingExecutor{
		arrived: make(chan struct{}, leaves),
		release: make(chan struct{}),
	}

	// Ten times over the load ceiling, and irrelevant: nothing these leaves do
	// touches a core.
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), leaves+4).WithGovernor(saturatedGovernor())
	stopped := make(chan error, 1)
	go func() { stopped <- scheduler.Run(context.Background(), graph) }()
	for launched := range leaves {
		select {
		case <-fake.arrived:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of %d independent leaves launched: host load throttled work that costs the host nothing",
				launched, leaves)
		}
	}
	close(fake.release)
	if err := <-stopped; err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, node := range graph.Nodes {
		if node.State != plan.StateDone {
			t.Fatalf("node %d = %s, want done", node.ID, node.State)
		}
	}
	fake.mutex.Lock()
	peak := fake.peak
	fake.mutex.Unlock()
	if peak != leaves {
		t.Fatalf("peak concurrency = %d, want all %d independent leaves at once", peak, leaves)
	}
}
