package resident

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The scenario the mechanism exists for: worker A runs out of budget with B
// waiting on it. The repair subtree must consume A's partial, live under the
// same job, and hold B until the remainder actually lands.
func TestReplanOverrunSplicesRepairAndRewiresWaiters(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole job"},
		{ID: "job-a", Parent: "job", Brief: "the oversized part"},
		{ID: "job-b", Parent: "job", Brief: "consumes a", Needs: []store.Need{{NodeID: "job-a", Kind: store.FeedsInto}}},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "test"}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("job-a", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}

	nodeA, _, _ := graph.Node("job-a")
	planned := func(_ context.Context, goal, prefix string) (store.Subtree, error) {
		if !strings.Contains(goal, "partial progress text") || !strings.Contains(goal, "/tmp/partial.md") {
			t.Fatalf("replan goal does not carry the partial result:\n%s", goal)
		}
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: prefix + "-n9", Brief: "finish it", Title: "Finish"},
			{ID: prefix + "-n5", Parent: prefix + "-n9", Brief: "remaining piece", Title: "Remaining piece"},
		}}, nil
	}
	spliced, sink, err := ReplanOverrun(context.Background(), graph, nodeA, "partial progress text", []string{"/tmp/partial.md"}, planned)
	if err != nil {
		t.Fatal(err)
	}
	if spliced != 2 || sink != "job-a-x1-n9" {
		t.Fatalf("spliced=%d sink=%q", spliced, sink)
	}

	// The repair belongs to the same job and consumes A's digest.
	repairEntry, ok, err := graph.Node("job-a-x1-n5")
	if err != nil || !ok {
		t.Fatalf("repair entry missing: %v", err)
	}
	sinkNode, _, _ := graph.Node(sink)
	if sinkNode.Parent != "job" || repairEntry.Parent != sink {
		t.Fatalf("repair parents wrong: sink under %q, entry under %q", sinkNode.Parent, repairEntry.Parent)
	}

	// B now waits for the finished remainder as well as A; nothing repair-side
	// is ready until A's partial lands.
	edges, err := graph.ActiveEdges()
	if err != nil {
		t.Fatal(err)
	}
	sinkFeedsB, aFeedsEntry := false, false
	for _, edge := range edges {
		if edge.From == sink && edge.To == "job-b" {
			sinkFeedsB = true
		}
		if edge.From == "job-a" && edge.To == "job-a-x1-n5" {
			aFeedsEntry = true
		}
	}
	if !sinkFeedsB || !aFeedsEntry {
		t.Fatalf("wiring incomplete: sink→b=%t a→entry=%t\n%v", sinkFeedsB, aFeedsEntry, edges)
	}
	ready, err := graph.Ready(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range ready {
		if node.ID == "job-b" || strings.HasPrefix(node.ID, "job-a-x1") {
			t.Fatalf("%s is ready while A still runs", node.ID)
		}
	}

	// A lands its partial; the repair entry becomes ready, B still waits.
	if err := graph.Complete(claim, "partial progress text"); err != nil {
		t.Fatal(err)
	}
	ready, _ = graph.Ready(10)
	readyIDs := map[string]bool{}
	for _, node := range ready {
		readyIDs[node.ID] = true
	}
	if !readyIDs["job-a-x1-n5"] || readyIDs["job-b"] {
		t.Fatalf("after A lands: ready=%v, want repair entry ready and b held", readyIDs)
	}

	// The journal reproduces the added edges.
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild after edge additions: %v", err)
	}

	// One oversized estimate never cascades: re-expansions do not re-expand.
	repair, _, _ := graph.Node("job-a-x1-n5")
	spliced, _, err = ReplanOverrun(context.Background(), graph, repair, "more partial", nil, planned)
	if err != nil || spliced != 0 {
		t.Fatalf("re-expansion of a re-expansion: spliced=%d err=%v", spliced, err)
	}
}
