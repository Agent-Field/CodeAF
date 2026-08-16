package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// spliceModelTree is a job with two leaves, one of which is already claimed, so
// "moves at its next call" and "finishes where it started" are both visible in
// the same rebinding.
func spliceModelTree(t *testing.T, graph *Store) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "the job", Stage: 2},
		{ID: "running-leaf", Parent: "job", Brief: "already in flight", Stage: 1},
		{ID: "waiting-leaf", Parent: "job", Brief: "not started", Stage: 1},
	}}, Provenance{
		Origin: OriginUser, SessionID: "models", Intent: "a job with a pin",
		WorkModel: "cheap-model",
	}); err != nil {
		t.Fatalf("splice model tree: %v", err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "other-job", Brief: "an unrelated job", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "models", Intent: "another job"}); err != nil {
		t.Fatalf("splice sibling: %v", err)
	}
}

// TestSetSubtreeWorkModelMovesLiveWorkOnly is the whole verb: the pin every
// live node under the root will be read from at its next claim moves, settled
// work keeps the model it was actually done on, and nothing outside the subtree
// is touched.
func TestSetSubtreeWorkModelMovesLiveWorkOnly(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "setmodel.db"))
	spliceModelTree(t, graph)
	claim := mustClaim(t, graph, "running-leaf", "worker-1")
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start: %v", err)
	}
	doneClaim := mustClaim(t, graph, "waiting-leaf", "worker-2")
	if err := graph.Complete(doneClaim, "finished on the cheap model"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	rebinding, err := graph.SetSubtreeWorkModel("job", "strong-model", "asked mid-run")
	if err != nil {
		t.Fatalf("set subtree model: %v", err)
	}
	if rebinding.Root != "job" || rebinding.Model != "strong-model" {
		t.Fatalf("rebinding = %+v", rebinding)
	}
	// The job root and the leaf still in flight; the completed leaf is not work
	// that is still to be done.
	if len(rebinding.Nodes) != 2 || rebinding.Nodes[0] != "job" || rebinding.Nodes[1] != "running-leaf" {
		t.Fatalf("rebound %v, want [job running-leaf]", rebinding.Nodes)
	}
	if rebinding.Running != 1 {
		t.Fatalf("running count = %d, want 1", rebinding.Running)
	}

	assert := func(stage string) {
		t.Helper()
		for id, want := range map[string]string{
			"job":          "strong-model",
			"running-leaf": "strong-model",
			"waiting-leaf": "cheap-model",
			"other-job":    "",
		} {
			node, found, err := graph.Node(id)
			if err != nil || !found {
				t.Fatalf("%s: read %q: %v found=%t", stage, id, err, found)
			}
			if node.Provenance.WorkModel != want {
				t.Fatalf("%s: %q work model = %q, want %q", stage, id, node.Provenance.WorkModel, want)
			}
		}
	}
	assert("after rebinding")

	// The row is a view of the journal, so the only proof that matters is that
	// a replay reaches the same answer — including for the settled leaf, whose
	// model must not be re-pointed by a rebinding it was never part of.
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	assert("after rebuild")
}

// TestSetSubtreeWorkModelNeverInterruptsRunningWork is the 5.10 law stated as a
// test: a claimed, started node keeps its claim, its owner, its token and its
// status across the rebinding, because the only thing that changed is what the
// next call will use.
func TestSetSubtreeWorkModelNeverInterruptsRunningWork(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "uninterrupted.db"))
	spliceModelTree(t, graph)
	claim := mustClaim(t, graph, "running-leaf", "worker-1")
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start: %v", err)
	}
	before, _, err := graph.Node("running-leaf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SetSubtreeWorkModel("job", "strong-model", "asked mid-run"); err != nil {
		t.Fatalf("set subtree model: %v", err)
	}
	after, _, err := graph.Node("running-leaf")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.Owner != before.Owner ||
		after.ClaimToken != before.ClaimToken || after.CancelRequested || after.Held {
		t.Fatalf("running leaf changed under the rebinding: %+v -> %+v", before, after)
	}
	// The claim it is holding still settles, which is the practical form of the
	// same law: the turn in flight was never told anything happened.
	if err := graph.Complete(claim, "finished the turn it was already on"); err != nil {
		t.Fatalf("complete after rebinding: %v", err)
	}
}

// TestSetSubtreeWorkModelMovesWhoRanItButNotWhoPlannedIt covers the split-slot
// job: the model the remaining leaves are handed moves with the work, and the
// model that structured the job stays where history left it.
func TestSetSubtreeWorkModelMovesWhoRanItButNotWhoPlannedIt(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "slots.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "split", Brief: "planned by one, worked by another", Stage: 2},
		{ID: "split-leaf", Parent: "split", Brief: "the work", Stage: 1},
	}}, Provenance{
		Origin: OriginUser, SessionID: "models", Intent: "split slots",
		PlanModel: "architect-model", RunModel: "hands-model",
	}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	if _, err := graph.SetSubtreeWorkModel("split", "strong-model", "asked mid-run"); err != nil {
		t.Fatalf("set subtree model: %v", err)
	}
	for _, id := range []string{"split", "split-leaf"} {
		node, _, err := graph.Node(id)
		if err != nil {
			t.Fatal(err)
		}
		if node.Provenance.PlanModel != "architect-model" {
			t.Fatalf("%q plan model = %q, want it unmoved", id, node.Provenance.PlanModel)
		}
		if node.Provenance.RunModel != "strong-model" || node.Provenance.WorkModel != "strong-model" {
			t.Fatalf("%q = work %q run %q, want both on the new model",
				id, node.Provenance.WorkModel, node.Provenance.RunModel)
		}
	}
}

// TestSetSubtreeWorkModelRefusesCleanly covers everything that is not a live
// subtree: a missing root, an empty root, an empty model. None of them is a
// fault, and none of them writes anything.
func TestSetSubtreeWorkModelRefusesCleanly(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "refusals.db"))
	spliceModelTree(t, graph)

	if _, err := graph.SetSubtreeWorkModel("ghost", "strong-model", "why"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing root err = %v, want ErrNotFound", err)
	}
	for _, blank := range []struct{ root, model string }{{"", "strong-model"}, {"job", ""}, {"job", "   "}} {
		if _, err := graph.SetSubtreeWorkModel(blank.root, blank.model, "why"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("root %q model %q err = %v, want ErrInvalid", blank.root, blank.model, err)
		}
	}

	// Asking for the model a subtree already runs on changes nothing and
	// journals nothing: the second ask is not an event.
	if _, err := graph.SetSubtreeWorkModel("job", "settled-model", "first ask"); err != nil {
		t.Fatal(err)
	}
	again, err := graph.SetSubtreeWorkModel("job", "settled-model", "second ask")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Nodes) != 0 {
		t.Fatalf("re-asking rebound %v, want nothing", again.Nodes)
	}

	// A settled subtree has nothing left to re-point, and says so rather than
	// rewriting what already ran.
	claim := mustClaim(t, graph, "other-job", "worker-1")
	if err := graph.Complete(claim, "done"); err != nil {
		t.Fatal(err)
	}
	settled, err := graph.SetSubtreeWorkModel("other-job", "strong-model", "too late")
	if err != nil {
		t.Fatalf("settled subtree err = %v, want a clean empty rebinding", err)
	}
	if len(settled.Nodes) != 0 {
		t.Fatalf("settled subtree rebound %v, want nothing", settled.Nodes)
	}
	node, _, _ := graph.Node("other-job")
	if node.Provenance.WorkModel != "" {
		t.Fatalf("settled node was re-pointed to %q", node.Provenance.WorkModel)
	}
}

// TestSetModelCommandNeedsALiveTarget checks the funnel rather than the store
// surface: the verb is validated exactly like the other mid-run verbs, and the
// permanent spine is still refused.
func TestSetModelCommandNeedsALiveTarget(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "funnel.db"))
	spliceModelTree(t, graph)

	if _, err := graph.RequestCommand(Command{
		SessionID: "models", Kind: CommandSetModel, Target: "job", Instruction: "strong-model",
	}); err != nil {
		t.Fatalf("live target was refused: %v", err)
	}
	if _, err := graph.RequestCommand(Command{
		SessionID: "models", Kind: CommandSetModel, Target: RootID, Instruction: "strong-model",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("spine target err = %v, want ErrInvalid", err)
	}
	if _, err := graph.RequestCommand(Command{
		SessionID: "models", Kind: CommandSetModel, Target: "ghost", Instruction: "strong-model",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing target err = %v, want ErrNotFound", err)
	}
	claim := mustClaim(t, graph, "other-job", "worker-1")
	if err := graph.Complete(claim, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(Command{
		SessionID: "models", Kind: CommandSetModel, Target: "other-job", Instruction: "strong-model",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("settled target err = %v, want ErrInvalid", err)
	}
}
