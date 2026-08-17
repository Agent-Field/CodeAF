package session

// THE HARNESS QUESTION: the moment between "somebody said something" and "the
// model was sent it".
//
// A sub-harness has no slash command (docs/SUBHARNESS.md). If running one
// required typing its name, the registry would be a menu, and a menu is a thing
// people forget they have — the harness that exists to do research properly
// would sit unused beside a turn doing research badly. So the turn itself is the
// trigger: what a person said is matched against the registry, and a strong
// match raises ONE line asking whether they meant it.
//
// Three laws hold this to something a person can live with.
//
//   - IT IS A QUESTION, NEVER A ROUTING. Nothing runs because a matcher said so.
//     A harness runs because somebody said yes to a card that named it, and the
//     answer that costs nothing — no — leaves an ordinary turn behind, already
//     recorded, already about to be sent. Detection cannot lose a turn.
//   - IT IS ASKED ONCE PER TURN, HERE, before the first request. Not per step,
//     not per tool call: a question that could arrive mid-turn would be a
//     question about a sentence the model has already half-answered.
//   - IT IS SILENT WHEN NOBODY IS WATCHING. No registry, no runner, or no
//     surface that answers questions (Config.AskConsent) and this file does
//     nothing at all — not one extra branch a person can observe, and not one
//     turn that behaves differently than it did before any of this existed.
//     That is the same law consent.go keeps for the same reason: a headless run
//     must never block on a question nobody will ever be shown.
//
// The scoring is [subharness.Score] and it is deliberately dull: a table lookup
// over the designer's own cue list, pure, deterministic, no model call. That
// package says why at length.

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ResolveHarness answers one EventHarnessOffer: true runs the harness, false is
// the ordinary turn. An id nobody is waiting on — an offer whose turn was
// interrupted, a second click — is dropped rather than reported, exactly as a
// late consent answer is.
func (a *Agent) ResolveHarness(id uint64, run bool) {
	a.mu.Lock()
	answers, waiting := a.harnessAsks[id]
	if waiting {
		delete(a.harnessAsks, id)
	}
	a.mu.Unlock()
	if !waiting {
		return
	}
	// Buffered to one and read at most once, so this never blocks and never
	// needs the lock held across it.
	answers <- run
}

// routeHarness is the whole of detection's place in a turn, called once from
// [Agent.runTurn] before anything is sent anywhere.
//
// It reports (answered, completed). answered=false is the ordinary turn — no
// registry, no match, or a person who said no — and the loop carries on as if
// this function did not exist. answered=true means the turn is OVER: the
// harness ran and its report is in the transcript, or it failed, or the turn
// was interrupted while the question was up.
func (a *Agent) routeHarness(ctx context.Context, hub *eventHub, user userMessage, started time.Time) (bool, bool) {
	match, ok := a.harnessMatch(user)
	if !ok {
		return false, false
	}
	run, err := a.askHarness(ctx, hub, match)
	if err != nil {
		// The turn died under the question — an interrupt, a closed agent. The
		// turn ends the way every interrupted turn ends, and nothing ran.
		hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started)})
		return true, false
	}
	if !run {
		return false, false
	}

	entry := match.Entry
	hub.send(Event{Kind: EventHarnessRun, Text: entry.Name, Hint: entry.Description})
	report, err := a.config.RunHarness(ctx, entry.Name, match.Turn.Text)
	if err != nil {
		hub.send(Event{Kind: EventError, Err: err, Usage: a.sealTurn(Usage{Turns: 1}, started)})
		return true, false
	}
	// The report is the turn's answer, so it is recorded as one. A harness whose
	// run said nothing records nothing rather than an empty assistant message:
	// the run happened, the events said so, and a blank turn in the transcript
	// is a thing later requests would carry forever.
	if report = strings.TrimSpace(report); report != "" {
		a.record(ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: report}}})
		hub.send(Event{Kind: EventTextDelta, Text: report})
	}
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{Turns: 1}, started)})
	// And the name, on the same terms the ordinary turn takes it (title.go): a
	// session whose first turn was a harness run is still a session with a
	// subject.
	a.maybeTitle(ctx, hub)
	return true, true
}

// harnessRoute is one turn matched against one registry.
type harnessRoute struct {
	subharness.Match
	// Turn is what was matched, carried through because the runner is handed
	// the person's words rather than the id of a message it cannot read.
	Turn subharness.Turn
}

// harnessMatch is the detection pass: the cheap refusals, then the score.
//
// It is a method only for the config; the deciding is [subharness.Best] and
// nothing here adds to it.
func (a *Agent) harnessMatch(user userMessage) (harnessRoute, bool) {
	// The refusals, cheapest first. Every existing caller of this package fails
	// the first one and pays two nil checks per turn for the whole feature.
	if a.config.RunHarness == nil || len(a.config.Harnesses) == 0 {
		return harnessRoute{}, false
	}
	if !a.config.AskConsent {
		return harnessRoute{}, false
	}
	// ONLY WHAT A PERSON TYPED IS MATCHED. A woken turn (task_run.go, jobs.go)
	// opens with an empty message and reads its note off the steering queue, and
	// a note the SESSION wrote is not somebody asking for a harness — offering
	// one against a task's own completion report would be the harness talking
	// itself into work nobody requested.
	if user.empty() || user.wake || user.authored {
		return harnessRoute{}, false
	}
	turn := subharness.Turn{Text: user.text()}
	if strings.TrimSpace(turn.Text) == "" {
		return harnessRoute{}, false
	}
	match, ok := subharness.Best(turn, a.config.Harnesses)
	if !ok {
		return harnessRoute{}, false
	}
	return harnessRoute{Match: match, Turn: turn}, true
}

// askHarness emits one offer and waits for the answer or for the turn to end.
//
// The wait is on the TURN's context, which is what makes Interrupt work on a
// pending card, and nothing here holds a.mu across it — the lock Interrupt
// needs must never be held by something waiting on a person.
func (a *Agent) askHarness(ctx context.Context, hub *eventHub, match harnessRoute) (bool, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return false, errAgentClosed
	}
	a.harnessSeq++
	id := a.harnessSeq
	answers := make(chan bool, 1)
	if a.harnessAsks == nil {
		a.harnessAsks = make(map[uint64]chan bool, 1)
	}
	a.harnessAsks[id] = answers
	a.mu.Unlock()

	hub.send(Event{
		Kind: EventHarnessOffer,
		ID:   id,
		Text: match.Entry.Name,
		// The entry's own sentence, so the card can say what saying yes would
		// get somebody without the surface writing a description of its own.
		Hint: match.Entry.Description,
	})

	select {
	case run := <-answers:
		return run, nil
	case <-ctx.Done():
		a.forgetHarness(id)
		return false, ctx.Err()
	}
}

// forgetHarness drops an offer nobody will answer. Without it an interrupted
// turn would leave its question in the map for the life of the session.
func (a *Agent) forgetHarness(id uint64) {
	a.mu.Lock()
	delete(a.harnessAsks, id)
	a.mu.Unlock()
}
