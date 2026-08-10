package store

import (
	"path/filepath"
	"testing"
)

// What runs a leaf is provenance, not configuration. It has to survive the same
// replay work_model and craft survive, and it has to survive it twice over: the
// splice's own choice, and a single node the sizing pass judged atomic for a
// specialist while its siblings stayed ordinary.
func TestSubharnessSurvivesReopenAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subharness.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "coding", Brief: "fix the failing tests", Stage: 1},
		{ID: "coding-leaf", Parent: "coding", Brief: "fix the parser", Stage: 2},
	}}, Provenance{
		Origin: OriginUser, SessionID: "s", Intent: "fix the failing tests",
		Subharness: "swe",
	}); err != nil {
		t.Fatal(err)
	}
	// A mixed subtree: the splice chose nothing, one node did.
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "mixed", Brief: "ship the release", Stage: 1},
		{ID: "mixed-code", Parent: "mixed", Brief: "land the fix", Stage: 2, Subharness: "swe"},
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
			if node.Subharness != "swe" || node.Provenance.Subharness != "swe" {
				t.Fatalf("%s: %s = %q (provenance %q), want swe", stage, id, node.Subharness, node.Provenance.Subharness)
			}
		}
		// The node's own choice stands alone, and does not spread to a sibling.
		if node := read(stage, "mixed-code"); node.Subharness != "swe" || node.Provenance.Subharness != "" {
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
