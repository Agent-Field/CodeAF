package plan

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

// foldClient scripts a build whose bind pass folds one duplicate away, and
// keeps the shared catalog block every pass was sent.
type foldClient struct {
	mutex  sync.Mutex
	shared map[string]string
}

func (c *foldClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var system, user, block string
	for _, message := range messages {
		text := textOf(message)
		if message.Role == "system" {
			system = text
			continue
		}
		if block == "" {
			block = text
		}
		user += text
	}
	c.mutex.Lock()
	if c.shared == nil {
		c.shared = map[string]string{}
	}
	c.shared[system] = block
	c.mutex.Unlock()

	switch system {
	case spinePrompt:
		return textResponse(`{"stages":[
			{"title":"Inspect","summary":"Inspect the change."},
			{"title":"Write","summary":"Write the review."}]}`), nil
	case groundPrompt:
		return textResponse(`{"settled":[],"open":[]}`), nil
	case fanoutPrompt:
		if strings.Contains(user, "stage 1") {
			return textResponse(`{"parts":[{"title":"Diff","summary":"Read the diff."}]}`), nil
		}
		return textResponse(`{"parts":[
			{"title":"Review","summary":"Write REVIEW.md."},
			{"title":"Summary","summary":"Write REVIEW.md again."}]}`), nil
	case bindPrompt:
		return textResponse(`{"bindings":[],"duplicates":[{"node":3,"same_as":2}]}`), nil
	case sizePromptWith(Anchors()):
		return textResponse(`{"sizes":[
			{"node":1,"size":"atomic","split_into":[]},
			{"node":2,"size":"atomic","split_into":[]},
			{"node":3,"size":"atomic","split_into":[]}]}`), nil
	case auditPrompt:
		return textResponse(`{"checks":[{"node":2,"ok":true,"missing":[]}]}`), nil
	}
	return nil, fmt.Errorf("unexpected planning call: %.48s…", system)
}

// TestAuditSeesTheFoldedCatalog guards the one render the whole-graph passes
// share. Bind, size and audit are all sent the same block, because it is the
// same premise and it is what the prefix cache keys on — but bind can fold a
// duplicate away between the render and audit, and audit must then be told
// about a plan that no longer contains it. Reusing the render unconditionally
// would have audit judging a node that had already been deleted.
func TestAuditSeesTheFoldedCatalog(t *testing.T) {
	client := &foldClient{}
	options := Options{Ensemble: EnsembleNever, SpineSamples: 1, NodeBudget: 20}
	graph, err := Build(context.Background(), client, "review the pull request", options)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if graph.Node(3) != nil {
		t.Fatal("the duplicate was not folded away; the test proves nothing")
	}

	client.mutex.Lock()
	defer client.mutex.Unlock()
	bound := client.shared[bindPrompt]
	audited := client.shared[auditPrompt]
	if !strings.Contains(bound, "3. Summary") {
		t.Errorf("bind was not shown the duplicate it is asked to spot:\n%s", bound)
	}
	if strings.Contains(audited, "3. Summary") {
		t.Errorf("audit was shown a node bind had already folded away:\n%s", audited)
	}
	if !strings.Contains(audited, "2. Review") {
		t.Errorf("audit was not shown the surviving node:\n%s", audited)
	}
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
