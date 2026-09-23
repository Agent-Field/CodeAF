package taxonomy

import (
	"math"
	"sort"
	"sync"
	"time"
)

// Policy is what ONE class of failure earns. There is exactly one per [Class]
// and it is a value in a registry, never a branch in a caller.
//
// THE REGISTRY IS THE POINT. A switch over the class at each site is three
// answers that start the same and drift the first time somebody fixes one of
// them; a policy object means the answer for a class is written once, in one
// place, and every site that classifies gets that answer whether or not its
// author had heard of it.
type Policy interface {
	// Class is the one class this policy answers for.
	Class() Class
	// Decide is the policy's answer. The limits it is handed have already been
	// floored ([Limits.floor]), so an implementation never has to defend itself
	// against a zero.
	Decide(Evidence, Limits) Verdict
}

var (
	registryMu sync.RWMutex
	policies   = map[Class]Policy{}
)

// Register puts a policy in the registry, replacing whatever answered for that
// class before. It is called from this package's own init for the three classes
// that exist, and it is exported so a test can pin a policy without reaching
// into the map.
func Register(p Policy) {
	if p == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	policies[p.Class()] = p
}

// PolicyFor is the registry read [Classify] makes.
func PolicyFor(class Class) (Policy, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	policy, ok := policies[class]
	return policy, ok
}

// Registered is every class that has a policy, in a stable order. A structural
// test reads it to hold this package to "exactly one policy per class".
func Registered() []Class {
	registryMu.RLock()
	defer registryMu.RUnlock()
	classes := make([]Class, 0, len(policies))
	for class := range policies {
		classes = append(classes, class)
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i] < classes[j] })
	return classes
}

func init() {
	Register(transportPolicy{})
	Register(capabilityPolicy{})
	Register(workPolicy{})
	Register(shapePolicy{})
}

// ── transport ───────────────────────────────────────────────────────────────

// transportPolicy retries on the same tier and gets out of the way.
//
// THREE THINGS IT NEVER DOES, and each of them was a measured failure:
//
//   - It never ends a turn. [Verdict.EndsTurn] is false for every verdict this
//     returns, including the one that gives up: a request that ran out of
//     attempts is a request that is over, and a turn is not.
//   - It never counts toward a lift. Nothing about who served a request is
//     evidence about who was asked.
//   - It never moves the tier. The endpoint is the suspect, so the answer is a
//     different endpoint — which is what [Verdict.Rotate] asks the routing
//     underneath for, and which the routing already knows how to do.
type transportPolicy struct{}

func (transportPolicy) Class() Class { return Transport }

// Decide walks ONE budget with THREE endings, and which ending it is depends on
// nothing but how much of the budget is gone and whether the caller has anywhere
// else to ask.
//
// AND THE SPENT BUDGET IS NOT THE END OF THE STORY, which is the change. It used
// to be: a model whose ladder ran out ended the request, and a fallback chain the
// person had configured was never asked unless the failure happened to be a cut
// stream — so a refusal storm on one model killed a turn while another model in
// the same session answered every call put to it. A budget is spent on a MODEL,
// and a model is not the last thing there is.
func (transportPolicy) Decide(e Evidence, l Limits) Verdict {
	spent, allowed := transportBudget(e, l)
	// A TERMINAL TRANSPORT HAS ALREADY TAKEN ITS ONE RECOVERY. The report keeps
	// it in transport accounting while preventing an endpoint retry or model hop
	// from repeating a credential failure only the person can repair.
	if e.TerminalTransport {
		return Verdict{Action: ActionReport, Reason: ReasonUnauthorized, Attempts: spent}
	}
	// A PAUSED PLAN HAS NO AUTOMATIC MOVE. The dispatcher either uses the
	// separately authorised metered door itself or returns the typed pause; an
	// endpoint walk or model hop here would evade that billing decision.
	if e.PlanPaused {
		return Verdict{Action: ActionReport, Reason: ReasonPlanPaused, Attempts: spent}
	}
	// A MODEL THE ROUTER NO LONGER CARRIES HAS NO BUDGET TO SPEND. Every attempt
	// buys the identical 404 — there is no machine to rotate to, because the id
	// itself is gone — so the budget is declared spent at once and the move is
	// the only one there is: another model, or an ending that says so (#838).
	if e.Withdrawn {
		if e.FallbackAvailable {
			return Verdict{Action: ActionHop, Reason: ReasonWithdrawn, Attempts: spent}
		}
		return Verdict{Action: ActionGiveUp, Reason: ReasonWithdrawn, Attempts: spent}
	}
	// AND AN ACCOUNT THAT COULD NOT BE SERVED HAS NO MOVE AT ALL. Every machine,
	// every shape and every model answer it identically, so there is nothing to
	// buy with a retry and nothing to hop to; what is owed is the sentence. It
	// stays TRANSPORT because it is still not evidence about the model or about
	// the work, and [Verdict.EndsTurn] reads the ACTION for exactly this case.
	if e.Unserved {
		return Verdict{Action: ActionReport, Reason: ReasonUnauthorized, Attempts: spent}
	}
	// AND A SPENT SHAPE LADDER HAS NO ATTEMPTS LEFT TO SPEND EITHER. The transport
	// has already sent this question in every shape it has; asking again buys the
	// identical diagnosis, and the only move left is the caller's own chain.
	if e.Spent {
		if e.FallbackAvailable {
			return Verdict{Action: ActionHop, Reason: ReasonUnservable, Attempts: spent}
		}
		return Verdict{Action: ActionGiveUp, Reason: ReasonUnservable, Attempts: spent}
	}
	// AND A PERSON WAITING ON THE ONLY MACHINE THERE IS WAITS FOR AS LONG AS
	// THEY LIKE. This rung is above the deadline on purpose, which nothing else
	// here is.
	//
	// Every bound below exists because SOMETHING ELSE could be done with the
	// time: another endpoint, another model, an ending that frees the person to
	// go and fix it. A watched conversation against one machine with no chain
	// has none of those. Giving up returns the person to a prompt from which
	// the only sensible move is to ask the same question again, so the ending is
	// not a move, it is the harness doing the retrying for them badly. What it
	// costs to keep asking is nothing — the machine is theirs, the tokens are
	// free, and the loop says on the screen what it is doing and stops the
	// moment they say so.
	//
	// The deadline still ends every other shape, and it still ends this one
	// where nobody is watching ([Evidence.Watched]).
	if waitsForEver(e) {
		return Verdict{
			Action:    ActionRetry,
			Reason:    transportReason(e),
			Attempts:  0,
			Backoff:   waitFor(e, spent, l.TransportBackoff),
			Unbounded: true,
		}
	}
	// AND A CALLER WITH NO TIME LEFT HAS NOTHING TO SPEND EITHER. It is the
	// count's replacement: the plan's deadline is what bounds a failing request
	// now, and a caller whose deadline is gone says so
	// ([Evidence.OutOfTime]) rather than arriving with a number that has run
	// out. The REASON is still the failure's own — nothing about running out of
	// time tells anybody what went wrong — which is the whole of why this is not
	// [Evidence.Spent].
	if e.OutOfTime {
		if e.FallbackAvailable {
			return Verdict{Action: ActionHop, Reason: transportReason(e), Attempts: spent, Rotate: true}
		}
		return Verdict{Action: ActionGiveUp, Reason: transportReason(e), Attempts: spent}
	}
	// AND AN ORDINARY FAILURE ASKS AGAIN FOR AS LONG AS THE CALLER HAS. `allowed`
	// is zero for one — the deadline is what bounds it, and it arrives above this
	// line — so a count is compared only where there really is one, which is a
	// reply that came apart ([transportBudget]).
	if allowed == 0 || spent < allowed {
		return Verdict{
			Action:   ActionRetry,
			Reason:   transportReason(e),
			Attempts: allowed,
			Backoff:  waitFor(e, spent, l.TransportBackoff),
			Rotate:   true,
		}
	}
	if e.FallbackAvailable {
		return Verdict{
			Action:   ActionHop,
			Reason:   transportReason(e),
			Attempts: allowed,
			// The next model is asked by somebody else's routing, exactly as a
			// retry is, so the ask still wants to land wherever that routing
			// would put it rather than on the lane this one died on.
			Rotate: true,
		}
	}
	return Verdict{
		Action:   ActionGiveUp,
		Reason:   transportReason(e),
		Attempts: allowed,
	}
}

// waitsForEver is the one shape with no bound at all: a cut stream, against a
// machine there is no alternative to, with no model chain behind it, in front
// of a person who can see the waiting and end it.
//
// ALL FOUR ARE LOAD-BEARING. A cut rather than a refusal, because a refusal is
// an endpoint saying no and a cut is one saying nothing yet. One machine,
// because with a pool the next attempt is somewhere else and has its own short
// allowance. No chain, because a person who configured a next model asked for
// the hop and should get it. And watched, because the same loop with nobody in
// front of it is a hang.
func waitsForEver(e Evidence) bool {
	return e.Cut && e.OneMachine && e.Watched && !e.FallbackAvailable
}

// transportBudget is how much of this model's budget is gone and how much it
// had, and the two kinds of spending are the whole of it.
//
// A REQUEST THAT FAILED SPENDS NOTHING HERE. A refusal, a reset or a deadline is
// evidence that the endpoint is failing and TIME is the thing that mends it, so
// what bounds asking again is the caller's own deadline and not a count kept in
// this file (the paragraph at the foot of this comment has the measurement).
// What is left of the old pair is the WAIT that goes in front of each rung,
// which is [Limits.TransportBackoff] and is still the person's to lengthen.
//
// A STREAM THE GUARD CUT spends a shorter allowance, because it is not that
// evidence: the request was served, at once, and the REPLY came apart. Nothing
// here is worth waiting out, and the same four rungs that make sense for a socket
// would keep a model that has lost the thread going four times over a context
// that is only getting worse. Its three allowances are the three things that can
// be true about a cut:
//
//   - DEGENERATION IS WORTH ASKING ONCE MORE. If the same transcript comes back
//     as soup twice, the transcript is the suspect and the person is told rather
//     than charged for a third.
//   - A CUT THAT REROUTED NOTHING IS WORTH ASKING ONCE MORE, and for a harder
//     reason: nothing moved. Routing is off, or the stream died before any chunk
//     named who served it, so the next ask lands in the same place by the same
//     rules and a second one buys nothing at all.
//   - SILENCE THAT DID REROUTE IS WORTH ASKING TWICE MORE. A quiet endpoint is
//     very often a bad draw out of a pool, and the asks after it are genuinely
//     served by somebody else.
//
// The allowance is stated as a TOTAL — attempts, not retries — so that a caller
// comparing two of them is comparing like with like.
//
// ── AND AN ORDINARY FAILURE HAS NO ALLOWANCE HERE AT ALL ────────────────────
//
// It had one until 2026-09-11: `Limits.TransportAttempts`, a count of sends,
// walked by a caller whose transport was ALREADY bounded by the plan's deadline
// (docs/design/recovery/DESIGN.md §4). Two budgets on one axis multiply, and the
// person could be told neither. So the deadline is the whole bound and it
// reaches this policy the way every other fact does — as evidence: a caller
// whose plan is spent says so ([Evidence.Spent]), which is answered above this
// line as the hop or the ending it is. `allowed` of zero means UNBOUNDED HERE
// and is never compared against; a cut keeps its own allowances, because those
// count a SHAPE OF REPLY that came apart rather than a length of patience.
func transportBudget(e Evidence, l Limits) (spent, allowed int) {
	spent = e.Attempt
	if e.Cut {
		spent = e.Cuts
	}
	if spent < 1 {
		spent = 1
	}
	if !e.Cut {
		return spent, 0
	}
	switch {
	case e.Degenerate:
		return spent, DegenerateCutAttempts
	case e.OneMachine:
		return spent, OneMachineCutAttempts
	case !e.Rerouted:
		return spent, BlindCutAttempts
	}
	return spent, SilentCutAttempts
}

// The cut allowances. They are constants rather than [Limits] rows because the
// number a person turns is "how patient is this harness with the wire", and each
// of these is a fact about a SHAPE of failure that patience does not mend — see
// [transportBudget] for why each is the number it is.
// They are EXPORTED because a caller's own tests have to state the same numbers
// this policy walks, and a test holding a copy of a budget is a test that goes on
// passing after the budget moves.
const (
	SilentCutAttempts     = 3
	DegenerateCutAttempts = 2
	BlindCutAttempts      = 2
	OneMachineCutAttempts = 4
)

// OneMachineCutCeiling is the longest a wait between two asks of the same
// machine ever grows to. It is short because the thing being waited for is
// local and free to ask, and the cost of asking too often is nothing while the
// cost of asking too rarely is the person's own time ([waitFor]).
const OneMachineCutCeiling = 10 * time.Second

// waitFor is how long to wait before asking again, and it is TWO answers
// because there are two kinds of transport failure.
//
// A REFUSAL, A RESET OR A DEADLINE is an endpoint under strain, and the pause is
// the whole point of asking again: 2s, 4s, 8s, doubling off the one knob, which
// is the schedule the turn loop already walked.
//
// AN EMPTY 200 OR A MANGLED TOOL CALL IS NOT STRAIN. The endpoint answered, at
// once, with something that was not an answer — it is up, it is fast, and it is
// broken in a way that eight seconds of waiting does not mend. What mends it is
// being served by somebody else, which is [Verdict.Rotate] and costs no time at
// all. Waiting here would spend fourteen seconds of a person's turn to arrive at
// exactly the same request.
func waitFor(e Evidence, attempt int, base time.Duration) time.Duration {
	if e.Empty || e.Malformed {
		return 0
	}
	// AND NEITHER IS A CUT STREAM, WHEN THERE IS SOMEWHERE ELSE TO SEND IT. The
	// request was served and the reply came apart; what mends that is a
	// different endpoint, which costs no time at all.
	//
	// ONE MACHINE IS THE EXCEPTION AND IT IS THE WHOLE POINT OF THE FIELD. A
	// person on their own base url has no other endpoint, so the move that
	// makes waiting pointless does not exist for them: the identical request
	// goes back to the identical server, and sending it again the same instant
	// asks a machine that has just answered nothing to answer now. Time is the
	// only mend left, so the cut takes the same doubling schedule a refusal
	// does. Two immediate asks ten seconds apart, which is what this returned
	// before, is not patience.
	if e.Cut && !e.OneMachine {
		return 0
	}
	if base <= 0 || attempt < 1 {
		return 0
	}
	// THE DOUBLING SATURATES RATHER THAN WRAPS. `base << (attempt-1)` is a
	// signed shift, and past about thirty-five asks of a one-second base it
	// runs off the top of an int64 into zero or a negative duration, which a
	// caller reads as "no wait" — so the unbounded wait on one machine went
	// back to asking as fast as the machine could fail after its thirty-fifth
	// ask (#1358's hot loop, one level down).
	wait := time.Duration(math.MaxInt64)
	if shift := attempt - 1; shift < 63 && base <= time.Duration(math.MaxInt64)>>shift {
		wait = base << shift
	}
	// AND A WAIT ON ONE MACHINE CLIMBS TO A CEILING AND STAYS THERE, which is
	// the difference between polling and doubling away.
	//
	// The doubling is a manner towards a SHARED service: every attempt costs
	// somebody money, adds load to something under strain, and the polite thing
	// is to back away. None of that is true of a machine that belongs to the
	// person waiting on it. What they are waiting for — weights finishing their
	// load, a single slot coming free — finishes at a moment nobody can predict
	// and everybody wants noticed AT ONCE, and a schedule that has reached four
	// minutes between asks turns a server that came back in ninety seconds into
	// four more minutes of a person watching a spinner. So it doubles while
	// doubling is cheap and then holds, and the asks go on arriving.
	if e.Cut && e.OneMachine && wait > OneMachineCutCeiling {
		return OneMachineCutCeiling
	}
	return wait
}

// transportReason is the phrase the journal carries. It names the SHAPE and not
// the payload: an autopsy grouping a thousand lines wants "the reply arrived
// empty" four hundred times, not four hundred distinct sentences.
func transportReason(e Evidence) string {
	switch {
	case e.Withdrawn:
		return ReasonWithdrawn
	case e.TerminalTransport:
		return ReasonUnauthorized
	case e.Unserved:
		return ReasonUnauthorized
	case e.Spent:
		return ReasonUnservable
	case e.Empty:
		return ReasonEmpty
	case e.Malformed:
		return ReasonMalformed
	case e.Timeout:
		return ReasonTimeout
	case e.Idle:
		return ReasonIdle
	case e.Degenerate:
		return ReasonDegenerate
	case e.Cut:
		return ReasonCut
	case e.Wire:
		return ReasonWire
	case e.Named():
		return ReasonRefused
	// AND AN UNNAMED PACE IS ITS OWN SHAPE. It used to fall through to
	// [ReasonUnserved] — "the provider could not serve it" — which is the wrong
	// claim twice over: the provider CAN serve it, and what a person reading the
	// row needs to know is that nothing about this is theirs to act on and
	// nothing about it is the model's fault. Its move is the wait it named and
	// then another model, and no other shape has that pair.
	case e.Status == 429:
		return ReasonPaced
	case e.Status > 0:
		return ReasonUnserved
	}
	return ReasonUnreached
}

// The shapes a transport failure comes in, named.
//
// They are EXPORTED because a caller has to say the same thing to a PERSON that
// this says to the journal, and it cannot spell that from the phrase itself: the
// journal wants one grouping word per shape and a person wants a sentence with
// no machinery in it ("the endpoint refused" is the right journal line and the
// wrong thing to put on somebody's screen). A caller switching over these
// constants writes its own words for a shape THIS package named, which is one
// list rather than two — and the day a shape is added, every caller that did not
// grow a case for it still compiles and falls to its own default.
const (
	ReasonEmpty        = "the reply arrived empty"
	ReasonMalformed    = "the tool call did not parse"
	ReasonTimeout      = "nobody answered in time"
	ReasonIdle         = "it went silent and stopped working"
	ReasonDegenerate   = "the reply stopped being language"
	ReasonCut          = "the reply stopped part-way"
	ReasonWire         = "the connection did not hold"
	ReasonRefused      = "the endpoint refused"
	ReasonPaced        = "the account is being asked to slow down"
	ReasonUnserved     = "the provider could not serve it"
	ReasonPlanPaused   = "the plan is paused"
	ReasonUnreached    = "the request did not reach anybody"
	ReasonWithdrawn    = "the model is no longer carried"
	ReasonUnauthorized = "this account could not be served"
	ReasonUnservable   = "no shape of this request could be served"
	ReasonOverflow     = "the request did not fit"
	ReasonTooBig       = "the request did not fit even after it was shortened"
	ReasonOurBytes     = "the request itself could not be served"
)

// ── capability ──────────────────────────────────────────────────────────────

// capabilityPolicy is the ONLY policy that may buy a stronger tier, and it buys
// one at a time, late, under a cap, and gives it back.
//
// ── WHY IT COUNTS SEMANTIC FAILURES AND NOTHING ELSE ──
//
// A refutation is a MEASURED failure of the work: a fresh reader ran the check
// and said, with evidence, that the job is not done. That is the only signal in
// this system that is genuinely about the model. A refutation of a round whose
// calls died on the wire is not one — there was nothing for the check to pass —
// and counting it is exactly how four bad responses in a row bought seven times
// the price for the rest of a run. [Tally] is where the two are kept apart, and
// [Evidence.Refuted] is already the filtered count when it arrives here.
//
// ── WHY IT COMES BACK DOWN ──
//
// The lift was bought against a specific finding. When the next check passes,
// the finding is closed and the tier is being paid for nothing. Nothing in this
// build looked for that moment before, so every lift was permanent — which is
// how a single bad minute at a provider became the price of a whole run.
type capabilityPolicy struct{}

func (capabilityPolicy) Class() Class { return Capability }

func (capabilityPolicy) Decide(e Evidence, l Limits) Verdict {
	if e.Passed {
		if e.Escalated {
			return Verdict{Action: ActionDeescalate,
				Reason: "the check passed, so the lifted tier is not buying anything"}
		}
		return Verdict{Action: ActionHold, Reason: "the check passed"}
	}
	// THE CAP IS ASKED BEFORE THE COUNT. Work already standing on a lifted tier
	// that has spent its ceiling has no cheaper answer left in this package, and
	// a verdict that said "escalate" and then failed to spend would be a lie in
	// the journal. It comes back as WORK — the caller's own business, with the
	// report attached — which is the one honest thing left to say.
	if e.Escalated && l.TierCapUSD > 0 && e.SpentUSD >= l.TierCapUSD {
		return Verdict{Class: Work, Action: ActionReport,
			Reason: "the lifted tier has spent what this work may spend on it"}
	}
	if e.Refuted >= l.SemanticFailures {
		return Verdict{Action: ActionEscalate,
			Reason: "the check found gaps again on the same tier"}
	}
	if e.TransportSeen > 0 {
		return Verdict{Action: ActionHold,
			Reason: "the last round lost calls on the wire, so the tier is not the suspect"}
	}
	return Verdict{Action: ActionHold, Reason: "the check found gaps"}
}

// ── work ────────────────────────────────────────────────────────────────────

// workPolicy takes NO ACTION. The job could not be done, or the request itself
// is what is wrong; either way this package has nothing to sell and hands the
// verdict back with the evidence on it for whoever owns the landing.
type workPolicy struct{}

func (workPolicy) Class() Class { return Work }

func (workPolicy) Decide(e Evidence, _ Limits) Verdict {
	reason := "the work did not come back done"
	if e.Status >= 400 && !e.Named() {
		reason = "the request itself was refused"
	}
	return Verdict{Action: ActionReport, Reason: reason}
}

// ── shape ───────────────────────────────────────────────────────────────────

// shapePolicy answers the one class whose move is neither a machine nor a model
// but the REQUEST, and it has exactly two moves because there are exactly two
// things that can be wrong with one.
//
// ── IT DID NOT EXIST, AND A REGEX STOOD WHERE IT SHOULD HAVE ────────────────
//
// internal/session's turn loop computed a verdict and then, forty lines later,
// asked `isContextOverflow(errMsg) || !isRetryable(errMsg)` over the provider's
// prose and returned on it — so a string decided and the verdict was thrown
// away. Both halves of that test are this class: an overflow is a request that
// did not fit, and the whole reason `isRetryable` was consulted at all is that
// nothing typed said "our own bytes". Both are facts now ([Evidence.Overflow],
// [Evidence.OurBytes]) and both are decided here.
type shapePolicy struct{}

func (shapePolicy) Class() Class { return Shape }

func (shapePolicy) Decide(e Evidence, _ Limits) Verdict {
	if e.Overflow {
		// COMPACTION IS OFFERED ONCE PER TURN AND THEN NEVER AGAIN. A request
		// that still does not fit after it was made smaller is not going to fit,
		// and offering the same move a second time is a loop with a person
		// watching it. The second one comes back as WORK — the honest verdict,
		// because there is nothing left for this package to sell.
		if e.Compacted {
			return Verdict{Class: Work, Action: ActionReport, Reason: ReasonTooBig}
		}
		return Verdict{Action: ActionCompact, Reason: ReasonOverflow}
	}
	// AND THE ROUTER READING OUR OWN BYTES IS THE OTHER HALF. No machine was
	// asked, so no machine is the suspect and none of them is the answer; the
	// transport's relaxation ladder is the reshaping and it has already run by
	// the time this is read (internal/provider's endpoints.go). What is left is
	// to end the request naming the SHAPE, which is neither the wire's fault nor
	// the work's — and saying which is the whole of what this verdict buys.
	return Verdict{Action: ActionReshape, Reason: ReasonOurBytes}
}
