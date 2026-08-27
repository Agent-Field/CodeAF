// Package taxonomy is the response boundary: the one place in this build that
// answers "what kind of failure was that?" and hands back what to do about it.
//
// ── WHY THERE IS A BOUNDARY AT ALL ──────────────────────────────────────────
//
// Three completely different things used to arrive at the harness looking the
// same, and each site answered for itself:
//
//   - A TRANSPORT failure — an endpoint that refused, stalled, or answered with
//     an empty 200 — read as the MODEL being unable to do the work, which bought
//     a stronger tier that was never needed. On a measured five-run comparison
//     that misreading was 57–82% of the bill on three of the runs ($9.5–15.8
//     each), while the run that never rolled four bad responses in a row cost
//     $2.33 in total.
//   - A TRANSPORT failure read as "the turn is over": an empty 200 was written
//     down once and the loop stopped, ending a whole run eighteen minutes in.
//   - A TRANSPORT failure read as "wait forever": an errand's deadline was
//     logged and dropped with nothing said about it.
//
// None of those is a judgement about the work, and until this package none of
// them could be told apart from one.
//
// ── THE THREE CLASSES ───────────────────────────────────────────────────────
//
// [Transport] is the wire: nobody answered, or somebody answered with something
// that was not an answer. It is retried on the SAME tier with the endpoint
// rotated underneath, it never ends a turn, and it NEVER counts toward buying a
// stronger model — because it says nothing whatever about the model.
//
// [Capability] is the model: a fresh check read the work and found it wanting,
// on a tier that had every chance. That, and only that, is what buys a lift —
// after [Limits.SemanticFailures] of them, under a cost cap, and it comes back
// down again the moment a check passes.
//
// [Work] is the work: the job could not be done, or the request itself is what
// is wrong. There is nothing to retry and nothing to buy, so this package takes
// no action at all and hands the verdict to the caller with the evidence on it.
//
// ── ONE BOUNDARY, NOT A SWITCH IN EVERY CALLER ──────────────────────────────
//
// [Classify] is the only entry. It reads the evidence into a class, looks the
// class's single [Policy] out of the registry, and returns that policy's
// [Verdict]. The numbers the policies read are [Limits], which a caller resolves
// once from the person's settings — so N, K and the cap are configuration in one
// file rather than constants sprinkled across the sites that happen to need
// them.
package taxonomy

import (
	"fmt"
	"strings"
	"time"
)

// Class is what a failure ACTUALLY was, as distinct from what it looked like at
// the call site that caught it.
//
// The values are lowercase words because they are written into the journal
// verbatim and a bench counts them by name.
type Class string

const (
	// Transport is the wire: a refusal from an upstream, a stall, a deadline, an
	// empty 200, arguments that did not parse. It is a fact about who served the
	// request and never about who was asked.
	Transport Class = "transport"

	// Capability is the model: a check that read the finished work and found
	// gaps in it, on a tier that had a fair chance at the job.
	Capability Class = "capability"

	// Work is the work: the job itself could not be done, or the request the
	// harness assembled is what the provider is refusing. Neither has an answer
	// this package can take, so it takes none.
	Work Class = "work"
)

// Action is what the class's policy says to do next.
type Action string

const (
	// ActionRetry asks again on the SAME tier, with the endpoint rotated
	// underneath by whatever routing the caller already has. It is the transport
	// answer and it is the only action that carries a [Verdict.Backoff].
	ActionRetry Action = "retry"

	// ActionGiveUp is the transport budget spent. The request is over; the TIER
	// is untouched and nothing about this counts toward a lift, which is the
	// whole distinction this package exists to keep.
	ActionGiveUp Action = "give_up"

	// ActionHold is "stay where you are". It is what a capability verdict says
	// before the semantic failures have added up, and what it says about a
	// refutation that had a transport failure underneath it.
	ActionHold Action = "hold"

	// ActionEscalate buys one tier, once.
	ActionEscalate Action = "escalate"

	// ActionDeescalate gives it back. A check that passes on a lifted tier is
	// the evidence that the lift is no longer being paid for anything, and
	// nothing in this build used to look for it.
	ActionDeescalate Action = "deescalate"

	// ActionReport hands the verdict to the caller with nothing done about it.
	// It is the whole of the [Work] policy, and it is also where a lifted tier
	// that has spent its cap lands.
	ActionReport Action = "report"
)

// Evidence is everything the boundary is allowed to reason from.
//
// It is deliberately small and deliberately dumb: booleans and counts that the
// call site reads off the helpers it already has ([provider.RefusalFrom],
// [provider.CutFrom], a tally the work carries), never an error to be
// re-grepped here. A second pattern list in this package would be a second
// answer to a question the adapter already answers, and the first thing to
// drift.
type Evidence struct {
	// Status is the HTTP status the failure arrived under, 0 when it never
	// reached one.
	Status int

	// Upstream is the provider the router NAMED as the one that refused, empty
	// when the router refused on its own account. The emptiness is the fact: a
	// 4xx that named nobody is our own bytes being read and rejected, and every
	// endpoint alive will say the same thing about the same request.
	Upstream string

	// Message is the failure's own sentence, for the journal line's `reason`
	// when nothing more specific applies.
	Message string

	// Empty is an HTTP 200 that carried no content and no tool calls. It is not
	// a short answer; it is an endpoint that did not answer.
	Empty bool

	// Malformed is a tool call whose arguments did not parse — the shape that
	// arrives as `function.arguments must be valid JSON` or an unterminated
	// string, from one endpoint in a pool while its neighbours are fine.
	Malformed bool

	// Timeout is a deadline or a stall wall: context deadline exceeded, a read
	// that never finished.
	Timeout bool

	// Idle is a subprocess that is alive, silent, and doing nothing.
	Idle bool

	// Cut is a stream the guard ended.
	Cut bool

	// Wire is the catch-all the caller's OWN transport pattern matched: a socket
	// that hung up, a name that would not resolve, an upstream that reset before
	// headers. It carries no status and no shape of its own, and this package
	// deliberately keeps no list of its own to recognise it by — the caller has
	// one and a second would drift.
	Wire bool

	// Attempt is which transport attempt this is, 1-based. Nothing else uses it.
	Attempt int

	// Refuted is how many SEMANTIC failures this tier has: checks that read the
	// finished work and found gaps, with no transport failure under them. A
	// refutation of a round whose calls died on the wire is not one of these,
	// and [Tally] is what keeps the two apart.
	Refuted int

	// TransportSeen is how many transport failures happened under this piece of
	// work. It is carried so a capability verdict can say WHY it is holding.
	TransportSeen int

	// Passed is a check that has just passed. It is the de-escalation question
	// and the only piece of good news this struct carries.
	Passed bool

	// Escalated says the work is already on a tier something lifted it onto.
	Escalated bool

	// SpentUSD is what the lifted tier has cost this piece of work so far.
	SpentUSD float64
}

// Verdict is the boundary's answer: what it was, what to do, and one phrase
// saying why — which is what the journal line carries.
type Verdict struct {
	// Class is what the evidence was.
	Class Class

	// Action is what the class's policy says to do.
	Action Action

	// Reason is one short phrase, plain words, written into the journal and read
	// by a person doing an autopsy. It is never a stack trace and never a
	// payload.
	Reason string

	// Attempts is how many transport tries the policy allows in total, so a
	// caller's loop bound is the policy's number rather than its own constant.
	// Zero on every non-transport verdict.
	Attempts int

	// Backoff is how long to wait before the retry this verdict asks for. Zero
	// on everything that is not [ActionRetry].
	Backoff time.Duration

	// Rotate says the retry should be served by somebody else if the routing
	// underneath can arrange it. It is true for exactly the failures where the
	// endpoint is the suspect.
	Rotate bool
}

// Escalates reports whether this verdict is one that buys a stronger tier. It
// is a method rather than a comparison at each call site because "does this
// count toward a lift" is the question the whole package exists to answer, and
// two spellings of it would be two answers.
func (v Verdict) Escalates() bool { return v.Action == ActionEscalate }

// Retries reports whether the caller should ask again.
func (v Verdict) Retries() bool { return v.Action == ActionRetry }

// EndsTurn reports whether this verdict is allowed to end a turn.
//
// A TRANSPORT VERDICT NEVER IS, whatever else it says. Giving up on a request is
// the end of the REQUEST; the turn it belonged to is the caller's business, and
// the measured failure this package was written for was a turn that ended
// because one 200 came back empty.
func (v Verdict) EndsTurn() bool { return v.Class != Transport }

// String is the one-line form a log takes.
func (v Verdict) String() string {
	return fmt.Sprintf("%s · %s · %s", v.Class, v.Action, v.Reason)
}

// Classify is THE BOUNDARY. Every site in this build that decides what a bad
// response means asks this and does what the verdict says.
//
// It reads the evidence into a class ([classOf]), looks that class's single
// policy out of the registry, and returns the policy's answer with the class
// stamped on it — so a policy cannot disagree with the classification that
// selected it.
//
// An unregistered class cannot happen with the policies this package registers
// in its own init, but the fallback is stated rather than panicked: a harness
// that cannot classify a failure must still be able to hand it to somebody, and
// the safe hand-off is the one that takes no action.
func Classify(e Evidence, l Limits) Verdict {
	class := classOf(e)
	policy, ok := PolicyFor(class)
	if !ok {
		return Verdict{Class: Work, Action: ActionReport,
			Reason: "no policy is registered for " + string(class)}
	}
	verdict := policy.Decide(e, l.floor())
	verdict.Class = class
	if strings.TrimSpace(verdict.Reason) == "" {
		verdict.Reason = strings.TrimSpace(e.Message)
	}
	return verdict
}

// classOf is the taxonomy itself, and the ORDER OF ITS QUESTIONS IS THE WHOLE
// ARGUMENT: the cheap explanation is asked first, every time.
//
//  1. A check that PASSED is the only good news here and it is a fact about the
//     model, so it goes to the capability policy, which is the only one that
//     knows how to give a tier back.
//  2. Then every transport shape. An empty 200, arguments that did not parse, a
//     cut stream, a deadline, a silent subprocess — none of them is evidence
//     about the model, and reading any of them as one is what bought the bill.
//  3. Then the status. 5xx and 429 are the wire by definition. A 4xx that NAMED
//     an upstream is that upstream's refusal and another endpoint may serve it,
//     so it is transport too — this is the shape the measured 400s arrived in.
//     A 4xx that named NOBODY is the router reading our own bytes and saying no,
//     which no endpoint and no model will fix: that is work.
//  4. Only then, a refutation: a check that read the finished work and found
//     gaps, with the wire ruled out above.
//  5. Anything left is work.
func classOf(e Evidence) Class {
	if e.Passed {
		return Capability
	}
	if e.Empty || e.Malformed || e.Timeout || e.Idle || e.Cut || e.Wire {
		return Transport
	}
	switch {
	case e.Status >= 500:
		return Transport
	case e.Status == 429:
		return Transport
	case e.Status >= 400 && strings.TrimSpace(e.Upstream) != "":
		return Transport
	case e.Status >= 400:
		return Work
	}
	if e.Refuted > 0 {
		return Capability
	}
	return Work
}
