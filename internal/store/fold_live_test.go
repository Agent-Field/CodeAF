package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// The property this file locks: FOLDING HIDES NOTHING THAT IS STILL ALIVE.
//
// The incident. A job's continuation was running — seven minutes in, a cent
// spent, drawn plainly on the rail — under a lineage a retrospective had
// already filed away. The person asked to cancel it by name. Sixteen steps
// later, three board reads and five searches, the answer came back: there is no
// task matching that, nothing is running there. It was a false negative, and a
// false negative delivered in that voice is indistinguishable from a decision.
// The job had to be stopped with raw SQL.
//
// Folding is FILING, and filing is for work that is over. The compact view
// speaks for a fold's members through its root, which is right for members that
// have settled and wrong for one that has not. What is asserted here is the
// corpus and the two verbs: an open node inside filed history is readable,
// searchable and stoppable, and settled history stays filed.

func foldStore(t *testing.T, name string) *Store {
	t.Helper()
	return openTestStore(t, filepath.Join(t.TempDir(), name+".db"))
}

// filedWithALiveMember builds the shape the incident had: a settled job, folded
// away, with one member that is running again afterwards.
//
// The fold is taken through the real verb, which refuses an open subtree — so
// the member is re-opened after the filing, exactly as the continuation was.
func filedWithALiveMember(t *testing.T, graph *Store) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "task-4730", Title: "Map figures to pages", Brief: "map the figures to their pages"},
		{ID: "task-4730-x1", Parent: "task-4730", Title: "Map figures to pages, continued",
			Brief: "carry on mapping the figures to their pages"},
	}}, Provenance{Origin: OriginUser, SessionID: "fold", Intent: "map the figures to pages"}); err != nil {
		t.Fatal(err)
	}
	finish(t, graph, "task-4730-x1", Done)
	finish(t, graph, "task-4730", Done)
	if err := graph.Fold("task-4730", "The figures were mapped.", nil); err != nil {
		t.Fatalf("fold: %v", err)
	}
	// The continuation carries on. Claim's own guard keeps a filed row out of
	// the scheduler, so the running state is written the way a claim writes it.
	if _, err := graph.db.Exec(`UPDATE nodes SET status = ?, finished_at = NULL, owner = 'worker' WHERE id = ?`,
		Running, "task-4730-x1"); err != nil {
		t.Fatal(err)
	}
}

func TestAFiledJobsRunningMemberIsStillOnTheOpenCorpus(t *testing.T) {
	graph := foldStore(t, "open-corpus")
	filedWithALiveMember(t, graph)

	open, err := graph.OpenNodes()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(open))
	for _, node := range open {
		ids = append(ids, node.ID)
	}
	if len(ids) != 1 || ids[0] != "task-4730-x1" {
		t.Fatalf("the open corpus holds %v, want the running continuation alone", ids)
	}

	// The compact view is unchanged, deliberately: it answers a different
	// question and this fix does not widen it.
	active, err := graph.ActiveNodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range active {
		if node.ID == "task-4730-x1" {
			t.Fatal("the compact view grew a fold's member; only the open corpus should have it")
		}
	}
}

func TestAFiledJobsRunningMemberCanBeFoundByItsWords(t *testing.T) {
	graph := foldStore(t, "search")
	filedWithALiveMember(t, graph)

	// The verb's search: statuses supplied, which is what "cancel that" resolves
	// through. This is the read that answered "there is no such work".
	targets, err := graph.SearchSurgeryTargets("map figures to page", true, Pending, Claimed, Running)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range targets {
		if target.Node.ID == "task-4730-x1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the running continuation is unreachable by its own words; the search returned %d targets", len(targets))
	}
}

func TestAFiledJobsRunningMemberCanBeStopped(t *testing.T) {
	graph := foldStore(t, "stop")
	filedWithALiveMember(t, graph)

	if err := graph.RequestNodeCancel("task-4730-x1", "the user asked"); err != nil {
		t.Fatalf("cancelling the running continuation: %v", err)
	}
	node, found, err := graph.Node("task-4730-x1")
	if err != nil || !found {
		t.Fatalf("node found=%t err=%v", found, err)
	}
	if !node.CancelRequested {
		t.Fatal("the cancellation was journalled nowhere")
	}
}

// The mirror, and the reason folding exists at all: work that is over stays
// filed. A widening that swallowed history would have replaced one wrong board
// with a longer one.
func TestSettledHistoryStaysFiledAway(t *testing.T) {
	graph := foldStore(t, "history")
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "old", Title: "An errand from March", Brief: "run the March errand"},
		{ID: "old-n1", Parent: "old", Title: "The errand's one step", Brief: "do the step"},
	}}, Provenance{Origin: OriginUser, SessionID: "fold", Intent: "run the March errand"}); err != nil {
		t.Fatal(err)
	}
	finish(t, graph, "old-n1", Done)
	finish(t, graph, "old", Done)
	if err := graph.Fold("old", "The March errand was run.", nil); err != nil {
		t.Fatalf("fold: %v", err)
	}

	open, err := graph.OpenNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("settled history reached the open corpus: %d nodes", len(open))
	}
	// A verb still refuses it, and by its status rather than by pretending it
	// does not exist — which is the better of the two refusals.
	err = graph.RequestNodeCancel("old-n1", "the user asked")
	if err == nil {
		t.Fatal("a settled node accepted a cancellation")
	}
	if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("refusing a settled node: %v", err)
	}
	// The verb's search leaves filed history alone.
	targets, err := graph.SearchSurgeryTargets("March errand", true, Pending, Claimed, Running)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if strings.HasPrefix(target.Node.ID, "old") {
			t.Fatalf("filed history %q came back on a verb's search", target.Node.ID)
		}
	}
}
