package plan

import "testing"

// The one-sitting collapse: a chain of atomic nodes, one per stage, each
// needing only the one before it, is folded into a single leaf whose brief is
// the goal. Anything richer — width, an unmeasured node, a fidget of five — is
// left exactly as drawn.
func TestCollapseAtomicChain(t *testing.T) {
	chainOf := func(n int) *Graph {
		graph := &Graph{Goal: "the goal", Stages: make([]Stage, n)}
		previous := 0
		for i := 1; i <= n; i++ {
			node := Node{Kind: KindWork, Stage: i, Size: SizeAtomic, Subharness: LinearSubharness, Title: "link"}
			if previous != 0 {
				node.Needs = []int{previous}
			}
			previous = graph.Add(node)
		}
		return graph
	}

	graph := chainOf(3)
	if folded := collapseAtomicChain(graph); folded != 3 {
		t.Fatalf("collapseAtomicChain = %d, want 3", folded)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("collapsed graph has %d nodes, want 1", len(graph.Nodes))
	}
	single := graph.Nodes[0]
	if single.Brief != "the goal" || single.Kind != KindWork || single.Stage != 1 || len(single.Needs) != 0 {
		t.Fatalf("collapsed node = %+v, want one stage-1 work node carrying the goal", single)
	}
	if single.Undivided == "" {
		t.Fatal("collapsed node records no diagnosis of why it was left whole")
	}

	// A one-node graph is nothing to fold.
	if folded := collapseAtomicChain(chainOf(1)); folded != 0 {
		t.Fatalf("single node folded = %d, want 0", folded)
	}
	// Five links is past one sitting.
	if folded := collapseAtomicChain(chainOf(5)); folded != 0 {
		t.Fatalf("five-link chain folded = %d, want 0", folded)
	}
	// Width is not a chain: two nodes in stage 1 stand.
	wide := chainOf(2)
	wide.Nodes[0].Stage = 1
	wide.Nodes[1].Stage = 1
	wide.Nodes[1].Needs = nil
	if folded := collapseAtomicChain(wide); folded != 0 {
		t.Fatalf("wide graph folded = %d, want 0", folded)
	}
	// A node the ruler has not finished with is not collapsible.
	unmeasured := chainOf(2)
	unmeasured.Nodes[1].Size = SizeBorderline
	if folded := collapseAtomicChain(unmeasured); folded != 0 {
		t.Fatalf("borderline chain folded = %d, want 0", folded)
	}
	// A fan-in is not a chain.
	fanIn := chainOf(2)
	fanIn.Nodes[1].Needs = []int{fanIn.Nodes[0].ID, 99}
	if folded := collapseAtomicChain(fanIn); folded != 0 {
		t.Fatalf("fan-in graph folded = %d, want 0", folded)
	}
}
