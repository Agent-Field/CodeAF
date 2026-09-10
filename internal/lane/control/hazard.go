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
// WHAT THE CEILING BOUNDS IS REAL SILENCE — a wire that has stopped writing —
// and not the gap between two readable words. The argument is at
// [hazard.stillSince]; the short version is that a model writing six thousand
// tokens of reasoning is the opposite of a model that has stopped, and a bound
// that could not tell the two apart called the fastest stream of the day a
// stall at ten seconds.
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

// ── TWO QUESTIONS, NOT ONE ──────────────────────────────────────────────────
//
// The inequality above answers "DOES ACTING PAY". It does not answer "IS THIS
// LANE MISBEHAVING", and for a while this package used it for both — which is a
// defect and was measured as one. `bench/lanelab/REPORT.md` has the run: with a
// warmed store, where the belief about each lane is TIGHT and correct, the
// controller hedged healthy requests MORE often than it did from a cold one,
// because a cheap alternative and a well-known median make "another arm would
// probably be quicker" true on perfectly ordinary draws. Every one of those
// arms was a rational answer to the payoff question and a wrong answer to the
// question a person is actually asking, which is whether anything is wrong.
//
// So an act before the ceiling now requires BOTH:
//
//	(1) PAYOFF     W(s) > A + m, exactly as before, and
//	(2) ABNORMALITY  s is in the tail of the very distribution that clock reads.
//
// ── WHERE THE TAIL MASS COMES FROM, AND WHY IT IS NOT A KNOB ────────────────
//
// This is a Neyman–Pearson test and it is sized the way one is sized: fix the
// false-positive rate you are willing to pay, and let that fix the threshold.
//
// Under the null — the lane is fine — the silence on any one alarm opportunity
// is a draw from that clock's own survival, so the chance of it exceeding that
// clock's own (1 − p) quantile is exactly p. A request offers k such
// opportunities. By the union bound the chance that ANY of them raises a false
// act is at most k · p, so
//
//	k · p  ≤  the per-request false-act budget
//	p      =  [falseActBudget] / k
//	z      =  Φ⁻¹(1 − p)                       ([deviate])
//
// and the threshold is [Survival.Quantile](z) of whichever survival is
// governing. The union bound is CONSERVATIVE — the true rate is at most k·p —
// so the gate is at least as strict as the budget asks, never looser.
//
// **The budget is not a new number.** It is §K's own acceptance criterion:
// no more than 2% of healthy requests may carry an arm
// (`docs/design/waiting/DESIGN.md` §K, and Dean & Barroso's measured figure for
// how much extra traffic removes most of a p99). The test is sized to the bound
// it has to meet, which is the whole of the derivation.
//
// **k IS COUNTED FROM THE REQUEST'S OWN SHAPE, NEVER CHOSEN.** A request has
// exactly one first token, and its run of thought is asked about once, as a
// whole; the drift clock is asked once per gap between two visible tokens, and
// how many of those there are is [Plan.Expected], which the transport already
// says before the stream starts. So
//
//	k = 1 (first token) + 1 (the thought) + Expected (the gaps)
//
// and where nobody said how long the answer would be, k is the two
// opportunities the request CERTAINLY has. Counting the unknown ones at a
// guess would be inventing evidence; counting them at zero is the honest floor,
// and the ceiling still bounds everything the looser gate lets through.
//
// ── WHAT THE GATE CANNOT DO ─────────────────────────────────────────────────
//
// IT CANNOT MOVE TIME-TO-ACTION. The ceiling is untouched and is still absolute:
// at [Plan.Ceiling] the controller acts whatever both tests say, and what it
// does there — hedge, ask, report — is unchanged. And a genuinely stalled lane
// crosses any quantile of its own distribution within seconds by construction:
// that is what a stall IS. The gate refuses acts on TYPICAL draws and on
// nothing else.
//
// IT CANNOT REFUSE ON A NUMBER NOBODY MEASURED. An unknown survival has no
// quantile — [Survival.Quantile] answers zero — so the gate is open, and the
// payoff test cannot fire either because [Survival.Remaining] on that belief is
// nothing at all. A plan with no belief is bounded by its ceiling and by
// nothing else, which is exactly what it was before: WHERE NOTHING IS MEASURED
// THE ANSWER IS "KEEP WAITING WHILE THE WIRE IS ALIVE", never an invented
// number.

// ── A TOKEN IS PROGRESS ONLY WHILE THE STREAM IS KEEPING UP ────────────────
//
// The per-gap tail test above cannot recognize a uniformly slow stream. A run
// of gaps that are each ordinary enough on their own can together say that the
// visible rate has collapsed, and raising the one-gap threshold enough to hear
// that run would also condemn healthy jitter. So each VISIBLE-to-visible gap g
// contributes its standard-normal deviate under the lane's gap belief:
//
//	zi = (ln g - Gap.Mu) / Gap.Sigma
//
// After n gaps, sum(zi) / sqrt(n) is standard normal under that belief and can
// be compared with the SAME derived z above. At one gap it is exactly the test
// already made by [hazard.abnormal], so this adds no second threshold.
//
// The sum and its weight both decay by exp(-g / Ceiling) before the new gap is
// added. THE COLLAPSE MUST BE CURRENT: evidence from a bad patch is forgotten
// on the same horizon as the bound it can stop resetting, and repeated looks
// do not accumulate forever. This pair gates only whether a visible token is
// credited as progress; it raises no act and enters no payoff arithmetic. Once
// the aggregate says the stream is not keeping up, the existing absolute
// ceiling gets the same honest silence it gets from a stream that stopped.
//
// This clock deliberately reads the PAGE while the drift clock in [hazard.assess]
// reads the WIRE. [Plan.Gap] is the measured interval between visible tokens,
// and a model that interleaves long thoughts between single words really is
// delivering visible text too slowly for the person waiting. Past a ceiling of
// that rate it is rescued; the think-duration clock has already had its say.

// falseActBudget is the share of HEALTHY requests that may carry an act raised
// by the arithmetic rather than by the ceiling.
//
// IT IS §K's OWN ACCEPTANCE CRITERION AND NOT A SECOND NUMBER: "false hedges on
// a healthy lane ≤ 2% of requests". The abnormality gate is sized to the bound
// it has to meet — see the derivation above — so the two cannot drift apart,
// and moving this figure means moving the design's acceptance.
const falseActBudget = 0.02

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
	// progress is the last CREDITED visible progress — the request going out
	// counts as the first — and it is what W and the floor measure from.
	// wrote is the last visible token whether or not it earned that credit;
	// delta is the last sign of the endpoint WRITING, visible or not, which is
	// the liveness clock's own origin AND the ceiling's, because a wire that is
	// writing is not a wire that has gone silent ([hazard.stillSince]). think
	// is when the run of thought began.
	progress time.Time
	wrote    time.Time
	delta    time.Time
	think    time.Time
	// drift and weight are the decayed sum of visible-gap deviates and its
	// effective count. They decide only whether a visible token still counts as
	// progress; the existing ladder remains the only thing that raises an act.
	drift  float64
	weight float64
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
	// z is the abnormality gate's threshold, in standard deviations of the
	// governing survival's own log-normal. It is derived once from the shape of
	// this request — see the derivation at the top of this file — because k is
	// [Plan.Expected] plus two and neither moves after the plan is built.
	z float64
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
		z:        deviate(falseActBudget / float64(opportunities(plan))),
	}
}

// opportunities is k: how many chances this request gives the arithmetic to
// raise a false act. It is COUNTED and never chosen — see the derivation at the
// top of this file — from the two the request certainly has and the one per gap
// between visible tokens that [Plan.Expected] says there will be.
func opportunities(plan Plan) int {
	k := 2
	if plan.Expected > 0 {
		k += plan.Expected
	}
	return k
}

// deviate is z with Φ(z) = 1 − tail: how many standard deviations out a draw
// has to be before it is rarer than `tail`.
//
// IT IS FOUND BY BISECTION rather than by a table of fitted constants, in the
// same spirit as [hazard.Deadline] and for a better reason: Φ is monotone, the
// bracket is exact, sixty halvings put z inside a part in 10^17, and a rational
// approximation would be six magic numbers in a file whose whole argument is
// that it has none. It is computed once per request.
func deviate(tail float64) float64 {
	if tail <= 0 {
		return math.Inf(1)
	}
	if tail >= 0.5 {
		return 0
	}
	low, high := 0.0, 40.0
	for range 60 {
		middle := low + (high-low)/2
		if 1-phi(middle) > tail {
			low = middle
		} else {
			high = middle
		}
	}
	return high
}

// Note folds in one moment of the stream and says what to do about it.
//
// THE THREE COUNTS ARE NOT INTERCHANGEABLE and this is the only place that
// matters. Visible text is measured against the visible-gap belief and resets
// the clock only while it is keeping up. A hidden delta is the endpoint writing
// where nobody can read, so it moves the PHASE and the liveness clock and
// leaves both visible clocks exactly where they were. A heartbeat moves neither.
func (h *hazard) Note(reading Reading) Act {
	h.advance(reading.At)
	switch {
	case reading.Visible > 0:
		h.visible += reading.Visible
		h.phase = PhaseWriting
		if !h.wrote.IsZero() {
			gap := h.now.Sub(h.wrote)
			if gap > 0 && h.plan.Gap.Known() {
				decay := math.Exp(-gap.Seconds() / h.plan.Ceiling.Seconds())
				// The belief is per token, while one event may contain a whole
				// batch. Normalize its interval so healthy batching keeps credit.
				perToken := gap.Seconds() / float64(reading.Visible)
				surprise := (math.Log(perToken) - h.plan.Gap.Mu) / h.plan.Gap.Sigma
				h.drift = h.drift*decay + surprise
				h.weight = h.weight*decay + 1
			}
		}
		h.wrote = h.now
		if h.keepingUp() {
			h.progress = h.now
		}
		h.delta = h.now
	case reading.Hidden > 0:
		if h.phase == PhaseSilent {
			h.phase, h.think = PhaseThinking, h.now
		}
		h.delta = h.now
	}
	return h.verdict()
}

// keepingUp reports whether the measured visible rate still earns progress.
// An unknown gap belief decides nothing, and the first visible token has no
// preceding visible gap — its interval belongs to the first-token clock.
func (h *hazard) keepingUp() bool {
	return !h.plan.Gap.Known() || h.weight < 1 || h.drift/math.Sqrt(h.weight) <= h.z
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
	ceiling := h.stillSince().Add(h.plan.Ceiling)
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

// silence is s: how long this request has gone without visible progress. It is
// what the payoff arithmetic is asked about and what the floor guards, and it
// is the figure the model-call row records. THE CEILING ASKS [hazard.still]
// INSTEAD — silence with a readable word at the end of it is not the same
// question as silence with a dead wire at the end of it.
func (h *hazard) silence(now time.Time) time.Duration { return now.Sub(h.progress) }

// quiet is how long the WIRE has been still: the time since the endpoint last
// wrote anything at all, readable or not. It is what the stall clocks ask
// about, because a lane still writing is a lane that has not stopped — and a
// heartbeat is not writing, which is why [hazard.delta] never moves on one.
func (h *hazard) quiet(now time.Time) time.Duration { return now.Sub(h.delta) }

// ── WHAT THE CEILING IS A CEILING ON ────────────────────────────────────────
//
// THE CEILING BOUNDS REAL SILENCE, AND AN ENDPOINT THAT IS WRITING IS NOT
// SILENT. It used to be measured from [hazard.progress] — the last VISIBLE
// token — which is right about what a person is waiting through only while the
// visible stream is the whole story. It is not the whole story on a model that
// thinks: hidden deltas move [hazard.delta] and the phase and never `progress`,
// so a healthy run of reasoning reached the ceiling at exactly the ceiling, on
// every model that deliberates for longer than one, while the tokens were
// arriving at full rate and the surface was drawing them counting up. The
// measured case was 6,174 reasoning tokens over 108 seconds at 57 a second —
// the fastest thing on the wire that afternoon — reported as a stall at ten.
//
// The drift clock in [hazard.assess] already reads the wire for exactly this
// reason and says so; the ceiling overrode it. So the ceiling reads the wire
// too, and the ONE case where a stream that is writing is nonetheless a silence
// a person is really in keeps its old clock: a visible rate that has collapsed,
// which [hazard.keepingUp] measures against the lane's own gap belief and which
// the ceiling already answers with [RateReason].
//
// NOTHING HERE LOOSENS THE ABSOLUTE. A still wire still reaches the ceiling at
// the ceiling — a heartbeat is not writing and never moves [hazard.delta], so a
// router holding the line open by saying nothing in a well-formed way is bounded
// exactly as before — and a stream that has stopped is a stream whose quiet and
// whose silence are the same number.

// rateCollapsed reports whether visible text has arrived and stopped keeping up
// with the lane's own belief about its rate. It is one predicate because two
// readings of it — which clock the ceiling runs on, and which word the act
// carries — must always agree.
func (h *hazard) rateCollapsed() bool { return h.visible > 0 && !h.keepingUp() }

// stillSince is the moment real silence began: the last time the endpoint wrote
// anything at all, or, where the visible rate has collapsed, the last credited
// progress — because text arriving too slowly to read IS a person waiting in
// silence, whatever the wire is doing.
func (h *hazard) stillSince() time.Time {
	if h.rateCollapsed() {
		return h.progress
	}
	return h.delta
}

// still is how long that silence has run. It is what the ceiling bounds, and it
// is never longer than [hazard.silence]: the wire writes at least as often as
// the screen does.
func (h *hazard) still(now time.Time) time.Duration { return now.Sub(h.stillSince()) }

// firesAt reports whether the answer at that moment is to act. It is the whole
// decision with the choice of act taken out of it, so that the deadline search
// and the verdict cannot drift apart.
func (h *hazard) firesAt(now time.Time) bool {
	silence := h.silence(now)
	if silence < h.plan.Floor {
		return false
	}
	if h.still(now) >= h.plan.Ceiling {
		return true
	}
	// BOTH TESTS, AND THE SEARCH ABOVE STILL WORKS BECAUSE BOTH ARE MONOTONE.
	// W rises with s over the range a request lives in and "s past a fixed
	// quantile" is monotone by inspection, so their conjunction is monotone and
	// the bisection still finds the FIRST moment the answer flips.
	wait, cost, _, odd := h.assess(now)
	return odd && wait.Over(cost, h.plan.Margin)
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
	wait, cost, word, odd := h.assess(h.now)
	out := Act{Silence: silence, Wait: wait, Cost: cost}
	switch {
	case silence < h.plan.Floor:
		// Under the floor a second request is racing the network rather than
		// the lane, so nothing is acted on — but the arm may still commit.
		return h.hold(out)
	case odd && wait.Over(cost, h.plan.Margin):
		out.Reason = word
		return h.act(out, false)
	case h.still(h.now) >= h.plan.Ceiling:
		rate := h.rateCollapsed()
		if rate {
			out.Reason = RateReason
		} else {
			out.Reason = CeilingReason
		}
		out = h.act(out, true)
		if rate {
			// THE CEILING BOUNDS TIME TO ACTION, so asking the ladder begins a
			// fresh interval whether or not it has another act left. A spent
			// ladder is the strongest case for not asking again on the next reading.
			h.progress = h.now
		}
		return out
	default:
		return h.hold(out)
	}
}

// assess is W and A right now, with the machine word for whichever clock is
// governing. It is the arithmetic of §B and the only place either number is
// computed.
func (h *hazard) assess(now time.Time) (wait, cost Seconds, word string, odd bool) {
	cost = h.cost()
	switch h.phase {
	case PhaseThinking:
		// The liveness clock first: an endpoint that has stopped writing
		// altogether is a stall whatever it was writing. It governs only when
		// it would really act — BOTH tests — because a gap that is a perfectly
		// ordinary draw is not what is wrong with a thought that has gone on
		// too long, and the clock below is the one that would say so.
		quiet := h.quiet(now).Seconds()
		gap, oddGap := h.plan.Gap.Remaining(quiet), h.abnormal(h.plan.Gap, quiet)
		if oddGap && gap.Over(cost, h.plan.Margin) {
			return gap, cost, "drift", true
		}
		// And the duration clock, which prices the alternative's own thought:
		// leaving a long think costs a whole fresh one, so only what is left of
		// a pathological one is worth paying that for. Its own abnormality is
		// asked against the THINK survival: how long this model's whole run of
		// thought usually lasts, which is the only distribution that can tell a
		// model deliberating from a model hung.
		thought := now.Sub(h.think).Seconds()
		return h.plan.Think.Remaining(thought), cost.Plus(h.plan.Think.Mean()), "long think",
			h.abnormal(h.plan.Think, thought)
	case PhaseWriting:
		// AND THE DRIFT CLOCK READS THE WIRE, NOT THE PAGE. A model that writes
		// three words and then thinks for a second has not stalled: the
		// endpoint is writing where nobody can read, which is what
		// [hazard.delta] holds and what the same clock reads in the thinking
		// phase above. Measuring this one from the last VISIBLE word instead
		// would call an interleaved run of thought a stall and buy a second
		// request for a lane that never stopped. What the SILENCE bounds is the
		// person's wait, and that is the ceiling's question and the floor's.
		quiet := h.quiet(now).Seconds()
		return h.plan.Gap.Remaining(quiet), cost, "drift", h.abnormal(h.plan.Gap, quiet)
	default:
		silence := h.silence(now).Seconds()
		return h.plan.First.Remaining(silence), cost, "first token late",
			h.abnormal(h.plan.First, silence)
	}
}

// abnormal is the second test: has this wait gone past the point where a
// healthy lane's own distribution says it should still be waiting?
//
// A SURVIVAL NOBODY MEASURED HAS NO QUANTILE and answers zero, so the gate is
// open — which is right and costs nothing, because the payoff test is closed in
// exactly that case: [Survival.Remaining] on an unknown belief is nothing at
// all, and [Seconds.Over] cannot say yes without a figure. A plan with no
// belief is bounded by its ceiling, as it was.
func (h *hazard) abnormal(of Survival, waited float64) bool {
	return waited > of.Quantile(h.z)
}

// cost is A: what acting would cost, in seconds.
//
//	A = E[TTFT_a] + V / rate_a + λ · Δ$
//
// WITH λ AT ZERO NOTHING BUYS SPEED. Nobody is waiting, so no amount of money
// converts into seconds, and the controller can only ever report — which is the
// honest half of "a background errand is worth money and not haste".
//
// AND WHERE THERE IS NO ALTERNATIVE THERE IS NO COST, which is nothing rather
// than infinity. The two read the same way in the one comparison this figure is
// ever in ([Seconds.Over] cannot say yes without a number on both sides), and
// only one of them is true: a request with nowhere to go has not been priced at
// an unaffordable amount, it has not been priced at all.
func (h *hazard) cost() Seconds {
	alt, ok := h.costAlt()
	if !ok || h.plan.Lambda <= 0 {
		return Seconds{}
	}
	rate := alt.Rate
	if rate <= 0 {
		rate = h.rate()
	}
	rewrite := 0.0
	if rate > 0 {
		rewrite = float64(h.visible) / rate
	}
	return Measured(alt.First.Mean() + rewrite + h.plan.Lambda*alt.Extra)
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
	if h.acted[Commit] || h.visible == 0 || !h.acted[Hedge] || !out.Cost.Known() {
		return out
	}
	h.acted[Commit] = true
	out.Kind, out.Lane, out.Reason = Commit, h.plan.Lane, "earned"
	return out
}
