// Package control decides WHEN a wait has gone on long enough to act on, and
// it is the only thing in this build that decides that.
//
// ── ONE QUESTION, ASKED THE SAME WAY IN EVERY PHASE ─────────────────────────
//
// A request is silent for one of three reasons and until this package they were
// three separate rule sets: nothing has arrived at all, the endpoint is writing
// a run of thought nobody can read, or visible text was arriving and stopped.
// Three rule sets is three places to be wrong, and the measured defect was
// exactly that — a stall inside a run of thought was governed by none of them
// and waited two and a half minutes for a transport bound to notice.
//
// They are ONE question. Given a distribution over how long the next piece of
// VISIBLE PROGRESS takes, and given that we have already waited s without it,
// the expected remaining wait is
//
//	W(s) = E[T − s | T > s]
//
// and for a heavy-tailed T — which is what every measured lane is — W RISES
// with s. Somewhere it crosses what acting would cost: another arm's own wait,
// plus rewriting the visible text this one has already delivered, plus the
// money, converted through λ. That crossing is the moment to act, in every
// phase; only the distribution changes.
//
// ── WHAT THIS PACKAGE MAY NOT DO ────────────────────────────────────────────
//
// It holds no clock, opens no connection, reads no file and formats nothing for
// a person. Every method takes the moment as an argument, exactly as
// [internal/lane]'s chooser and watch already do, so a scenario written in
// minutes is tested in microseconds against a scripted belief. It imports
// nothing but the standard library — not even its parent — which is what keeps
// the arithmetic testable in isolation and the parent free to change.
//
// ── ROUTING AND WAITING ARE TWO QUESTIONS ───────────────────────────────────
//
// [internal/lane.Choice] answers WHICH LANE. This package answers WHEN TO ACT.
// They were one value once — a Choice with no alternative carried no deadline,
// so a cold ledger produced no routing opinion AND no clock, and a request with
// nothing to hedge to also had nothing watching it. A [Plan] is built for every
// token-generating call whether or not a Choice was made, and a plan with no
// alternatives still has a ceiling: the answer is then [Report] rather than
// silence.
package control

import (
	"math"
	"time"
)

// ── THE SHAPE OF A WAIT ─────────────────────────────────────────────────────

// Survival is a log-normal over one interval, in SECONDS.
//
// It is the only distribution this package knows, because it is the one the
// measured world has: seventeen lanes of one model published first-token
// percentiles whose p90 sits between 1.2× and 3.5× their p50, which is a
// log-normal with σ between 0.15 and 1.0 and nothing else.
//
// A zero Survival is NO BELIEF and reads as one everywhere: [Survival.Known] is
// false, and a controller given one falls back to its ceiling rather than to an
// invented number.
type Survival struct {
	// Mu is the mean of ln T with T in seconds, Sigma its standard deviation.
	// Sigma is the PREDICTIVE spread — how variable one draw is — and never the
	// spread of the estimate of the median, which shrinks to nothing after a few
	// dozen observations and would make every tail look impossible.
	Mu    float64
	Sigma float64
}

// Known reports whether anything is believed. A spread of zero is a claim of
// certainty about a single future draw, which nothing here may make.
func (s Survival) Known() bool { return s.Sigma > 0 }

// Mean is E[T] in seconds.
func (s Survival) Mean() float64 {
	if !s.Known() {
		return 0
	}
	return math.Exp(s.Mu + s.Sigma*s.Sigma/2)
}

// Quantile is the wait at z standard deviations, in seconds: Quantile(1.2816)
// is the p90.
func (s Survival) Quantile(z float64) float64 {
	if !s.Known() {
		return 0
	}
	return math.Exp(s.Mu + z*s.Sigma)
}

// Remaining is W(s) = E[T − s | T > s] in seconds: how much longer this wait is
// expected to go on GIVEN that it has already lasted s.
//
// IT IS THE WHOLE CONTROLLER IN ONE FUNCTION. For a log-normal it increases
// with s over the range any request lives in, which is the formal statement of
// the thing that feels wrong until it is written down: a stream four seconds
// late is not four seconds from finishing, it is a draw from the tail.
//
// Infinity is the honest answer far enough into the tail that the ratio below
// is two vanishing numbers divided by each other. Anything still running there
// should have been acted on long ago.
func (s Survival) Remaining(silence float64) float64 {
	if !s.Known() {
		return 0
	}
	if silence <= 0 {
		return s.Mean()
	}
	survival := 1 - phi((math.Log(silence)-s.Mu)/s.Sigma)
	if survival < 1e-12 {
		return math.Inf(1)
	}
	weighted := s.Mean() * phi((s.Mu+s.Sigma*s.Sigma-math.Log(silence))/s.Sigma)
	return weighted/survival - silence
}

// phi is the standard normal distribution function.
func phi(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }

// ── WHAT ONE REQUEST IS WATCHED WITH ────────────────────────────────────────

// Phase is which distribution governs the silence right now.
//
// The words are the machine's, not a person's: [internal/provider]'s phase
// clock owns what is said out loud, and a second vocabulary here would be a
// second vocabulary to keep in step.
type Phase uint8

const (
	// PhaseSilent is before anything at all has arrived. The distribution is
	// the serving lane's first-token belief.
	PhaseSilent Phase = iota
	// PhaseThinking is a run of reasoning: the endpoint IS writing and none of
	// it is on the screen. Two clocks run at once — how long this model's whole
	// thinking phase is expected to last, and how long a gap between two
	// thinking deltas may be — because a legitimately long think and a stalled
	// think look identical for the first few seconds and must not be answered
	// the same way.
	PhaseThinking
	// PhaseWriting is visible text arriving. The distribution is the gap
	// between two visible tokens, from the serving lane's believed rate.
	PhaseWriting
)

// Alternative is one lane acting could go to, with what it would cost.
type Alternative struct {
	// Lane is the machine, spelled as the wire spells it.
	Lane string
	// First is what it would take to say its first word FROM COLD, including
	// the second request's own handshake.
	First Survival
	// Rate is how fast it is believed to write, in tokens a second. Zero is
	// unknown, under which regenerating visible text is priced at the serving
	// lane's own rate rather than at a guess.
	Rate float64
	// Extra is what this second request is expected to add to the bill, in
	// dollars. It is what λ converts into seconds.
	Extra float64
}

// Purse is the spend rail the controller asks before it acts.
//
// It is an interface rather than a figure because the answer depends on what
// has been spent in the last hour, which is not this package's business to
// know. A purse that refuses is FINAL for that moment: the controller records
// the refusal and does not poll it.
type Purse interface {
	Allows(usd float64, now time.Time) bool
}

// Plan is everything one controller is built with. It is built for EVERY
// token-generating call, including the calls no lane preference was made for.
type Plan struct {
	// Lane is the machine expected to serve, empty when nobody was named. It is
	// re-pointed by [Controller.Serving] the moment the stream says who is
	// really answering.
	Lane string
	// Ceiling is the hard bound on time-to-action for this call: past it the
	// controller acts whatever it believes, because a belief that says "keep
	// waiting" past a person's patience is a belief answering the wrong
	// question. It is the role's, and every role has one.
	Ceiling time.Duration
	// Floor is the shortest silence that may be acted on. Below it a second
	// request is racing the network rather than the lane.
	Floor time.Duration
	// Lambda is what a second of this call's wait is worth, in SECONDS PER
	// DOLLAR. Zero is "nobody is waiting", under which no amount of money buys
	// speed and the controller only ever reports.
	Lambda float64
	// Margin is the hysteresis: how much better acting has to look before it is
	// done, in seconds. It is what stops a controller flapping at the crossing.
	Margin float64
	// First, Gap and Think are the three distributions of the three phases:
	// the serving lane's first token, the gap between two visible tokens, and
	// how long a whole thinking phase lasts on this MODEL. The third is a model
	// property and not a lane's — a lane cannot make a model think less — which
	// is why it is carried separately.
	First Survival
	Gap   Survival
	Think Survival
	// Alts are the lanes acting could go to, best first, already gated. An
	// empty list is a real state and the reason [Report] exists.
	Alts []Alternative
	// Pinned says a person named this lane themselves. It changes the ACT and
	// never the arithmetic: where an unpinned call hedges, a pinned one asks.
	Pinned bool
	// Purse is the spend rail, nil when nothing bounds it.
	Purse Purse
	// Expected is roughly how long this answer will be in visible tokens, zero
	// when nobody knows. It is what makes the commitment half of the same
	// inequality computable.
	Expected int
	// Began is when the request went out.
	Began time.Time
}

// ── WHAT THE CONTROLLER SAYS ────────────────────────────────────────────────

// Kind is what to do about a wait.
type Kind uint8

const (
	// None is "keep waiting", and it is the answer to almost every question.
	None Kind = iota
	// Hedge is another arm, on [Act.Lane]. It is the cheap answer and the one
	// the ladder reaches for first.
	Hedge
	// Ask is a pinned lane's hedge: the offer a person answers. It is raised
	// instead of acting, and it is withdrawn by the first visible token.
	Ask
	// Report is "there is nothing to hedge to and the wait is real". It is not
	// silence: it is the HUD saying so, which is the only honest thing left
	// when every reachable lane is believed slow.
	Report
	// Escalate hands the wait to the model ladder — the LAST rung, and not this
	// package's own: [internal/provider]'s endpoints.go owns it and there is
	// exactly one of it. The controller only ever says that its own rungs are
	// spent.
	Escalate
	// Commit says this arm has earned the answer: whatever else is in flight
	// costs more than finishing here. It is the same inequality read the other
	// way, which is why there is no separate commitment constant.
	Commit
)

// Act is one verdict, with the numbers it was made on.
//
// The numbers ride along so that the call log can say why: a row that records
// the action without the wait and the cost that justified it is a row nobody
// can autopsy. Reason is a short machine word and is never shown to a person.
type Act struct {
	Kind    Kind
	Lane    string
	Reason  string
	Silence time.Duration
	// Wait is E[remaining] in seconds at the moment of the act, and Cost what
	// acting was expected to cost in the same unit.
	Wait float64
	Cost float64
}

// CeilingReason is the machine word [Act.Reason] carries when the bound is what
// raised the act rather than the arithmetic under it.
//
// IT IS SPELLED ONCE AND EXPORTED because two layers read it — the call log,
// which says why a request acted, and the tests of the wire, which script a
// controller of their own — and a second spelling would be a second answer to
// "why did it act" on the one row somebody autopsies.
const CeilingReason = "ceiling"

// RateReason is the machine word [Act.Reason] carries when a collapsed visible
// rate stopped earning progress and the ceiling raised the act.
//
// IT IS SPELLED ONCE AND EXPORTED because the call log and tests that script a
// controller both read it, and a second spelling would be a second answer to
// "why did it act" on the one row somebody autopsies.
const RateReason = "rate collapsed"

// Reading is one moment of a stream's life as the read loop sees it.
//
// THE THREE COUNTS ARE NOT INTERCHANGEABLE. Visible is text on the screen and
// is the only thing that can reset the deadline, while its measured rate keeps
// up. Hidden is the endpoint writing where nobody can read — a run of thought,
// a tool call being assembled — and it keeps the stream alive and moves the
// phase without counting as progress. Beat is the router's own comment line:
// proof about the PATH and about nothing else, so it never resets the silence
// clock.
type Reading struct {
	At      time.Time
	Visible int
	Hidden  int
	Beat    bool
}

// Controller is one request's waiting policy.
//
// It is PURE and it is driven by whoever owns the stream: one read loop and one
// beat, serialized by that owner exactly as today's watch is.
type Controller interface {
	// Note folds in one moment of the stream and says what to do about it.
	Note(Reading) Act
	// Quiet says nothing has arrived by now, and asks the same question.
	Quiet(now time.Time) Act
	// Serving re-points the controller at the machine that is really answering,
	// with what is believed about it. A router honours any of an order and says
	// which way it went on every chunk.
	Serving(lane string, first, gap Survival, now time.Time)
	// Deadline is the next moment worth waking for, so the beat arms a timer
	// rather than polls. It moves as the belief and the phase move, and a zero
	// moment means there is nothing scheduled.
	Deadline() time.Time
	// Phase is which distribution is governing right now.
	Phase() Phase
	// Acted reports whether this controller has already fired an act of this
	// kind, which is how a caller tells a rescue that was refused from one that
	// was never asked for.
	Acted(Kind) bool
}

// Factory builds one. It is a function rather than a constructor so that a
// bench, a simulator and a test can install their own without this package
// growing a registry of its own.
type Factory func(Plan) Controller
