package resident

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// quietLog keeps recovered stacks out of the test output and hands back what
// the guard wrote.
func quietLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	return buffer
}

// TestRunnerSurvivesPanickingLeaf is the whole law in one test: a leaf that
// panics must land as a failure, release its claim, say so once in the thread,
// and leave the runner draining the rest of the board.
func TestRunnerSurvivesPanickingLeaf(t *testing.T) {
	logged := quietLog(t)
	s := openRunnerStore(t)
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "faulty", Brief: "slice a blind id", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "a leaf that faults"}); err != nil {
		t.Fatalf("splice faulty: %v", err)
	}
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "healthy", Brief: "ordinary work", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "a leaf that does not"}); err != nil {
		t.Fatalf("splice healthy: %v", err)
	}

	var mu sync.Mutex
	ran := map[string]bool{}
	runner := NewRunner(s, func(ctx context.Context, node store.Node) (ExecResult, error) {
		mu.Lock()
		ran[node.ID] = true
		mu.Unlock()
		if node.ID == "faulty" {
			id := node.ID
			return ExecResult{}, ranOutOfRange(id)
		}
		return ExecResult{Summary: "did " + node.Brief}, nil
	}, "fault-runner", 1)

	ctx := context.Background()
	for pass := 0; pass < 3; pass++ {
		if _, err := runner.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", pass, err)
		}
		runner.Wait()
	}

	faulty, found, err := s.Node("faulty")
	if err != nil || !found {
		t.Fatalf("read faulty: found=%t err=%v", found, err)
	}
	if faulty.Status != store.Failed {
		t.Fatalf("the faulted leaf did not land failed: %+v", faulty)
	}
	if !strings.Contains(faulty.Error, "internal fault") {
		t.Fatalf("failure text does not name the fault: %q", faulty.Error)
	}
	if len(runner.slots) != 0 {
		t.Fatalf("the faulted leaf leaked its worker slot: %d held", len(runner.slots))
	}

	mu.Lock()
	healthyRan := ran["healthy"]
	mu.Unlock()
	if !healthyRan {
		t.Fatal("the rest of the board stalled behind the faulted leaf")
	}
	healthy, found, err := s.Node("healthy")
	if err != nil || !found || healthy.Status != store.Done {
		t.Fatalf("healthy leaf = %+v found=%t err=%v", healthy, found, err)
	}

	messages, err := s.Messages("s1", 0, 50)
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	notices := 0
	for _, message := range messages {
		if message.Body == faultNotice && message.NodeID == "faulty" {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("expected exactly one quiet fault notice, got %d", notices)
	}
	if !strings.Contains(logged.String(), "resident/runner leaf faulty") {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

// ranOutOfRange reproduces the shape that killed a live surface: a blind slice
// on an id with no plan prefix.
func ranOutOfRange(id string) error {
	cut := strings.LastIndex(id, "-n")
	_ = id[:cut]
	return nil
}

// TestRunnerSurvivesPanickingLanding covers the other half of the worker: a
// fault after execution returns must still settle the node, never strand it
// claimed where no later tick will look.
func TestRunnerSurvivesPanickingLanding(t *testing.T) {
	quietLog(t)
	s := openRunnerStore(t)
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "landing", Brief: "land badly", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "landing fault"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	runner := NewRunner(s, func(ctx context.Context, node store.Node) (ExecResult, error) {
		// A service request with no registry behind it faults where the landing
		// stops it — standing in for any step between execution and completion.
		return ExecResult{Summary: "done", ServiceRequests: []executor.ServiceRequest{{Name: "ghost"}}}, nil
	}, "landing-runner", 1)

	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	runner.Wait()

	node, found, err := s.Node("landing")
	if err != nil || !found {
		t.Fatalf("read landing: found=%t err=%v", found, err)
	}
	if node.Status != store.Failed {
		t.Fatalf("landing fault did not settle the node: %+v", node)
	}
	if len(runner.slots) != 0 {
		t.Fatalf("the landing fault leaked its worker slot: %d held", len(runner.slots))
	}
}

// TestReconcilerSurvivesPanickingTick proves the loop outlives one bad pass:
// the command is still pending and the next tick reaches it again.
func TestReconcilerSurvivesPanickingTick(t *testing.T) {
	logged := quietLog(t)
	graph := openStore(t)
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "s1", Kind: store.CommandSplice, Instruction: "do the thing",
	}); err != nil {
		t.Fatalf("request command: %v", err)
	}

	compiled := 0
	reconciler := New(graph, func(context.Context, string, string) (Compiled, error) {
		compiled++
		panic("the compiler blew up")
	}, nil)

	for pass := 0; pass < 2; pass++ {
		if err := reconciler.tickGuarded(context.Background()); err != nil {
			t.Fatalf("pass %d escaped as an error: %v", pass, err)
		}
	}
	if compiled != 2 {
		t.Fatalf("the tick after the fault never reached the command: %d passes", compiled)
	}
	if !strings.Contains(logged.String(), "resident/reconciler tick") {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

// TestRunnerTickFaultDoesNotLeakASlot proves a fault between taking a worker
// slot and spawning gives back both the slot and the claim, so the runner
// neither starves one worker per fault nor strands the node it had claimed.
func TestRunnerTickFaultDoesNotLeakASlot(t *testing.T) {
	quietLog(t)
	s := openRunnerStore(t)
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "practice-leaf", Brief: "practice work", Stage: 1, Group: store.PracticeGroup},
	}}, store.Provenance{Origin: store.OriginSelf, Intent: "practice"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{Summary: "practised"}, nil
	}, "slot-runner", 1)
	// A runner assembled without its constructor faults exactly where it
	// registers practice cancellation — after the slot and claim are taken.
	runner.activePractice = nil

	// The dispatch loop's two rails, driven exactly as Serve drives them, so
	// this fault injection also pins what a fault is allowed to cost.
	gate := newRunnerQuietGate()
	faults := newRunnerFaultBackoff()
	now := time.Now()
	for pass := 0; pass < 3; pass++ {
		dispatched, faulted, err := runner.tickGuarded(context.Background())
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if dispatched != 0 {
			t.Fatalf("pass %d dispatched %d", pass, dispatched)
		}
		if !faulted {
			t.Fatalf("pass %d reported a clean pass over a panicking one", pass)
		}
		watermark, watermarkErr := s.LatestEventSeq()
		settleTimedPass(&gate, &faults, watermark, watermarkErr, dispatched, faulted, now)
		// A fault is not proof that a timed pass would find nothing, so it may
		// never arm the quiet gate — the bug this pins slept 15s on one panic.
		if gate.skip(watermark, watermarkErr, now) {
			t.Fatalf("pass %d armed the quiet gate off a fault", pass)
		}
		// The first fault costs nothing: the next tick, 500ms later, reads the
		// same durable graph and tries again. Only a repeat is made to wait.
		if held := faults.hold(now); held != (pass > 0) {
			t.Fatalf("pass %d hold = %t, want %t", pass, held, pass > 0)
		}
		runner.Wait()
	}
	if wait := runnerFaultWait(faults.consecutive); wait != 2*runnerFaultStep {
		t.Fatalf("third consecutive fault waits %s, want %s", wait, 2*runnerFaultStep)
	}
	if runnerFaultWait(99) != runnerFaultCap {
		t.Fatalf("the fault backoff does not cap at %s", runnerFaultCap)
	}
	if len(runner.slots) != 0 {
		t.Fatalf("slots leaked: %d held", len(runner.slots))
	}

	node, found, err := s.Node("practice-leaf")
	if err != nil || !found {
		t.Fatalf("read practice leaf: found=%t err=%v", found, err)
	}
	if node.Status != store.Pending {
		t.Fatalf("the claimed node was stranded at %v", node.Status)
	}

	// With the map back, the same runner drains the same node — and one clean
	// pass clears the rail a run of faults built up.
	runner.activePractice = map[string]context.CancelFunc{}
	dispatched, faulted, err := runner.tickGuarded(context.Background())
	if err != nil || faulted || dispatched != 1 {
		t.Fatalf("recovered tick: dispatched=%d faulted=%t err=%v", dispatched, faulted, err)
	}
	watermark, watermarkErr := s.LatestEventSeq()
	settleTimedPass(&gate, &faults, watermark, watermarkErr, dispatched, faulted, now)
	if faults.hold(now) || faults.consecutive != 0 {
		t.Fatalf("a clean pass did not clear the fault rail: %+v", faults)
	}
	// A pass that dispatched has freed no slot yet, so it may not arm the gate
	// either — its own landing is the next thing that moves the journal.
	if gate.skip(watermark, watermarkErr, now) {
		t.Fatalf("a dispatching pass armed the quiet gate")
	}
	runner.Wait()
	node, _, err = s.Node("practice-leaf")
	if err != nil {
		t.Fatalf("read practice leaf: %v", err)
	}
	if node.Status != store.Done {
		t.Fatalf("the runner did not recover: %+v", node)
	}
}
