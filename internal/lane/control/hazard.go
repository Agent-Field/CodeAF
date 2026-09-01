package control

import (
	"math"
	"time"
)

// ── THE CONTROLLER ──────────────────────────────────────────────────────────
//
// One inequality, three distributions, six acts. Everything below is the
// arithmetic of `docs/design/waiting/DESIGN.md` §B and nothing else: no clock,
// no connection, no vocabulary a person reads.
//
// At every moment the controller holds two numbers.
//
//	W(s)  what going on waiting is expected to cost, in seconds
//	A     what acting is expected to cost, in the same seconds
//
// It acts when W(s) > A + m, and at [Plan.Ceiling] whatever the two say,
// because a belief that recommends waiting past a person's patience is
// answering a question nobody asked. THE CEILING IS THE ONE ABSOLUTE: a plan
// with no belief at all still reaches it, which is the whole of "from zero
// history, every call is bounded".
//
// The three phases differ only in which distribution stands behind W:
//
//   - SILENT, nothing has arrived: the serving lane's first token, measured
//     from the moment the request went out.
//   - THINKING, reasoning deltas and nothing on the screen: TWO clocks, because
//     a legitimately long think and a stalled think look identical for the
//     first few seconds and must not be answered the same way. The liveness
//     clock asks whether the endpoint is still writing at all, against the gap
//     between two deltas. The duration clock asks whether this thought is
//     pathologically long, against how long this model's whole thinking phase
//     lasts — AND IT PRICES THE ALTERNATIVE'S OWN THOUGHT INTO A, because the
//     second request would have to think the same thing. That one term is what
//     stops a minute of deliberation being answered with a second minute of it.
//   - WRITING, visible text arriving: the gap between two visible tokens.
//
// Heartbeats move nothing. `: OPENROUTER PROCESSING` is proof about the path
// and about nothing else, and a silence clock a router could hold open by
// saying nothing in a well-formed way is not a clock.

// fallbackCeiling bounds a plan that arrived with none.
//
// A plan with no ceiling is a defect — every role has one ([internal/lane].
// Role.Ceiling) — and the conservative reading of a defect is the figure a
// person watching an empty line is asked to wait, not forever. It is spelled
// here rather than imported because this package imports nothing.
const fallbackCeiling = 10 * time.Second

// hazard is one request's controller.
//
// It is driven by whoever owns the stream, one call at a time, exactly as the
// watch it replaces was. Every field is a moment or a count; there is no state
// machine beyond [Phase] and no timer of any kind.
type hazard struct {
	plan  Plan
	phase Phase
	// now is the latest moment anybody has told it about, and it is the only
	// reason this type ever compares two times.
	now time.Time
	// progress is the last VISIBLE progress — the request going out counts as
	// the first — and it is what both W and the ceiling measure from. delta is
	// the last sign of the endpoint WRITING, visible or not, which is the
	// liveness clock's own origin. think is when the run of thought began.
	progress time.Time
	delta    time.Time
	think    time.Time
	// visible is the text already on the screen, in tokens. It is what a
	// rescue would have to write again, and hidden tokens are deliberately not
	// in it: a run of thought is money already spent and nothing a person would
	// watch disappear.
	visible int
	// used is how many alternatives have been sent to, so a second act goes
	// somewhere new rather than to the lane that has already been asked.
	used int
	// acted records which kinds have fired, which is how an offer is raised
	// once and a wait is reported once.
	acted [Commit + 1]bool
	// refused remembers a purse that said no. A purse that refuses is final for
	// this request: polling it every beat would be asking the same question of
	// the same numbers.
	refused bool
}

// New builds the controller this build waits with. It is the [Factory] the
// parent package installs, and the only one a shipped file may call.
func New(plan Plan) Controller {
	if plan.Ceiling <= 0 {
		plan.Ceiling = fallbackCeiling
	}
	if plan.Floor < 0 {
		plan.Floor = 0
	}
	return &hazard{
		plan:     plan,
		now:      plan.Began,
		progress: plan.Began,
		delta:    plan.Began,
		think:    plan.Began,
	}
}

// Note folds in one moment of the stream and says what to do about it.
//
// THE THREE COUNTS ARE NOT INTERCHANGEABLE and this is the only place that
// matters. Visible text is progress and resets the clock. A hidden delta is the
// endpoint writing where nobody can read, so it moves the PHASE and the
// liveness clock and leaves the silence exactly where it was. A heartbeat moves
// neither.
func (h *hazard) Note(reading Reading) Act {
	h.advance(reading.At)
	switch {
	case reading.Visible > 0:
		h.visible += reading.Visible
		h.phase = PhaseWriting
		h.progress, h.delta = h.now, h.now
	case reading.Hidden > 0:
		if h.phase == PhaseSilent {
			h.phase, h.think = PhaseThinking, h.now
		}
		h.delta = h.now
	}
	return h.verdict()
}

// Quiet says nothing has arrived by now, and asks the same question.
func (h *hazard) Quiet(now time.Time) Act {
	h.advance(now)
	return h.verdict()
}

// Serving re-points the controller at the machine that is really answering.
//
// A router honours any of an order and says which way it went on every chunk.
// The first-token belief moves only while nothing has arrived yet: past the
// first delta it has nothing left to bound, and a lane named after the answer
// started must not reopen a window that has already closed.
func (h *hazard) Serving(lane string, first, gap Survival, now time.Time) {
	h.advance(now)
	if lane != "" {
		h.plan.Lane = lane
	}
	if gap.Known() {
		h.plan.Gap = gap
	}
	if first.Known() && h.phase == PhaseSilent {
		h.plan.First = first
	}
}

// Phase is which distribution is governing right now.
func (h *hazard) Phase() Phase { return h.phase }

// Acted reports whether an act of this kind has already fired.
func (h *hazard) Acted(kind Kind) bool {
	if int(kind) >= len(h.acted) {
		return false
	}
	return h.acted[kind]
}

// Deadline is the next moment the answer could change, so a beat can sleep
// until it rather than poll.
//
// IT IS FOUND RATHER THAN DERIVED, and a search is the honest way to find it: A
// is a sum of four terms and W is a ratio of two normal integrals, so there is
// no closed form for where they cross, and there does not need to be. The
// ceiling answers yes by construction, so the window between the floor and it
// always brackets a crossing; forty halvings put it inside a microsecond; and
// over the range past the floor, at the spread this build waits against, W only
// rises — so the crossing found is the first one.
//
// IT IS NEVER IN THE PAST. A moment already gone is a timer that fires
// immediately and forever, which is how a beat becomes a spin.
func (h *hazard) Deadline() time.Time {
	ceiling := h.progress.Add(h.plan.Ceiling)
	low := h.progress.Add(h.plan.Floor)
	if low.Before(h.now) {
		low = h.now
	}
	if !low.Before(ceiling) || h.firesAt(low) {
		return h.notPast(low)
	}
	for range 40 {
		middle := low.Add(ceiling.Sub(low) / 2)
		if h.firesAt(middle) {
			ceiling = middle
		} else {
			low = middle
		}
	}
	return h.notPast(ceiling)
}

// advance moves the moment forward. A moment already seen is not a moment: the
// stream loop and the beat both drive this and neither owns the other's clock.
func (h *hazard) advance(now time.Time) {
	if now.After(h.now) {
		h.now = now
	}
}

// notPast is the one guard [hazard.Deadline] is written around.
func (h *hazard) notPast(moment time.Time) time.Time {
	if moment.Before(h.now) {
		return h.now
	}
	return moment
}

// silence is s: how long this request has gone without visible progress.
func (h *hazard) silence(now time.Time) time.Duration { return now.Sub(h.progress) }

// firesAt reports whether the answer at that moment is to act. It is the whole
// decision with the choice of act taken out of it, so that the deadline search
// and the verdict cannot drift apart.
func (h *hazard) firesAt(now time.Time) bool {
	silence := h.silence(now)
	if silence < h.plan.Floor {
		return false
	}
	if silence >= h.plan.Ceiling {
		return true
	}
	wait, cost, _ := h.assess(now)
	return wait > cost+h.plan.Margin
}

// verdict is the one place an act is decided, so that the ladder and the
// once-only rules are impossible to route around.
//
// EVERY VERDICT CARRIES ITS NUMBERS, including the ones that decide to keep
// waiting: the silence, W and A are what the phase clock draws and what the
// call log has to hold, and a controller that only reported them when it acted
// would be a controller nobody could autopsy.
func (h *hazard) verdict() Act {
	silence := h.silence(h.now)
	wait, cost, word := h.assess(h.now)
	out := Act{Silence: silence, Wait: wait, Cost: cost}
	switch {
	case silence < h.plan.Floor:
		// Under the floor a second request is racing the network rather than
		// the lane, so nothing is acted on — but the arm may still commit.
		return h.hold(out)
	case wait > cost+h.plan.Margin:
		out.Reason = word
		return h.act(out, false)
	case silence >= h.plan.Ceiling:
		out.Reason = CeilingReason
		return h.act(out, true)
	default:
		return h.hold(out)
	}
}

// assess is W and A right now, with the machine word for whichever clock is
// governing. It is the arithmetic of §B and the only place either number is
// computed.
func (h *hazard) assess(now time.Time) (wait, cost float64, word string) {
	cost = h.cost()
	switch h.phase {
	case PhaseThinking:
		// The liveness clock first: an endpoint that has stopped writing
		// altogether is a stall whatever it was writing.
		if gap := h.plan.Gap.Remaining(now.Sub(h.delta).Seconds()); gap > cost+h.plan.Margin {
			return gap, cost, "drift"
		}
		// And the duration clock, which prices the alternative's own thought:
		// leaving a long think costs a whole fresh one, so only what is left of
		// a pathological one is worth paying that for.
		return h.plan.Think.Remaining(now.Sub(h.think).Seconds()), cost + h.plan.Think.Mean(), "long think"
	case PhaseWriting:
		return h.plan.Gap.Remaining(h.silence(now).Seconds()), cost, "drift"
	default:
		return h.plan.First.Remaining(h.silence(now).Seconds()), cost, "first token late"
	}
}

// cost is A: what acting would cost, in seconds.
//
//	A = E[TTFT_a] + V / rate_a + λ · Δ$
//
// WITH λ AT ZERO NOTHING BUYS SPEED. Nobody is waiting, so no amount of money
// converts into seconds, and the controller can only ever report — which is the
// honest half of "a background errand is worth money and not haste".
func (h *hazard) cost() float64 {
	alt, ok := h.costAlt()
	if !ok || h.plan.Lambda <= 0 {
		return math.Inf(1)
	}
	rate := alt.Rate
	if rate <= 0 {
		rate = h.rate()
	}
	rewrite := 0.0
	if rate > 0 {
		rewrite = float64(h.visible) / rate
	}
	return alt.First.Mean() + rewrite + h.plan.Lambda*alt.Extra
}

// rate is what the lane serving this stream is believed to write at, in tokens
// a second, read back out of the gap between two of them. Zero is unknown, and
// unknown prices a rewrite at nothing rather than at a guess.
func (h *hazard) rate() float64 {
	if gap := h.plan.Gap.Mean(); gap > 0 {
		return 1 / gap
	}
	return 0
}

// costAlt is the lane the arithmetic is priced against: the next one worth
// sending to while there is one, and the last one sent to once there is not, so
// that [Commit] still has a real number to be cheaper than.
func (h *hazard) costAlt() (Alternative, bool) {
	switch {
	case h.used < len(h.plan.Alts):
		return h.plan.Alts[h.used], true
	case len(h.plan.Alts) > 0:
		return h.plan.Alts[len(h.plan.Alts)-1], true
	}
	return Alternative{}, false
}

// act is the ladder: which of the six this moment is, in the order
// docs/ARCHITECTURE.md already sets.
//
// The purse is asked LAST and only when a hedge is really about to go out,
// because asking it is spending it: [internal/lane.Budget.Allow] counts the arm
// it allows, and a controller that polled it while deciding to report something
// else would spend somebody's allowance on a decision nobody acted on.
// AND AT THE CEILING λ STOPS DECIDING. What a second is worth is what makes a
// wait worth money, and for a role nobody is watching it is nothing — but the
// ceiling is not about money at all. It is the promise that no call this build
// makes waits longer than that, whatever the arithmetic said, so a wait that
// reaches it with an affordable alternative in hand becomes the act it would
// have been for a person: a rescue, or the question when a person named the
// machine. Reporting there would be saying the wait is real while holding
// somewhere better to be.
func (h *hazard) act(out Act, ceiling bool) Act {
	if h.plan.Pinned {
		// A PIN IS ASKED, NEVER OVERRIDDEN, and it is asked once: a second
		// offer for one request is nagging.
		if h.acted[Ask] {
			return out
		}
		out.Kind, out.Lane = Ask, h.offered()
	} else if alt, ok := h.reachable(ceiling); ok {
		out.Kind, out.Lane = Hedge, alt.Lane
		h.used++
	} else if h.used > 0 && h.used >= len(h.plan.Alts) && !h.acted[Escalate] {
		// Every gate-passing lane has been tried and none answered. The model
		// ladder owns what happens next; this package only says its own rungs
		// are spent.
		out.Kind = Escalate
	} else if h.acted[Report] {
		return out
	} else {
		// Nowhere better to go, and silence is never an option: the wait is
		// real and it is said out loud.
		out.Kind = Report
	}
	h.acted[out.Kind] = true
	return out
}

// offered is the lane an offer would rescue to: the best the frontier named,
// whether or not the purse would allow it, because a person answering "yes" is
// spending their own patience and is told where it would go.
func (h *hazard) offered() string {
	if len(h.plan.Alts) == 0 {
		return ""
	}
	return h.plan.Alts[0].Lane
}

// reachable is the next alternative worth acting on, and whether there is one.
//
// A purse that refuses is final for this request. λ at zero refuses too — with
// nobody waiting, no amount of money buys speed — EXCEPT at the ceiling, where
// the question is no longer what a second is worth: see [hazard.act].
func (h *hazard) reachable(ceiling bool) (Alternative, bool) {
	if h.used >= len(h.plan.Alts) || h.refused {
		return Alternative{}, false
	}
	if h.plan.Lambda <= 0 && !ceiling {
		return Alternative{}, false
	}
	alt := h.plan.Alts[h.used]
	if h.plan.Purse != nil && !h.plan.Purse.Allows(alt.Extra, h.now) {
		h.refused = true
		return Alternative{}, false
	}
	return alt, true
}

// hold is the other half of the same inequality: staying is cheaper than
// leaving, so if anything else is in flight this arm has earned the answer.
//
// THERE IS NO COMMITMENT CONSTANT. What used to be sixty-four tokens is the
// rewrite term of A, which grows with the text on the screen: a four-hundred
// token reply commits early and a four-thousand token one commits late, for the
// same reason and out of the same arithmetic.
func (h *hazard) hold(out Act) Act {
	if h.acted[Commit] || h.visible == 0 || !h.acted[Hedge] || math.IsInf(out.Cost, 1) {
		return out
	}
	h.acted[Commit] = true
	out.Kind, out.Lane, out.Reason = Commit, h.plan.Lane, "earned"
	return out
}
