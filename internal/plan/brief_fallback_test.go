package plan

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// briefFailClient answers every planning pass but the brief one through
// passClient, and answers the brief pass with a reply that is not language —
// the exact shape of the failure this file is about. refuse names the node
// whose calls fail; the empty string fails all of them. failures caps how many
// of that node's calls fail, so a test can let the retry land.
type briefFailClient struct {
	refuse   string
	failures int

	mutex    sync.Mutex
	refused  int
	answered int
}

func (c *briefFailClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var system, target string
	for _, message := range messages {
		if message.Role == "system" {
			system = textOf(message)
			continue
		}
		target = textOf(message)
	}
	if system != briefPrompt && system != briefWithCriterion {
		return (&passClient{}).CompleteWithMessages(ctx, messages, options...)
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.refuse == "" || strings.Contains(target, strconv.Quote(c.refuse)) {
		if c.failures == 0 || c.refused < c.failures {
			c.refused++
			return nil, errors.New("the reply stopped being language and was cut")
		}
	}
	c.answered++
	return response(`{"instruction":"A written instruction, from a model that answered."}`), nil
}

func (c *briefFailClient) counts() (refused, answered int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.refused, c.answered
}

// briefTwoLeaves is the graph both of the first two tests are run over: one node
// whose brief call answers and one whose brief call does not.
func briefTwoLeaves() *Graph {
	graph := &Graph{Goal: "read the registers and write the result", NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Berlin", Summary: "read the Berlin register"})
	graph.Add(Node{Stage: 1, Title: "Lisbon", Summary: "read the Lisbon register",
		Sources: []string{"register.txt"}})
	return graph
}

// writeBriefsWith runs the brief pass over a graph exactly as Briefs does, with
// a journal wired in — which Briefs has no parameter for and which is where the
// account of a composed brief has to land.
func writeBriefsWith(graph *Graph, client Completer, journal BriefJournal) Usage {
	writer := newBriefWriter(context.Background(), client, true, nil, journal)
	writer.sink = graph.deliverableSink()
	shared := graph.context() + "\nThe full plan:\n" + graph.briefCatalog()
	for _, id := range graph.writtenLeaves() {
		node := graph.Node(id)
		writer.launch(shared, *node, nil, graph.deliverableLine(id))
	}
	usage, _ := writer.apply(graph)
	return usage
}

// THE LAW, at the seam it was broken at. A six-node plan was thrown away
// because one node's brief call came back cut: the graph was sound, five of its
// instructions were written, and the whole goal ran as a single oversized
// worker. The node that could not be written for now keeps a brief composed
// from what the plan already knows about it, and the plan keeps its shape.
func TestABriefThatFailsTwiceIsComposedAndTheGraphIsKept(t *testing.T) {
	graph := briefTwoLeaves()
	client := &briefFailClient{refuse: "Lisbon"}

	var journalled []store.NodeBrief
	writeBriefsWith(graph, client, func(_ *Graph, _ int, brief store.NodeBrief) {
		journalled = append(journalled, brief)
	})

	if len(graph.Nodes) != 2 {
		t.Fatalf("the graph lost nodes to a brief that could not be written: %d left", len(graph.Nodes))
	}
	if got := graph.Nodes[0].Brief; !strings.HasPrefix(got, "A written instruction") {
		t.Fatalf("the healthy leaf lost its written brief: %q", got)
	}
	// Composed from the node's own title, what the plan said it is for, and the
	// files it is expected to touch — which is the whole of what the plan knows.
	lisbon := graph.Nodes[1]
	if lisbon.Brief != ComposedBrief(Node{Title: "Lisbon", Summary: "read the Lisbon register",
		Sources: []string{"register.txt"}}) {
		t.Fatalf("the failed leaf was not composed for: %q", lisbon.Brief)
	}
	for _, want := range []string{"Lisbon", "read the Lisbon register", "register.txt"} {
		if !strings.Contains(lisbon.Brief, want) {
			t.Errorf("the composed brief does not carry %q:\n%s", want, lisbon.Brief)
		}
	}
	if lisbon.Spec.Instruction != lisbon.Brief {
		t.Errorf("spec instruction %q != brief %q", lisbon.Spec.Instruction, lisbon.Brief)
	}
	// One call and exactly one retry, on the same bytes.
	if refused, answered := client.counts(); refused != 2 || answered != 1 {
		t.Errorf("brief calls: %d refused and %d answered, want one call plus one retry for the "+
			"failing node and one call for the other", refused, answered)
	}
	// And the account of it: an autopsy can tell which brief nobody wrote.
	faults := 0
	for _, brief := range journalled {
		if brief.Fault == "" {
			continue
		}
		faults++
		if !strings.Contains(brief.Fault, "stopped being language") {
			t.Errorf("the journalled fault does not say what happened: %q", brief.Fault)
		}
		if brief.Brief != lisbon.Brief {
			t.Errorf("the journal recorded a different brief than the node carries: %q", brief.Brief)
		}
	}
	if len(journalled) != 2 || faults != 1 {
		t.Fatalf("journalled %d briefs with %d faults, want one per node and one fault", len(journalled), faults)
	}
}

// The retry is a retry and not a formality: a call that fails once and answers
// the second time leaves the model's own instruction on the node, with nothing
// composed and nothing journalled against it.
func TestABriefThatFailsOnceIsRetriedAndWritten(t *testing.T) {
	graph := briefTwoLeaves()
	client := &briefFailClient{refuse: "Lisbon", failures: 1}

	var journalled []store.NodeBrief
	writeBriefsWith(graph, client, func(_ *Graph, _ int, brief store.NodeBrief) {
		journalled = append(journalled, brief)
	})

	for _, node := range graph.Nodes {
		if !strings.HasPrefix(node.Brief, "A written instruction") {
			t.Fatalf("%s did not end up with the written brief: %q", node.Title, node.Brief)
		}
	}
	if refused, answered := client.counts(); refused != 1 || answered != 2 {
		t.Fatalf("brief calls: %d refused and %d answered, want one refusal and both nodes answered",
			refused, answered)
	}
	for _, brief := range journalled {
		if brief.Fault != "" {
			t.Errorf("a brief the retry wrote was journalled as a fault: %q", brief.Fault)
		}
	}
}

// The whole of it, through the door a run actually uses. Every brief call in the
// build fails, twice each, and Build still hands back the graph it drew — with
// no error, because a brief is one leaf's instruction and never the graph's
// right to exist.
func TestBuildKeepsTheGraphWhenEveryBriefFails(t *testing.T) {
	graph, err := Build(context.Background(), &briefFailClient{}, "review the change and write REVIEW.md",
		Options{Ensemble: EnsembleNever, SpineSamples: 1, NodeBudget: 20, Briefs: true})
	if err != nil {
		t.Fatalf("Build returned an error for briefs it could not write: %v", err)
	}
	if graph == nil || len(graph.Nodes) < 2 {
		t.Fatalf("the drawn graph did not survive: %+v", graph)
	}
	leaves := graph.writtenLeaves()
	if len(leaves) == 0 {
		t.Fatal("the build produced no leaves to brief")
	}
	for _, id := range leaves {
		node := graph.Node(id)
		if strings.TrimSpace(node.Brief) == "" {
			t.Errorf("node %d (%s) came out of the build with no instruction at all", id, node.Title)
		}
		if node.Brief != ComposedBrief(*node) {
			t.Errorf("node %d (%s) was not composed for: %q", id, node.Title, node.Brief)
		}
	}
}

// One composition, reached from two roads. The scheduler composes at dispatch
// for a graph planned without briefs; the planner composes during the build for
// a call that would not answer. A second spelling of "what a node says when
// nobody wrote its instruction" is the drift this names.
func TestComposedBriefIsWrittenFromWhatThePlanKnows(t *testing.T) {
	composed := ComposedBrief(Node{Title: "Read register.txt", Summary: "List every entry it holds",
		Sources: []string{"register.txt", "ledger.txt"}})
	for _, want := range []string{"Read register.txt", "List every entry it holds", "register.txt; ledger.txt"} {
		if !strings.Contains(composed, want) {
			t.Errorf("the composition dropped %q:\n%s", want, composed)
		}
	}
	// The goal is not in it. Every leaf is handed the goal beside its brief, and
	// a composition that repeated it would say the same thing twice in one
	// prompt.
	if strings.Contains(composed, "larger goal") {
		t.Errorf("the composition restates the goal:\n%s", composed)
	}
	// A gathering node is the harness's own, in every road that reaches it.
	if ComposedBrief(Node{Kind: KindSynthesis, Title: "Gather"}) != synthesisBrief {
		t.Error("the gathering node's instruction is no longer the harness's own")
	}
	// Nothing known, nothing invented: the emptiness law holds here too.
	if got := ComposedBrief(Node{}); got != "" {
		t.Errorf("a node the plan knows nothing about was given words anyway: %q", got)
	}
}
