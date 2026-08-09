package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// A leaf whose first attempt failed may be handed to a different kind of worker
// for its second, and the promise has to be durable or it is not a promise: a
// resident that dies mid-retry releases the claim, and whoever claims the node
// next must claim it for the worker it was moved to.
//
// Journaled, therefore, and not merely written to the row — a rebuild that
// replayed the splice alone would quietly hand the leaf back to the worker that
// had already failed at it.
func TestNodeWorkerChangeSurvivesReopenAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "fix the failing tests", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "s", Intent: "fix the failing tests"}); err != nil {
		t.Fatal(err)
	}
	if node, _, err := graph.Node("job"); err != nil || node.Subharness != "" {
		t.Fatalf("a fresh node already names a worker: %q (%v)", node.Subharness, err)
	}
	changed, err := graph.SetNodeSubharness("job", "swe", "escalated from linear after a failed attempt")
	if err != nil || !changed {
		t.Fatalf("the worker change did not land: changed=%t err=%v", changed, err)
	}
	// Idempotent: the same answer twice is not a second event.
	if changed, err := graph.SetNodeSubharness("job", "swe", "again"); err != nil || changed {
		t.Fatalf("re-stating the same worker changed something: changed=%t err=%v", changed, err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	assert := func(stage string) {
		t.Helper()
		node, found, err := reopened.Node("job")
		if err != nil || !found {
			t.Fatalf("%s: read job: found=%t err=%v", stage, found, err)
		}
		if node.Subharness != "swe" {
			t.Fatalf("%s: worker = %q, want the one the retry was moved to", stage, node.Subharness)
		}
	}
	assert("reopened")
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")
}

// The store carries the choice and holds no opinion about it: a name it has
// never heard of is remembered exactly as faithfully as a registered one, and
// whether it reaches a worker is settled at dispatch by the registry. What it
// does refuse is a node whose work is over.
func TestNodeWorkerChangeIsRememberedNotJudged(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "worker.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "review the change", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "s", Intent: "review the change"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SetNodeSubharness("job", "a-worker-nobody-registered", "the judge said so"); err != nil {
		t.Fatalf("the store refused a name it does not know: %v", err)
	}
	if node, _, _ := graph.Node("job"); node.Subharness != "a-worker-nobody-registered" {
		t.Fatalf("worker = %q", node.Subharness)
	}
	if _, err := graph.SetNodeSubharness("no-such-node", "swe", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing node answered %v", err)
	}
	claim, claimed, err := graph.Claim("job", "test")
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%t err=%v", claimed, err)
	}
	if err := graph.Complete(claim, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SetNodeSubharness("job", "swe", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("settled work accepted a new worker: %v", err)
	}
}
