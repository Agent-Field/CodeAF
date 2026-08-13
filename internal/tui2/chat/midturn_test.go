package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A turn that speaks before it is finished must not lose the stream.
//
// `say` (internal/head/acts.go) is the head putting one line in front of a
// person while it is still working, and it goes through the ordinary posting
// door — so what lands in the journal is an agent row with no node behind it,
// which is the exact shape [endsTurn] reads as "the reply this turn was
// previewing has arrived". It is not. The turn is still running, and retiring
// the live region on it threw away the words already streamed and left every
// delta after it with nowhere to go: the rest of the answer then appeared whole
// at the next poll, which is the thing this wave exists to stop.
//
// The stream itself says which it is. The head's loop is call, tool, call — the
// provider's Finished lands BEFORE the tool boundaries (internal/head/loop.go
// runs executeWatched after CompleteWithMessages returns) — so a tool boundary
// seen since the last Finished means another call is coming, and a row that
// arrives in that window is the turn talking rather than the turn ending.
func TestAnInterimSayDoesNotRetireTheLiveTurn(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)

	// The person asks; the head opens a turn and starts a tool call.
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "how is it going?"})
	poll(t, app)
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamFinished, Session: testSession})
	stream(app, StreamEvent{Kind: StreamToolBegin, Session: testSession, Delta: "looking at the work"})

	// Mid-loop, the head says one line out loud through the ordinary door.
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "still going — the second check is running now"})
	poll(t, app)

	if !app.turn.active {
		t.Fatal("an interim line retired the live turn: the rest of the answer will appear whole")
	}

	// The turn's real answer then streams, as it must.
	stream(app, StreamEvent{Kind: StreamToolEnd, Session: testSession})
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession,
		Delta: `{"reply":"both checks came back clean`})

	out := frame(app)
	if !strings.Contains(out, "both checks came back clean") {
		t.Fatalf("the answer after an interim line did not stream:\n%s", out)
	}
	if !strings.Contains(out, "still going — the second check is running now") {
		t.Fatalf("the interim line was lost from the transcript:\n%s", out)
	}
}

// The other half of the same law: the reply that really does end the turn still
// ends it, and the live region goes with it.
func TestTheReplyThatEndsTheTurnStillRetiresTheLiveRegion(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)

	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "how is it going?"})
	poll(t, app)
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession,
		Delta: `{"reply":"both checks came back clean"}`})
	stream(app, StreamEvent{Kind: StreamFinished, Session: testSession})

	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "both checks came back clean"})
	poll(t, app)

	if app.turn.active {
		t.Fatal("the durable reply did not retire the live turn")
	}
	out := frame(app)
	if !strings.Contains(out, "both checks came back clean") {
		t.Fatalf("the settled reply is not on screen:\n%s", out)
	}
	if strings.Contains(out, "thinking") {
		t.Fatalf("an awaiting line outlived the turn it belonged to:\n%s", out)
	}
}

// A window with no stream behind it — a visitor, or a head that never reported a
// boundary — retires its turn on the durable row exactly as it always did. The
// guard above may not become a way for an awaiting line to live forever.
func TestATurnThatNeverStreamedStillRetiresOnItsDurableReply(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, nil, nil)

	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "anyone there?"})
	poll(t, app)
	app.beginTurn(app.watermark)

	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "here"})
	poll(t, app)

	if app.turn.active {
		t.Fatal("a turn nobody streamed hung on its awaiting line")
	}
}

// A reply that diverges from what was streamed — a malformed or fallback
// response, the case v1 calls normalizeLandedTarget — still ends the turn. The
// guard is keyed to a tool call being in flight, not to the words matching, so
// there is no shape of answer that can leave the region standing.
func TestADivergentReplyStillRetiresTheLiveRegion(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)

	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "go on"})
	poll(t, app)
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"one thing`})
	stream(app, StreamEvent{Kind: StreamFinished, Session: testSession})

	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "something else entirely"})
	poll(t, app)

	if app.turn.active {
		t.Fatal("a reply that disagreed with the stream left the live region standing forever")
	}
}
