package plan

import "testing"

// TestSpliceMarksParentExpanded guards a bug that cost a whole subtree.
//
// Splice held a *Node into g.Nodes while adding children, and Add appends to
// that slice. Once the backing array grew, the pointer aimed at the old one and
// every write through it — including the flag saying "this node has been
// expanded" — was silently dropped. The parent stayed marked as unexpanded work
// and the next recursion level expanded it a second time, duplicating every
// child. Nothing errored; the graph just quietly did the work twice.
//
// The child count here is deliberately larger than the parent graph, so append
// is guaranteed to reallocate.
func TestSpliceMarksParentExpanded(t *testing.T) {
	graph := &Graph{Goal: "root", Stages: []Stage{{Title: "One"}}, NextID: 1}
	upstream := graph.Add(Node{Stage: 1, Title: "Upstream", Size: SizeAtomic})
	parent := graph.Add(Node{Stage: 1, Title: "Parent", Size: SizeOversized})
	if err := graph.AddNeed(parent, upstream); err != nil {
		t.Fatalf("AddNeed: %v", err)
	}

	sub := &Graph{Goal: "parent", Stages: []Stage{{Title: "One"}}, NextID: 1}
	for _, title := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		sub.Add(Node{Stage: 1, Title: title, Size: SizeAtomic})
	}

	if err := graph.Splice(parent, sub); err != nil {
		t.Fatalf("Splice: %v", err)
	}

	node := graph.Node(parent)
	if node.Kind != KindSynthesis {
		t.Errorf("parent kind = %q, want %q — it would be expanded again", node.Kind, KindSynthesis)
	}
	if node.Size != SizeUnknown {
		t.Errorf("parent size = %q, want cleared — it would be reselected as oversized", node.Size)
	}
	if len(node.Needs) != 8 {
		t.Errorf("parent needs %v, want its 8 children", node.Needs)
	}
	for _, need := range node.Needs {
		if child := graph.Node(need); child == nil || child.Parent != parent {
			t.Errorf("need %d is not a child of the parent", need)
		}
	}
	// Children with no upstream inside the subtree take the parent's inputs, or
	// they would start before data they were promised exists.
	for _, child := range graph.Nodes {
		if child.Parent != parent {
			continue
		}
		if !contains(child.Needs, upstream) {
			t.Errorf("child %d (%s) needs %v, want it to inherit %d", child.ID, child.Title, child.Needs, upstream)
		}
	}
	if graph.hasCycle() {
		t.Error("splice produced a cycle")
	}
}

// TestSpliceRejectsFrozenParent keeps the dynamism rule honest: work that has
// started may not be restructured underneath itself.
func TestSpliceRejectsFrozenParent(t *testing.T) {
	graph := &Graph{Goal: "root", Stages: []Stage{{Title: "One"}}, NextID: 1}
	parent := graph.Add(Node{Stage: 1, Title: "Parent", State: StateRunning})
	sub := &Graph{NextID: 1}
	sub.Add(Node{Stage: 1, Title: "A"})
	sub.Add(Node{Stage: 1, Title: "B"})

	if err := graph.Splice(parent, sub); err == nil {
		t.Fatal("Splice on a running node succeeded, want refusal")
	}
	if len(graph.Nodes) != 1 {
		t.Errorf("graph gained nodes from a refused splice: %d", len(graph.Nodes))
	}
}

// TestWavesIgnoreStages is the central claim of the design: a node that needs
// nothing starts immediately, whatever stage produced it.
func TestWavesIgnoreStages(t *testing.T) {
	graph := &Graph{Goal: "root", Stages: []Stage{{Title: "One"}, {Title: "Two"}}, NextID: 1}
	first := graph.Add(Node{Stage: 1, Title: "First"})
	late := graph.Add(Node{Stage: 2, Title: "Independent"})
	dependent := graph.Add(Node{Stage: 2, Title: "Dependent"})
	if err := graph.AddNeed(dependent, first); err != nil {
		t.Fatalf("AddNeed: %v", err)
	}

	waves := graph.Waves()
	if len(waves) != 2 {
		t.Fatalf("waves = %d, want 2", len(waves))
	}
	if !contains(waves[0], late) {
		t.Errorf("stage-2 node with no inputs is in wave %v, want wave 0", waves)
	}
	if !contains(waves[1], dependent) {
		t.Errorf("dependent node not in wave 1: %v", waves)
	}
}
