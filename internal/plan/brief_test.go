package plan

import (
	"strings"
	"testing"
)

// TestOneNodeOwnsTheDeliverable is the fix for five agents writing REVIEW.md
// over the top of each other. Every brief is told who produces the goal's
// deliverable, and across a whole graph exactly one of them is told it is
// itself — for any shape the graph takes.
func TestOneNodeOwnsTheDeliverable(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func(g *Graph) int // returns the node expected to own it
	}{
		{
			name: "a finished graph, owned by the synthesis",
			build: func(g *Graph) int {
				g.Add(Node{Stage: 1, Title: "Berlin"})
				g.Add(Node{Stage: 1, Title: "Lisbon"})
				g.Add(Node{Stage: 1, Title: "Warsaw"})
				g.addSynthesis()
				return g.Nodes[len(g.Nodes)-1].ID
			},
		},
		{
			name: "a chain, owned by its last node",
			build: func(g *Graph) int {
				first := g.Add(Node{Stage: 1, Title: "Gather"})
				last := g.Add(Node{Stage: 2, Title: "Report"})
				if err := g.AddNeed(last, first); err != nil {
					t.Fatalf("AddNeed: %v", err)
				}
				return last
			},
		},
		{
			name: "a single node, which owns everything it is",
			build: func(g *Graph) int {
				return g.Add(Node{Stage: 1, Title: "Draft"})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := &Graph{Goal: "write REVIEW.md", Stages: []Stage{{Title: "One"}, {Title: "Two"}}, NextID: 1}
			want := test.build(graph)

			owners := 0
			for _, node := range graph.Nodes {
				line := graph.deliverableLine(node.ID)
				if strings.Contains(line, "owns the final deliverable") {
					owners++
					if node.ID != want {
						t.Errorf("node %d (%s) was told it owns the deliverable, want node %d", node.ID, node.Title, want)
					}
					continue
				}
				if !strings.Contains(line, "not here") {
					t.Errorf("node %d (%s) is neither told it owns the deliverable nor whose it is: %q", node.ID, node.Title, line)
				}
			}
			if owners != 1 {
				t.Errorf("%d nodes were told they own the deliverable, want exactly 1", owners)
			}
		})
	}
}

// TestOpenGraphNamesTheOwnerByRole covers the timing hazard. Briefs are written
// while the graph is still being built, before the synthesis that will gather
// the sinks exists, so a non-owner still has to be told the deliverable is
// somewhere else.
func TestOpenGraphNamesTheOwnerByRole(t *testing.T) {
	graph := &Graph{Goal: "write REVIEW.md", Stages: []Stage{{Title: "One"}}, NextID: 1}
	first := graph.Add(Node{Stage: 1, Title: "Berlin"})
	graph.Add(Node{Stage: 1, Title: "Lisbon"})

	line := graph.deliverableLine(first)
	if strings.Contains(line, "owns the final deliverable") {
		t.Error("a sibling among several sinks was told it owns the deliverable")
	}
	if !strings.Contains(line, "the final step that assembles every result") {
		t.Errorf("the owner is not named while the graph is still open: %q", line)
	}
}

// TestBriefPromptStatesSingleOwnership pins the instruction that turns the line
// above into words the executing agent actually reads.
func TestBriefPromptStatesSingleOwnership(t *testing.T) {
	for _, want := range []struct {
		name   string
		phrase string
	}{
		{"exactly one node", "Exactly one node produces the goal's final deliverable"},
		{"the brief is told which", "and you are told which"},
		{"non-owners hand over", "hands its own result over instead of writing any"},
		{"the reason", "they each write\nthe same file over the top of the others"},
		{"the owner is told so", "say that it is\nthis agent's alone"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if !strings.Contains(briefPrompt, want.phrase) {
				t.Errorf("brief prompt no longer states %s: missing %q", want.name, want.phrase)
			}
		})
	}
}
