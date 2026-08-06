package resident

import (
	"context"
	"errors"
	"path/filepath"
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
