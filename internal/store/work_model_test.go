package store

import (
	"path/filepath"
	"testing"
)

// The model a user named for one job is provenance, not configuration: it has
// to survive the same replay that charter_id and retry_of survive, or a
// rebuilt graph would quietly re-run the job on whatever the slot holds now.
func TestWorkModelProvenanceSurvivesReopenAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work-model.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "pinned", Brief: "benchmark the parser", Stage: 1},
		{ID: "pinned-leaf", Parent: "pinned", Brief: "run the benchmark", Stage: 2},
	}}, Provenance{
		Origin: OriginUser, SessionID: "words", Intent: "benchmark the parser with gemini",
		WorkModel: "google/gemini-3-pro",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "ordinary", Brief: "read the file", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "words", Intent: "read the file"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	assert := func(stage string) {
		t.Helper()
		for _, id := range []string{"pinned", "pinned-leaf"} {
			node, found, err := reopened.Node(id)
			if err != nil || !found {
				t.Fatalf("%s: read %s: found=%t err=%v", stage, id, found, err)
			}
			if node.Provenance.WorkModel != "google/gemini-3-pro" {
				t.Fatalf("%s: %s work model = %q", stage, id, node.Provenance.WorkModel)
			}
		}
		node, found, err := reopened.Node("ordinary")
		if err != nil || !found || node.Provenance.WorkModel != "" {
			t.Fatalf("%s: ordinary work stopped being ordinary: %+v found=%t err=%v", stage, node, found, err)
		}
	}
	assert("reopened")
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")
}
