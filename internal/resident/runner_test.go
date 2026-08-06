package resident

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
