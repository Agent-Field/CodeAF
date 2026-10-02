package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/telemetry"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// countingTestStream is the session a linked host sends a chat process, as the
// events the surface would draw: a tool call of each verdict, a harness answer
// that never ran a tool, two turns with a voided attempt between them, and
// rows the counters must not read. The first turn's whole cost rides its own
// event; the totals are two turns, three model calls with one failure, and
// two tool calls with one failure.
func countingTestStream() []session.Event {
	return []session.Event{
		{Kind: session.EventToolBegin, Tool: "bash"},
		{Kind: session.EventToolEnd, Tool: "bash"},
		{Kind: session.EventToolFailed, Tool: "edit", Hint: "oldText did not match"},
		{Kind: session.EventToolFailed, Tool: "propose_task", HarnessMade: true},
		{Kind: session.EventTextDelta, Text: "partial"},
		{Kind: session.EventTurnDone, Usage: session.Usage{Calls: 1, Input: 100, Output: 20, CostUSD: 0.123456}},
		{Kind: session.EventRetrying, Text: "the endpoint went quiet — asking again"},
		{Kind: session.EventTurnDone, Usage: session.Usage{Calls: 1, Input: 30, Output: 5}},
		{Kind: session.EventNotice, Text: "row news"},
	}
}

// TestTelemetryEventsTeeCountsAHostedSession: a linked session's counts are
// the events it receives, read on the way through — two turns, three model
// calls of which the voided attempt is the one failure, two tool calls of
// which the failed call is the one failure, and the turn's whole cost on the
// call that carries it. The harness answer counts nothing, and neither does
// any row the counters have no fact for.
func TestTelemetryEventsTeeCountsAHostedSession(t *testing.T) {
	telemetry.ResetCountersForTest(t)

	src := make(chan session.Event)
	out := countedEvents(src)
	go func() {
		defer close(src)
		for _, event := range countingTestStream() {
			src <- event
		}
	}()
	for range out {
	}

	want := telemetry.SessionStats{
		Turns:            2,
		ModelCalls:       3,
		ModelCallsFailed: 1,
		ToolCalls:        2,
		ToolCallsFailed:  1,
		CostUSD:          0.123456,
	}
	if got := telemetry.Snapshot(); got != want {
		t.Fatalf("hosted stream counted %+v, want %+v", got, want)
	}
}

// TestTelemetryEventsTeeLeavesAnInProcessSessionAlone: the gate counts from
// events ONLY where the session runs in another process. An agent whose
// session this process runs is the gate's refusal — handed back as it was, so
// the same stream through it changes nothing (the source has already counted
// it, and counting again would double every number) — and the linked type is
// the one the gate wraps.
func TestTelemetryEventsTeeLeavesAnInProcessSessionAlone(t *testing.T) {
	telemetry.ResetCountersForTest(t)

	// countingStubAgent is an agent shaped like the in-process door's: it
	// implements the surface's interface by embedding it and answers Submit
	// with one stream it was given.
	agent := &countingStubAgent{}
	gated := countedAgent(agent)
	if gated != tui3.Agent(agent) {
		t.Fatalf("countedAgent wrapped an agent the session runs in this process")
	}
	stream, err := gated.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	for range stream {
	}
	if got := telemetry.Snapshot(); got != (telemetry.SessionStats{}) {
		t.Fatalf("the gate-off stream counted %+v, want nothing", got)
	}

	if _, ok := countedAgent(&remote.Agent{}).(*countingAgent); !ok {
		t.Fatalf("countedAgent left a linked session's agent uncounted")
	}
}

// countingStubAgent is the gate test's in-process agent.
type countingStubAgent struct {
	tui3.Agent
}

// Submit hands back one stream carrying the same events the hosted test reads.
func (s *countingStubAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	src := make(chan session.Event)
	go func() {
		defer close(src)
		for _, event := range countingTestStream() {
			src <- event
		}
	}()
	return src, nil
}

// TestTelemetryTheHostedBootAgentIsCounted: the agent the shared door hands the
// surface is the counting tee, and so is the handle its Resume and Fresh hand
// back — one object under one name (chatv3_host_shared_test.go), counted.
func TestTelemetryTheHostedBootAgentIsCounted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client, _ := swapClient(t, &swapEngine{})
	options, _ := hostOptions(onePipeFleet("devbox", client), client.Welcome(), false)
	if _, counted := options.Agent.(*countingAgent); !counted {
		t.Fatalf("the hosted door's boot agent is a %T, so a hosted chat's session_ended carries zeros again", options.Agent)
	}
	next, err := options.Resume("/srv/app/b.jsonl")
	if err != nil {
		t.Fatalf("resume over the connection: %v", err)
	}
	if next != options.Agent {
		t.Fatal("the counted door's Resume handed back a different handle from the one the surface holds")
	}
}

// TestTelemetryAConversationOpenedBesideIsCounted: a conversation the ordinary
// engine door opens beside the first one runs in the engine too, so the turns
// and tools its stream reports reach this process's tally like the first's.
func TestTelemetryAConversationOpenedBesideIsCounted(t *testing.T) {
	telemetry.ResetCountersForTest(t)
	far := &farMachine{workspace: "/home/somebody/api"}
	farHost(t, far)
	options, _, done := besideDoor(t, far)
	defer done()

	conv, err := options.Start("")
	if err != nil {
		t.Fatalf("start a conversation beside: %v", err)
	}
	stream, err := conv.Agent.Submit(context.Background(), "beside")
	if err != nil {
		t.Fatalf("submit into the conversation beside: %v", err)
	}
	engineSide := far.held(conv.SessionFile)
	if engineSide == nil {
		t.Fatalf("the engine opened no conversation for %q", conv.SessionFile)
	}
	waitUntilBeside(t, "the beside conversation had a turn in flight", engineSide.running)
	engineSide.mu.Lock()
	lane := engineSide.turn
	engineSide.mu.Unlock()
	lane <- session.Event{Kind: session.EventToolEnd, Tool: "bash"}
	lane <- session.Event{Kind: session.EventTurnDone, Usage: session.Usage{Calls: 1, CostUSD: 0.5}}
	close(lane)
	for range stream {
	}

	got := telemetry.Snapshot()
	if got.Turns != 1 || got.ModelCalls != 1 || got.ToolCalls != 1 || got.CostUSD != 0.5 {
		t.Fatalf("the conversation opened beside counted %+v, want one turn, one call, one tool and its cost", got)
	}
}

// queueingAgent is a far agent whose follow-ups WAIT: each one is a stream held
// open until the turn would start, and the take-back closes it unrun, the way
// [session.Agent.UnqueueFollowUp] does.
type queueingAgent struct {
	quietAgent
	mu     sync.Mutex
	queued []chan session.Event
}

func (q *queueingAgent) FollowUp(string) (<-chan session.Event, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch := make(chan session.Event)
	q.queued = append(q.queued, ch)
	return ch, nil
}

func (q *queueingAgent) UnqueueFollowUp(ch <-chan session.Event) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, held := range q.queued {
		if (<-chan session.Event)(held) == ch {
			q.queued = append(q.queued[:i], q.queued[i+1:]...)
			close(held)
			return true
		}
	}
	return false
}

// TestTelemetryTheCountingTeeTakesAQueuedMessageBack: THE CLICK ON A QUEUED ROW
// WORKS ON A HOSTED CHAT. The tee hands the surface a COPY of each follow-up's
// stream, and the surface takes a message back by the channel it holds — so a
// take-back promoted straight through named a stream the remote agent had never
// minted and answered false every time. Over the real wire, through the tee the
// hosted door builds, the take-back comes out, the copy closes with no events,
// and a second press is false.
func TestTelemetryTheCountingTeeTakesAQueuedMessageBack(t *testing.T) {
	far := &queueingAgent{}
	loop, err := remote.Loopback(
		remote.Hello{Version: remote.Version, Workspace: "/srv/app"},
		remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/a.jsonl"}, nil
		}},
	)
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	agent := countedAgent(loop.Client.Agent())
	if _, counted := agent.(*countingAgent); !counted {
		t.Fatalf("the linked agent was not wrapped by the counting tee: %T", agent)
	}
	ch, err := agent.FollowUp("and the changelog")
	if err != nil {
		t.Fatalf("FollowUp: %v", err)
	}
	unqueuer, ok := agent.(interface {
		UnqueueFollowUp(<-chan session.Event) bool
	})
	if !ok {
		t.Fatal("the counting tee does not offer the take-back, so the surface draws no click")
	}
	if !unqueuer.UnqueueFollowUp(ch) {
		t.Fatal("the take-back answered false for a message the tee queued")
	}
	select {
	case ev, open := <-ch:
		if open {
			t.Fatalf("the taken-back follow-up streamed %v, want a closed channel", ev.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the taken-back follow-up's stream was never closed")
	}
	if unqueuer.UnqueueFollowUp(ch) {
		t.Fatal("the same message was taken back twice")
	}
}
