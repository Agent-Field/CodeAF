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
// WHAT IS HELD IS EVERY QUESTION THAT ARRIVES ON A TURN'S STREAM AND CAN BE
// ANSWERED OVER THIS WIRE. The four kinds below are four of the resolve-doors
// [WrappedAgent] carries. The harness lane's two cards — a design, a subharness
// intake — are deliberately absent: they never travel a stream, and the session
// replays whichever of them is still standing onto every new subscription
// (standinglane.go), so remembering them here would hand one question over
// twice.

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
// EVERY CARD IS TRACKED, AND WAITING IS A FACT ABOUT A SURFACE RATHER THAN
// ABOUT THE ROOM. A card is waiting for the window in front of you when that
// window has never been sent it: the surfaces the frame actually went to are
// remembered on the card, and every other surface — including one that dialled
// a second ago — is owed it. Handing it to the surface that already drew it
// would draw the same question twice, and that is the only case being excluded.
//
// THIS IS DELIBERATELY NOT "IS ANYBODY ATTACHED". A terminal that was killed is
// still in the room until its socket reports the end, which is a scheduling
// delay away and not a fact anybody can wait for; a welcome that asked whether
// the room was empty would tell the window that just replaced that terminal
// there was nothing to answer. Asking what THIS surface has seen is a question
// the answer to which cannot be late.
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
	// seen is the arrival number of every surface this card's frame was sent
	// to ([server.arrived], unique per attach). A surface not named here has
	// never drawn this question and is owed it.
	seen map[uint64]struct{}
}

func newHeldSet() *heldSet {
	return &heldSet{items: map[heldKey]*heldItem{}}
}

// raise records one event, if that event is a question. It is called for every
// event on every stream, so the cheap answer — this is a text delta — has to be
// the first one.
//
// `drawn` is the arrival number of every surface this event's frame is being
// sent to, which is the whole of the difference between a card somebody is
// looking at and a card nobody is.
func (h *heldSet) raise(event EventWire, stream uint64, drawn []uint64) {
	key, ok := heldKeyOf(event.Event)
	if !ok {
		return
	}
	if existing, found := h.items[key]; found {
		// A question asked twice is one question. The card can legitimately be
		// re-emitted — a session swap replays nothing, but a lane that retries
		// its own offer would — and each re-emission only adds to the list of
		// surfaces that have drawn it.
		for _, arrived := range drawn {
			existing.seen[arrived] = struct{}{}
		}
		return
	}
	seen := make(map[uint64]struct{}, len(drawn))
	for _, arrived := range drawn {
		seen[arrived] = struct{}{}
	}
	h.order = append(h.order, key)
	h.items[key] = &heldItem{
		question: HeldQuestion{
			Kind:   key.kind,
			Event:  event,
			Stream: stream,
			Since:  time.Now(),
		},
		seen: seen,
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

// waitingFor is what a welcome carries and what Held.Questions answers: the
// questions this surface has not been sent, oldest first, because the oldest is
// the one that has been holding a turn the longest.
func (h *heldSet) waitingFor(arrived uint64) []HeldQuestion {
	var out []HeldQuestion
	for _, key := range h.order {
		item := h.items[key]
		if item == nil {
			continue
		}
		if _, drawn := item.seen[arrived]; drawn {
			continue
		}
		out = append(out, item.question)
	}
	return out
}

// outstanding is how many questions are unanswered, whoever is or is not
// looking at them. It is what the idle policy reads: a card nobody has answered
// is a turn that has stopped, and the conversation holding it is not idle even
// while somebody has it on screen.
func (h *heldSet) outstanding() int { return len(h.items) }

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
		// ONLY THE OFFER ARRIVES HERE. A design card is sent through
		// emitHarness, which reaches the harness lane's watchers and no turn's
		// stream, so it never passes this classifier — and it does not need to:
		// internal/session replays a card that is still standing onto every new
		// subscription, so the window that comes back is handed it there
		// (standinglane.go). Holding it here as well would draw one question
		// twice.
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
