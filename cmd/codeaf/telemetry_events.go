package main

// ── counting a hosted session from the events it receives ───────────────────
//
// The session counters tick where the session loop runs (internal/session's
// sealTurn and executeTool, internal/provider's calllog). Over a host link the
// loop runs in the session host — a separate process, or a separate machine —
// while session_started and session_ended are spooled by the chat process,
// whose own tally has seen none of it: a hosted chat's end event carried zeros
// for every count. The events the chat process receives already say what
// happened, so the agent it hands the surface counts them on the way through.
//
// A session this process runs itself — --no-host, --once with no host
// answering, onboarding, --debug (chatv3_local.go's [v3TakeHostRoad]) — is
// already counted at the source, and counting its events again would double
// every number. The tell is the agent's own type: a *remote.Agent fronts a
// session another process is running, a *session.Agent is this process's own.

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/telemetry"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// countedAgent wraps the surface's agent with the counter tee when the session
// it fronts runs in another process, and hands back anything else unchanged.
//
// The gate is the agent's type and not a flag the launch carries, because the
// type IS the fact the gate is about: the agent the chat process opened is a
// *remote.Agent exactly when the conversation's turns, calls and tools run in
// the session host it is linked to, and a *session.Agent exactly when they run
// here and the source has already counted them.
func countedAgent(agent tui3.Agent) tui3.Agent {
	far, linked := agent.(*remote.Agent)
	if !linked {
		return agent
	}
	return &countingAgent{Agent: far}
}

// countingAgent is a linked session's agent with the counting tee in front of
// its four event doors.
//
// IT EMBEDS THE CONCRETE AGENT AND NOT THE SURFACE'S INTERFACE, for the same
// reason besideAgent does: the surface reaches capability doors through narrow
// interface assertions (internal/tui3's steerAgent, attachable and their
// kind), and promoting the concrete type's methods keeps every one of them
// answered. Only the four doors that hand back a stream are overridden; the
// wrapper asserts to *remote.Agent no better than the agent it wraps does, so
// a door that could not reach that type before still cannot. Steer is left as
// it was on purpose: its tail is the running turn's own stream, of which the
// surface keeps exactly one reader (internal/tui3's steer.go), and the turn's
// events are already counted on the Submit stream that pump reads.
type countingAgent struct {
	*remote.Agent
}

// Submit is [tui3.Agent.Submit] with the stream counted as it is handed on.
func (c *countingAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	events, err := c.Agent.Submit(ctx, text)
	if err != nil {
		return events, err
	}
	return countedEvents(events), nil
}

// SubmitStanding is [tui3.Agent.SubmitStanding] with the stream counted.
func (c *countingAgent) SubmitStanding(ctx context.Context, text string) (<-chan session.Event, error) {
	events, err := c.Agent.SubmitStanding(ctx, text)
	if err != nil {
		return events, err
	}
	return countedEvents(events), nil
}

// SubmitImage is [tui3.Agent.SubmitImage] with the stream counted.
func (c *countingAgent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	events, err := c.Agent.SubmitImage(ctx, text, images)
	if err != nil {
		return events, err
	}
	return countedEvents(events), nil
}

// FollowUp is [tui3.Agent.FollowUp] with the stream counted.
func (c *countingAgent) FollowUp(text string) (<-chan session.Event, error) {
	events, err := c.Agent.FollowUp(text)
	if err != nil {
		return events, err
	}
	return countedEvents(events), nil
}

// countedEvents returns src with the count taken on the way through: one
// goroutine copies the stream event for event, counts first and hands on, and
// closes the copy when the stream closes. The copy is the same shape the
// stream it wraps is (internal/remote's stream.out) — unbuffered, so the flow
// the surface already backs up against is the flow it keeps, one hop longer.
func countedEvents(src <-chan session.Event) <-chan session.Event {
	out := make(chan session.Event)
	guard.Go("chatv3/telemetry-events", func() {
		defer close(out)
		for event := range src {
			countEvent(event)
			out <- event
		}
	})
	return out
}

// countEvent reads one event into the process's tally, on the terms the
// in-process chokepoints would have counted the same work. A stream an
// in-process session never carries these facts on (its counters are already
// ticked at the source), so every reader of this function is a linked session.
func countEvent(event session.Event) {
	switch event.Kind {
	case session.EventTurnDone:
		// A sealed turn, and every provider request the turn reports
		// (internal/session's Usage.Calls — answered requests; a voided attempt
		// rides EventRetrying below). The event carries the turn's whole cost
		// and not a cost per call, so the first call carries it and the rest
		// carry nothing: the total is the number the chokepoint would have
		// counted.
		telemetry.CountTurn()
		telemetry.CountTokens(event.Usage.Input, event.Usage.Output)
		for i := 0; i < event.Usage.Calls; i++ {
			cost := 0.0
			if i == 0 {
				cost = event.Usage.CostUSD
			}
			telemetry.CountModelCall(true, cost)
		}
	case session.EventRetrying:
		// One voided attempt: the request went out and its answer was thrown
		// away (internal/session's retrynews.go). It is the one event that
		// names a single dead call — [session.EventError] ends the whole turn,
		// and a turn the patience ran out on counts its last dead attempt
		// nowhere on the wire.
		telemetry.CountModelCall(false, 0)
	case session.EventToolEnd:
		telemetry.CountToolCall(true)
	case session.EventToolFailed:
		// A failure the HARNESS wrote is a door's own answer and not a tool
		// call that ran; the event carries the fact for exactly the counters
		// that read events, and [session.Agent.executeTool] skips it for the
		// same reason.
		if !event.HarnessMade {
			telemetry.CountToolCall(false)
		}
	}
}
