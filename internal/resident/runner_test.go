package resident

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func openRunnerStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func spliceChain(t *testing.T, s *store.Store) {
	t.Helper()
	// The deliverable owns its subtree: the root node is the goal, the child
	// is the work feeding it, so children land first and the parent last.
	err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "second", Brief: "write", Stage: 2,
			Needs: []store.Need{{NodeID: "first", Kind: store.FeedsInto}}},
		{ID: "first", Parent: "second", Brief: "gather", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "do the thing"})
	if err != nil {
		t.Fatalf("splice chain: %v", err)
	}
}

func TestRunnerExecutesDependencyChain(t *testing.T) {
	s := openRunnerStore(t)
	spliceChain(t, s)

	var mu sync.Mutex
	ran := make([]string, 0, 2)
	runner := NewRunner(s, func(ctx context.Context, node store.Node) (ExecResult, error) {
		mu.Lock()
		ran = append(ran, node.ID)
		mu.Unlock()
		return ExecResult{Summary: "did " + node.Brief, PromptTokens: 100, CompletionTokens: 20, Cost: 0.01}, nil
	}, "test-runner", 2)

	ctx := context.Background()
	if _, err := runner.Tick(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	runner.Wait()
	if _, err := runner.Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	runner.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 || ran[0] != "first" || ran[1] != "second" {
		t.Fatalf("expected chain order [first second], got %v", ran)
	}
	second, ok, err := s.Node("second")
	if err != nil || !ok {
		t.Fatalf("read second: ok=%v err=%v", ok, err)
	}
	if second.Status != store.Done || second.Summary != "did write" {
		t.Fatalf("second not landed: %+v", second)
	}
	total, err := s.Usage()
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if total.Nodes != 2 || total.PromptTokens != 200 || total.Cost < 0.019 {
		t.Fatalf("usage not recorded: %+v", total)
	}
}

func TestRunnerRecordsExecutionFailure(t *testing.T) {
	s := openRunnerStore(t)
	err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "doomed", Brief: "explode", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "boom"})
	if err != nil {
		t.Fatalf("splice: %v", err)
	}

	runner := NewRunner(s, func(ctx context.Context, node store.Node) (ExecResult, error) {
		return ExecResult{}, errors.New("the tool caught fire")
	}, "test-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	runner.Wait()

	node, ok, err := s.Node("doomed")
	if err != nil || !ok {
		t.Fatalf("read doomed: ok=%v err=%v", ok, err)
	}
	if node.Status != store.Failed || node.Error != "the tool caught fire" {
		t.Fatalf("failure not recorded: %+v", node)
	}
}

func TestRunnerCooperativeCancelReleasesClaimBeforeCancelling(t *testing.T) {
	s := openRunnerStore(t)
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "cancel-live", Brief: "long running work", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "long running work"}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finishTurn := make(chan struct{})
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		close(started)
		<-finishTurn
		return ExecResult{Summary: "partial", PromptTokens: 20, CompletionTokens: 5, Cost: 0.10}, nil
	}, "cancel-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-started
	node, found, err := s.Node("cancel-live")
	if err != nil || !found || node.Status != store.Running {
		t.Fatalf("running node = %+v found=%t err=%v", node, found, err)
	}
	oldClaim := store.Claim{ID: node.ID, Owner: node.Owner, Token: node.ClaimToken}
	if err := s.RequestNodeCancel(node.ID, "cancelled by user"); err != nil {
		t.Fatal(err)
	}
	close(finishTurn)
	runner.Wait()

	node, found, err = s.Node("cancel-live")
	if err != nil || !found || node.Status != store.Cancelled || node.Owner != "" || node.CancelRequested {
		t.Fatalf("cancelled node = %+v found=%t err=%v", node, found, err)
	}
	if err := s.Complete(oldClaim, "stale completion"); !errors.Is(err, store.ErrClaimLost) {
		t.Fatalf("old claim completion error = %v, want ErrClaimLost", err)
	}
	events, err := s.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	released, cancelled := -1, -1
	for index, event := range events {
		if event.NodeID != "cancel-live" {
			continue
		}
		if event.Kind == store.EventNodeReleased {
			released = index
		}
		if event.Kind == store.EventNodeCancelled {
			cancelled = index
		}
	}
	if released < 0 || cancelled < 0 || released >= cancelled {
		t.Fatalf("release/cancel event order = %d/%d", released, cancelled)
	}
}

func TestRunnerRespectsWorkerSlots(t *testing.T) {
	s := openRunnerStore(t)
	err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "a", Brief: "a", Stage: 1},
		{ID: "b", Parent: "a", Brief: "b", Stage: 1},
		{ID: "c", Parent: "a", Brief: "c", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "parallel"})
	if err != nil {
		t.Fatalf("splice: %v", err)
	}

	runner := NewRunner(s, func(ctx context.Context, node store.Node) (ExecResult, error) {
		return ExecResult{Summary: "ok"}, nil
	}, "test-runner", 1)
	dispatched, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("one worker slot should dispatch exactly 1, got %d", dispatched)
	}
	runner.Wait()
}

func TestRunnerPausesClaimsAndPostsOneRailQuestion(t *testing.T) {
	s := openRunnerStore(t)
	spliceChain(t, s)
	if err := s.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 1}); err != nil {
		t.Fatal(err)
	}
	ran := 0
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		ran++
		return ExecResult{Summary: "done"}, nil
	}, "rail-runner", 1).WithDailyBudgetUSD(1)

	for tick := 0; tick < 2; tick++ {
		dispatched, err := runner.Tick(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if dispatched != 0 {
			t.Fatalf("tick %d dispatched %d nodes at the rail", tick, dispatched)
		}
	}
	if ran != 0 {
		t.Fatalf("executor ran %d times at the rail", ran)
	}
	first, ok, err := s.Node("first")
	if err != nil || !ok || first.Status != store.Pending {
		t.Fatalf("paused first node = %+v ok=%t err=%v", first, ok, err)
	}
	messages, err := s.Messages("s1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	questions := 0
	for _, message := range messages {
		if strings.HasPrefix(message.Body, store.DailyRailQuestionPrefix) {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("rail questions = %d, want exactly one: %+v", questions, messages)
	}
}

// TestRunnerPausesOnlyTheTaskThatReachedItsCeiling is the scoped half of the
// rail: a task over its ceiling stops being claimed and says so once, while
// everything outside that subtree keeps running. No ceiling is set on the free
// job, and nothing about it changes.
func TestRunnerPausesOnlyTheTaskThatReachedItsCeiling(t *testing.T) {
	s := openRunnerStore(t)
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "capped", Brief: "the expensive task", Stage: 2},
		{ID: "capped-leaf", Parent: "capped", Brief: "its work", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "expensive"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "free", Brief: "an unrelated task", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "unrelated"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage(store.NodeUsage{NodeID: "capped-leaf", Cost: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCeiling("capped", 1, "test:user"); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	ran := make([]string, 0, 2)
	runner := NewRunner(s, func(_ context.Context, node store.Node) (ExecResult, error) {
		mu.Lock()
		ran = append(ran, node.ID)
		mu.Unlock()
		return ExecResult{Summary: "done"}, nil
	}, "task-rail-runner", 2)

	for tick := 0; tick < 2; tick++ {
		if _, err := runner.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		runner.Wait()
	}

	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 1 || ran[0] != "free" {
		t.Fatalf("ran %v, want only the ungoverned job", ran)
	}
	leaf, ok, err := s.Node("capped-leaf")
	if err != nil || !ok || leaf.Status != store.Pending {
		t.Fatalf("capped leaf = %+v ok=%t err=%v, want still pending", leaf, ok, err)
	}
	messages, err := s.Messages("s1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	questions := 0
	for _, message := range messages {
		if strings.HasPrefix(message.Body, store.TaskRailQuestionPrefix) {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("task rail questions = %d, want exactly one: %+v", questions, messages)
	}
}

func TestReflexMicroLeafIsJournaledClaimedSettledAndRebuildSafe(t *testing.T) {
	s := openRunnerStore(t)
	ask := "Read VERSION and report its value."
	command, err := s.RequestCommand(store.Command{
		SessionID: "reflex-session", Kind: store.CommandSplice, Reflex: true, Instruction: ask,
	})
	if err != nil {
		t.Fatal(err)
	}
	compileCalls := 0
	planCalls := 0
	reconciler := New(s, func(context.Context, string, string) (Compiled, error) {
		compileCalls++
		return Compiled{Goal: "should not compile"}, nil
	}, func(context.Context, Compiled) (store.Subtree, error) {
		planCalls++
		return store.Subtree{}, nil
	})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compileCalls != 0 || planCalls != 0 {
		t.Fatalf("compiler/planner calls = %d/%d, want zero", compileCalls, planCalls)
	}

	id := fmt.Sprintf("reflex-%d", command.Seq)
	node, ok, err := s.Node(id)
	if err != nil || !ok {
		t.Fatalf("node %q: ok=%t err=%v", id, ok, err)
	}
	if node.Group != ReflexGroup || node.Brief != ask || node.Parent != store.RootID ||
		node.Provenance.Origin != store.OriginUser ||
		node.Provenance.SessionID != "reflex-session" || node.Provenance.Intent != ask {
		t.Fatalf("reflex provenance = %+v node=%+v", node.Provenance, node)
	}
	settled, _, err := s.CommandBySeq(command.Seq)
	if err != nil || settled.Status != store.CommandApplied || !settled.Reflex {
		t.Fatalf("reflex command = %+v err=%v", settled, err)
	}

	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{Summary: "VERSION is 2.0"}, nil
	}, "reflex-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	node, ok, err = s.Node(id)
	if err != nil || !ok || node.Status != store.Done || node.Summary != "VERSION is 2.0" {
		t.Fatalf("settled reflex = %+v ok=%t err=%v", node, ok, err)
	}
	events, err := s.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[store.EventKind]bool{}
	for _, event := range events {
		if event.NodeID == id {
			if event.Kind == store.EventSubtreeSpliced && strings.Contains(string(event.Payload), id) {
				seen[event.Kind] = true
			}
			seen[event.Kind] = true
		}
	}
	for _, kind := range []store.EventKind{store.EventSubtreeSpliced, store.EventNodeClaimed, store.EventNodeStarted, store.EventNodeCompleted} {
		if !seen[kind] {
			t.Errorf("reflex lifecycle omitted %s", kind)
		}
	}
	if _, found, err := s.DeliveryGateFor(id); err != nil || found {
		t.Fatalf("delivery gate found=%t err=%v, want none", found, err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, ok, err := s.Node(id)
	if err != nil || !ok || rebuilt.Status != store.Done || rebuilt.Group != ReflexGroup ||
		rebuilt.Provenance.Intent != ask {
		t.Fatalf("rebuilt reflex = %+v ok=%t err=%v", rebuilt, ok, err)
	}
}

func TestReflexPromotionCarriesPartialIntoCompiledJob(t *testing.T) {
	s := openRunnerStore(t)
	ask := "Inspect the parser and fix the reported edge case."
	command, err := s.RequestCommand(store.Command{
		SessionID: "promotion-session", Kind: store.CommandSplice, Reflex: true, Instruction: ask,
	})
	if err != nil {
		t.Fatal(err)
	}
	var compiledInstruction, compiledContext string
	reconciler := New(s,
		func(_ context.Context, instruction, graphContext string) (Compiled, error) {
			compiledInstruction, compiledContext = instruction, graphContext
			return Compiled{Goal: "Fix and verify the parser edge case"}, nil
		},
		func(_ context.Context, compiled Compiled) (store.Subtree, error) {
			if len(compiled.BuildsOn) != 1 || compiled.BuildsOn[0] != fmt.Sprintf("reflex-%d", command.Seq) {
				return store.Subtree{}, fmt.Errorf("builds_on = %v", compiled.BuildsOn)
			}
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID: "proper-job", Brief: compiled.Goal, Stage: 1,
			}}}, nil
		},
	)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("reflex-%d", command.Seq)
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{Summary: "partial: isolated the failing escape sequence", Promote: true}, nil
	}, "reflex-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	if compiledInstruction != ask {
		t.Fatalf("compiled instruction = %q, want exact ask %q", compiledInstruction, ask)
	}
	for _, want := range []string{ask, "partial: isolated the failing escape sequence", id} {
		if !strings.Contains(compiledContext, want) {
			t.Errorf("compiled context omitted %q:\n%s", want, compiledContext)
		}
	}
	partial, ok, err := s.Node(id)
	if err != nil || !ok || partial.Status != store.Done ||
		partial.Summary != "partial: isolated the failing escape sequence" {
		t.Fatalf("promoted partial = %+v ok=%t err=%v", partial, ok, err)
	}
	proper, ok, err := s.Node("proper-job")
	if err != nil || !ok || proper.Provenance.Intent != ask ||
		proper.Provenance.SessionID != "promotion-session" {
		t.Fatalf("compiled job = %+v ok=%t err=%v", proper, ok, err)
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, edge := range snapshot.Edges {
		if edge.From == id && edge.To == "proper-job" && edge.Kind == store.FeedsInto {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("promotion partial did not feed compiled job: %+v", snapshot.Edges)
	}
	messages, err := s.Messages("promotion-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Body != reflexPromotionLine {
		t.Fatalf("promotion messages = %+v", messages)
	}
}

func TestFailedReflexStillDistills(t *testing.T) {
	s := openRunnerStore(t)
	ask := "Read the local status file."
	if _, err := s.RequestCommand(store.Command{
		SessionID: "failed-reflex", Kind: store.CommandSplice, Reflex: true, Instruction: ask,
	}); err != nil {
		t.Fatal(err)
	}
	var gotGoal, gotOutcome string
	var gotFailed bool
	reconciler := New(s, nil, nil).WithDistiller(
		func(_ context.Context, goal, outcome string, failed bool) ([]Learned, error) {
			gotGoal, gotOutcome, gotFailed = goal, outcome, failed
			return nil, nil
		},
	)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{}, errors.New("status file was unreadable")
	}, "reflex-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotGoal != ask || gotOutcome != "status file was unreadable" || !gotFailed {
		t.Fatalf("distill input = goal %q outcome %q failed=%t", gotGoal, gotOutcome, gotFailed)
	}
}

// spliceIndependentLeaves hangs count unordered leaves under one goal, which is
// the shape a fan-out has once the plans are in: nothing between them, all of
// them ready at once.
func spliceIndependentLeaves(t *testing.T, s *store.Store, count int) {
	t.Helper()
	nodes := []store.NodeSpec{{ID: "goal", Brief: "the deliverable", Stage: 0}}
	for index := range count {
		nodes = append(nodes, store.NodeSpec{
			ID: fmt.Sprintf("leaf-%d", index), Parent: "goal",
			Brief: fmt.Sprintf("leaf %d", index), Stage: 1,
		})
	}
	if err := s.Splice(store.RootID, store.Subtree{Nodes: nodes},
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "leaves, no order"}); err != nil {
		t.Fatalf("splice leaves: %v", err)
	}
}

func governedRunner(t *testing.T, s *store.Store, governor *executor.Governor,
	workers int, release <-chan struct{}) *Runner {
	t.Helper()
	return NewRunner(s, func(ctx context.Context, node store.Node) (ExecResult, error) {
		<-release
		return ExecResult{Summary: "did " + node.Brief}, nil
	}, "governed", workers).WithGovernor(governor)
}

// The measurement this fix came from: peak concurrency of three leaves in seven
// of eight live runs, and one job holding thirteen ready leaves behind three
// running ones for twenty minutes on a sixteen-core machine that was idle. The
// leaves were parked on sockets waiting for a model; the load they were gated
// on was somebody else's, and nothing they did could ever bring it down. A leaf
// is admitted because its dependencies landed, and for no other reason.
//
// The host is not merely overruled here, it cannot be consulted: every leaf
// this process runs is a goroutine on a socket, so there is no class of work
// left for a load reading to decide anything about. The gate under test is the
// real one, so however loaded the machine running this test is, every ready
// leaf is claimed.
func TestASaturatedHostDoesNotThrottleLeavesParkedOnSockets(t *testing.T) {
	s := openRunnerStore(t)
	const leaves = 12
	spliceIndependentLeaves(t, s, leaves)
	release := make(chan struct{})
	runner := governedRunner(t, s, executor.NewGovernor(), leaves, release)

	dispatched, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if dispatched != leaves {
		t.Fatalf("dispatched %d of %d leaves, want every slot filled", dispatched, leaves)
	}
	close(release)
	runner.Wait()
}

// The daily rail is summed from the usage table and from nowhere else, so a
// leaf whose spend never reaches that table is money the user is never told
// about. The failure branch was the one that never wrote it — and it is the
// branch that pays most, because escalation has usually run the whole task
// twice before it gives up.
func TestAFailedLeafsSpendReachesTheDailyRail(t *testing.T) {
	s := openRunnerStore(t)
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "doomed", Brief: "an expensive way to fail", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "an expensive way to fail"}); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{
			PromptTokens: 900_000, CompletionTokens: 40_000, Cost: 12.50,
		}, errors.New("both attempts came back empty")
	}, "rail-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	node, found, err := s.Node("doomed")
	if err != nil || !found || node.Status != store.Failed {
		t.Fatalf("node = %+v found=%t err=%v", node, found, err)
	}
	rail, err := s.DailyRailToday(20)
	if err != nil {
		t.Fatal(err)
	}
	if rail.Spend < 12.49 || rail.Spend > 12.51 {
		t.Fatalf("daily rail spend = $%.2f, want the $12.50 the failure actually cost", rail.Spend)
	}
	impact, err := s.Impact("doomed", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if impact.Cost < 12.49 {
		t.Fatalf("the node's own impact = $%.2f; surgery consent would quote a free job", impact.Cost)
	}
}

// A firing's per-firing budget is reserved at admission, which is what stops
// tomorrow's firings. Nothing stopped this one: six leaves under a $0.50
// reservation could journal $3 and run to the end, and the ceiling only ever
// bit the day after it was breached.
func TestAPracticeFiringThatOutrunsItsReservationStops(t *testing.T) {
	s := openRunnerStore(t)
	expires := time.Now().Add(time.Hour)
	charter, err := store.NewCharter("practice-rail", "Practice measured gaps",
		store.WatchSpec{Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "idle", Cadence: time.Minute}},
		"idle and executable", store.CharterAction{Template: "practice"},
		store.CharterRails{PerFiringBudgetUSD: 0.50, MaxFiringsPerDay: 2, ExpiresAt: &expires},
		store.CharterActive, store.Ratification{Origin: store.OriginSelf, Evidence: "test policy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharter(charter.WithProposalShape(store.PracticeCharterShape)); err != nil {
		t.Fatal(err)
	}
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "firing", Brief: "practice the gap", Stage: 1, Group: store.PracticeGroup},
		{ID: "firing-n1", Parent: "firing", Brief: "the next leaf", Stage: 1, Group: store.PracticeGroup},
	}}, store.Provenance{Origin: store.OriginSelf, Intent: "practice", CharterID: charter.ID}); err != nil {
		t.Fatal(err)
	}
	// What the firing has already journaled, well past its half-dollar bound.
	if err := s.RecordUsage(store.NodeUsage{NodeID: "firing", Cost: 2.90}); err != nil {
		t.Fatal(err)
	}

	claimed := 0
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		claimed++
		return ExecResult{Summary: "more spend", Cost: 0.60}, nil
	}, "practice-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if claimed != 0 {
		t.Fatalf("the runner claimed %d leaves of a firing already over its budget", claimed)
	}
	leaf, found, err := s.Node("firing-n1")
	if err != nil || !found {
		t.Fatalf("leaf found=%t err=%v", found, err)
	}
	if leaf.Status != store.Cancelled {
		t.Fatalf("leaf status = %s, want cancelled — a pending leaf nobody may claim keeps its root open forever", leaf.Status)
	}
	if !strings.Contains(leaf.Error, "per-firing budget") {
		t.Fatalf("leaf reason = %q, want the bound named on the node's own record", leaf.Error)
	}
}

// The same firing under its bound is untouched: the rail must stop overspending
// and nothing else.
func TestAPracticeFiringInsideItsReservationRunsNormally(t *testing.T) {
	s := openRunnerStore(t)
	expires := time.Now().Add(time.Hour)
	charter, err := store.NewCharter("practice-ok", "Practice measured gaps",
		store.WatchSpec{Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "idle", Cadence: time.Minute}},
		"idle and executable", store.CharterAction{Template: "practice"},
		store.CharterRails{PerFiringBudgetUSD: 5, MaxFiringsPerDay: 2, ExpiresAt: &expires},
		store.CharterActive, store.Ratification{Origin: store.OriginSelf, Evidence: "test policy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharter(charter.WithProposalShape(store.PracticeCharterShape)); err != nil {
		t.Fatal(err)
	}
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "firing2", Brief: "practice the gap", Stage: 1, Group: store.PracticeGroup},
		{ID: "firing2-n1", Parent: "firing2", Brief: "the next leaf", Stage: 1, Group: store.PracticeGroup},
	}}, store.Provenance{Origin: store.OriginSelf, Intent: "practice", CharterID: charter.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage(store.NodeUsage{NodeID: "firing2", Cost: 0.10}); err != nil {
		t.Fatal(err)
	}
	claimed := 0
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		claimed++
		return ExecResult{Summary: "landed", Cost: 0.05}, nil
	}, "practice-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if claimed != 1 {
		t.Fatalf("claimed %d leaves, want the one that was well inside its budget", claimed)
	}
}

// The dispatch loop asks the store three questions on every pass — deferred
// overruns, the ready set, whether the user is idle — and it used to ask them
// twice a second forever. Every claimable leaf is journaled, so an unmoved
// watermark is proof the ready set cannot have changed.
func TestRunnerQuietGateSkipsOnlyWhatCannotHaveChanged(t *testing.T) {
	now := time.Now()
	gate := newRunnerQuietGate()
	if gate.skip(7, nil, now) {
		t.Fatal("a fresh gate skipped its first pass")
	}

	// A pass that dispatched nothing arms the gate at the watermark it read.
	gate.settle(7, nil, 0, now)
	if !gate.skip(7, nil, now.Add(time.Second)) {
		t.Fatal("an unmoved journal did not let the pass be skipped")
	}
	if gate.skip(8, nil, now.Add(time.Second)) {
		t.Fatal("a moved journal was slept through")
	}
	if gate.skip(7, errors.New("store is busy"), now.Add(time.Second)) {
		t.Fatal("an unreadable watermark was taken as proof of quiet")
	}
	if gate.skip(7, nil, now.Add(runnerQuietCeiling)) {
		t.Fatal("the ceiling did not force a pass")
	}

	// A pass that dispatched has freed no slot yet, and a nudge is news the
	// held watermark cannot account for. Both disarm.
	gate.settle(7, nil, 1, now)
	if gate.skip(7, nil, now) {
		t.Fatal("a dispatching pass armed the gate")
	}
	gate.settle(7, nil, 0, now)
	gate.disarm()
	if gate.skip(7, nil, now) {
		t.Fatal("a nudge left the gate armed")
	}
}

// And the gate never costs a claim. Work journaled by somebody else — no nudge,
// no landing in this process — still moves the watermark, and the next timed
// pass claims it.
func TestRunnerServeStillClaimsWorkItWasNeverNudgedAbout(t *testing.T) {
	graph := openRunnerStore(t)
	runner, spans := recordingRunner(t, graph, time.Millisecond, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- runner.Serve(ctx) }()

	// Let the loop go quiet over an empty graph first, so the claim below can
	// only come from the watermark moving.
	time.Sleep(200 * time.Millisecond)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "unannounced", Brief: "work nobody nudged about", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "flow", Intent: "work nobody nudged about"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(spans()) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if len(spans()) == 0 {
		t.Fatal("the quiet gate slept through work that was journaled beside it")
	}
	cancel()
	<-served
}

// The probe shape driven through the whole scheduler, with one researcher held
// live for the duration: three sections fanned out, an assembler that reads all
// three, and a delivery that reads the four.
//
// The store refuses the claim on its own (see the store's own floor test); this
// is the pass over every gate that stands in front of that refusal — the ready
// listing, the open-children skip, the governor, the rails, the slots — pinning
// that none of them can hand a worker a node whose inputs are still live, no
// matter how many free slots the run has to fill.
func TestTheAssemblerNeverRunsBesideASectionStillBeingWritten(t *testing.T) {
	graph := openRunnerStore(t)
	researchers := []string{"task-8-n1", "task-8-n2", "task-8-n3"}
	const assembler = "task-8-n4"
	const held = "task-8-n2"

	nodes := []store.NodeSpec{{ID: "task-8", Brief: "Compare how three countries measure road distance", Stage: 2}}
	inputs := make([]store.Need, 0, len(researchers))
	for _, id := range researchers {
		nodes = append(nodes, store.NodeSpec{ID: id, Parent: "task-8", Brief: "research " + id, Stage: 1})
		inputs = append(inputs, store.Need{NodeID: id, Kind: store.FeedsInto})
	}
	nodes = append(nodes, store.NodeSpec{ID: assembler, Parent: "task-8", Brief: "assemble the three sections",
		Stage: 1, Needs: inputs})
	nodes[0].Needs = append(inputs, store.Need{NodeID: assembler, Kind: store.FeedsInto})
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: nodes}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "compare three countries"}); err != nil {
		t.Fatalf("splice fan-out: %v", err)
	}

	release := make(chan struct{})
	var mu sync.Mutex
	var order []string
	var violation string
	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		mu.Lock()
		order = append(order, node.ID)
		if node.ID == assembler && violation == "" {
			for _, id := range researchers {
				section, ok, err := graph.Node(id)
				if err != nil || !ok || section.Status != store.Done {
					violation = fmt.Sprintf("the assembler started while %s was %s", id, section.Status)
				}
			}
		}
		mu.Unlock()
		if node.ID == held {
			<-release
		}
		return ExecResult{Summary: "wrote " + node.ID}, nil
	}, "test-runner", 8)

	ctx := context.Background()
	// Slots to spare and nothing else to do: every tick here is the scheduler
	// looking for anything at all it is allowed to start.
	for range 6 {
		if _, err := runner.Tick(ctx); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	mu.Lock()
	dispatched := append([]string(nil), order...)
	mu.Unlock()
	for _, id := range dispatched {
		if id == assembler || id == "task-8" {
			t.Fatalf("%s was dispatched while %s was still running; ran %v", id, held, dispatched)
		}
	}

	close(release)
	runner.Wait()
	for range 4 {
		if _, err := runner.Tick(ctx); err != nil {
			t.Fatalf("tick after release: %v", err)
		}
		runner.Wait()
	}

	mu.Lock()
	defer mu.Unlock()
	if violation != "" {
		t.Fatal(violation)
	}
	if len(order) != 5 {
		t.Fatalf("the job ran %v, want all five nodes", order)
	}
	if order[3] != assembler || order[4] != "task-8" {
		t.Fatalf("run order %v — the assembler and the delivery must come last", order)
	}
	brief, ok, err := graph.Node("task-8")
	if err != nil || !ok || brief.Status != store.Done {
		t.Fatalf("the delivery did not land: ok=%v err=%v", ok, err)
	}
}
