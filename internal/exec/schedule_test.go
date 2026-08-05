package exec

import (
	"context"
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

func (b *blockingExecutor) Skill() string { return "linear" }

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
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 4)

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
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 1)
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
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 4)
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

func (w *wedgedExecutor) Skill() string { return "linear" }

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

	scheduler := NewScheduler(NewRegistry(panickyExecutor{}), workspace(t), 1)
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

func (panickyExecutor) Skill() string { return "linear" }
func (panickyExecutor) Run(ctx context.Context, task Task) (*Outcome, error) {
	panic("boom")
}
