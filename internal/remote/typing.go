package remote

import (
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE KEYSTROKE, OVER A CONNECTION ────────────────────────────────────────
//
// A PERSON WHO HAS STARTED WRITING IS THE ONLY WARNING THIS BUILD EVER GETS
// that a request is about to go out, and internal/session's [session.Agent]
// spends it on two things: a fresh measurement of the two machines the next
// turn is most likely to use, and a connection already open when it does. The
// second of those is what stops a think-pause costing a handshake —
// internal/provider's probe.go says so in its own header, and it is the half
// nobody could see missing.
//
// IT WAS BROKEN IN TWO PLACES AND BOTH ARE MENDED IN THE SAME CHANGE, because
// mending one of them would have delivered nothing.
//
// THE SURFACE COULD NOT SEND IT. internal/tui3 reaches for the capability
// through an optional interface ([typingAgent]), which a `*session.Agent`
// satisfies and a `*remote.Agent` did not — and the default road holds a
// `*remote.Agent` talking to a detached engine (cmd/aforge/chatv3_local.go's
// v3TakeHostRoad is true for every launch but `--no-host`, `--debug`, first-run
// setup and a hostless `--once`). So the assertion failed silently, on the road
// every person is on. [TestEverySurfaceDoorTheEngineHasCrossesTheWire] is what
// stops that class of gap coming back quietly.
//
// AND THE ENGINE COULD NOT ACT ON IT EITHER, WHICH IS THE OLDER HALF.
// internal/session's [session.Agent.probeClientLanes] asserts an optional
// prober on its completer, and every construction path wraps that completer in
// `sessionCompleter` — which forwarded two methods and not this one. So even
// in process, with the keystroke arriving perfectly, the assertion failed and
// no probe had ever been bought. internal/session's
// [TestTheCompleterWrapperForwardsEveryDoorTheAdapterOffers] is that layer's
// version of the same law.

// typingDoor is an agent that can be told a person has started writing. It is
// an optional door on the engine's side for the reason it is one on the
// surface's: a task node's agent is deliberately silent here, and a capability
// with nothing behind it is ABSENT rather than present and refusing.
type typingDoor interface{ Typing() }

var _ typingDoor = (*session.Agent)(nil)

// typingHush is the shortest gap between two of these frames on one
// connection.
//
// THE BUDGET THAT MATTERS IS THE ENGINE'S, not this one: the prober buys at
// most one pair every twenty seconds per model and asks every gate it has
// inside the call (internal/lane's probe.go). This exists only so that a fast
// typist does not put a frame on the wire per character to be refused eighty
// times a second at the other end. It is leading-edge — the FIRST keystroke
// goes at once, because the whole value of the signal is how early it arrives
// — and a second under it is the honest floor for a hint that is free to lose.
const typingHush = time.Second

// typingBeat is the hush, kept per connection.
type typingBeat struct {
	mu   sync.Mutex
	last time.Time
}

// due reports whether a frame may go now, and records that it did.
func (b *typingBeat) due(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.last.IsZero() && now.Sub(b.last) < typingHush {
		return false
	}
	b.last = now
	return true
}

// Typing tells the engine somebody is writing. IT DOES NOT WAIT, it reports
// nothing, and a connection that cannot carry it swallows it — which is the
// same contract [session.Agent.Typing] has and the reason the surface may call
// it on every character (internal/tui3's [app.laneTyping]).
func (a *Agent) Typing() {
	if a == nil || a.c == nil {
		return
	}
	if !a.c.typing.due(time.Now()) {
		return
	}
	a.c.notify(MethodTyping, nil)
}
