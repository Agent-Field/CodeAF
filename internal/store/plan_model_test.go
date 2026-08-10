package store

import (
	"path/filepath"
	"testing"
)

// Who structured the job is as durable as who was asked to work it. A plan slot
// moved after the splice, or a process restarted an hour later, must not be able
// to rewrite the answer — and the empty value has to keep meaning what it has
// always meant: the plan followed the work.
func TestPlanModelProvenanceSurvivesReopenAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan-model.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "split", Brief: "structure the migration", Stage: 1},
		{ID: "split-leaf", Parent: "split", Brief: "land the migration", Stage: 2},
	}}, Provenance{
		Origin: OriginUser, SessionID: "slots", Intent: "plan this with the strong model",
		WorkModel: "moonshotai/kimi-k2", PlanModel: "anthropic/claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "ordinary", Brief: "read the file", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "slots", Intent: "read the file"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	assert := func(stage string) {
		t.Helper()
		for _, id := range []string{"split", "split-leaf"} {
			node, found, err := reopened.Node(id)
			if err != nil || !found {
				t.Fatalf("%s: read %s: found=%t err=%v", stage, id, found, err)
			}
			if node.Provenance.PlanModel != "anthropic/claude-opus-5" {
				t.Fatalf("%s: %s plan model = %q", stage, id, node.Provenance.PlanModel)
			}
			if node.Provenance.WorkModel != "moonshotai/kimi-k2" {
				t.Fatalf("%s: %s work model = %q", stage, id, node.Provenance.WorkModel)
			}
		}
		node, found, err := reopened.Node("ordinary")
		if err != nil || !found || node.Provenance.PlanModel != "" {
			t.Fatalf("%s: a job nobody split reads as split: %+v found=%t err=%v", stage, node, found, err)
		}
	}
	assert("reopened")
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")
}
