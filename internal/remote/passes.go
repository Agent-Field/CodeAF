package remote

// A COMPACTION PASS ASKED FOR OUTSIDE ANY TURN IS SAID TO EVERY WINDOW.
//
// A pass that runs inside a turn says itself on the turn's own stream. One a
// surface asks for with [MethodCompact] has no stream: the call answers when
// the pass is over, which can be two minutes when it writes a summary, and a
// surface waiting on that answer has nothing to draw in the meantime. So the
// pass's own events — [session.EventSummaryStarted] when a summary is being
// written, then [session.EventCompacted] or [session.EventSummaryFailed] — go
// out as `pass` frames to everybody watching, with the facts ahead of each so
// the weight and the summarizing flag a window reads are already the new ones.
//
// An older client ignores the frame kind (client.go's reader), and an older
// engine never sends it, which leaves a surface exactly where it was: the
// call's answer is the end of the pass.

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/session"
)

// passFrameKind is the frame a pass asked for outside any turn travels as.
const passFrameKind = "pass"

// passRoom is how many unread passes a client holds before it drops the next:
// a surface that stopped reading is not owed a backlog.
const passRoom = 8

// compactSaid runs the pass the surface asked for, saying it as it goes when
// the agent can.
func (sess *Session) compactSaid(agent WrappedAgent) error {
	observed, ok := agent.(interface {
		CompactObserved(context.Context, func(session.Event)) error
	})
	if !ok {
		return agent.Compact(context.Background())
	}
	return observed.CompactObserved(context.Background(), sess.tellPass)
}

// tellPass states the facts and then the pass's event to everybody watching.
// It is called on the pass's own goroutine with no session lock held.
func (sess *Session) tellPass(event session.Event) {
	switch event.Kind {
	case session.EventSummaryStarted, session.EventCompacted, session.EventSummaryFailed:
	default:
		return
	}
	sess.announce()
	payload, err := json.Marshal(WireEvent(event))
	if err != nil {
		return
	}
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	watching := sess.watchingLocked()
	sess.mu.Unlock()
	frame := Frame{Kind: passFrameKind, Payload: payload}
	for _, surface := range watching {
		_ = surface.send(frame)
	}
}

// Passes is the compaction passes asked for outside any turn, as their events
// arrive. A surface that does not read it loses nothing but those lines: the
// facts carry the weight, and the journal carries the pass.
func (c *Client) Passes() <-chan session.Event { return c.passes }

// passFrame takes one pass event off the wire.
func (c *Client) passFrame(payload []byte) {
	var wire EventWire
	if json.Unmarshal(payload, &wire) != nil {
		return
	}
	select {
	case c.passes <- wire.Unwire():
	default:
	}
}

// ContextUse is the conversation's weight against its window, read from the
// facts the engine last stated — no round trip.
func (a *Agent) ContextUse() session.ContextUse {
	facts := a.c.facts.read()
	return session.ContextUse{Tokens: facts.ContextTokens, Window: facts.ContextWindow, Threshold: facts.CompactAt, Summarizing: facts.Summarizing}
}
