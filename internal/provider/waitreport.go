package provider

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── WHAT A RACE LEAVES BEHIND ───────────────────────────────────────────────
//
// One question that became more than one request has two audiences afterwards
// and they want different things. A CALLER wants the slot: which lane answered,
// which was cancelled, what the rescue cost, and — while it is still happening
// — that one is in flight at all, because the only interesting state of a
// rescue is the one that is over before the call returns. A LOG wants the row:
// when the silence was acted on, what was done, how many arms it took, and what
// the arms that did not answer cost.
//
// Both are written here so that hedge.go is about arms and cancels and neither
// audience has a second place to look.

// ── WHAT THE LEDGER AND THE LOG ARE TOLD AFTERWARDS ─────────────────────────

// HedgeReport is what one request's rescue cost and bought, written into the
// caller's own slot exactly like [ServedEndpoint].
//
// It is a slot rather than a field on the response for the reason the served
// endpoint is: the SDK's response type is the OpenAI shape, and racing is this
// adapter's own bookkeeping. `internal/session`'s usage ledger reads it to
// write `hedged`, `lane` and `hedge_waste_usd` on the row.
type HedgeReport struct {
	mu sync.Mutex
	// hedged is whether a second request went out at all.
	hedged bool
	// winner is the lane whose answer the caller got, loser the lane that was
	// cancelled — both empty when no stream named one.
	winner string
	loser  string
	// reason is the controller's own machine word ("first token late", "drift",
	// "long think", "ceiling", "no heartbeat", "rate collapsed"). It is for
	// the log and never for a person.
	reason string
	// waste is what the arms that did not answer are estimated to have cost. It
	// is an ESTIMATE and says so: a cancelled stream delivers no usage frame,
	// so what is known is how many tokens it had written and what the lane
	// charges for them.
	waste float64
	// primary is the lane the FIRST request was served by, whichever arm went
	// on to win. It is a different fact from the winner and the loser, and it
	// is the one a surface needs: "this answer started on cloudflare and
	// finished on coreweave" cannot be said from a pair whose names swap places
	// depending on who won.
	primary string
	// fault records that the act was about the path rather than the lane, which
	// is what keeps the belief out of it.
	fault bool
	// action is what was done about the wait, in the controller's own word, and
	// silence how long it had run when that happened. arms is how many requests
	// this one question became.
	action  string
	silence time.Duration
	arms    int
	// onStart is told the moment a rescue goes out, and again the moment one
	// dies. It is the ONE thing on this slot that is not read afterwards, and it
	// exists because the only interesting state of a rescue is the one that is
	// over before the call returns: while the second request is in flight and
	// nobody has committed, which is the sentence a person reads on the status
	// line. A caller that registers nothing is told nothing, and nothing here
	// waits on it.
	//
	// IT CARRIES THE REASON AND NOT ONLY THE LANE, and it carries a retraction.
	// A lane name alone made a refusal and a slow answer indistinguishable at
	// the surface, so `…your request's provider.only preference permits only:
	// coreweave` was drawn as `· slow · trying nextbit…` and stayed on the
	// screen for ten minutes after the arm it was about had died (issue #266).
	// [RescueNews] is what goes through it now, narrowed from the one refusal
	// object so that the word a person reads is the word the ledger acted on.
	onStart func(RescueNews)
}

// Hedged reports whether a second request went out.
func (h *HedgeReport) Hedged() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hedged
}

// Lanes are the lane that answered and the lane that was cancelled.
func (h *HedgeReport) Lanes() (winner, loser string) {
	if h == nil {
		return "", ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.winner, h.loser
}

// Primary is the lane the first request was served by, empty when no stream
// named one. See the field for why it is not [HedgeReport.Lanes]'s loser.
func (h *HedgeReport) Primary() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.primary
}

// OnHedgeStart registers what to do about a rescue while it is still
// happening: once when one goes out, and once more if the machine it went to
// fails. It is called from the race's own goroutine and must not block; a nil
// function unregisters.
//
// IT IS SET BEFORE THE CALL AND NEVER DURING ONE. The slot belongs to the
// caller and is stamped on the context before the request goes out
// ([WithHedgeReport]), which is the only moment at which nothing is reading it.
func (h *HedgeReport) OnHedgeStart(fn func(RescueNews)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onStart = fn
}

// started tells the caller a rescue is in flight, outside the lock so that a
// slow reader cannot stall the race that is trying to rescue an answer.
func (h *HedgeReport) started(news RescueNews) {
	h.tell(news)
}

// ended retracts a rescue this slot has already announced, because the machine
// it named has failed.
//
// A SURFACE MAY ONLY BE LEFT SHOWING A CLAIM THAT IS STILL TRUE. `trying
// nextbit…` is a promise about the present tense, and nothing withdrew it when
// nextbit died — so the sentence sat on the status line until it aged out of a
// ten-minute window, describing a request that had already failed.
func (h *HedgeReport) ended(news RescueNews) {
	news.Failed = true
	h.tell(news)
}

// tell posts one piece of rescue news outside the lock, so that a slow reader
// cannot stall the race it is reading about.
func (h *HedgeReport) tell(news RescueNews) {
	if h == nil {
		return
	}
	h.mu.Lock()
	fn := h.onStart
	h.mu.Unlock()
	if fn != nil {
		fn(news)
	}
}

// Reason is the controller's machine word for why something was done.
func (h *HedgeReport) Reason() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reason
}

// Action is what was done about the wait — "hedge", "ask", "borrow", "report",
// "escalate", "commit" — and empty on a call nothing had to be done about.
func (h *HedgeReport) Action() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.action
}

// Silence is how long the stream had been silent when it was acted on.
func (h *HedgeReport) Silence() time.Duration {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.silence
}

// Arms is how many requests this one question put on the wire.
func (h *HedgeReport) Arms() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.arms
}

// Waste is the estimated dollars the arms that did not answer cost.
func (h *HedgeReport) Waste() float64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.waste
}

// PathFault reports whether the act was about a dead path rather than a slow
// lane. A ledger row that carried this as an ordinary demotion would be blaming
// an endpoint for somebody's network.
func (h *HedgeReport) PathFault() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fault
}

func (h *HedgeReport) note(fn func(*HedgeReport)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	fn(h)
}

type hedgeReportContextKey struct{}

// WithHedgeReport asks the adapter to write down, in the caller's own slot,
// what the race did to the calls made under ctx.
func WithHedgeReport(ctx context.Context, slot *HedgeReport) context.Context {
	if slot == nil {
		return ctx
	}
	return context.WithValue(ctx, hedgeReportContextKey{}, slot)
}

// HedgeReportFrom returns the slot in force for ctx, nil when none was opened —
// which every method here answers correctly, so a caller never tests.
func HedgeReportFrom(ctx context.Context) *HedgeReport {
	slot, _ := ctx.Value(hedgeReportContextKey{}).(*HedgeReport)
	return slot
}

// ── WHAT THE ROW SAYS ABOUT THE WAIT ────────────────────────────────────────

// waitFacts is everything the model-call log records about one arm's wait: what
// was believed, what happened, and what was done. It is gathered here because
// this is the only place all three are known at once (calllog.go writes it).
type waitFacts struct {
	// lane is the machine the preference NAMED for this arm — the head of the
	// order, or the lane a rescue demanded.
	lane string
	// deadline is when this arm was going to be acted on, from the moment it
	// went out, and ttft how long its first token really took.
	deadline time.Duration
	ttft     time.Duration
	// silence is how long the wait had run when something was done about it,
	// action what was done, reason why the controller did it, and wait and cost
	// the two numbers the inequality was decided on.
	silence time.Duration
	action  string
	reason  string
	wait    float64
	cost    float64
	// arms is how many requests this one question became, hedged whether that
	// was more than one, and waste what the arms that did not answer cost.
	arms   int
	hedged bool
	waste  float64
	// note is one sentence about something this call decided that no other
	// field can say. It is empty on almost every row.
	note    string
	refused string
}

// facts is what this arm's row carries about its wait.
func (w *streamWatch) facts() (waitFacts, bool) {
	if w == nil || w.race == nil {
		return waitFacts{}, false
	}
	// RACE FACTS ARE READ OUTSIDE THE WATCH LOCK. [hedgeRace.spend] holds the
	// race lock while reading the other watches, so taking that same pair in
	// the opposite order here lets two arms recording at once each wait for the
	// lock the other owns. The lane belongs to the race and does not need the
	// watch's protection; read it before entering this critical section, just as
	// spend is read after leaving it below.
	lane := w.race.askedLane(w.arm)
	w.mu.Lock()
	facts := waitFacts{
		lane:     lane,
		deadline: w.armed,
		silence:  w.silence,
		action:   actionWord(w.acted.Kind),
		reason:   w.acted.Reason,
		wait:     w.acted.Wait,
		cost:     w.acted.Cost,
	}
	if !w.first.IsZero() && !w.began.IsZero() {
		facts.ttft = w.first.Sub(w.began)
	}
	w.mu.Unlock()
	facts.arms, facts.waste, facts.note, facts.refused = w.race.spend(w.arm)
	facts.hedged = facts.arms > 1
	return facts, true
}

// actionWord is the controller's verdict as the log spells it. It is a table
// rather than a method on [control.Kind] because the words are this layer's
// vocabulary: the controller decides, and the log and the surface are what have
// to agree about what to call it.
func actionWord(kind control.Kind) string {
	switch kind {
	case control.Hedge:
		return "hedge"
	case control.Ask:
		return "ask"
	case control.Report:
		return "report"
	case control.Escalate:
		return "escalate"
	case control.Commit:
		return "commit"
	}
	return ""
}
