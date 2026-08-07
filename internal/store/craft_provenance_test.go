package store

import (
	"path/filepath"
	"testing"
)

// The craft version a leaf actually ran under is the key every survival
// statistic groups by. It has to survive the same replay work_model and
// charter_id survive, or a rebuilt graph would credit a workflow's results to
// whatever version happens to be checked out later.
func TestCraftProvenanceSurvivesReopenAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "craft.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "presentation-1", Brief: "deliver the deck", Stage: 2},
		{ID: "presentation-1~outline", Parent: "presentation-1", Brief: "outline it", Stage: 1},
	}}, Provenance{
		Origin: OriginUser, SessionID: "craft", Intent: "run the presentation craft",
		Craft: "presentation@abc123",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "ordinary", Brief: "read the file", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "craft", Intent: "read the file"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	assert := func(stage string) {
		t.Helper()
		for _, id := range []string{"presentation-1", "presentation-1~outline"} {
			node, found, err := reopened.Node(id)
			if err != nil || !found {
				t.Fatalf("%s: read %s: found=%t err=%v", stage, id, found, err)
			}
			if node.Provenance.Craft != "presentation@abc123" {
				t.Fatalf("%s: %s craft = %q", stage, id, node.Provenance.Craft)
			}
		}
		node, found, err := reopened.Node("ordinary")
		if err != nil || !found || node.Provenance.Craft != "" {
			t.Fatalf("%s: ordinary work claimed a craft: %+v found=%t err=%v", stage, node, found, err)
		}
	}
	assert("reopened")
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")
}
