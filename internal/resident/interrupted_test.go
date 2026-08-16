package resident

import (
	"context"
	"fmt"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A leaf that dies because the process carrying it went away was interrupted,
// not failed.
//
// The incident: three nodes of one job journaled
// `Post ".../chat/completions": context canceled` five milliseconds after the
// chat surface detached. Nothing about the provider had gone wrong, but the
// journal said node_failed, so the parent replanned around a child that had
// never failed and the retrospect filed a lesson recommending retries. Release
// is the honest settlement — the same one ReleaseOrphans gives a leaf that was
// still claimed when a process was killed — and it is what lets the next
// resident pick the work up.
func TestInterruptedLeafGoesBackOnTheQueueRatherThanFailing(t *testing.T) {
	s := openRunnerStore(t)
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "only", Brief: "gather", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "do the thing"}); err != nil {
		t.Fatalf("splice: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runner := NewRunner(s, func(leaf context.Context, node store.Node) (ExecResult, error) {
		// The shape of the real thing: the surface goes away mid-POST, and the
		// provider call comes back holding the runner's own cancellation.
		cancel()
		<-leaf.Done()
		return ExecResult{PromptTokens: 500, Cost: 0.05},
			fmt.Errorf("Post \"https://openrouter.ai/api/v1/chat/completions\": %w", leaf.Err())
	}, "test-runner", 1)

	if _, err := runner.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	runner.Wait()

	node, ok, err := s.Node("only")
	if err != nil || !ok {
		t.Fatalf("read node: ok=%v err=%v", ok, err)
	}
	if node.Status == store.Failed {
		t.Fatalf("an interrupted leaf was journaled as a failure: %q", node.Error)
	}
	if node.Status != store.Pending {
		t.Fatalf("node status = %v, want pending so the next resident claims it", node.Status)
	}
	// The money was still spent, whatever the store then decided about the row.
	total, err := s.Usage()
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if total.PromptTokens != 500 {
		t.Fatalf("an interrupted leaf's spend went unrecorded: %+v", total)
	}
}

// The counterweight: a leaf that fails on its own, while the runner's context
// is perfectly healthy, is still a failure. Releasing those would turn every
// genuine fault into an infinite retry.
func TestLeafThatFailsOnItsOwnIsStillAFailure(t *testing.T) {
	s := openRunnerStore(t)
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "only", Brief: "gather", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "do the thing"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	runner := NewRunner(s, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{}, fmt.Errorf("the tool refused")
	}, "test-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	runner.Wait()
	node, ok, err := s.Node("only")
	if err != nil || !ok {
		t.Fatalf("read node: ok=%v err=%v", ok, err)
	}
	if node.Status != store.Failed {
		t.Fatalf("node status = %v, want failed", node.Status)
	}
}
