package session

// THE TURN LOOP STARTING A PROPOSAL WHILE ITS REPLY IS STILL ARRIVING, AS TESTS.
// These drive a whole turn through a scripted stream that announces calls one
// at a time, and read what the person would have seen between the two.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// proposalCall is one propose_task call as the stream would close it.
func proposalCall(id, title string) ai.ToolCall {
	return ai.ToolCall{ID: id, Type: "function",
		Function: ai.ToolCallFunction{Name: "propose_task", Arguments: string(proposalArgs(title))}}
}

// proposalWatch reads one turn's lane on its own goroutine, answering every live
// proposal yes the moment its card goes up and saying so on cards, so a
// scripted stream can wait for the card of the call it has just closed.
type proposalWatch struct {
	cards     chan TaskNotice
	withdrawn chan TaskNotice
	done      chan []Event
}

func watchProposals(t *testing.T, agent *Agent, events <-chan Event) proposalWatch {
	t.Helper()
	watch := proposalWatch{
		cards:     make(chan TaskNotice, 8),
		withdrawn: make(chan TaskNotice, 8),
		done:      make(chan []Event, 1),
	}
	go func() {
		var collected []Event
		for event := range events {
			collected = append(collected, event)
			if event.Kind != EventTaskProposal || event.Task == nil {
				continue
			}
			if event.Task.Withdrawn != "" {
				watch.withdrawn <- *event.Task
				continue
			}
			agent.ResolveTask(event.Task.ID, TaskAnswer{Approved: true})
			watch.cards <- *event.Task
		}
		watch.done <- collected
	}()
	return watch
}

func (w proposalWatch) card(t *testing.T) TaskNotice {
	t.Helper()
	select {
	case card := <-w.cards:
		return card
	case <-time.After(5 * time.Second):
		t.Error("no card went up while the reply was still arriving")
		return TaskNotice{}
	}
}

func (w proposalWatch) finished(t *testing.T) []Event {
	t.Helper()
	select {
	case collected := <-w.done:
		return collected
	case <-time.After(10 * time.Second):
		t.Fatal("the turn did not finish")
		return nil
	}
}

// proposalSession is a watched conversation whose proposals are answered by
// the person (no clock) and whose nodes are recorded rather than run.
func proposalSession(t *testing.T, completer Completer) (*Agent, *TaskGraph) {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 0
	})
	graph := stubbedGraph(agent, func(node *TaskNode) { node.graph.complete(node, TaskDone) })
	return agent, graph
}

// THE FIRST PROPOSAL IS ON THE CARD BEFORE THE SECOND HAS CLOSED, and it is
// answered there — but nothing is admitted until the reply is whole, because the
// reply is what the node's brief quotes and a reply can still be cut.
func TestTheFirstProposalIsOnTheCardBeforeTheSecondCloses(t *testing.T) {
	first, second := proposalCall("p1", "Port the resume picker"), proposalCall("p2", "Rewrite the reconciler")
	var graph *TaskGraph
	var watch proposalWatch
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, first)
			card := watch.card(t)
			if card.Title != "Port the resume picker" {
				t.Errorf("the card that went up was %q", card.Title)
			}
			if nodes := admitted(graph); nodes != 0 {
				t.Errorf("%d nodes were admitted while the reply was still arriving", nodes)
			}
			emitReady(t, ctx, second)
			watch.card(t)
			return callsResponse(first, second), nil
		},
		finalText("handed off"),
	}}
	agent, g := proposalSession(t, completer)
	graph = g
	events, err := agent.Submit(context.Background(), "port the picker and rewrite the reconciler")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	watch = watchProposals(t, agent, events)
	watch.finished(t)

	if nodes := admitted(graph); nodes != 2 {
		t.Fatalf("%d nodes were admitted, want both proposals", nodes)
	}
	// THE TRANSCRIPT IS THE ONE THE BATCH ALWAYS WROTE: one reply, then one
	// result per call in call order. (What follows is the landings' own notes,
	// which a stubbed node sends the moment it is admitted.)
	recorded := messagesOf(agent)
	if got, want := transcriptRoles(agent)[:5], []string{"system", "user", "assistant", "tool", "tool"}; !equalStrings(got, want) {
		t.Fatalf("transcript roles open %v, want %v", got, want)
	}
	if recorded[3].ToolCallID != "p1" || recorded[4].ToolCallID != "p2" {
		t.Fatalf("results are paired %q, %q; want p1 then p2", recorded[3].ToolCallID, recorded[4].ToolCallID)
	}
}

// A CUT REPLY TAKES ITS PROPOSAL BACK. The attempt that died had already put a
// card up and been answered; the retry asks again, and exactly one task is
// admitted — the retried one — with the dead attempt's card settled as withdrawn.
func TestACutReplyWithdrawsTheProposalItHadStarted(t *testing.T) {
	dead, retried := proposalCall("p-dead", "Port the resume picker"), proposalCall("p-live", "Port the resume picker")
	var watch proposalWatch
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, dead)
			watch.card(t)
			return nil, errors.New("provider returned error: 502 bad gateway")
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, retried)
			watch.card(t)
			return callsResponse(retried), nil
		},
		finalText("handed off"),
	}}
	agent, graph := proposalSession(t, completer)
	events, err := agent.Submit(context.Background(), "port the picker")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	watch = watchProposals(t, agent, events)
	watch.finished(t)

	if nodes := admitted(graph); nodes != 1 {
		t.Fatalf("%d nodes were admitted, want exactly the retried proposal", nodes)
	}
	select {
	case gone := <-watch.withdrawn:
		if gone.Withdrawn != taskWithdrawnReason {
			t.Fatalf("the dead attempt's card settled as %q", gone.Withdrawn)
		}
	default:
		t.Fatal("the dead attempt's card was never taken back")
	}
	results := 0
	for _, role := range transcriptRoles(agent) {
		if role == "tool" {
			results++
		}
	}
	if got, want := transcriptRoles(agent)[:4], []string{"system", "user", "assistant", "tool"}; !equalStrings(got, want) || results != 1 {
		t.Fatalf("transcript roles %v, want the retried reply recorded once with its one result", transcriptRoles(agent))
	}
	if recorded := messagesOf(agent); recorded[3].ToolCallID != "p-live" {
		t.Fatalf("the one result answers %q, want the retried call", recorded[3].ToolCallID)
	}
	if pending := agent.PendingTasks(); len(pending) != 0 {
		t.Fatalf("PendingTasks() = %v after the turn", pending)
	}
}

// A REPLY THAT ENDS WITHOUT THE CALL TAKES IT BACK TOO. The transport can rescue
// a stream by switching requests mid-answer, and the call the first request
// closed is then in no response at all; nothing retries, so the finished
// response is what withdraws it.
func TestAReplyThatEndsWithoutTheCallTakesItBack(t *testing.T) {
	orphan := proposalCall("p-orphan", "Port the resume picker")
	var watch proposalWatch
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, orphan)
			watch.card(t)
			return textResponse("on reflection, I will do it here"), nil
		},
	}}
	agent, graph := proposalSession(t, completer)
	events, err := agent.Submit(context.Background(), "port the picker")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	watch = watchProposals(t, agent, events)
	watch.finished(t)

	if nodes := admitted(graph); nodes != 0 {
		t.Fatalf("%d nodes were admitted for a call no response carried", nodes)
	}
	select {
	case <-watch.withdrawn:
	default:
		t.Fatal("the orphaned card was never taken back")
	}
}

// A WRITE BEFORE A PROPOSAL KEEPS ITS ORDER. The proposal's card may go up while
// the reply is arriving; the write, which cannot be taken back, still waits for
// the reply to be whole, and the results are recorded in call order.
func TestAWriteBeforeAProposalKeepsItsOrder(t *testing.T) {
	write := ai.ToolCall{ID: "w1", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"out.txt","content":"x"}`}}
	proposal := proposalCall("p1", "Port the resume picker")
	writes := make(chan string, 4)
	var watch proposalWatch
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, write)
			emitReady(t, ctx, proposal)
			watch.card(t)
			select {
			case <-writes:
				t.Error("the write started before the reply was whole")
			default:
			}
			return callsResponse(write, proposal), nil
		},
		finalText("done"),
	}}
	agent, graph := proposalSession(t, completer)
	var kept []bare.Tool
	for _, tool := range agent.tools {
		if tool.Name == "write" {
			tool = countingTool("write", writes, nil)
		}
		kept = append(kept, tool)
	}
	agent.tools = kept
	events, err := agent.Submit(context.Background(), "write the file and port the picker")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	watch = watchProposals(t, agent, events)
	watch.finished(t)

	if len(writes) != 1 {
		t.Fatalf("the write ran %d times, want once", len(writes))
	}
	if nodes := admitted(graph); nodes != 1 {
		t.Fatalf("%d nodes were admitted, want the proposal", nodes)
	}
	recorded := messagesOf(agent)
	if recorded[3].ToolCallID != "w1" || recorded[4].ToolCallID != "p1" {
		t.Fatalf("results are paired %q, %q; want the write then the proposal", recorded[3].ToolCallID, recorded[4].ToolCallID)
	}
}
