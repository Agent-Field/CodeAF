package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// The permanent spine is Running by construction: it is the trunk every job
// splices under, not work anyone finishes. Ready excludes a node whose hard
// dependency is not done, failed or cancelled — and the root is none of those
// and never will be — so a dependency on it is a wait with no end.
//
// This is written from an incident, not from a hypothesis. A compiler answered
// builds_on:["root"], continuity wired the edge because a node by that name
// existed, and the job was deadlocked from the instant it was created. It
// compiled cleanly, spliced cleanly, and then sat pending with an empty
// started_at for the entire 900-second ceiling while the resident ticked beside
// it once a second with nothing it was permitted to claim. Nothing was wrong
// with the dispatcher, the lease, or the queue; the work was simply never ready,
// and no retry, wake signal or supervision could ever have made it so.
func TestASpliceMayNotDependOnThePermanentSpine(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	err := store.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "task-2", Brief: "the work that could never start", Stage: 1,
			Needs: []Need{{NodeID: RootID, Kind: FeedsInto}}},
	}}, Provenance{Origin: OriginUser, SessionID: "s1", Intent: "do the thing"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("splice with a dependency on the spine = %v, want ErrInvalid", err)
	}
	if _, found, err := store.Node("task-2"); err != nil || found {
		t.Fatalf("the refused subtree was admitted anyway: found=%v err=%v", found, err)
	}
}

// The same invariant on the path that adds a dependency after admission, which
// just-in-time replanning uses to point existing consumers at new work.
func TestAnEdgeMayNotBeAddedFromThePermanentSpine(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	if err := store.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "task-2", Brief: "ordinary work", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "s1", Intent: "do the thing"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddEdge(RootID, "task-2", FeedsInto); !errors.Is(err, ErrInvalid) {
		t.Fatalf("AddEdge from the spine = %v, want ErrInvalid", err)
	}
	// And the node it was aimed at is still claimable, which is the whole point.
	ready, err := store.Ready(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != "task-2" {
		t.Fatalf("ready = %+v, want the work still claimable after the refusal", ready)
	}
}
