package lane

import "time"

// ── THE WATCH: RESCUING A REQUEST THAT IS ALREADY SLOW ──────────────────────
//
// Everything before this point picks a lane before the request goes out. The
// watch is the half that can still act after it has gone, and it is worth
// having because a first-token distribution with a heavy tail has a property
// that feels wrong until it is written down: FOR A LOG-NORMAL, THE LONGER YOU
// HAVE WAITED, THE LONGER YOU SHOULD EXPECT TO GO ON WAITING. A stream that is
// four seconds late is not four seconds from finishing; it is a draw from the
// tail, and the cheapest thing to do with it is to ask somebody else.
//
// So the hedge time is derived, per request, from the posterior of the lane we
// expect to serve — the smallest wait at which the expected REMAINING wait
// exceeds what the alternative would take from cold, plus what the second
// request costs. A lane whose normal is four hundred milliseconds hedges at
// about a second; a lane whose normal is two seconds does not hedge there at
// all. That is the whole reason there is no constant in this file.
//
// Three things are watched and they are different claims:
//
//   - THE HEARTBEAT. A router emits comment lines before the first token. A
//     stream with neither a heartbeat nor a byte for well past its deadline is
//     a dead path, not a slow lane, and the lane's belief is NOT charged for
//     it — that would be teaching the ledger a fact about a machine that was
//     never asked.
//   - THE FIRST TOKEN. Late past the deadline is a hedge.
//   - THE GAPS BETWEEN TOKENS, once they flow, as a one-sided CUSUM against
//     what this lane's believed rate says a gap should be. A single long gap
//     is buffered delivery — an answer arriving in lumps is still an answer.
//     An accumulating drift is a lane that has BECOME slow mid-answer.
//
// And a commitment rule, because sunk cost is real here: past the first tokens
// the stream is only abandoned when the drift alarms AND finishing here would
// take longer than redoing the whole answer somewhere else from zero.
//
// A HEDGE IS A MEASUREMENT. The second request is also the only cheap way to
// learn what the alternative lane would have done, so whichever way it lands it
// goes back into the ledger as a sighting.
//
// THIS FILE HEDGES NOTHING YET. The watch below tracks the stream honestly and
// never asks for a hedge; the budget refuses everything. Lane L-C fills both in
// and writes the transport half in `internal/provider/hedge.go`. Refusing is
// the right empty answer for both: an un-budgeted hedge is the one failure mode
// of this whole mechanism that costs real money.

// Watch follows one stream from the moment it is sent.
//
// It is fed by the stream loop and it holds no clock of its own: every method
// takes the moment as an argument, for the same reason the chooser does.
type Watch struct {
	// deadline and alt are the choice's own, decided at send time. The moment
	// a hedge is wanted is the worst possible moment to start choosing where
	// to send it.
	deadline time.Duration
	alt      string
	// belief is what was expected of the lane serving this stream. It is what
	// turns a gap into a surprise rather than into a threshold.
	belief Belief
	// began is when the request went out, and tokens counts what has arrived.
	began  time.Time
	tokens int
	// beat is the last sign of life of any kind — a heartbeat comment or a
	// token — and last is the last TOKEN. They are separate because they
	// answer different questions: whether the path is alive, and whether the
	// model is writing.
	beat time.Time
	last time.Time
	// hedged is set once, because a request gets one hedge and not a race.
	hedged bool
}

// NewWatch starts a watch over one stream.
func NewWatch(choice Choice, belief Belief, now time.Time) *Watch {
	return &Watch{
		deadline: choice.Deadline,
		alt:      choice.Alt,
		belief:   belief,
		began:    now,
		beat:     now,
	}
}

// Deadline is when this stream was expected to start answering, zero when the
// choice asked for no hedging. It is exposed so the transport can arm a timer
// against it rather than poll.
func (w *Watch) Deadline() time.Duration { return w.deadline }

// Alt is the lane a hedge would go to, empty when there is nobody worth hedging
// to.
func (w *Watch) Alt() string { return w.alt }

// Hedged reports whether this request has already spent its one hedge.
func (w *Watch) Hedged() bool { return w.hedged }

// Heartbeat records a sign of life that is not a token — the router's own
// comment line before the model has said anything. It is proof about the PATH
// and never about the endpoint.
func (w *Watch) Heartbeat(t time.Time) { w.beat = t }

// Token records that n tokens have now arrived, and says whether the stream
// should be given up on.
//
// It returns no verdict while lane L-C has not built the drift test. Watching
// without acting is the honest empty behaviour: the numbers are being kept and
// nothing is being spent on them.
func (w *Watch) Token(n int, t time.Time) Verdict {
	w.tokens = n
	w.beat, w.last = t, t
	return Verdict{}
}

// Silence records that nothing has arrived by t, and says whether that silence
// has gone on long enough to act on.
//
// It returns no verdict while lane L-C has not built the deadline arithmetic.
func (w *Watch) Silence(t time.Time) Verdict { return Verdict{} }

// ── THE BUDGET ──────────────────────────────────────────────────────────────

// Budget is what stops a rescue mechanism from becoming a second bill.
//
// Two limits, because the two failure modes are different. A TOKEN BUCKET
// bounds a storm — one bad minute in which every stream stalls must not double
// every request in flight. A SHARE OF SPEND bounds the slow leak, where a
// hedge that fires a little too eagerly is invisible per request and obvious at
// the end of the month.
type Budget struct {
	// perMinute is the bucket's size and refill, in hedges.
	perMinute int
	// share is the fraction of recent spend hedging may add, 0 to 1.
	share float64
}

// NewBudget states a budget. A zero or negative bucket is a budget that allows
// nothing, which is how hedging is switched off.
func NewBudget(perMinute int, share float64) *Budget {
	return &Budget{perMinute: perMinute, share: share}
}

// Allow reports whether one more hedge, expected to cost costEstimate dollars,
// may be sent now.
//
// It refuses everything while lane L-C has not built the bucket. A budget that
// said yes without counting would be the one empty implementation in this
// package that could cost somebody money.
func (b *Budget) Allow(now time.Time, costEstimate float64) bool { return false }
