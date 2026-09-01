package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// A graph written by a build that had more than one worker carries hand-over
// events this build never writes, and it still has to open, replay and read
// back exactly what it recorded. The row is a view of the journal, so the proof
// is a rebuild: the event alone, with no row written beside it, must produce
// the row.
func TestAJournaledWorkerChangeStillReplays(t *testing.T) {
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
	// The journal an older build would have left behind, written straight into
	// the log the way that build wrote it.
	tx, err := graph.beginWrite()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := appendEvent(tx, "job", EventNodeWorkerChanged,
		nodeWorkerPayload{Subharness: "retired-worker", Reason: "a build that had one"}); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	node, found, err := reopened.Node("job")
	if err != nil || !found {
		t.Fatalf("read job: found=%t err=%v", found, err)
	}
	if node.Subharness != "retired-worker" {
		t.Fatalf("the rebuild forgot an older build's hand-over: %q", node.Subharness)
	}
}

// The worker that RAN is a different fact from the worker a node was assigned,
// and it lives in a different column for one reason: the assignment is empty on
// nearly every node there has ever been — nothing routes a node — so a reader
// asking "who did this work" was reading a table of blanks. This is the fact
// that column could never hold, and like every other fact about a node it is
// journaled and therefore survives a rebuild.
func TestTheWorkerThatRanIsItsOwnFactAndSurvivesARebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ran.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "fix the failing tests", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "s", Intent: "fix the failing tests"}); err != nil {
		t.Fatal(err)
	}
	if node, _, err := graph.Node("job"); err != nil || node.Ran != "" {
		t.Fatalf("a node nobody has run already names a worker: %q (%v)", node.Ran, err)
	}
	// A blank is the one value this column may never hold: it is the absence
	// the whole seam exists to remove.
	if _, err := graph.RecordNodeRan("job", "  ", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a blank worker was accepted: %v", err)
	}
	if changed, err := graph.RecordNodeRan("job", "linear", ""); err != nil || !changed {
		t.Fatalf("the worker did not land: changed=%t err=%v", changed, err)
	}
	// The same worker again is the same fact, not a hand-over.
	if changed, err := graph.RecordNodeRan("job", "linear", ""); err != nil || changed {
		t.Fatalf("recording the same worker twice read as a change: changed=%t err=%v", changed, err)
	}
	// The store carries names and holds no opinion about which ones exist: a
	// name this build could not construct is recorded as faithfully as one it
	// can, because remembering is its job and resolving is the registry's.
	if changed, err := graph.RecordNodeRan("job", "retired-worker", "read off an older graph"); err != nil || !changed {
		t.Fatalf("a name this build does not have was refused: changed=%t err=%v", changed, err)
	}
	// It says nothing about the assignment, which is still, correctly, that
	// nothing routed this node.
	if node, _, err := graph.Node("job"); err != nil || node.Ran != "retired-worker" || node.Subharness != "" {
		t.Fatalf("ran=%q assigned=%q (%v)", node.Ran, node.Subharness, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if node, _, err := graph.Node("job"); err != nil || node.Ran != "retired-worker" {
		t.Fatalf("the rebuild forgot who ran the node: %q (%v)", node.Ran, err)
	}
	// A node that does not exist is a caller error and not a silent success:
	// the record is only worth anything if it is on the node it is about.
	if _, err := graph.RecordNodeRan("nobody", "linear", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a record against a node nobody has heard of: %v", err)
	}
}
