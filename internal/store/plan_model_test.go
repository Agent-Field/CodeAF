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
		RunModel: "moonshotai/kimi-k2",
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
			// The model the work ran on is half the split and survives the same
			// way the other half does.
			if node.Provenance.RunModel != "moonshotai/kimi-k2" {
				t.Fatalf("%s: %s run model = %q", stage, id, node.Provenance.RunModel)
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

// A job spliced by a build that never recorded who ran it keeps replaying as the
// truth that build wrote down: the planner is still named, and the run model is
// empty rather than guessed. The event payload for such a splice simply has no
// run_model key, which is exactly the shape this splice produces.
func TestASpliceWithoutARunModelReplaysWithoutOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-split.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "legacy", Brief: "land the migration", Stage: 1},
	}}, Provenance{
		Origin: OriginUser, SessionID: "slots", Intent: "land the migration",
		PlanModel: "zai/glm-5-2",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	node, found, err := graph.Node("legacy")
	if err != nil || !found {
		t.Fatalf("read legacy: found=%t err=%v", found, err)
	}
	if node.Provenance.PlanModel != "zai/glm-5-2" {
		t.Fatalf("the planner was lost in replay: %q", node.Provenance.PlanModel)
	}
	if node.Provenance.RunModel != "" {
		t.Fatalf("replay invented a run model: %q", node.Provenance.RunModel)
	}
}
