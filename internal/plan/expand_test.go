package plan

import (
	"strings"
	"testing"
)

// TestPhaseShapedSplitIsAbsorbed is the structural half of the subject rule.
//
// The prompt tells the model not to return phases; this is what happens when it
// does anyway. A phase split arrives either as one part (the honest answer for a
// procedure) or as parts that are each still the whole procedure in disguise,
// and both are refused, so the parent keeps the sequence inside itself instead
// of becoming siblings that race each other.
func TestPhaseShapedSplitIsAbsorbed(t *testing.T) {
	for _, test := range []struct {
		name     string
		children []Node
		keep     bool
	}{
		{
			name:     "procedure returned whole",
			children: []Node{{Title: "Review", Size: SizeBorderline}},
		},
		{
			name: "phases that each restate the parent",
			children: []Node{
				{Title: "Read", Size: SizeOversized},
				{Title: "Test", Size: SizeOversized},
				{Title: "Write", Size: SizeOversized},
			},
		},
		{
			name: "majority still oversized",
			children: []Node{
				{Title: "Read", Size: SizeOversized},
				{Title: "Test", Size: SizeOversized},
				{Title: "Write", Size: SizeOversized},
				{Title: "Diff", Size: SizeAtomic},
				{Title: "Notes", Size: SizeAtomic},
			},
		},
		{
			name: "subjects that shrank",
			children: []Node{
				{Title: "Parser", Size: SizeAtomic},
				{Title: "Loader", Size: SizeAtomic},
				{Title: "Writer", Size: SizeBorderline},
			},
			keep: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			sub := &Graph{Goal: "parent", NextID: 1}
			for _, child := range test.children {
				sub.Add(child)
			}
			if got := worthKeeping(nil, expansion{nodeID: 1, sub: sub}); got != test.keep {
				t.Errorf("worthKeeping = %v, want %v", got, test.keep)
			}
		})
	}
}

// TestPhaseSplitLeavesTheNodeWhole runs the same contract through the level
// loop, which is where absorption actually has to happen: a refused expansion
// must leave the node as work, not as a synthesis over four racing phases.
func TestPhaseSplitLeavesTheNodeWhole(t *testing.T) {
	client := &stubClient{reply: func(system, _ string) string {
		switch {
		case strings.Contains(system, "You list the parts of one stage"):
			return `{"parts":[
				{"title":"Read","summary":"Read the code","sources":["the tree"]},
				{"title":"Test","summary":"Run the tests","sources":["the suite"]},
				{"title":"Write","summary":"Write it up","sources":["the review"]}]}`
		default:
			return `{"sizes":[
				{"node":1,"size":"oversized","split_into":[]},
				{"node":2,"size":"oversized","split_into":[]},
				{"node":3,"size":"oversized","split_into":[]}]}`
		}
	}}

	graph := &Graph{Goal: "review the pull request", Stages: []Stage{{Title: "Review"}}, NextID: 1}
	parent := graph.Add(Node{Stage: 1, Title: "Review", Summary: "Review the change",
		Size: SizeOversized, Parts: []string{"read", "test"}})

	spliced, _, err := ExpandLevel(t.Context(), client, graph, Options{MaxDepth: 2, NodeBudget: 40})
	if err != nil {
		t.Fatalf("ExpandLevel: %v", err)
	}
	if spliced != 0 {
		t.Fatalf("spliced %d nodes from a phase-shaped split, want 0", spliced)
	}
	node := graph.Node(parent)
	if node.Kind != KindWork {
		t.Errorf("parent kind = %q, want %q — the procedure must stay one node", node.Kind, KindWork)
	}
	if len(graph.Nodes) != 1 {
		t.Errorf("graph grew to %d nodes on a refused expansion", len(graph.Nodes))
	}
}
