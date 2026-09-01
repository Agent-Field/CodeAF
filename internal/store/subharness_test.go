package store

import (
	"path/filepath"
	"testing"
)

// What runs a leaf is provenance, not configuration, and a graph written by a
// build that had more than one worker still has to open and read back exactly
// what it recorded. It survives the same replay work_model and craft survive,
// and it survives it twice over: the splice's own choice, and a single node
// named on its own while its siblings stayed ordinary.
func TestSubharnessSurvivesReopenAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subharness.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "coding", Brief: "fix the failing tests", Stage: 1},
		{ID: "coding-leaf", Parent: "coding", Brief: "fix the parser", Stage: 2},
	}}, Provenance{
		Origin: OriginUser, SessionID: "s", Intent: "fix the failing tests",
		Subharness: "retired-worker",
	}); err != nil {
		t.Fatal(err)
	}
	// A mixed subtree: the splice chose nothing, one node did.
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "mixed", Brief: "ship the release", Stage: 1},
		{ID: "mixed-code", Parent: "mixed", Brief: "land the fix", Stage: 2, Subharness: "retired-worker"},
		{ID: "mixed-note", Parent: "mixed", Brief: "write the note", Stage: 2},
	}}, Provenance{Origin: OriginUser, SessionID: "s", Intent: "ship the release"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	read := func(stage, id string) Node {
		t.Helper()
		node, found, err := reopened.Node(id)
		if err != nil || !found {
			t.Fatalf("%s: read %s: found=%t err=%v", stage, id, found, err)
		}
		return node
	}
	assert := func(stage string) {
		t.Helper()
		// The splice's choice is inherited by the whole subtree.
		for _, id := range []string{"coding", "coding-leaf"} {
			node := read(stage, id)
			if node.Subharness != "retired-worker" || node.Provenance.Subharness != "retired-worker" {
				t.Fatalf("%s: %s = %q (provenance %q), want the splice's own choice", stage, id, node.Subharness, node.Provenance.Subharness)
			}
		}
		// The node's own choice stands alone, and does not spread to a sibling.
		if node := read(stage, "mixed-code"); node.Subharness != "retired-worker" || node.Provenance.Subharness != "" {
			t.Fatalf("%s: mixed-code = %q (provenance %q)", stage, node.Subharness, node.Provenance.Subharness)
		}
		for _, id := range []string{"mixed", "mixed-note"} {
			if node := read(stage, id); node.Subharness != "" {
				t.Fatalf("%s: %s stopped being ordinary work: %q", stage, id, node.Subharness)
			}
		}
	}
	assert("reopened")
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")
}

// The bug this test exists for, in one splice: a job whose planner sized one
// node for the generalist. Admission fills a node's worker in from the job only
// when the node named none of its own — and while the generalist was spelled as
// the empty string, "sized for the generalist" and "never sized" were the same
// bytes, so every deliberately generalist node of a routed job was admitted
// under the job's name. Measured live: twelve leaves sized generalist, twelve
// leaves run on something else. Nothing routes a job any more; the replay of a
// graph that does still has to obey it.
func TestGeneralistNodeIsNotPromotedByTheJobsWorker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generalist.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "ship the parser fix", Stage: 1},
		{ID: "job-code", Parent: "job", Brief: "write the patch", Stage: 2, Subharness: "retired-worker"},
		// Sized, and sized for the generalist. This is the node under test.
		{ID: "job-note", Parent: "job", Brief: "write the release note", Stage: 2, Subharness: "linear"},
		// Never sized at all. This is the node that still inherits.
		{ID: "job-tidy", Parent: "job", Brief: "tidy the changelog", Stage: 2},
	}}, Provenance{
		Origin: OriginUser, SessionID: "s", Intent: "ship the parser fix",
		Subharness: "retired-worker",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	assert := func(stage string) {
		t.Helper()
		for _, want := range []struct{ id, worker string }{
			{"job-code", "retired-worker"},
			{"job-note", "linear"},
			{"job-tidy", "retired-worker"},
			{"job", "retired-worker"},
		} {
			node, found, err := reopened.Node(want.id)
			if err != nil || !found {
				t.Fatalf("%s: read %s: found=%t err=%v", stage, want.id, found, err)
			}
			if node.Subharness != want.worker {
				t.Fatalf("%s: %s runs on %q, want %q", stage, want.id, node.Subharness, want.worker)
			}
			// What the job as a whole was spliced for is kept beside the
			// node's own answer rather than instead of it, so a reader can
			// still see that the generalist node sat inside a routed job.
			if node.Provenance.Subharness != "retired-worker" {
				t.Fatalf("%s: %s lost the job's own choice: %q", stage, want.id, node.Provenance.Subharness)
			}
		}
	}
	assert("reopened")
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")
}
