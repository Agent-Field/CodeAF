package resident

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The sentinel's four edits, mirrored onto the store: add splices, remove
// cancels, rewire replaces edges, retitle amends — and each refuses anything
// that already started.
func TestApplyRevisionMirrorsSentinelEditsOntoTheStore(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job-n9", Brief: "synthesize"},
		{ID: "job-n1", Parent: "job-n9", Brief: "research approach A", Title: "Approach A"},
		{ID: "job-n2", Parent: "job-n9", Brief: "build on A", Title: "Build on A",
			Needs: []store.Need{{NodeID: "job-n1", Kind: store.FeedsInto}}},
		{ID: "job-n3", Parent: "job-n9", Brief: "summarize findings", Title: "Summarize",
			Needs: []store.Need{{NodeID: "job-n2", Kind: store.FeedsInto}}},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "test"}); err != nil {
		t.Fatal(err)
	}

	planGraph := &plan.Graph{Goal: "the goal", Nodes: []plan.Node{
		{ID: 9, Title: "Synthesis", Kind: plan.KindSynthesis},
		{ID: 1, Title: "Approach A", State: plan.StateDone},
		{ID: 2, Title: "Build on B instead", Summary: "approach A was a dead end; build on B"},
		{ID: 3, Title: "Summarize", Summary: "summarize what B produced"},
		{ID: 4, Title: "Research approach B", Summary: "the result showed B is viable", Needs: []int{1}},
	}}

	operations := []plan.Operation{
		{Op: "add", Node: 4, Reason: "result contradicts approach A", Applied: true},
		{Op: "remove", Node: 2, Reason: "A is dead", Applied: true},
		{Op: "rewire", Node: 3, Needs: []int{4}, Reason: "summary now reads from B", Applied: true},
		{Op: "retitle", Node: 3, Reason: "scope shifted", Applied: true},
	}
	applied, notes := ApplyRevision(graph, planGraph, "job", "job-n9", operations)
	if applied != 4 {
		t.Fatalf("applied=%d notes=%v", applied, notes)
	}

	added, ok, _ := graph.Node("job-n4")
	if !ok || added.Parent != "job-n9" || added.Title != "Research approach B" {
		t.Fatalf("added node wrong: ok=%t %+v", ok, added)
	}
	removed, _, _ := graph.Node("job-n2")
	if removed.Status != store.Cancelled {
		t.Fatalf("removed node status = %s", removed.Status)
	}
	amended, _, _ := graph.Node("job-n3")
	if amended.Title != "Summarize" || amended.Brief == "summarize findings" {
		t.Fatalf("retitle did not amend: %+v", amended)
	}

	edges, _ := graph.ActiveEdges()
	oldEdge, newEdge := false, false
	for _, edge := range edges {
		if edge.To == "job-n3" && edge.From == "job-n2" {
			oldEdge = true
		}
		if edge.To == "job-n3" && edge.From == "job-n4" {
			newEdge = true
		}
	}
	if oldEdge || !newEdge {
		t.Fatalf("rewire wrong: old=%t new=%t %v", oldEdge, newEdge, edges)
	}

	// The journal reproduces every revision.
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild after revision: %v", err)
	}
	rebuilt, _, _ := graph.Node("job-n2")
	if rebuilt.Status != store.Cancelled {
		t.Fatalf("rebuild lost the cancellation: %s", rebuilt.Status)
	}

	// The store is the law: editing work that started is refused, not applied.
	claim, ok, err := graph.Claim("job-n1", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: %v", err)
	}
	_ = graph.Start(claim)
	applied, notes = ApplyRevision(graph, planGraph, "job", "job-n9", []plan.Operation{
		{Op: "remove", Node: 1, Reason: "should be refused", Applied: true},
	})
	if applied != 0 || len(notes) == 0 {
		t.Fatalf("started node was edited: applied=%d notes=%v", applied, notes)
	}
}
