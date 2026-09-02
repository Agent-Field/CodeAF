package plan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// countingPlanner answers every pass and records which ones were reached, so a
// test can assert what a build did NOT buy.
type countingPlanner struct {
	mutex     sync.Mutex
	stages    string
	sizeReply string
	chain     string
	passes    []string
}

func (c *countingPlanner) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var system string
	for _, message := range messages {
		if message.Role == "system" {
			system = textOf(message)
		}
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	switch system {
	case spinePrompt:
		c.passes = append(c.passes, "spine")
		return textResponse(c.stages), nil
	case groundPrompt:
		c.passes = append(c.passes, "ground")
		return textResponse(`{"settled":[],"open":[]}`), nil
	case fanoutPrompt:
		c.passes = append(c.passes, "fanout")
		return textResponse(`{"parts":[{"title":"Part","summary":"Do the part."}]}`), nil
	case bindPrompt:
		c.passes = append(c.passes, "bind")
		return textResponse(`{"bindings":[],"duplicates":[]}`), nil
	case sizePromptWith(Anchors()):
		c.passes = append(c.passes, "size")
		if c.sizeReply != "" {
			return textResponse(c.sizeReply), nil
		}
		return textResponse(`{"sizes":[{"node":1,"size":"atomic","split_into":[]},{"node":2,"size":"atomic","split_into":[]}]}`), nil
	case auditPrompt:
		c.passes = append(c.passes, "audit")
		return textResponse(`{"checks":[]}`), nil
	case sequencePrompt:
		c.passes = append(c.passes, "stages")
		return textResponse(c.chain), nil
	}
	return nil, fmt.Errorf("unexpected planning call: %.48s…", system)
}

// made counts one pass, which is how a test says "exactly one sizing call" as
// opposed to "sizing happened".
func (c *countingPlanner) made(pass string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	seen := 0
	for _, reached := range c.passes {
		if reached == pass {
			seen++
		}
	}
	return seen
}

func (c *countingPlanner) reached(pass string) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, seen := range c.passes {
		if seen == pass {
			return true
		}
	}
	return false
}

const ungatedRemainder = "Finish work a previous agent started. Run pytest and show the output."

// The extension replan used to buy a whole project pipeline to plan one
// remainder node. Measured on two real extensions: 13,828 and 23,964 prompt
// tokens across seven passes, five to eight times the entire structuring cost
// of the jobs they were repairing, for graphs of one and three nodes.
//
// The judgement is the model's and it is one it already makes: the spine's own
// prompt tells it that exactly one stage is the right answer when the goal has
// no real gate, and calls that common rather than a failure. Undivided takes
// that answer at its word and stops.
func TestAnUngatedRemainderStopsAtOneWorker(t *testing.T) {
	client := &countingPlanner{stages: `{"stages":[{"title":"Run pytest","summary":"Run the suite and show what it printed."}]}`}
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, Briefs: true, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("an ungated remainder planned %d nodes, want 1", len(graph.Nodes))
	}
	if brief := graph.Nodes[0].Brief; !strings.Contains(brief, "Run pytest and show the output") {
		t.Fatalf("the one worker was not given the remainder: %q", brief)
	}
	for _, unbought := range []string{"fanout", "bind", "audit", "stages"} {
		if client.reached(unbought) {
			t.Fatalf("an ungated remainder still bought the %s pass: %v", unbought, client.passes)
		}
	}
	// One call is what the shortcut now pays, and it is the only thing standing
	// between a spine that was asked about gates and a worker that will be
	// handed the whole remainder.
	if made := client.made("size"); made != 1 {
		t.Fatalf("the shortcut made %d sizing calls, want exactly one: %v", made, client.passes)
	}
}

// THE SHORTCUT IS NOT A SIZE VERDICT. A remainder the spine finds nothing gated
// in, and the ruler puts past one worker's reach, is the leaf that exhausted
// itself being handed back to itself. It falls through to the pipeline, where
// the division of a sequence lives.
func TestAnOversizedRemainderIsNotHandedToOneWorker(t *testing.T) {
	replies := func() *countingPlanner {
		return &countingPlanner{
			stages: `{"stages":[{"title":"Finish","summary":"Finish the remainder."}]}`,
			// The whole is past one worker's reach; the stages it is made of
			// are not.
			sizeReply: `{"sizes":[{"node":1,"size":"oversized","split_into":["one","two"]},{"node":2,"size":"atomic","split_into":[]},{"node":3,"size":"atomic","split_into":[]}]}`,
			chain: `{"stages":[{"title":"Read","summary":"Read what is there.","needs":[]},` +
				`{"title":"Change","summary":"Make the change.","needs":[1]},` +
				`{"title":"Prove","summary":"Show that it holds.","needs":[2]}]}`,
		}
	}

	client := replies()
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 1, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Three ordered stages under the node they were drawn for, and the
	// one-sitting collapse leaves them alone: the links sit at depth 1, and the
	// whole they came from is the node the ruler put past one worker's reach.
	links := 0
	for _, node := range graph.Nodes {
		if node.Kind == KindWork && node.Depth == 1 {
			links++
		}
	}
	if links != 3 {
		t.Fatalf("an oversized remainder kept %d ordered stages of 3, in %d nodes: %v",
			links, len(graph.Nodes), client.passes)
	}
	// Binding and audit have nothing to say about a single stage with one node
	// in it; what the fall-through is for is the fan-out, the ruler, and the
	// stage question underneath them.
	for _, required := range []string{"fanout", "size", "stages"} {
		if !client.reached(required) {
			t.Fatalf("the fall-through skipped the %s pass: %v", required, client.passes)
		}
	}
	// A remainder is allowed one level of division, and this is what depth zero
	// costs it: the same remainder, the same rulings, and nowhere for the
	// oversized node to go.
	shallow := replies()
	flat, err := Build(context.Background(), shallow, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 0, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if shallow.reached("stages") || len(flat.Nodes) != 1 {
		t.Fatalf("depth zero divided the remainder after all (%d nodes): %v", len(flat.Nodes), shallow.passes)
	}
}

// The shortcut is a shortcut, not a ceiling. A remainder the model judges
// genuinely staged still gets the whole pipeline, because that judgement is
// the only thing the shortcut is keyed on. Whether the stages then survive is
// the pipeline's own evidence question: sized atomic end to end, a stage
// chain is one sitting and collapses; anything heavier and the stages stand.
func TestAGatedRemainderStillGetsTheWholePipeline(t *testing.T) {
	// The ruler finds real weight in the second stage: the graph keeps the
	// shape the spine drew.
	client := &countingPlanner{
		stages: `{"stages":[
		{"title":"Gather","summary":"Collect the numbers."},
		{"title":"Write","summary":"Write them up."}]}`,
		sizeReply: `{"sizes":[{"node":1,"size":"atomic","split_into":[]},{"node":2,"size":"borderline","split_into":[]}]}`,
	}
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, Briefs: false, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) < 2 {
		t.Fatalf("a two-stage remainder with weight in it collapsed to %d nodes", len(graph.Nodes))
	}
	for _, required := range []string{"fanout", "bind", "size"} {
		if !client.reached(required) {
			t.Fatalf("a staged remainder skipped the %s pass: %v", required, client.passes)
		}
	}

	// Every link measured atomic: the same remainder is one sitting, and the
	// pipeline says so from its own readings.
	atomic := &countingPlanner{stages: `{"stages":[
		{"title":"Gather","summary":"Collect the numbers."},
		{"title":"Write","summary":"Write them up."}]}`}
	folded, err := Build(context.Background(), atomic, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, Briefs: false, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(folded.Nodes) != 1 {
		t.Fatalf("an all-atomic chain kept %d nodes, want 1", len(folded.Nodes))
	}
	if !atomic.reached("size") {
		t.Fatal("the collapse happened without the sizing pass's evidence")
	}
}

// Without the option nothing moves: a fresh one-stage project still fans out,
// because a project with one stage still has parallel parts inside it and
// finding them is the whole point of planning.
func TestAOneStageProjectStillFansOut(t *testing.T) {
	client := &countingPlanner{stages: `{"stages":[{"title":"Do it","summary":"Do the whole thing."}]}`}
	if _, err := Build(context.Background(), client, "review the change end to end", Options{
		SpineSamples: 1, NodeBudget: 12, Ensemble: EnsembleNever,
	}); err != nil {
		t.Fatal(err)
	}
	if !client.reached("fanout") {
		t.Fatalf("an ordinary one-stage build stopped at the spine: %v", client.passes)
	}
}
