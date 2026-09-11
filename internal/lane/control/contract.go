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
	"strings"
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
// IT ANSWERS IN [Seconds] AND NEVER IN A FLOAT, because two of its three
// answers are not numbers. A belief nobody measured says NOTHING — it used to
// say zero, which is a claim that the wait is over — and a wait far enough into
// the tail that the ratio below is two vanishing numbers divided by each other
// is PAST PRICING: it used to say +Inf, and that infinity travelled through the
// act into the model-call log, where it cost the row its number and bought a
// sentence about a float instead (internal/calllog's finite.go).
func (s Survival) Remaining(silence float64) Seconds {
	if !s.Known() {
		return Seconds{}
	}
	if silence <= 0 {
		return Measured(s.Mean())
	}
	survival := 1 - phi((math.Log(silence)-s.Mu)/s.Sigma)
	if survival < 1e-12 {
		return PastPricing()
	}
	weighted := s.Mean() * phi((s.Mu+s.Sigma*s.Sigma-math.Log(silence))/s.Sigma)
	return Measured(weighted/survival - silence)
}

// ── A FIGURE THE LADDER MAY REASON WITH, OR NOTHING AT ALL ──────────────────
//
// THE ARITHMETIC HAS THREE ANSWERS AND A FLOAT CAN ONLY SPELL ONE. Two of the
// three used to be spelled as infinity — W(s) far out in the tail, and what
// acting costs when there is nowhere to act TO — and an infinity is a number,
// so everything downstream reasoned with it as one. It crossed `wait > cost +
// margin` by construction, it reached [Act], and it reached the model-call log,
// where the one thing JSON cannot spell took the row's own figure off it and
// left a sentence about a float in its place. A row that says nothing is the
// emptiness law working; a row that says "cost_s was +Inf" is a program
// explaining its own arithmetic to somebody who asked what happened.
//
// So the three answers are three states, and the only comparison the controller
// ever makes is [Seconds.Over], which has to be given both of them before it
// can say yes.
type reach uint8

const (
	// nothing is no belief and no alternative: the question cannot be priced
	// because the material to price it with was never measured.
	nothing reach = iota
	// figure is a number of seconds.
	figure
	// pastPricing is a wait so far into the tail that the belief holding it can
	// no longer put a number on it. It is not unknown — it is known to be worse
	// than anything a finite cost could be — and it is the one state that makes
	// [Seconds.Over] true without arithmetic.
	pastPricing
)

// Seconds is a figure in seconds that may be a number, may be unknown, and may
// be past what its belief can price. The zero value is UNKNOWN, so a field
// nobody filled in decides nothing.
type Seconds struct {
	seconds float64
	reach   reach
}

// Measured is a real number of seconds.
//
// A FIGURE THAT IS NOT A NUMBER IS PAST PRICING AND NEVER A FIGURE. The three
// states above exist because two of the arithmetic's answers are not numbers,
// and an overflow is the third way to reach the same place: an exponential that
// ran off the end of a float is a cost nobody could state, which is exactly
// what [PastPricing] means. Answering it here rather than letting the value
// through is what keeps the one thing JSON cannot spell out of the model-call
// log by construction rather than by the rescue in internal/calllog's finite.go
// — which is the last line of defence and not the road.
func Measured(seconds float64) Seconds {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return PastPricing()
	}
	return Seconds{seconds: seconds, reach: figure}
}

// PastPricing is a wait past anything its belief can put a number on.
func PastPricing() Seconds { return Seconds{reach: pastPricing} }

// Get is the number and whether there is one. Only a measured figure has one:
// an unknown has nothing to give, and one past pricing has no number to give
// even though it is the largest answer there is.
func (s Seconds) Get() (float64, bool) { return s.seconds, s.reach == figure }

// Known reports whether this figure says anything at all — a number, or that
// the wait is past pricing. It is what the commitment half of the inequality
// asks: an arm may only be committed to against a cost somebody could state.
func (s Seconds) Known() bool { return s.reach != nothing }

// Plus adds seconds to a figure. Adding to nothing is still nothing, and adding
// to a wait past pricing leaves it past pricing: neither is a number, and a sum
// with a number in it does not make one.
func (s Seconds) Plus(extra float64) Seconds {
	if s.reach != figure {
		return s
	}
	return Measured(s.seconds + extra)
}

// Over is the payoff test — W(s) > A + m — and it is the ONLY comparison this
// package makes between two of these.
//
// IT NEEDS BOTH SIDES BEFORE IT CAN SAY YES, which is the whole reason the type
// exists. A cost nobody can state is not a cheap alternative: it is no
// alternative, and acting toward it cannot pay. A wait nobody has measured is
// not a short wait; it simply is not evidence. The one asymmetric answer is a
// wait past pricing against a cost that is a number, which is yes by
// construction — that is what past pricing means.
func (s Seconds) Over(cost Seconds, margin float64) bool {
	if s.reach == nothing || cost.reach != figure {
		return false
	}
	if s.reach == pastPricing {
		return true
	}
	return s.seconds > cost.seconds+margin
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
//
// ── IT IS ALSO THE CALL'S ONE BUDGET ────────────────────────────────────────
//
// It was the hazard's alone until 2026-09-10. The hazard reads it to decide
// WHEN a wait has gone on long enough; the dispatcher (internal/provider's
// dispatch.go) reads the same object to decide WHAT to do when a call has
// failed, and writes its moves back into [Plan.Moves]. One plan, one deadline,
// one list of what has been tried — which is the whole of
// docs/design/recovery/DESIGN.md §3 clause 1, and the reason the eleven budgets
// of its §2 could be deleted rather than tuned.
type Plan struct {
	// Lane is the machine expected to serve, empty when nobody was named. It is
	// re-pointed by [Controller.Serving] the moment the stream says who is
	// really answering.
	Lane string
	// Model is what is being asked, and every move of one plan carries it. The
	// dispatcher NEVER changes it: there is exactly one model hop in this build
	// and it belongs to the session (docs/design/recovery/DESIGN.md §4).
	Model string
	// Deadline is when this call stops trying, in the person's own time, and it
	// is the ONLY bound on how long a failed call may go on recovering.
	//
	// IT REPLACED A PRODUCT NOBODY COULD STATE. Attempts times paced sends
	// times arms times rungs times models, each with its own wall clock over
	// it, is not a number anybody could have told a person before this field
	// existed; `lane.Role.GiveUp` is, and it is measured (lane/roles.go's
	// [TurnGiveUp]). A zero moment is NO deadline, which is the honest reading
	// for a plan somebody built by hand and the reason [Plan.Spent] asks.
	Deadline time.Time
	// SpendUSD is what this whole call may cost, zero being unbounded. It is
	// the second half of one budget: the purse below bounds one ACT, and this
	// bounds the call that keeps taking them.
	SpendUSD float64
	// Moves is what this question has already tried, shared by every arm of it.
	// A nil log is empty and decides nothing.
	Moves *MoveLog
	// Comeback is what the machine that last refused asked us to wait, zero
	// when it asked for nothing. It is the one input [Next] needs that changes
	// between moves, and it is what makes the single legal repeat legal.
	Comeback time.Duration
	// Shapes are the relaxation rungs this particular request has, in the order
	// they are climbed, spelled as the person reads them ("removed reasoning").
	// A request carrying no tools, no cap and no knob has none, and then the
	// ladder is not a move this call has.
	Shapes []string
	// ShapeRefused says the last refusal was about the request's SHAPE and not
	// about the machine that answered it: "no endpoints found that can handle the
	// requested parameters" is the router saying that every machine it can see
	// has already answered for this body, so another machine is not a move.
	//
	// IT IS THE ONE FACT [Next] CANNOT WORK OUT FOR ITSELF. The serving set of
	// such a request is usually OPEN — nobody named a pool — and an open set has
	// another machine in it forever (see [Plan.serving]), so a ladder that asked
	// for its rung without this would be handed a machine every time and would
	// never climb. It is set by the one caller that holds such a refusal's own
	// body (internal/provider's dispatch.go, [Client.recoverFromRefusal]).
	ShapeRefused bool
	// AccountRefused says the last refusal was about the ACCOUNT and not about
	// the machine that relayed it: a router with a pool behind this model asked
	// the whole key to slow down and named no pool while doing it. Every machine
	// it could have picked is behind the same ceiling.
	//
	// IT IS [Plan.ShapeRefused]'S SIBLING AND THE SAME KIND OF FACT: something
	// [Next] cannot see for itself, because the serving set of such a request is
	// usually OPEN — nobody named a pool — and an open set has another machine in
	// it forever ([Plan.serving]). That rule is sound only because each body
	// carries a longer exclusion list than the last, and an account ceiling is
	// exactly the refusal that gives the list nothing to grow by. Without this
	// field the generator answered it with a machine move every time and the
	// dispatcher sent the identical bytes again behind a doubling wait, for as
	// long as the deadline lasted — ninety seconds of `waiting` on 2026-09-11
	// with nothing whatever changing between the sends.
	//
	// A BASE WITH NO POOL BEHIND IT IS NOT THIS. An endpoint that paces us and
	// has one machine is saying "come back later" and repeating really is all
	// there is; what makes a pace an ACCOUNT'S is that a set exists and the
	// refusal named none of it. The one caller that holds the refusal decides
	// both halves (internal/provider's dispatch.go).
	AccountRefused bool
	// Role is who the call is being made for, spelled as `lane.Role` spells it.
	// It is carried rather than looked up so the dispatcher, the hazard and the
	// row all read the same word.
	Role string
	// Ceiling is the hard bound on time-to-action for this call: past it the
	// controller acts whatever it believes, because a belief that says "keep
	// waiting" past a person's patience is a belief answering the wrong
	// question. It is the role's, and every role has one.
	//
	// IT BOUNDS A STILL WIRE. An endpoint that is writing — visibly, or a run
	// of reasoning nobody can read — is not a silence this figure is about, and
	// the one exception is a visible rate that has collapsed, which is a person
	// waiting whatever the wire is doing. See [hazard.stillSince].
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

// Spent reports whether this call has run out of the one thing that bounds it.
//
// IT IS THE ONLY PLACE A CLOCK ENDS A CALL. Everything the eleven controllers
// used to end a call with — three faults, six paced sends, sixty patient ones,
// two minutes of watched pacing, ten of patient pacing, four arms, seven rungs
// — is gone, and this is what is left. A plan with no deadline is never spent,
// which is the honest reading for one somebody built by hand.
func (p Plan) Spent(now time.Time) bool {
	return !p.Deadline.IsZero() && !now.Before(p.Deadline)
}

// Left is how much of the deadline remains, zero once it is spent and zero when
// there is no deadline at all. It is what a countdown is drawn from.
func (p Plan) Left(now time.Time) time.Duration {
	if p.Deadline.IsZero() || !now.Before(p.Deadline) {
		return 0
	}
	return p.Deadline.Sub(now)
}

// serving is every machine this request may go to, best belief first: the head
// of the choice, then the alternatives the frontier named.
//
// AN EMPTY SET IS AN OPEN SET AND NEVER A SET OF ONE. "This request may go
// anywhere the router likes" is a real and common state — no preference was
// carried, the chooser held fewer than two beliefs, the base is not a router —
// and reading it as one machine wide would be this build inventing a pool it
// cannot see. [Next] answers an open set with a machine move every time, and
// what bounds the walk is the deadline, because nothing else honestly can.
func (p Plan) serving() []string {
	set := make([]string, 0, len(p.Alts)+1)
	seen := map[string]bool{}
	add := func(lane string) {
		lane = strings.TrimSpace(lane)
		if lane == "" {
			return
		}
		key := strings.ToLower(lane)
		if seen[key] {
			return
		}
		seen[key] = true
		set = append(set, lane)
	}
	add(p.Lane)
	for _, alt := range p.Alts {
		add(alt.Lane)
	}
	return set
}

// head is the machine a shape move is expected to go back to: the one the
// choice named, or nobody.
func (p Plan) head() string { return strings.TrimSpace(p.Lane) }

// Serving is [Plan.serving] for a caller that has to SAY how many machines
// there are — the ordinal a person reads, `2 of 5`, whose denominator was a
// constant nobody could justify until this field existed.
func (p Plan) Serving() []string { return p.serving() }

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
	// Wait is E[remaining] at the moment of the act, and Cost what acting was
	// expected to cost. Both are [Seconds] rather than floats because either
	// can honestly be nothing — an unmeasured belief, a request with nowhere to
	// act to — and a row that carries no number is the emptiness law rather
	// than a gap.
	Wait Seconds
	Cost Seconds
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
// THE THREE COUNTS ARE NOT INTERCHANGEABLE. Visible is the answer arriving —
// text on the screen, or the arguments of a tool call being assembled, which
// are the answer as surely as text is and are what a rescue would have to
// write again — and it is the only thing that can reset the deadline, while its
// measured rate keeps up. Hidden is the endpoint writing where nobody can read —
// a run of thought — and it keeps the stream alive and moves the phase without
// counting as progress. Beat is the router's own comment line: proof about the
// PATH and about nothing else, so it never resets the silence clock.
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
