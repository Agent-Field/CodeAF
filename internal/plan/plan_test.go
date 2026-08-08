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

// The plan must be the size of the ask. Real sessions produced multi-node
// ceremony for single-deliverable requests and then spent further rounds
// verifying it, so the judgment is written into the two prompts that actually
// decide decomposition — the stage split and the part split — and into no
// third place, because expansion is those same two prompts one level down.
func TestBothDecompositionPromptsCarryTheProportionJudgment(t *testing.T) {
	for name, want := range map[string]string{
		"as large as the goal and no larger": "Make the plan exactly as large as the goal, and no larger",
		"a division must say what it buys":   "Keep a division only when you can say what it\nbuys",
		"one deliverable defaults to one":    "a goal that asks for one finished thing is one piece of work by default",
		"whole is a correct answer":          "returning it whole is a correct answer rather than a failure to decompose",
		"checking is part of the work":       "Checking the work is part of doing it, never a piece of work of its own",
		"no node exists to check another":    "Do not\nadd anything whose purpose is to look at, confirm, review, or verify what\nanother part produced",
	} {
		if !strings.Contains(proportionRule, want) {
			t.Errorf("the proportion rule no longer states %s: %q missing", name, want)
		}
	}
	for prompt, text := range map[string]string{
		"spine":  spinePrompt,
		"fanout": fanoutPrompt,
	} {
		if !strings.Contains(text, proportionRule) {
			t.Errorf("the %s prompt does not carry the proportion rule", prompt)
		}
	}
}

// The planner is handed every kind of work there is, so a rule that reaches for
// one kind's nouns quietly mis-plans every other kind. The judgment is about
// what a division buys, which is sayable without naming a single subject.
func TestTheProportionRuleNamesNoDomain(t *testing.T) {
	// Nouns from the domains the planner is most often used on, plus the
	// process words that would turn the rule into a template for one shape of
	// work rather than a judgment about any.
	for _, forbidden := range []string{
		"code", "repo", "file", "test suite", "commit", "pull request", "deploy",
		"report", "document", "essay", "article", "email", "spreadsheet",
		"vendor", "market", "customer", "research", "dataset", "model",
		"design", "sprint", "ticket", "requirement", "stakeholder",
	} {
		if strings.Contains(strings.ToLower(proportionRule), forbidden) {
			t.Errorf("the proportion rule names a domain: %q", forbidden)
		}
	}
	// And it must not have become a number: a threshold is wrong at both ends
	// and the whole point is that the model makes the judgment.
	for _, forbidden := range []string{"at most", "no more than", "fewer than", "nodes", "1 to", "2 to"} {
		if strings.Contains(strings.ToLower(proportionRule), forbidden) {
			t.Errorf("the proportion rule became a cap rather than a judgment: %q", forbidden)
		}
	}
}
