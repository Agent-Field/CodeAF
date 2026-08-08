package plan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
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

// The other half of the same hole. The gathering node executes as an ordinary
// leaf and its output is the whole of what the person reads, but it was refused
// an instruction for not being KindWork — so what reached the executor, and what
// the delivery gate then judged the finished job against, was the harness's own
// two-line stub.
func TestTheDeliverableOwnerIsWrittenAnInstruction(t *testing.T) {
	graph := &Graph{Goal: "compare three cities and write the result", NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Berlin", Summary: "read Berlin"})
	graph.Add(Node{Stage: 1, Title: "Lisbon", Summary: "read Lisbon"})
	graph.addSynthesis()
	sink := graph.Nodes[len(graph.Nodes)-1].ID

	client := &briefFanoutClient{}
	if _, err := Briefs(context.Background(), client, graph); err != nil {
		t.Fatal(err)
	}
	if calls := client.count(); calls != 3 {
		t.Fatalf("brief calls = %d, want one per leaf and one for the deliverable owner", calls)
	}
	owner := graph.Node(sink)
	if owner == nil || strings.TrimSpace(owner.Brief) == "" || owner.Brief == owner.Summary {
		t.Fatalf("the deliverable owner still carries the harness stub: %q", owner.Brief)
	}
	// And it is told the thing only it is told: the deliverable is its own.
	if !strings.Contains(client.targetFor(sink), "owns the final deliverable") {
		t.Fatalf("the owner's instruction does not say it owns the deliverable:\n%s", client.targetFor(sink))
	}
}

type briefFanoutClient struct {
	mutex   sync.Mutex
	targets []string
}

func (c *briefFanoutClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.mutex.Lock()
	c.targets = append(c.targets, textOf(messages[len(messages)-1]))
	c.mutex.Unlock()
	return response("Do this part and hand over its concrete result."), nil
}

func (c *briefFanoutClient) count() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return len(c.targets)
}

// targetFor finds the call written for one node, by the id its target message
// opens with.
func (c *briefFanoutClient) targetFor(id int) string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, target := range c.targets {
		if strings.HasPrefix(target, fmt.Sprintf("Write the instruction for node %d,", id)) {
			return target
		}
	}
	return ""
}
