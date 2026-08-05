package plan

import (
	"strings"
	"testing"
)

// TestBindPromptStatesBothReasons pins that mutation is written as a reason for
// an edge in its own right, not as a footnote to information flow.
func TestBindPromptStatesBothReasons(t *testing.T) {
	for _, want := range []struct {
		name   string
		phrase string
	}{
		{"two reasons, equal weight", "There are two reasons for an edge, and they count equally"},
		{"information reason", "INFORMATION — this node cannot produce"},
		{"state reason", "STATE — that node changes the shared material"},
		{"mutators first", "Whoever\nchanges shared material runs before everyone who reads it"},
		{"no fact need cross", "even when no fact\ncrosses over"},
		{"readers alone need no edge", "only read the same unchanged\nmaterial need no edge"},
		{"same-stage edges are for state alone", "You may also list a\nnode from this same stage, but only for STATE"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if !strings.Contains(bindPrompt, want.phrase) {
				t.Errorf("bind prompt no longer states %s: missing %q", want.name, want.phrase)
			}
		})
	}
}

// TestBindPromptExemptsGatheringNodes pins the exception to "an empty list is
// the normal answer". Without it, the node that assembles the deliverable reads
// the empty answer as the blessed one — a real run left the review writer with
// no inputs and it launched at t=0 next to the work it was meant to consume.
func TestBindPromptExemptsGatheringNodes(t *testing.T) {
	for _, phrase := range []string{
		"The exception is a node that gathers",
		"Name the nodes whose outputs it assembles",
	} {
		if !strings.Contains(bindPrompt, phrase) {
			t.Errorf("bind prompt no longer exempts gathering nodes: missing %q", phrase)
		}
	}
}

// TestSameStageEdgeOrdersMutatorFirst is the structural half of the rule. The
// run that motivated it had four stage-1 siblings sharing one workspace, one of
// them patching it; before this, an edge between siblings was silently dropped
// as "not from an earlier stage" and all four ran at once.
func TestSameStageEdgeOrdersMutatorFirst(t *testing.T) {
	graph := &Graph{Goal: "review the pull request", Stages: []Stage{{Title: "Review"}}, NextID: 1}
	patch := graph.Add(Node{Stage: 1, Title: "Apply"})
	read := graph.Add(Node{Stage: 1, Title: "Read"})
	test := graph.Add(Node{Stage: 1, Title: "Test"})

	graph.setNeeds(read, []int{patch})
	graph.setNeeds(test, []int{patch})

	for _, reader := range []int{read, test} {
		if !contains(graph.Node(reader).Needs, patch) {
			t.Errorf("node %d does not wait for the node that patches the tree: %v", reader, graph.Node(reader).Needs)
		}
	}
	if len(graph.Node(patch).Needs) != 0 {
		t.Errorf("the mutating node waits for %v, want nothing", graph.Node(patch).Needs)
	}
	if graph.hasCycle() {
		t.Error("same-stage ordering produced a cycle")
	}
}

// TestSetNeedsFiltersImpossibleEdges keeps the acyclicity guarantee honest now
// that one class of edge is no longer safe by construction.
func TestSetNeedsFiltersImpossibleEdges(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func(g *Graph) (target int, want []int)
	}{
		{
			name: "earlier stage is kept",
			build: func(g *Graph) (int, []int) {
				early := g.Add(Node{Stage: 1, Title: "Early"})
				late := g.Add(Node{Stage: 2, Title: "Late"})
				g.setNeeds(late, []int{early})
				return late, []int{early}
			},
		},
		{
			name: "later stage is dropped",
			build: func(g *Graph) (int, []int) {
				early := g.Add(Node{Stage: 1, Title: "Early"})
				late := g.Add(Node{Stage: 2, Title: "Late"})
				g.setNeeds(early, []int{late})
				return early, nil
			},
		},
		{
			name: "self reference is dropped",
			build: func(g *Graph) (int, []int) {
				only := g.Add(Node{Stage: 1, Title: "Only"})
				other := g.Add(Node{Stage: 1, Title: "Other"})
				g.setNeeds(only, []int{only, other})
				return only, []int{other}
			},
		},
		{
			name: "a mutual pair keeps only the first edge",
			build: func(g *Graph) (int, []int) {
				first := g.Add(Node{Stage: 1, Title: "First"})
				second := g.Add(Node{Stage: 1, Title: "Second"})
				g.setNeeds(second, []int{first})
				g.setNeeds(first, []int{second})
				return first, nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := &Graph{Goal: "goal", Stages: []Stage{{Title: "One"}, {Title: "Two"}}, NextID: 1}
			target, want := test.build(graph)
			got := graph.Node(target).Needs
			if len(got) != len(want) {
				t.Fatalf("needs = %v, want %v", got, want)
			}
			for index := range want {
				if got[index] != want[index] {
					t.Fatalf("needs = %v, want %v", got, want)
				}
			}
			if graph.hasCycle() {
				t.Error("graph has a cycle")
			}
		})
	}
}

// TestBindCoversStageOne checks that the pass is actually asked about the stage
// where the racing siblings live. Stage 1 used to be skipped outright.
func TestBindCoversStageOne(t *testing.T) {
	client := &stubClient{reply: func(_, user string) string {
		if strings.Contains(user, "stage 1") {
			return `{"bindings":[{"node":2,"needs":[1]},{"node":3,"needs":[1]}],"duplicates":[]}`
		}
		return `{"bindings":[],"duplicates":[]}`
	}}

	graph := &Graph{Goal: "review the pull request", Stages: []Stage{{Title: "Review"}}, NextID: 1}
	patch := graph.Add(Node{Stage: 1, Title: "Apply", Summary: "Apply the diff to the tree"})
	read := graph.Add(Node{Stage: 1, Title: "Read", Summary: "Read the changed code"})
	test := graph.Add(Node{Stage: 1, Title: "Test", Summary: "Run the suite"})

	if _, err := Bind(t.Context(), client, graph); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if !client.asked("stage 1") {
		t.Fatal("bind never asked about stage 1, so siblings can never be ordered")
	}
	for _, reader := range []int{read, test} {
		if !contains(graph.Node(reader).Needs, patch) {
			t.Errorf("node %d needs %v, want it to wait for %d", reader, graph.Node(reader).Needs, patch)
		}
	}
}

// TestBindSkipsAStageWithNothingToPointAt keeps the extra call off plans that
// cannot use it: one node in stage 1 has no sibling to be ordered against.
func TestBindSkipsAStageWithNothingToPointAt(t *testing.T) {
	client := &stubClient{reply: func(_, _ string) string {
		return `{"bindings":[],"duplicates":[]}`
	}}
	graph := &Graph{Goal: "goal", Stages: []Stage{{Title: "One"}, {Title: "Two"}}, NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Only", Summary: "The one thing in stage 1"})
	graph.Add(Node{Stage: 2, Title: "After", Summary: "Comes later"})

	usage, err := Bind(t.Context(), client, graph)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if client.asked("stage 1") {
		t.Error("bind spent a call on a stage-1 node with no sibling to wait for")
	}
	if !client.asked("stage 2") {
		t.Error("bind skipped stage 2")
	}
	if usage.Calls != 1 {
		t.Errorf("usage counts %d calls, want 1 — a stage that was never asked is not a call", usage.Calls)
	}
}
