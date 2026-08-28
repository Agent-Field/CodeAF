package session

// The model argument on the wire: what the proposal carries, what the node is
// admitted with, and what the model is told when its word names nothing.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// proposeModelCall is task_test.go's proposeCall with a model on it.
func proposeModelCall(title, model string) step {
	arguments, _ := json.Marshal(taskArguments{
		Title:       title,
		Summary:     "two lines the person reads",
		Brief:       "the whole brief\n" + taskBriefMark,
		Deliverable: "d",
		Acceptance:  "the file is there",
		Model:       model,
	})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("call-task", "propose_task", string(arguments)), nil
	}
}

// A named model reaches the node, and the proposal says so before it starts.
func TestTaskProposalCarriesTheModelItWillRunOn(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeModelCall("Sweep the deprecated calls", "claude-opus-5"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
		config.TaskModels = func() []string { return testModels }
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("did the thing", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "sweep them")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	proposal, ok := firstOfKind(collected, EventTaskProposal)
	if !ok {
		t.Fatalf("no proposal reached the surface: %v", kinds(collected))
	}
	if proposal.Task.Model != "anthropic/claude-opus-5" {
		t.Fatalf("proposal model = %q, want the resolved id", proposal.Task.Model)
	}
	if len(proposal.Task.ModelOptions) != 0 {
		t.Fatalf("an unambiguous word raised options: %v", proposal.Task.ModelOptions)
	}

	node := ran.await(t)
	if model := node.model(); model != "anthropic/claude-opus-5" {
		t.Fatalf("the node was admitted on %q, want the model the proposal named", model)
	}
	waitDoneNode(t, node)
	// The receipt names the model back, because a word resolving to an id is the
	// one thing about this call the model cannot see for itself.
	if output := toolOutput(t, collected, "propose_task"); !strings.Contains(output, "anthropic/claude-opus-5") {
		t.Fatalf("tool result = %q, want it to name the model", output)
	}
	// And every update carries it, so a card landing minutes later can still say
	// whose hands did the work.
	update, ok := firstOfKind(collected, EventTaskUpdate)
	if ok && update.Task.Model != "anthropic/claude-opus-5" {
		t.Fatalf("update model = %q, want the node's own", update.Task.Model)
	}
}

// A PROPOSAL THAT NAMES NO MODEL RUNS ON THE CONFIGURED ONE, and says so on the
// wire — the surface has to draw it either way.
func TestTaskProposalWithoutAModelTakesTheConfiguredOne(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeCall("Fix the nil-map crash", "the whole brief"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
		config.TaskModel = "anthropic/claude-sonnet-5"
		config.TaskModels = func() []string { return testModels }
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("did the thing", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "fix the crash")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	proposal, ok := firstOfKind(collected, EventTaskProposal)
	if !ok {
		t.Fatalf("no proposal reached the surface: %v", kinds(collected))
	}
	if proposal.Task.Model != "anthropic/claude-sonnet-5" {
		t.Fatalf("proposal model = %q, want the configured task model", proposal.Task.Model)
	}
	if node := ran.await(t); node.model() != "anthropic/claude-sonnet-5" {
		t.Fatalf("the node runs on %q, want the configured task model", node.model())
	}
	// Nothing was asked for, so nothing is said back: a receipt reciting the
	// default every time is a line nobody reads.
	//
	// THE FIRST LINE IS WHERE THE MODEL WOULD BE NAMED (`task 1 started on
	// <model>: <title>`), and the check is bounded to it: the lines under it are
	// prose about what happens next, and they are free to contain the word "on".
	output := toolOutput(t, collected, "propose_task")
	if headline, _, _ := strings.Cut(output, "\n"); strings.Contains(headline, " on ") {
		t.Fatalf("tool result headline = %q, want no model named for a task that asked for none", headline)
	}
}

// AN UNKNOWN SLUG IS AN ORDINARY TOOL ERROR, and nothing is admitted: the model
// reads the near matches and calls again.
func TestTaskProposalWithAnUnknownModelIsARefusalAndAdmitsNothing(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeModelCall("Rewrite the reconciler", "openai/gpt-9"),
		finalText("understood"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
		config.TaskModels = func() []string { return testModels }
	})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		t.Errorf("node %d ran on a model that does not exist", node.id)
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "rewrite it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if _, asked := firstOfKind(collected, EventTaskProposal); asked {
		t.Fatal("a model nobody has still got as far as asking the person")
	}
	if nodes := admitted(graph); nodes != 0 {
		t.Fatalf("an unknown model admitted %d nodes", nodes)
	}
	kind, found := toolResultKind(collected, "propose_task")
	if !found || kind != EventToolFailed {
		t.Fatalf("the call did not fail: %v", kinds(collected))
	}
	output := toolOutput(t, collected, "propose_task")
	for _, want := range []string{"openai/gpt-9", "openai/gpt-5"} {
		if !strings.Contains(output, want) {
			t.Fatalf("tool result = %q, want it to name %q", output, want)
		}
	}
}

// ONE WORD, TWO MODELS: the shortlist goes to the person on the proposal they
// were being shown anyway, and the one they pick is what the node is admitted
// with. Nothing is refused and nothing waits for a second question.
func TestTaskProposalAsksWhichModelWhenOneWordFitsSeveral(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeModelCall("Sweep the deprecated calls", "opus"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		// No clock: this proposal is settled by the answer and nothing else.
		config.TaskAutoApproveSeconds = 0
		config.TaskModels = func() []string { return testModels }
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("did the thing", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "sweep them with opus")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var offered []string
	collected := drainAnsweringTasks(t, events, func(event Event) {
		offered = event.Task.ModelOptions
		agent.ResolveTask(event.Task.ID, TaskAnswer{
			Approved: true,
			Model:    "anthropic/claude-opus-4.8",
		})
	})

	if len(offered) != 2 || offered[0] != "anthropic/claude-opus-5" {
		t.Fatalf("the proposal offered %v, want both opus rows, closest first", offered)
	}
	proposal, _ := firstOfKind(collected, EventTaskProposal)
	if proposal.Task.Model != "anthropic/claude-opus-5" {
		t.Fatalf("the card would have shown %q, want the leading option", proposal.Task.Model)
	}
	node := ran.await(t)
	if model := node.model(); model != "anthropic/claude-opus-4.8" {
		t.Fatalf("the node runs on %q, want the one the person picked", model)
	}
	waitDoneNode(t, node)
}

// SILENCE STILL STARTS THE WORK, on the model the card was showing. An
// ambiguity the harness raised is not a reason for a task to stop.
func TestTaskProposalShortlistIsSettledByTheClockToo(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeModelCall("Sweep the deprecated calls", "opus"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
		config.TaskModels = func() []string { return testModels }
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("did the thing", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "sweep them with opus")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	node := ran.await(t)
	if model := node.model(); model != "anthropic/claude-opus-5" {
		t.Fatalf("the clock started the node on %q, want the closest match", model)
	}
	waitDoneNode(t, node)
}
