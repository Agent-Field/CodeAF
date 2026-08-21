package session

// The standing contract: how a conversation proposes a standing item to the
// person and hears the answer. It is the proposal half of internal/standing's
// contract, written by hand before the lanes started, in the shape
// task_contract.go already proved for propose_task.
//
// A STANDING ITEM IS A TASK WITH A WHEN. That is the whole framing, and it is
// why the card below is the task card with two more bands: when it wakes, and
// what it costs per run. The proposal rides the same hub, waits on the same
// kind of answer channel, and is ignored when late for the same reason
// [Agent.ResolveTask] ignores a late answer.
//
// NOTHING STANDS UNTIL THE ANSWER IS YES, AND NOBODY PRESENT MEANS NO. A task
// proposal approves on silence because its work is bounded and watched; a
// standing item spends forever, which crosses the consequence gate. So the card
// carries NO clock while somebody is there to read it — it waits, and a turn
// that ends with it unanswered leaves nothing behind — and an unwatched session
// (a headless run, a firing) cannot ratify one at all. The session lane
// implements askStanding on that law.

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// StandingNotice is the card. It is the payload of EventStandingProposal
// (ID is the token a surface hands back to [Agent.ResolveStanding]) and of
// EventStandingUpdate (ID is zero; Item is the item as it now stands).
type StandingNotice struct {
	ID uint64
	// Item is the proposed item, complete, as it would be created on a yes:
	// the person's words, when, what it does, the rails. A surface draws it
	// and never reshapes it; what the person changes comes back in the answer.
	Item standing.Item
	// WhenWords and CostWords are the two sentences the card leads with, in
	// the model's own words at proposal time: "Mondays at 9am", "about $0.02 a
	// run, at most once a day". The surface quotes them; it does not compute
	// them from the spec, because a spec read back as cron is a spec nobody
	// can check.
	WhenWords string
	CostWords string
	// Guessed says the model invented the cadence because the person gave
	// none, and the card should ask rather than state: "about every 2 minutes
	// — you didn't say, so that's my guess. Right?"
	Guessed bool
	// Options are the answers THIS card offers, from [StandingOptions].
	//
	// THE ENGINE SAYS WHICH CHIPS A CARD HAS, so the conversation's card, home's
	// answer row and the keys this session will actually take are one decision
	// made once. A one-off reminder offers no `once`, because doing "say time to
	// leave" NOW is not a smaller version of doing it at six — it is a different
	// thing, and usually nothing. A surface reads this rather than reasoning
	// from the item itself; the zero value means the kind's full row
	// ([AnswerOptions]).
	Options []AnswerOption
	// Deadline is ALWAYS ZERO from this build, and a surface draws no meter
	// for a zero. A standing card is read by a person, and a card that ended
	// itself while they were reading it was never answered — so the wait ends
	// on their answer, on an interrupt, or on the session closing, and never on
	// a clock (tools_standing.go). The field stays because the event's shape
	// does.
	Deadline time.Time
	// Update is set on EventStandingUpdate: "stood", "fired", "paused",
	// "resumed", "stopped", "needs-you", "failed". Empty on a proposal.
	Update string
	// Text is the one line an update carries: what it said, what it landed,
	// what it is stopped on.
	Text string
}

// StandingAnswer is what the person said to a card.
type StandingAnswer struct {
	// Approved stands the item up as proposed, or as changed below.
	Approved bool
	// Once says "do it once, not standing": the action runs now as an
	// ordinary turn or task and nothing is created.
	Once bool
	// Change is the person's free-text correction — "make it 8pm", "every
	// weekday" — which goes back to the model to re-propose. Nothing is
	// created on a change.
	Change string
}

// ResolveStanding answers one EventStandingProposal. An id nobody is waiting
// on — a card the clock already declined, a second click, an interrupted turn
// — is ignored, exactly as [Agent.ResolveTask] ignores a late answer.
func (a *Agent) ResolveStanding(id uint64, answer StandingAnswer) {
	a.mu.Lock()
	answers, waiting := a.standingAnswers[id]
	if waiting {
		delete(a.standingAnswers, id)
	}
	a.mu.Unlock()
	if !waiting {
		return
	}
	answers <- answer
}

// Standing is the seam every door sets so a conversation can propose, list,
// pause and stop items, and so firings can reach it. Nil means the ambient
// side is off: the belt tool is absent and no card is ever drawn — a
// capability that cannot work is absent, not broken.
type Standing struct {
	Store *standing.Store
	// Watch is this machine's own scheduler, and BACKGROUND CHECKS ARE ON BY
	// DEFAULT: the first item that ever stands installs the timer without
	// asking anybody, and the conversation says one dim line saying so and
	// where to turn it off. Nobody is asked because the question had one
	// sensible answer — something you asked to happen every morning is
	// something you asked to happen on the mornings you do not open a terminal
	// — and the switch lives in /settings under `background checks`
	// (internal/config's KeyStandingBackground).
	//
	// Nil means there is no timer on this host (a remote engine, a test, an
	// operating system the package cannot arrange one for), and then nothing is
	// installed and nothing is said.
	Watch standing.Watch
	// DailyRailUSD is quoted on the card beside the per-run cap.
	DailyRailUSD float64
}
