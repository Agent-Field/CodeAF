package plan

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// passClient scripts one whole build, routing by system prompt the way
// scriptClient does. It plays the exact failure a real run produced: bind
// answers nothing at all, and audit blesses the late node as finishable —
// which it technically is, by redoing everything upstream itself.
type passClient struct{}

func (c *passClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var system, user string
	for _, message := range messages {
		text := textOf(message)
		if message.Role == "system" {
			system = text
			continue
		}
		user += text
	}
	switch system {
	case spinePrompt:
		return textResponse(`{"stages":[
			{"title":"Inspect","summary":"Inspect the change."},
			{"title":"Write","summary":"Write the review."}]}`), nil
	case groundPrompt:
		return textResponse(`{"settled":[],"open":[]}`), nil
	case fanoutPrompt:
		if strings.Contains(user, "stage 1") {
			return textResponse(`{"parts":[
				{"title":"Diff","summary":"Read the diff."},
				{"title":"Tests","summary":"Run the tests."}]}`), nil
		}
		return textResponse(`{"parts":[{"title":"Review","summary":"Write REVIEW.md."}]}`), nil
	case bindPrompt:
		return textResponse(`{"bindings":[],"duplicates":[]}`), nil
	case sizePromptWith(Anchors()):
		return textResponse(`{"sizes":[
			{"node":1,"size":"atomic","split_into":[]},
			{"node":2,"size":"atomic","split_into":[]},
			{"node":3,"size":"atomic","split_into":[]}]}`), nil
	case auditPrompt:
		return textResponse(`{"checks":[{"node":3,"ok":true,"missing":[]}]}`), nil
	}
	return nil, fmt.Errorf("unexpected planning call: %.48s…", system)
}

// TestBuildAnchorsLooseLateNode is the regression test for the run where the
// review writer launched at t=0. Bind returns no edges and audit answers ok,
// so nothing model-side wires the stage-2 node — the anchor must, and the node
// must never be announced as ready to start immediately.
func TestBuildAnchorsLooseLateNode(t *testing.T) {
	var ready []string
	options := Options{
		Ensemble:     EnsembleNever,
		SpineSamples: 1,
		OnReady:      func(node Node, _ time.Duration) { ready = append(ready, node.Title) },
	}
	graph, err := Build(context.Background(), &passClient{}, "review the pull request and deliver REVIEW.md", options)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var writer *Node
	for index := range graph.Nodes {
		if graph.Nodes[index].Title == "Review" {
			writer = &graph.Nodes[index]
		}
	}
	if writer == nil {
		t.Fatal("the stage-2 writer is missing from the graph")
	}
	if len(writer.Needs) != 2 || !contains(writer.Needs, 1) || !contains(writer.Needs, 2) {
		t.Errorf("writer needs %v, want the stage-1 frontier [1 2]", writer.Needs)
	}
	for _, title := range ready {
		if title == "Review" {
			t.Error("the late writer was announced ready to start at t=0")
		}
	}
	// With the writer anchored it is the only work-node sink, so the appended
	// synthesis gathers it alone rather than racing every stage-1 node into
	// its own context.
	for _, node := range graph.Nodes {
		if node.Kind != KindSynthesis {
			continue
		}
		if len(node.Needs) != 1 || node.Needs[0] != writer.ID {
			t.Errorf("synthesis needs %v, want just the writer %d", node.Needs, writer.ID)
		}
	}
	if graph.hasCycle() {
		t.Error("build produced a cycle")
	}
}
