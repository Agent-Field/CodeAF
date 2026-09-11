package session

// A PROPOSAL HELD AT ITS COMMIT, AS TESTS. Each of these drives the belt's own
// propose_task on a context carrying a [bare.Hold] — exactly what the turn loop
// does when a proposal's arguments close while the rest of the reply is still
// arriving — and then releases or withdraws it the way the loop does once it
// knows whether the reply arrived whole.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// heldProposal is one propose_task call started on a held context, and where
// its result will land.
type heldProposal struct {
	hold   *bare.Hold
	result chan toolResult
}

func holdProposal(t *testing.T, agent *Agent, args json.RawMessage) heldProposal {
	t.Helper()
	tool := beltTool(t, agent, "propose_task")
	held := heldProposal{hold: bare.NewHold(), result: make(chan toolResult, 1)}
	ctx := bare.WithHold(context.Background(), held.hold)
	go func() {
		text, isError, err := tool.Execute(ctx, args)
		if err != nil {
			text, isError = err.Error(), true
		}
		held.result <- toolResult{text: text, isError: isError}
	}()
	return held
}

func (h heldProposal) await(t *testing.T) toolResult {
	t.Helper()
	select {
	case result := <-h.result:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("the held proposal never returned")
		return toolResult{}
	}
}

// proposalArgs is one well-formed propose_task call carrying the brief mark.
func proposalArgs(title string) json.RawMessage {
	arguments, _ := json.Marshal(taskArguments{
		Title:       title,
		Summary:     "two lines the person reads",
		Brief:       "the whole brief\n" + taskBriefMark,
		Deliverable: "the file",
		Acceptance:  "the file is there",
	})
	return arguments
}

// nextTurnEvent reads the turn's lane until an event of this kind arrives.
func nextTurnEvent(t *testing.T, events <-chan Event, want EventKind) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Kind == want {
				return event
			}
		case <-deadline:
			t.Fatalf("no %v arrived", want)
			return Event{}
		}
	}
}

// heldClock replaces the proposal clock with one the test fires, so a countdown
// that runs out is a line in the test rather than a real wait.
func heldClock(agent *Agent) chan time.Time {
	expiry := make(chan time.Time, 1)
	agent.taskTimer = func(time.Duration) (<-chan time.Time, func()) {
		return expiry, func() {}
	}
	return expiry
}

// THE CARD GOES UP AT ONCE AND THE WORK WAITS FOR THE REPLY. A proposal started
// while its reply is still arriving puts its card in front of the person and
// starts its clock; the clock may even run out; and still nothing is admitted
// until the hold is released — and what the node is then handed was compiled
// from the transcript as it stands at the release, which is the whole reply.
func TestAHeldProposalShowsItsCardAndAdmitsOnlyOnRelease(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 15
	})
	expiry := heldClock(agent)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.graph.complete(node, TaskDone)
	})
	events := watched(agent)

	held := holdProposal(t, agent, proposalArgs("Port the resume picker"))
	card := nextTurnEvent(t, events, EventTaskProposal)
	if card.Task == nil || card.Task.Deadline.IsZero() || card.Task.Withdrawn != "" {
		t.Fatalf("the card went up as %+v, want a live proposal with its clock running", card.Task)
	}
	// The clock runs out while the reply is still arriving: silence has said yes,
	// and the answer waits for the reply rather than admitting on its own.
	expiry <- time.Now()
	if nodes := admitted(graph); nodes != 0 {
		t.Fatalf("%d nodes were admitted before the reply was whole", nodes)
	}
	select {
	case result := <-held.result:
		t.Fatalf("the held call returned before its release: %+v", result)
	default:
	}

	// The loop records the whole reply, then releases the call.
	const framing = "The parser keeps its old API; the picker is the part to hand out."
	agent.recordAssistant(ai.Message{Role: "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: framing}},
		ToolCalls: []ai.ToolCall{{ID: "call-held", Type: "function",
			Function: ai.ToolCallFunction{Name: "propose_task", Arguments: string(proposalArgs("Port the resume picker"))}}},
	}, provider.MessageReasoning{})
	held.hold.Release()

	result := held.await(t)
	if result.isError || !strings.Contains(result.text, fmt.Sprintf("task %d started", card.Task.ID)) {
		t.Fatalf("the released proposal answered %+v", result)
	}
	node := ran.await(t)
	if node.id != card.Task.ID {
		t.Fatalf("node %d ran, want the one on the card (%d)", node.id, card.Task.ID)
	}
	quoted := false
	for _, quote := range node.spec.admission.Quotes {
		if strings.Contains(quote.Text, framing) {
			quoted = true
		}
	}
	if !quoted {
		t.Fatalf("the node was not handed the words of the reply that proposed it: %+v", node.spec.admission.Quotes)
	}
}

// A WITHDRAWN PROPOSAL LEAVES NOTHING BEHIND THAT SAYS IT HAPPENED. The card
// settles with the reason, the question comes off every window with the same
// words, a late answer finds nothing to answer, and nothing is admitted. (A yes
// given before the withdrawal admits nothing either: the loop's own test,
// [TestACutReplyWithdrawsTheProposalItHadStarted], answers every card it sees.)
func TestAWithdrawnProposalTakesItsCardAndQuestionBack(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 15
	})
	heldClock(agent)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		t.Errorf("a withdrawn proposal ran node %d", node.id)
	})
	events := watched(agent)
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	held := holdProposal(t, agent, proposalArgs("Rewrite the reconciler"))
	card := nextTurnEvent(t, events, EventTaskProposal)
	waitForAsk(t, asks, EventQuestion)

	held.hold.Withdraw()
	result := held.await(t)
	if !result.isError || !strings.Contains(result.text, "taken back") {
		t.Fatalf("the withdrawn call answered %+v", result)
	}
	settled := nextTurnEvent(t, events, EventTaskProposal)
	if settled.Task == nil || settled.Task.ID != card.Task.ID || settled.Task.Withdrawn != taskWithdrawnReason {
		t.Fatalf("the card was settled as %+v, want it withdrawn with the reason", settled.Task)
	}
	if !settled.Task.Deadline.IsZero() {
		t.Fatal("a withdrawn card still carries a clock")
	}
	gone := waitForAsk(t, asks, EventQuestionWithdrawn)
	if gone.Question.Withdrawn == nil || gone.Question.Withdrawn.Reason != taskWithdrawnReason {
		t.Fatalf("the question was withdrawn as %+v, want the card's own reason", gone.Question.Withdrawn)
	}
	if pending := agent.PendingTasks(); len(pending) != 0 {
		t.Fatalf("PendingTasks() = %v after the withdrawal", pending)
	}
	if open := agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("OpenQuestions() = %+v after the withdrawal", open)
	}
	agent.ResolveTask(card.Task.ID, TaskAnswer{Approved: true})
	if nodes := admitted(graph); nodes != 0 {
		t.Fatalf("a withdrawn proposal admitted %d nodes", nodes)
	}
}

// WHAT THE PERSON DID FIRST IS WHAT COUNTS, even when the reply is still
// arriving. A "no" given before the clock ran out is a no, though both are
// waiting by the time the reply is whole — the wait reads them as they happen,
// not as a pair to choose between afterwards.
func TestANoGivenWhileTheReplyArrivesBeatsTheClockThatFollows(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 15
	})
	expiry := heldClock(agent)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		t.Errorf("a declined proposal ran node %d", node.id)
	})
	events := watched(agent)

	held := holdProposal(t, agent, proposalArgs("Rewrite the reconciler"))
	card := nextTurnEvent(t, events, EventTaskProposal)
	agent.ResolveTask(card.Task.ID, TaskAnswer{Approved: false})
	expiry <- time.Now()
	held.hold.Release()

	result := held.await(t)
	if result.isError || !strings.Contains(result.text, "the person declined this task") {
		t.Fatalf("a no given before the clock ran out answered %+v", result)
	}
	if nodes := admitted(graph); nodes != 0 {
		t.Fatalf("a declined proposal admitted %d nodes", nodes)
	}
}

// A WITHDRAWN PROPOSAL GIVES ITS FAN SLOT BACK. A node's proposals are capped,
// and a slot held by a card that was taken back would be one piece of work the
// node can never hand out — for the rest of its life, over a call it never made.
func TestAWithdrawnProposalGivesItsFanSlotBack(t *testing.T) {
	nest := newNest(t, nil, nil)
	for i := 0; i < taskFanLimit; i++ {
		held := holdProposal(t, nest.node, pieceArgs(fmt.Sprintf("taken back %d", i)))
		held.hold.Withdraw()
		if result := held.await(t); !result.isError {
			t.Fatalf("withdrawn piece %d answered %+v", i, result)
		}
	}
	for i := 0; i < taskFanLimit; i++ {
		if answer := nest.handOut(t, fmt.Sprintf("piece %d", i)); strings.HasPrefix(answer, "no:") {
			t.Fatalf("piece %d was refused after the withdrawals: %s", i, answer)
		}
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != taskFanLimit {
		t.Fatalf("%d pieces were admitted, want %d", len(kids), taskFanLimit)
	}
}

// A REFUSAL IS SETTLED BEFORE ANYTHING IS SHOWN, so a held call that could never
// have been a proposal puts up no card at all and answers its refusal the moment
// it is released — the same words, the same flag, as the batch gives it.
func TestAHeldProposalThatIsRefusedShowsNothing(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 15
	})
	events := watched(agent)
	held := holdProposal(t, agent, json.RawMessage(`{"title":"t","summary":"s","brief":"b","deliverable":"d"}`))
	held.hold.Release()
	result := held.await(t)
	if !result.isError || !strings.Contains(result.text, "acceptance is required") {
		t.Fatalf("the refused call answered %+v", result)
	}
	select {
	case event := <-events:
		if event.Kind == EventTaskProposal {
			t.Fatalf("a refused proposal put up a card: %+v", event.Task)
		}
	default:
	}
}

// WHICH TOOLS MAY START EARLY AND MUTATE IS A PROPERTY OF THE TOOL, and on the
// conversation's belt it is exactly the proposal. The day another tool is built
// in two halves this test is the conversation about it; `quick_task` is named
// because it is the one a reader would expect here and it is deliberately not a
// staged tool — its commit is the worker's first request, whose brief quotes the
// reply that asked for it, so nothing of it can run before the reply is whole.
func TestOnlyTheProposalIsStagedOnTheConversationsBelt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	var staged []string
	for _, tool := range agent.beltTools() {
		if tool.Stages() {
			staged = append(staged, tool.Name)
		}
	}
	if !equalStrings(staged, []string{"propose_task"}) {
		t.Fatalf("the staged tools on the belt are %v, want only propose_task", staged)
	}
	if !agent.stagesEarly("propose_task") {
		t.Fatal("propose_task does not start early")
	}
	for _, name := range []string{quickTaskToolName, "write", "bash", "read", "no_such_tool"} {
		if agent.stagesEarly(name) {
			t.Fatalf("%s would start early held at a commit it does not have", name)
		}
	}
}
