package lane

import (
	"math"
	"time"
)

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
// request costs. The chooser computes it and hands it over in [Choice.Deadline];
// a watch given none falls back to the serving lane's own believed p90 first
// token, clamped to a range where hedging can pay for itself at all. A lane
// whose normal is four hundred milliseconds hedges at about a second; a lane
// whose normal is two seconds does not hedge there at all. That is the whole
// reason there is no constant for "slow" in this file.
//
// Three things are watched and they are different claims:
//
//   - THE HEARTBEAT. A router emits comment lines before the first token. A
//     stream with neither a heartbeat nor a byte for well past its deadline is
//     a dead path, not a slow lane, and the lane's belief is NOT charged for
//     it — that would be teaching the ledger a fact about a machine that was
//     never asked. [Watch.PathFault] is how the caller knows not to.
//   - THE FIRST TOKEN. Late past the deadline is a hedge.
//   - THE GAPS BETWEEN TOKENS, once they flow, as a one-sided CUSUM against
//     what this lane's believed rate says a gap should be. A single long gap
//     is buffered delivery — an answer arriving in lumps is still an answer.
//     An accumulating drift is a lane that has BECOME slow mid-answer.
//
// THE THREE NUMBERS IN THE DRIFT TEST ARE NOT THRESHOLDS ON SPEED. The slack k
// and the alarm height h are measured in NATS OF SURPRISE against the lane's
// own believed rate, so they mean the same thing on a lane that writes six
// tokens a second and on one that writes two hundred; that is exactly what the
// fixed thirty-tokens-a-second in the strike table could not do. The one
// wall-clock figure left, the fifteen seconds at which a single gap alarms on
// its own, is today's [Sighting.Gap] meaning kept unchanged on purpose: a
// quarter-minute of nothing is a complaint whatever the lane's normal is.
//
// And a commitment rule, because sunk cost is real here: past the first tokens
// the stream is only abandoned when the drift alarms AND finishing here would
// take longer than redoing the whole answer somewhere else from zero.
//
// A HEDGE IS A MEASUREMENT. The second request is also the only cheap way to
// learn what the alternative lane would have done, so whichever way it lands it
// goes back into the ledger as a sighting.
//
// The watch is PURE: it holds no clock, no lock and no channel, every method
// takes the moment as an argument, and it fires at most one verdict in its
// life. Whoever drives it — one stream loop, one goroutine at a time — owns the
// serialization, and `internal/provider/hedge.go` is that owner.

const (
	// deadlineFloor and deadlineCeiling bound a derived deadline. Below the
	// floor a hedge is racing the network rather than the lane — the second
	// request has its own handshake to pay — and above the ceiling nobody is
	// still reading anyway, so a bound that never fires is a bound that lies.
	deadlineFloor   = 700 * time.Millisecond
	deadlineCeiling = 8 * time.Second
	// deadPathFloor is the shortest silence that may be read as a dead path.
	// Two deadlines is the shape of the rule — a path that has said nothing at
	// all for twice as long as the lane's whole expected wait is not a slow
	// lane — and three seconds is the floor under it, because a fast lane's
	// deadline doubled is still less time than a TLS handshake and a cold
	// connection can honestly take.
	deadPathFloor = 3 * time.Second
	// lumpGap is one gap long enough to complain about on its own, whatever
	// the lane's believed rate. It is today's velocity ledger's LagGap and it
	// keeps its meaning exactly.
	lumpGap = 15 * time.Second
	// commitTokens is where an answer stops being cheap to abandon. Past it a
	// drift alarm is not enough on its own: see [Watch.worthLeaving].
	commitTokens = 64
	// driftSlack (k) is how much slower than believed a gap may be before it
	// counts as evidence at all, and driftAlarm (h) is how much evidence is
	// needed. Both are in nats of log-gap, so a lane's own rate is what they
	// are measured against. k = 0.5 lets a gap be about a two-thirds again
	// longer than expected without accumulating anything, which is ordinary
	// jitter; h = 3.0 then needs several such gaps in a row, or one very much
	// worse, before the answer is given up on.
	driftSlack = 0.5
	driftAlarm = 3.0
)

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
	// altTTFT and altRate are what the frontier believed about the lane a
	// hedge would go to — milliseconds and tokens per second — read out of the
	// choice once, because the commitment rule compares finishing here against
	// starting there and a watch that had to ask the ledger mid-stream would be
	// reading a belief the choice never saw.
	altTTFT float64
	altRate float64
	// began is when the request went out, and tokens counts what has arrived.
	began  time.Time
	tokens int
	// expected is how long this answer is thought to be, in output tokens. It
	// is the other half of the commitment rule and it is set by whoever knows
	// it ([Watch.SetExpectedTokens]); zero means "no idea", under which a
	// committed stream is never abandoned.
	expected int
	// beat is the last sign of life of any kind — a heartbeat comment or a
	// token — and last is the last TOKEN. They are separate because they
	// answer different questions: whether the path is alive, and whether the
	// model is writing.
	beat time.Time
	last time.Time
	// drift is the CUSUM's accumulated surprise, in nats, never below zero.
	drift float64
	// hedged is set once, because a request gets one hedge and not a race.
	hedged bool
	// fault records that the verdict was about the PATH rather than the lane,
	// so the caller can decline to charge a belief for it.
	fault bool
}

// NewWatch starts a watch over one stream.
func NewWatch(choice Choice, belief Belief, now time.Time) *Watch {
	watch := &Watch{
		deadline: choice.Deadline,
		alt:      choice.Alt,
		belief:   belief,
		began:    now,
		beat:     now,
	}
	if watch.deadline <= 0 {
		watch.deadline = derivedDeadline(belief)
	}
	// The alternative's numbers as the choice scored them, so that the
	// commitment rule and the picker's explanation are reading one arithmetic.
	for _, scored := range choice.Frontier {
		if scored.ID.Lane == choice.Alt {
			watch.altTTFT, watch.altRate = scored.TTFT, scored.Rate
			break
		}
	}
	return watch
}

// derivedDeadline is the fallback when the choice named none: the serving
// lane's own believed ninetieth-percentile first token, clamped.
//
// It is a fallback and not the rule. The chooser has the alternative's
// posterior and the price of a second in front of it and can solve for the
// moment the expected remaining wait exceeds a fresh start; a watch holding one
// belief can only say "this is already past what this lane usually does". A
// lane nothing is believed about gets no deadline at all, because a hedge fired
// on no evidence is a second bill for a guess.
func derivedDeadline(belief Belief) time.Duration {
	if !belief.TTFT.Known() {
		return 0
	}
	// 1.2816 is the standard normal's ninetieth percentile — the same figure the
	// sheet's prior fit is derived with. It is spelled out rather than named
	// because this is the only line in this file that needs it.
	p90 := belief.TTFT.Quantile(1.2816)
	if p90 <= 0 {
		return 0
	}
	deadline := time.Duration(p90 * float64(time.Millisecond))
	if deadline < deadlineFloor {
		return deadlineFloor
	}
	if deadline > deadlineCeiling {
		return deadlineCeiling
	}
	return deadline
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

// PathFault reports whether the verdict this watch gave was about the path
// rather than about the lane.
//
// It is the flag that keeps a belief honest. A connection that carried neither
// a heartbeat nor a byte says nothing about how fast the endpoint behind it
// writes — nothing ever reached it, or nothing ever came back — and charging
// the lane for it would demote a machine on the evidence of somebody's wifi.
func (w *Watch) PathFault() bool { return w.fault }

// SetExpectedTokens says roughly how long this answer is going to be.
//
// It is the sunk-cost half of the commitment rule: two hundred tokens in with
// twenty to go is a stream worth finishing however slowly it is going, and the
// only way to know that is to know how many were expected. Without it a
// committed stream is never abandoned, which is the safe direction — the cost
// of staying is one slow answer and the cost of leaving wrongly is the whole
// answer again.
func (w *Watch) SetExpectedTokens(n int) {
	if n > 0 {
		w.expected = n
	}
}

// Heartbeat records a sign of life that is not a token — the router's own
// comment line before the model has said anything. It is proof about the PATH
// and never about the endpoint.
func (w *Watch) Heartbeat(t time.Time) { w.beat = t }

// Token records that n tokens have now arrived, and says whether the stream
// should be given up on.
//
// The first token carries no gap — there is nothing before it to measure
// against — so it only stops the deadline clock. Every one after it is folded
// into the drift.
func (w *Watch) Token(n int, t time.Time) Verdict {
	previous := w.last
	w.tokens = n
	w.beat, w.last = t, t
	if previous.IsZero() {
		return Verdict{}
	}
	alarm, reason := w.alarm(t.Sub(previous), true)
	if !alarm {
		return Verdict{}
	}
	return w.verdict(reason, false)
}

// Silence records that nothing has arrived by t, and says whether that silence
// has gone on long enough to act on.
//
// It answers three different questions in one call because the stream loop has
// one timer to spend: is the path dead, is the first token late, and has the
// gap now open already gone on longer than this lane's drift can excuse. THE
// THIRD IS WHY SILENCE IS ASKED MID-STREAM AT ALL: a lane that stalls for
// twenty seconds delivers no token to notice it with, so a watch driven only by
// [Watch.Token] would find out about the stall when it ended.
func (w *Watch) Silence(t time.Time) Verdict {
	if w.hedged || w.alt == "" {
		return Verdict{}
	}
	if w.tokens == 0 {
		// THE DEAD PATH OUTRANKS THE LATE FIRST TOKEN, because it is a
		// different claim and the belief must not be charged for it. A router
		// that is working says so in comments long before the model does.
		if t.Sub(w.beat) >= w.deadPath() {
			w.fault = true
			return w.verdict("no heartbeat", true)
		}
		if w.deadline > 0 && t.Sub(w.began) > w.deadline {
			return w.verdict("first token late", true)
		}
		return Verdict{}
	}
	// A gap that is ALREADY this long is at least this long, so it is judged
	// without being folded in: the token that finally arrives folds the real
	// figure, once.
	alarm, reason := w.alarm(t.Sub(w.last), false)
	if !alarm {
		return Verdict{}
	}
	return w.verdict(reason, false)
}

// deadPath is how long a silence with no sign of life at all may run.
func (w *Watch) deadPath() time.Duration {
	dead := 2 * w.deadline
	if dead < deadPathFloor {
		return deadPathFloor
	}
	return dead
}

// alarm judges one gap against what this lane's believed rate says a gap should
// be, folding it into the drift when it is a gap that really happened.
//
// It returns the machine word for the log: a lump is one long delivery and a
// drift is a lane that has become slow, and the two are worth telling apart
// afterwards even though the answer to both is the same request.
func (w *Watch) alarm(gap time.Duration, fold bool) (bool, string) {
	if gap >= lumpGap {
		return true, "gap"
	}
	rate := w.belief.Rate.Mean()
	if rate <= 0 || gap <= 0 {
		// Nothing is believed about how fast this lane writes, so no gap is
		// surprising. Inventing an expectation here is the one thing the
		// ledger's emptiness law forbids.
		return false, ""
	}
	expected := 1 / rate
	drift := w.drift + math.Log(gap.Seconds()) - math.Log(expected) - driftSlack
	if drift < 0 {
		drift = 0
	}
	if fold {
		w.drift = drift
	}
	return drift >= driftAlarm, "drift"
}

// verdict is the one place a hedge is asked for, so that the commitment rule
// and the once-only rule are impossible to route around.
func (w *Watch) verdict(reason string, path bool) Verdict {
	if w.hedged || w.alt == "" || reason == "" {
		return Verdict{}
	}
	if !path && w.tokens >= commitTokens && !w.worthLeaving() {
		// Committed: the answer is already most of the way here and starting
		// again somewhere else would cost more than finishing slowly.
		return Verdict{}
	}
	w.hedged = true
	return Verdict{Hedge: true, Reason: reason}
}

// worthLeaving reports whether finishing on this lane really would take longer
// than redoing the whole answer on the alternative from cold.
//
// The comparison is deliberately asymmetric in the sunk cost's favour: what is
// left here is only the tokens still to come, while the alternative pays its
// first-token wait AND writes the answer from the beginning. That is what stops
// a router abandoning a nearly-finished reply to a lane that is merely faster.
func (w *Watch) worthLeaving() bool {
	rate := w.belief.Rate.Mean()
	if rate <= 0 || w.expected <= 0 {
		return false
	}
	remaining := float64(w.expected - w.tokens)
	if remaining <= 0 {
		return false
	}
	altRate := w.altRate
	if altRate <= 0 {
		altRate = rate
	}
	altTTFT := w.altTTFT
	if altTTFT <= 0 {
		altTTFT = w.belief.TTFT.Mean()
	}
	here := remaining / rate
	there := altTTFT/1000 + float64(w.expected)/altRate
	return here > there
}
