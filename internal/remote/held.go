package remote

// held.go is the WAITING ROOM: the questions a conversation raised while nobody
// was attached to it, kept so that the next surface to arrive is handed them
// instead of never hearing them at all.
//
// It exists because version 2 separated a conversation's life from a
// connection's. A card is a question the turn STOPS on — consent.go blocks the
// call, the standing lane waits for an answer that never times out, a harness
// offer holds the turn before its first request — so a card raised into an
// empty room used to be a turn that sat there until something else killed it.
// That single fact is what made half the --host gap list honest: a lane whose
// answer could not arrive was better left off the belt entirely than present
// and hanging. With a persistent engine the room is no longer empty, it is
// merely unattended, and the difference is this file.
//
// WHAT IS HELD IS EVERY QUESTION THIS WIRE CAN ANSWER, AND NOTHING ELSE. The
// four kinds below are exactly the four resolve-doors [WrappedAgent] carries,
// because a question nobody can answer over this connection is not a question
// worth remembering — it is a card that would be drawn, pressed, and resolve
// nothing. The subharness intake card is the one that is deliberately absent:
// its answer goes back through ResolveSubharness, which is not on this wire at
// all, so holding one would be offering a person a key that does nothing.

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The four kinds a [HeldQuestion] can carry, which are the four resolve-doors
// this wire has. They are spelled once here so the engine and the surface
// cannot disagree about a string; wire.go's HeldQuestion.Kind documents why an
// unknown one is skipped rather than refused.
const (
	HeldConsent  = "consent"
	HeldStanding = "standing"
	HeldHarness  = "harness"
	HeldConnect  = "connect"
)

// heldSet is one session's outstanding questions, oldest first.
//
// EVERY CARD IS TRACKED AND ONLY SOME ARE WAITING. A card raised while somebody
// is attached is on their screen, and handing it to the next surface as
// "held" would draw it twice; a card raised into an empty room has never been
// seen by anybody. So both are remembered, and the second is marked waiting
// immediately while the first becomes waiting the moment the last surface
// leaves without answering it — which is the same fact arriving a few minutes
// later.
//
// It is NOT independently locked. Every door below is called with the owning
// [Session]'s mutex held, because every one of them is part of a larger
// decision that mutex already guards — "record this event and say who is
// watching", "let this surface in and tell it what is outstanding". A second
// lock here would only be a second order to get wrong.
type heldSet struct {
	order []heldKey
	items map[heldKey]*heldItem
}

// heldKey is which question, across the two shapes of id this wire carries: the
// three lanes that mint a uint64 and the connect lane, whose id is a string
// (session.Event's ConnectID states why the two are different fields).
type heldKey struct {
	kind string
	id   uint64
	text string
}

type heldItem struct {
	question HeldQuestion
	// waiting says nobody has ever seen this card, so the next surface to
	// attach should be handed it.
	waiting bool
}

func newHeldSet() *heldSet {
	return &heldSet{items: map[heldKey]*heldItem{}}
}

// raise records one event, if that event is a question. It is called for every
// event on every stream, so the cheap answer — this is a text delta — has to be
// the first one.
//
// `alone` is whether the room was empty at the moment it was raised, which is
// the whole of the difference between a card somebody is looking at and a card
// nobody is.
func (h *heldSet) raise(event EventWire, stream uint64, alone bool) {
	key, ok := heldKeyOf(event.Event)
	if !ok {
		return
	}
	if existing, found := h.items[key]; found {
		// A question asked twice is one question. The card can legitimately be
		// re-emitted — a session swap replays nothing, but a lane that retries
		// its own offer would — and the waiting flag only ever moves toward
		// waiting, because a card that was once unseen stays unseen until it is
		// answered.
		existing.waiting = existing.waiting || alone
		return
	}
	h.order = append(h.order, key)
	h.items[key] = &heldItem{
		question: HeldQuestion{
			Kind:   key.kind,
			Event:  event,
			Stream: stream,
			Since:  time.Now(),
		},
		waiting: alone,
	}
}

// roomEmptied is the last surface leaving. Everything still outstanding is now
// a question nobody is looking at, which is what waiting means.
func (h *heldSet) roomEmptied() {
	for _, item := range h.items {
		item.waiting = true
	}
}

// answered drops a question because its resolve-door was called. It is keyed
// rather than searched so that answering a consent card cannot take down a
// harness offer that happens to share an id — two lanes, two counters
// (session.Event's ID field states that law).
func (h *heldSet) answered(kind string, id uint64, text string) {
	key := heldKey{kind: kind, id: id, text: text}
	if _, found := h.items[key]; !found {
		return
	}
	delete(h.items, key)
	for index, held := range h.order {
		if held == key {
			h.order = append(h.order[:index], h.order[index+1:]...)
			break
		}
	}
}

// settleConnect drops connect cards whose engine-side wait has ended without
// an answer. The event stream crosses this after the ask settles; retaining a
// card past that point would hand the next surface a key with no lock behind it.
func (h *heldSet) settleConnect(pending []string) {
	live := make(map[string]bool, len(pending))
	for _, id := range pending {
		live[id] = true
	}
	for _, key := range append([]heldKey(nil), h.order...) {
		if key.kind == HeldConnect && !live[key.text] {
			h.answered(HeldConnect, 0, key.text)
		}
	}
}

// waiting is what a welcome carries and what Held.Questions answers: the
// questions nobody has seen, oldest first, because the oldest is the one that
// has been holding a turn the longest.
func (h *heldSet) waiting() []HeldQuestion {
	var out []HeldQuestion
	for _, key := range h.order {
		if item := h.items[key]; item != nil && item.waiting {
			out = append(out, item.question)
		}
	}
	return out
}

// forget drops everything, which is what a session swap does to the questions
// of the conversation it replaced. A card belongs to the agent that raised it,
// and the surface has already been handed a fresh welcome that forgot
// everything before it — the same law [server.pump] states about a late event.
func (h *heldSet) forget() {
	h.order = nil
	h.items = map[heldKey]*heldItem{}
}

// heldKeyOf says which resolve-door answers an event, and whether any does.
//
// THE KINDS ARE READ FROM THE EVENT AND NEVER GUESSED. Each one names the field
// its answer travels back on — [WrappedAgent.ResolveConsent] takes Event.ID,
// ResolveStanding takes the id INSIDE the standing card, ResolveConnect takes
// the string in ConnectID — so a card whose id could not be read is not held,
// because holding it would promise an answer that lands nowhere.
func heldKeyOf(event session.Event) (heldKey, bool) {
	switch event.Kind {
	case session.EventConsentRequest:
		if event.ID == 0 {
			return heldKey{}, false
		}
		return heldKey{kind: HeldConsent, id: event.ID}, true

	case session.EventStandingProposal:
		if event.Standing == nil || event.Standing.ID == 0 {
			return heldKey{}, false
		}
		return heldKey{kind: HeldStanding, id: event.Standing.ID}, true

	case session.EventHarnessOffer, session.EventHarnessDesignDone:
		// One door answers both: an offer to RUN a harness and a finished page
		// asking to be SAVED both go back through ResolveHarness, which is why
		// they share a kind here (internal/session's harness.go and its
		// EventHarnessDesignDone both name that method).
		//
		// ONLY THE OFFER ACTUALLY ARRIVES HERE TODAY. A design card is sent
		// through emitHarness, which reaches the watchers of
		// [session.Agent.HarnessDesigns] and no turn's stream — a lane this wire
		// has no door for, which is why building a harness is switched off over
		// a connection at all (cmd/aforge's engine.go says so in prose). The
		// kind is named beside the offer because the two are ONE QUESTION with
		// one answer, and a classifier that knew about half of a door would be
		// the thing that quietly broke on the day the other half arrives.
		if event.ID == 0 {
			return heldKey{}, false
		}
		return heldKey{kind: HeldHarness, id: event.ID}, true

	case session.EventConnectAsk:
		if event.ConnectID == "" {
			return heldKey{}, false
		}
		return heldKey{kind: HeldConnect, text: event.ConnectID}, true
	}
	return heldKey{}, false
}
