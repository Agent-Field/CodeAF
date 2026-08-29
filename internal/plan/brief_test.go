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

// The criterion rides the call that was already being paid for, so the same
// answer now carries both halves and the graph has to end up holding both.
func TestBriefCallReturnsTheCriterion(t *testing.T) {
	graph := &Graph{Goal: "produce the thing", NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Only", Summary: "do it", Sources: []string{"the one place"}})

	client := &briefJSONClient{body: `{"instruction": "Do the work and hand back the result.",
	  "done": {"produces": ["the named result"],
	  "conditions": [{"kind": "run", "check": "the stated command", "expect": "it reports success"},
	                 {"kind": "read", "check": "the named result", "expect": "it is present"}]}}`}
	if _, err := Briefs(context.Background(), client, graph); err != nil {
		t.Fatal(err)
	}
	node := graph.Node(graph.Nodes[0].ID)
	if node.Brief != "Do the work and hand back the result." {
		t.Fatalf("brief = %q", node.Brief)
	}
	// Dual-write: the prose field and the object say the same thing.
	if node.Spec.Instruction != node.Brief {
		t.Fatalf("spec instruction %q != brief %q", node.Spec.Instruction, node.Brief)
	}
	if len(node.Spec.Done.Conditions) != 2 || node.Spec.Done.Conditions[0].Kind != CheckRun {
		t.Fatalf("criterion did not land: %+v", node.Spec.Done)
	}
	if len(node.Spec.Sources) != 1 || node.Spec.Sources[0] != "the one place" {
		t.Fatalf("spec sources = %#v", node.Spec.Sources)
	}
	// The words that make a criterion settleable by a third party are in the
	// prompt, not in this test's expectations of the model.
	if !strings.Contains(briefWithCriterion, "did not do the work") ||
		!strings.Contains(briefWithCriterion, "Write no condition the request did not ask for") {
		t.Fatal("the criterion block lost the clauses that bound it")
	}
	// And the instruction half is untouched, so the shared prefix is untouched.
	if !strings.HasPrefix(briefWithCriterion, briefPrompt) {
		t.Fatal("the criterion block was not appended to the instruction prompt")
	}
}

// An answer with no criterion in it is today's answer, and today's answer is
// legal everywhere. This is the compatibility half of the rollback proof.
func TestBriefWithoutDoneIsTodaysBrief(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "json with no done key", body: `{"instruction": "Do this part and hand over its concrete result."}`},
		{name: "prose, from a model that ignored the schema", body: "Do this part and hand over its concrete result."},
		{name: "a fenced object, from a router that fell back", body: "```json\n{\"instruction\": \"Do this part and hand over its concrete result.\"}\n```"},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := &Graph{Goal: "produce the thing", NextID: 1}
			graph.Add(Node{Stage: 1, Title: "Only", Summary: "do it"})
			if _, err := Briefs(context.Background(), &briefJSONClient{body: test.body}, graph); err != nil {
				t.Fatal(err)
			}
			node := graph.Node(graph.Nodes[0].ID)
			if node.Brief != "Do this part and hand over its concrete result." {
				t.Fatalf("brief = %q", node.Brief)
			}
			if !node.Spec.Done.Empty() {
				t.Fatalf("a criterion was invented from nothing: %+v", node.Spec.Done)
			}
			// Empty criterion, and the spec still renders as the object it is —
			// but the node reads exactly as it did before criteria existed.
			if node.Spec.Instruction != node.Brief {
				t.Fatalf("spec instruction %q != brief %q", node.Spec.Instruction, node.Brief)
			}
		})
	}
}

// The rollback switch itself: off, no schema is sent and the answer is prose.
func TestCriterionOffSendsTheOldPrompt(t *testing.T) {
	Criterion = false
	defer func() { Criterion = true }()

	graph := &Graph{Goal: "produce the thing", NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Only", Summary: "do it"})
	client := &briefJSONClient{body: "Do this part and hand over its concrete result."}
	if _, err := Briefs(context.Background(), client, graph); err != nil {
		t.Fatal(err)
	}
	if client.sawSchema() {
		t.Fatal("a schema was still attached with the criterion off")
	}
	if system := client.system(); system != briefPrompt {
		t.Fatalf("the system prompt was not the pre-criterion one:\n%s", system)
	}
	if node := graph.Node(graph.Nodes[0].ID); !node.Spec.Done.Empty() {
		t.Fatalf("a criterion arrived with the switch off: %+v", node.Spec.Done)
	}
}

type briefJSONClient struct {
	body    string
	mutex   sync.Mutex
	systems []string
	schemas int
}

func (c *briefJSONClient) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	c.mutex.Lock()
	c.systems = append(c.systems, textOf(messages[0]))
	// Applied to a request the way the adapter applies them, so a ceiling
	// option is not mistaken for a schema.
	request := &ai.Request{}
	for _, option := range options {
		_ = option(request)
	}
	if request.ResponseFormat != nil {
		c.schemas++
	}
	c.mutex.Unlock()
	return response(c.body), nil
}

func (c *briefJSONClient) sawSchema() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.schemas > 0
}

func (c *briefJSONClient) system() string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if len(c.systems) == 0 {
		return ""
	}
	return c.systems[0]
}
