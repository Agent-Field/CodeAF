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
	mutex  sync.Mutex
	stages string
	passes []string
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
		return textResponse(`{"sizes":[{"node":1,"size":"atomic","split_into":[]},{"node":2,"size":"atomic","split_into":[]}]}`), nil
	case auditPrompt:
		c.passes = append(c.passes, "audit")
		return textResponse(`{"checks":[]}`), nil
	}
	return nil, fmt.Errorf("unexpected planning call: %.48s…", system)
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
	for _, unbought := range []string{"fanout", "bind", "size", "audit"} {
		if client.reached(unbought) {
			t.Fatalf("an ungated remainder still bought the %s pass: %v", unbought, client.passes)
		}
	}
}

// The shortcut is a shortcut, not a ceiling. A remainder the model judges
// genuinely staged still gets the whole pipeline, because that judgement is
// the only thing the shortcut is keyed on.
func TestAGatedRemainderStillGetsTheWholePipeline(t *testing.T) {
	client := &countingPlanner{stages: `{"stages":[
		{"title":"Gather","summary":"Collect the numbers."},
		{"title":"Write","summary":"Write them up."}]}`}
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, Briefs: false, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) < 2 {
		t.Fatalf("a two-stage remainder collapsed to %d nodes", len(graph.Nodes))
	}
	for _, required := range []string{"fanout", "bind", "size"} {
		if !client.reached(required) {
			t.Fatalf("a staged remainder skipped the %s pass: %v", required, client.passes)
		}
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
