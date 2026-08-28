package session

// ONE MOVE, WHEN THE PROVIDER RATHER THAN THE WORK FAILED.
//
// A node whose worker died because nothing serving its model would answer has
// learned nothing about the brief, and the worktree it prepared is the part that
// was expensive. So it runs again, once, on the next model in the chain — and
// the row, the card and the report all say which two models were involved.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// remodelCompleter is [routedCompleter]'s narrow cousin: it answers the parent's
// turn, records the model every CHILD request rode, and offers a fallback chain.
type remodelCompleter struct {
	mu sync.Mutex
	// answers is what a child request on each model gets, by model id. A model
	// with no entry answers a plain report.
	answers map[string]func() (*ai.Response, error)
	// childModels is every model a child request actually rode, in order.
	childModels []string
	chain       []string
	parent      []step
	parentSeen  int
}

func (c *remodelCompleter) FallbackModels(string) []string { return c.chain }

func (c *remodelCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	child := false
	for _, message := range messages {
		if message.Role == "user" && strings.Contains(messageText(message), taskBriefMark) {
			child = true
			break
		}
	}
	c.mu.Lock()
	if !child {
		var next step
		if c.parentSeen < len(c.parent) {
			next = c.parent[c.parentSeen]
		}
		c.parentSeen++
		c.mu.Unlock()
		if next == nil {
			return textResponse("(unscripted)"), nil
		}
		return next(ctx, messages)
	}
	c.childModels = append(c.childModels, request.Model)
	answer := c.answers[request.Model]
	c.mu.Unlock()
	if answer != nil {
		return answer()
	}
	return textResponse("Wrote the thing."), nil
}

func (c *remodelCompleter) modelsAsked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.childModels...)
}

func remodelAgent(t *testing.T, completer Completer) (*Agent, *TaskGraph) {
	t.Helper()
	repo := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		// The audit is off: this file is about what happens BEFORE a node has an
		// account of itself to check.
		config.TaskAudit = false
	})
	return agent, agent.graph()
}

// THE WHOLE SHAPE: the model the node was admitted on refuses everything, the
// node runs again on the chain's next model, and both are named.
func TestANodeWhoseProviderFailedRunsAgainOnTheNextModel(t *testing.T) {
	completer := &remodelCompleter{
		chain:   []string{"other/model"},
		answers: map[string]func() (*ai.Response, error){},
		parent: []step{
			proposeCall("Add the greeting", "write hello.txt containing hi"),
			finalText("handed off"),
		},
	}
	completer.answers["test/model"] = func() (*ai.Response, error) {
		return nil, &provider.APIError{Status: 404, Message: "no endpoints found"}
	}
	agent, graph := remodelAgent(t, completer)

	events, err := agent.Submit(context.Background(), "add a greeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)

	asked := completer.modelsAsked()
	if len(asked) < 2 || asked[0] != "test/model" || asked[len(asked)-1] != "other/model" {
		t.Fatalf("the child rode %v, want the admitted model and then the chain's next", asked)
	}
	notice := node.notice()
	// THE ROW SAYS WHAT IT IS ACTUALLY RUNNING ON, and the id it was admitted
	// with is not overwritten by a rescue nobody chose.
	if notice.Model != "other/model" {
		t.Fatalf("the row names %q, want the model that actually ran it", notice.Model)
	}
	if got := node.model(); got != "test/model" {
		t.Fatalf("the admitted id was overwritten with %q; a rescue is not a choice", got)
	}
	// AND THE REPORT NAMES BOTH.
	for _, want := range []string{"test/model", "other/model", "stopped answering"} {
		if !strings.Contains(notice.Report, want) {
			t.Fatalf("the report %q does not contain %q", notice.Report, want)
		}
	}
}

// BOUNDED TO ONCE. The second failure is the node's real failure, and it still
// names both models so the person can see what was tried.
func TestANodeMovesModelOnceAndThenTheFailureIsReal(t *testing.T) {
	refuse := func() (*ai.Response, error) {
		return nil, &provider.APIError{Status: 404, Message: "no endpoints found"}
	}
	completer := &remodelCompleter{
		chain:   []string{"other/model", "third/model"},
		answers: map[string]func() (*ai.Response, error){"test/model": refuse, "other/model": refuse},
		parent: []step{
			proposeCall("Add the greeting", "write hello.txt containing hi"),
			finalText("handed off"),
		},
	}
	agent, graph := remodelAgent(t, completer)

	events, err := agent.Submit(context.Background(), "add a greeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := graph.node(1)
	waitDoneNode(t, node)

	if state := node.stateNow(); state != TaskFailed {
		t.Fatalf("state = %q, want the second failure to be real; report = %q", state, node.notice().Report)
	}
	for _, model := range completer.modelsAsked() {
		if model == "third/model" {
			t.Fatalf("a third model was tried; the chain is walked once per node: %v",
				completer.modelsAsked())
		}
	}
	report := node.notice().Report
	for _, want := range []string{"test/model", "other/model"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the failure %q does not name %q", report, want)
		}
	}
}

// A NODE THAT FAILED AT ITS WORK NEVER MOVES. Whether finished-looking work is
// actually finished is the audit's question, and re-rolling a model on it would
// be this file answering a question it was not asked.
func TestWorkThatMerelyEndedBadlyDoesNotMoveTheModel(t *testing.T) {
	if terminalProviderFailure(nil) {
		t.Fatal("no error is not a provider failure")
	}
	if terminalProviderFailure(errStr("the tool could not write that file")) {
		t.Fatal("an ordinary failure was read as the provider's")
	}
	// A CUT THE TURN ALREADY ANSWERED. The chain was walked inside the turn that
	// died, so the first model this would move to is the one that just failed
	// there — a whole second worker to learn nothing.
	if terminalProviderFailure(cutFailure(&provider.StreamCut{Reason: provider.CutSilent}, 3, nil)) {
		t.Fatal("a cut the turn loop already hopped on was read as a fresh provider failure")
	}
	// A CONTEXT OVERFLOW is a fact about the transcript, not about the model.
	if terminalProviderFailure(errStr("context length exceeded: 300000 tokens")) {
		t.Fatal("a context overflow was read as a provider failure")
	}
	// And the two shapes that are: a refusal, and a status the provider sent.
	if !terminalProviderFailure(&provider.APIError{Status: 404, Message: "no endpoints found"}) {
		t.Fatal("a provider refusal was not read as one")
	}
	if !terminalProviderFailure(&provider.RefusalError{Model: "a/b", Attempts: 4}) {
		t.Fatal("an exhausted refusal chain was not read as a provider failure")
	}
}
