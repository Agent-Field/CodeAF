package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The head's half of the folding law. The store's half is in
// internal/store/fold_live_test.go, with the incident written out.
//
// These are the two filters the belt applied to its own world: the enumeration
// dropped every folded node before the board was drawn, and the widening that
// puts the open corpus back was missing entirely. Both are exercised on plain
// values because both are decisions about a node and not about a database.

func liveNode(id, title string, status store.Status, folded bool) store.Node {
	return store.Node{
		ID: id, Parent: store.RootID, Title: title, Status: status, Folded: folded,
		Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "fold"},
	}
}

func TestTheBoardsEnumerationKeepsAFiledJobThatIsStillRunning(t *testing.T) {
	nodes := []store.Node{
		liveNode(store.RootID, "spine", store.Running, false),
		liveNode("task-4730", "Map figures to pages", store.Done, true),
		liveNode("task-4730-x1", "Map figures to pages, continued", store.Running, true),
		liveNode("march", "An errand from March", store.Done, true),
		liveNode("today", "Today's errand", store.Running, false),
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	kept := make(map[string]bool)
	for _, target := range boardEnumeration(nodes, byID) {
		kept[target.Node.ID] = true
	}
	if !kept["task-4730-x1"] {
		t.Fatal("a running continuation was dropped from the board because its lineage had been filed")
	}
	if !kept["today"] {
		t.Fatal("ordinary live work fell off the board")
	}
	// The mirror. Filing exists so settled work stops competing for the board,
	// and a widening that swallowed history would trade one wrong board for a
	// longer one.
	if kept["march"] || kept["task-4730"] {
		t.Fatal("settled history came back onto the moving board")
	}
	if kept[store.RootID] {
		t.Fatal("the permanent spine reached the board")
	}
}

// liveFolded is the one predicate both renderers share, so the two cannot drift
// into disagreeing about what history is.
func TestFiledAndStillOpenIsLiveWorkAndNothingElseIs(t *testing.T) {
	for _, probe := range []struct {
		node store.Node
		want bool
	}{
		{liveNode("a", "running under a fold", store.Running, true), true},
		{liveNode("b", "claimed under a fold", store.Claimed, true), true},
		{liveNode("c", "queued under a fold", store.Pending, true), true},
		{liveNode("d", "settled under a fold", store.Done, true), false},
		{liveNode("e", "failed under a fold", store.Failed, true), false},
		{liveNode("f", "running, unfiled", store.Running, false), false},
	} {
		if got := liveFolded(probe.node); got != probe.want {
			t.Fatalf("liveFolded(%s %s folded=%t) = %t, want %t",
				probe.node.ID, probe.node.Status, probe.node.Folded, got, probe.want)
		}
	}
}

// The widening is additive and order-preserving: the compact view keeps its own
// shape and the open corpus arrives behind it.
func TestWideningTheViewAddsTheOpenCorpusAndReordersNothing(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "one", "The first errand", "run the first errand")
	spliceSurgeryJob(t, graph, "two", "The second errand", "run the second errand")
	head := New(&beltClient{}, graph)

	active, err := graph.ActiveNodes()
	if err != nil {
		t.Fatal(err)
	}
	widened := head.widenWithOpen(active)
	if len(widened) != len(active) {
		t.Fatalf("a graph with nothing filed grew from %d to %d nodes", len(active), len(widened))
	}
	for index := range active {
		if widened[index].ID != active[index].ID {
			t.Fatalf("the widening reordered the view at %d: %q became %q",
				index, active[index].ID, widened[index].ID)
		}
	}

	// A view that had lost a live node gets it back, once, at the end.
	trimmed := make([]store.Node, 0, len(active))
	for _, node := range active {
		if node.ID != "two" {
			trimmed = append(trimmed, node)
		}
	}
	restored := head.widenWithOpen(trimmed)
	seen := 0
	for _, node := range restored {
		if node.ID == "two" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("the missing live node came back %d times", seen)
	}
	if restored[len(restored)-1].ID != "two" {
		t.Fatalf("the restored node landed at %q rather than behind the view", restored[len(restored)-1].ID)
	}
}

// A belt with no graph behind it must widen nothing rather than panic — the
// same guard every read on this belt carries.
func TestWideningASurfaceWithNoGraphIsHarmless(t *testing.T) {
	head := New(nil, nil)
	if got := head.widenWithOpen(nil); got != nil {
		t.Fatalf("a graphless surface widened to %v", got)
	}
	if _, err := head.headNodes(); err == nil {
		t.Fatal("a graphless surface answered a node read")
	} else if !strings.Contains(err.Error(), "graph") {
		t.Fatalf("the refusal does not name the missing graph: %v", err)
	}
}
