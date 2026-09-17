package main

import (
	"context"
	"testing"

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
		{Kind: session.EventTurnDone, Usage: session.Usage{Calls: 1, CostUSD: 0.123456}},
		{Kind: session.EventRetrying, Text: "the endpoint went quiet — asking again"},
		{Kind: session.EventTurnDone, Usage: session.Usage{Calls: 1}},
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
