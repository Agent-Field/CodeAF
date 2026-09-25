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
// ── THE FOUR CLASSES ────────────────────────────────────────────────────────
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
// [Work] is the work: the job could not be done. There is nothing to retry and
// nothing to buy, so this package takes no action at all and hands the verdict to
// the caller with the evidence on it.
//
// [Shape] is the REQUEST: a transcript longer than the model's window, or bytes
// the router itself read and refused. It joined the other three on 2026-09-10,
// and what stood where it belongs was a pair of regexes in internal/session's
// turn loop that ran over the provider's prose AFTER the verdict was computed and
// returned before the verdict could be read. Its moves are real — make the
// request smaller, or change its shape — which is why it is not [Work].
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

	// Shape is the REQUEST: not the wire, not the model, not the job — the bytes
	// this harness assembled are what nothing will serve. A transcript longer
	// than the window, a body the router read and rejected on its own account.
	//
	// IT IS SEPARATE FROM Work BECAUSE ITS MOVE IS REAL. The job is fine and the
	// model is fine; something about the request has to change, and changing it
	// is an action rather than a report. A turn whose request did not fit is a
	// turn that compacts and asks again — it was ending on a prose regex before
	// this class existed (loop.go's deleted `isContextOverflow(errMsg)`), forty
	// lines after the switch that had already classified it.
	Shape Class = "shape"
)

// Action is what the class's policy says to do next.
type Action string

const (
	// ActionRetry asks again on the SAME tier, with the endpoint rotated
	// underneath by whatever routing the caller already has. It is the transport
	// answer and it is the only action that carries a [Verdict.Backoff].
	ActionRetry Action = "retry"

	// ActionHop is the transport budget spent WITH SOMEWHERE LEFT TO GO: ask
	// the same question of the NEXT MODEL the caller has, on a budget of its
	// own.
	//
	// It is not a tier purchase and it must never be read as one. Nothing about
	// who served a request is evidence about who was asked, so this buys no
	// strength, counts toward no lift, and is reached only when one model's
	// whole budget has been spent on failures that were never about the model.
	// It exists because giving up while a chain the person configured had never
	// been asked was the measured failure: one 502 and three 429s inside
	// seventy-five seconds ended a turn while another model in the same session
	// was answering every call put to it.
	//
	// WHICH MODEL IS THE CALLER'S ANSWER, not this package's. The policy knows
	// only whether one exists ([Evidence.FallbackAvailable]); the order and the
	// cap belong to whatever holds the chain.
	ActionHop Action = "hop"

	// ActionGiveUp is the transport budget spent with nowhere left to go. The
	// request is over; the TIER is untouched and nothing about this counts
	// toward a lift, which is the whole distinction this package exists to keep.
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

	// ActionCompact is the [Shape] answer to a request that did not fit: make it
	// smaller and send the same question again. The machinery for it is the
	// caller's and already exists (internal/session's compaction pass); what is
	// new is that the MOVE is a verdict rather than a string test that ran before
	// the verdict was read.
	//
	// It is asked once per turn. [Evidence.Compacted] says the turn has already
	// paid for it, and a second overflow after a compaction is the honest end:
	// the request is what it is, and the policy hands it on as [Work].
	ActionCompact Action = "compact"

	// ActionReshape is the [Shape] answer to a request THE ROUTER ITSELF read and
	// refused ([Evidence.OurBytes]): no machine was asked, no machine will do
	// better, and the only move left is a different request.
	//
	// The reshaping is the transport's own relaxation ladder — the price ceiling,
	// the reasoning knob, the output cap, the tools, in that order
	// (internal/provider's endpoints.go) — which by the time this verdict reaches
	// a session has already been climbed. So the session's answer to it is the
	// one honest thing left: end the request, name the shape, and never call it a
	// judgement about the work or about the model. It is [ActionReport]'s
	// neighbour and NOT its synonym, because a person reading a failure row is
	// owed "the request could not be sent as it was" rather than "the work did
	// not come back done".
	ActionReshape Action = "reshape"
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

	// PlanPaused says a fixed-price subscription window is temporarily spent.
	// It is a transport fact with no recovery move: retrying, rotating endpoints
	// or hopping models would turn a spending boundary into ordinary pacing.
	PlanPaused bool

	// TerminalTransport says the transport has already exhausted the only
	// recovery it owns and the person must act. It is still transport evidence
	// rather than a finding about the model or the work, but its one verdict is
	// a report: another endpoint, request shape, or model cannot change it.
	TerminalTransport bool

	// Upstream is the provider the router NAMED as the one that refused, empty
	// when the router refused on its own account. The emptiness is the fact: a
	// 4xx that named nobody is our own bytes being read and rejected, and every
	// endpoint alive will say the same thing about the same request — UNLESS
	// Routing says otherwise.
	Upstream string

	// Routing says the ROUTER emptied the endpoint set for this request — a
	// list, an account setting, a price ceiling or a demand left it nothing to
	// ask — so another machine or another model can serve the same bytes. The
	// transport decided it at its refusal door (internal/provider's
	// [provider.RoutingRefusal]) and it is carried here as a fact, never
	// re-read from the sentence.
	//
	// IT IS THE ONE 4xx NAMING NOBODY THAT IS NOT OUR OWN BYTES, and before
	// this field existed it was indistinguishable from one: the measured turn of
	// 2026-09-10 ended on `the request itself was refused` for a 404 that an
	// account's privacy setting had produced, and the same words sent again a
	// minute later were answered.
	Routing bool

	// Account says the exclusion that emptied the set is the ACCOUNT'S OWN —
	// a privacy switch, a paid-training guardrail, a standing ignore list — so
	// it is true of every model and not of this one, and no machine was asked.
	//
	// It is a narrowing of Routing and never a substitute for it: both are set
	// together, because the MOVE is the same (somewhere else) and only the
	// journal line and the ledger care which list it was. It is here so that the
	// ledger's reading and the control answer come off ONE piece of evidence:
	// before this field, `provider/refusalobject.go` decided it for the ledger,
	// `taxonomy` decided a neighbouring question for the action, and
	// `session/taxonomy_boundary.go` decided a third for the ending.
	Account bool

	// Withdrawn says the ROUTER DOES NOT CARRY THIS MODEL any more — the sheet
	// has no row for it and its own error envelope names no upstream. Every
	// machine is fine and the request is fine; the id is gone.
	//
	// ITS MOVE IS THE MODEL AND NOTHING ELSE, which is why it is a field rather
	// than a shade of Routing. There is no machine to rotate to and no shape to
	// relax, so a retry budget spent on one is a person waiting through 2s, 4s
	// and 8s for three identical 404s (#838). It hops at once.
	Withdrawn bool

	// OurBytes says the router read THIS REQUEST and refused it on its own
	// account: a 4xx that named no upstream and is not Routing, Account or
	// Withdrawn. Nobody was asked, and every machine alive would say the same
	// thing about the same bytes.
	//
	// IT IS CARRIED RATHER THAN INFERRED. `Status >= 400 && Upstream == ""` was
	// the inference, and it was made in three places with three different lists
	// of exceptions bolted onto it — which is the defect docs/design/recovery
	// §2.6 counts. The transport decides it once, at its refusal door
	// ([provider.APIError.OurRequest]), and it travels here as a fact.
	OurBytes bool

	// Overflow says the request DID NOT FIT: the transcript is longer than the
	// window the model will take.
	//
	// IT IS STRUCTURAL AND NEVER A PHRASE. The old test was a seven-alternative
	// regex over the provider's prose (`context.?length|token.?limit|…`) run
	// forty lines after the verdict had already been computed, and it returned
	// before the verdict could be read — so a routing 404 whose sentence
	// happened to miss every pattern ended a turn that had three moves left.
	// What sets this is the request-too-large status and the router's own error
	// envelope code ([provider.APIError.Overflow]).
	Overflow bool

	// Compacted says this turn has ALREADY made the request smaller once. It is
	// the difference between [ActionCompact] and giving the overflow back as
	// [Work]: compaction that did not fit is a request that is not going to fit,
	// and asking for it twice is a loop.
	Compacted bool

	// Spent says the TRANSPORT'S OWN SHAPE LADDER has been climbed and has run
	// out: the price ceiling dropped, the reasoning knob off, the output cap off,
	// the tools off, and the router still refusing (internal/provider's
	// [provider.RefusalError]).
	//
	// IT IS A BUDGET THAT IS GONE AND NOT A JUDGEMENT. Nothing about the work has
	// been learned and nothing about the model has; what has been established is
	// that no SHAPE of this request will be served, which leaves exactly one move
	// — another model — and that move belongs to the caller. So it is Transport
	// with its budget declared spent, never [Work]: a turn that reads it as the
	// work ends with the router's own sentence on the screen while a chain the
	// person configured sits unasked, which is the failure
	// docs/design/recovery/DESIGN.md §5 is written about.
	Spent bool

	// OutOfTime says the CALLER'S OWN DEADLINE is gone: the plan's give-up, in
	// the person's time (`lane.Role.GiveUp` scaled by [Limits.Patience]), which
	// is the one thing that bounds how long a failing request may go on
	// recovering (docs/design/recovery/DESIGN.md §4).
	//
	// IT IS THE COUNT'S REPLACEMENT AND NOT THE LADDER'S. `TransportAttempts`
	// used to answer "is there another try in this" from a number; the number is
	// gone, because the transport under every caller was already bounded by the
	// same deadline and two budgets on one axis multiply. What is NOT said here
	// is anything about the failure: the reason a person reads stays the shape
	// that actually failed — a refusal, a reset, a reply that arrived empty —
	// where [Spent] would say "no shape of this request could be served", which
	// is a different claim and usually a false one.
	OutOfTime bool

	// Unserved says THE ACCOUNT could not be served at all: no key, a key this
	// model or bound plan door is not permitted, or a balance that ran out. It
	// arrives as a 401, 402 or 403, or as a vendor's structured payment/no-plan
	// refusal.
	//
	// IT IS THE ONE REFUSAL WITH NO MOVE IN IT. Another machine, another shape
	// and another model all lead to the identical answer, so there is nothing to
	// rotate to and nothing to relax — the person has to act. And it is
	// nevertheless not about the WORK, which is the half that matters to a task
	// node's ending: a run that died on somebody's expired key learned nothing
	// about the job and must not spend the job's ceiling (#513).
	//
	// It is carried rather than switched on, because "401, 402, 403" written out
	// at a call site is exactly how the three-classifier defect was spelled
	// (session/taxonomy_boundary.go's deleted providerCouldNotServe).
	Unserved bool

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

	// Attempt is which transport attempt this is, 1-based. It counts the
	// requests this model FAILED OUTRIGHT — a refusal, a reset, a deadline —
	// and never the ones a stream guard cut, which are counted beside it in
	// Cuts and spend a budget of their own shape.
	Attempt int

	// Cuts is how many of this model's attempts were streams the guard ended,
	// this one included and 0 when this failure is not a cut.
	//
	// IT IS THE SAME BUDGET AS Attempt AND A DIFFERENT KIND OF SPENDING. A
	// request that failed is evidence the endpoint is failing and earns the
	// full ladder with its waits; a stream that was cut is not — the request
	// succeeded and the reply came apart — so it earns a shorter allowance with
	// no wait in front of it. One model, one budget, two ways to spend it.
	Cuts int

	// Degenerate says the cut was the reply ceasing to be language — repetition,
	// jumbled text, the model's own internal markup — rather than the stream
	// going quiet. If the same transcript produces that twice, the transcript is
	// the suspect and a third ask spends the whole prompt to be told so again.
	Degenerate bool

	// Rerouted says this cut actually took an endpoint out of the caller's
	// routing, so the attempts after it are genuinely served by somebody else.
	//
	// FALSE IS THE CASE THAT MATTERS. A cut that struck nothing — routing turned
	// off by the person, or a stream that died before any chunk named who served
	// it — leaves the next attempt landing in the same place by the same rules,
	// so the extra asks buy nothing and the allowance narrows.
	Rerouted bool

	// Watched says A PERSON IS SITTING IN FRONT OF THIS TURN and can see what
	// it is doing — a conversation, rather than a task worker or a standing
	// check that nobody is looking at.
	//
	// IT IS WHAT MAKES WAITING FOR EVER SAFE. A harness that keeps asking a
	// machine that answers nothing, and says so on the screen, is being patient:
	// the person reads the line and stops it whenever they like. The same loop
	// where nobody is watching is a hang — it spends a worker's whole wall clock
	// on a server that may never answer, and there is no one to notice. So the
	// unbounded wait below is offered to the first and withheld from the second,
	// and the second keeps a count ([transportBudget]).
	Watched bool

	// OneMachine says the cut request had NO ENDPOINT DIVERSITY AT ALL — it
	// named no machine and none served it, which is a build with no router
	// behind it and a set of one.
	//
	// IT IS THE OPPOSITE CASE TO THE ONE ABOVE, not a second spelling of it.
	// Rerouted false with a pool means the next attempt lands in the same place
	// by the same rules, so the extra asks buy nothing. Rerouted false with ONE
	// machine means asking again is the only move there is, and the thing that
	// mends a machine which answered nothing is time — so the allowance grows
	// and a wait goes in front of each ask ([transportBudget], [waitFor]).
	OneMachine bool

	// FallbackAvailable says the caller has a NEXT MODEL to ask when this one's
	// budget is spent. It is the whole difference between [ActionHop] and
	// [ActionGiveUp], and it is the caller's fact: an empty chain, a completer
	// with none to offer and a person who pinned one model all spell it false,
	// and each of those makes the move ABSENT rather than broken.
	FallbackAvailable bool

	// Refuted is how many SEMANTIC failures this tier has: checks that read the
	// finished work and found gaps, with no transport failure under them. A
	// refutation of a round whose calls died on the wire is not one of these,
	// and [Tally] is what keeps the two apart.
	Refuted int

	// TransportSeen is how many transport failures happened under this piece of
	// work. It is carried so a capability verdict can say WHY it is holding.
	TransportSeen int

	// Found says a check has just read the finished work and named gaps in it.
	// It is what makes a finding a CAPABILITY question at all, and it is separate
	// from Refuted because the two answer different halves: Found is "a check
	// spoke", Refuted is "how many times it has spoken about this tier with the
	// wire ruled out". A finding whose round died on the wire has Found set and
	// Refuted unmoved, which is exactly the case that must hold rather than buy.
	Found bool

	// Passed is a check that has just passed. It is the de-escalation question
	// and the only piece of good news this struct carries.
	Passed bool

	// Escalated says the work is already on a tier something lifted it onto.
	Escalated bool

	// SpentUSD is what the lifted tier has cost this piece of work so far.
	SpentUSD float64
}

// Named reports that ONE MACHINE OWNED THIS REFUSAL: the router relayed
// somebody else's no and said whose, so the fact is about that machine and
// about nothing else — not the model, not the account, not the request.
//
// IT IS THE WHOLE DIFFERENCE BETWEEN A MOVE AND A WAIT, AND IT OUTRANKS THE
// STATUS. A refusal that named a machine is answered by another machine,
// whatever number it wore: the machine is taken off the next body and the next
// body goes out at once. A refusal that named NOBODY is the account's own
// ceiling or the router's reading of our own bytes — every machine answers it
// identically, nothing can be taken off the next body, and the only moves left
// are a wait and then another model.
//
// THE MEASURED FAILURE IS THE ORDER THIS REPLACED (2026-09-11 14:39–14:41, the
// owner's own task on deepseek/deepseek-v4.1-flash). `Status == 429` was asked
// before `Upstream != ""`, so a per-machine `(via Wafer: … temporarily
// rate-limited upstream)` rode the account-limiter road — wait, double, send the
// same order again — for eight sends and ninety seconds, while six other
// machines on the same model were answering in under five.
//
// It is a method rather than a comparison at each site because "is this refusal
// about a machine" is asked by the classifier, by the reason, and by the
// transport's own walk, and three spellings would be three answers.
func (e Evidence) Named() bool { return strings.TrimSpace(e.Upstream) != "" }

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

	// Unbounded says this retry has NO LIMIT OF ANY KIND — not a count, and not
	// the caller's deadline either. It is the one verdict a caller may not
	// convert into an ending by running out of time ([waitsForEver]), and it is
	// a field rather than an inference from [Verdict.Attempts] being zero
	// because zero already means something else and older: an ordinary failure
	// keeps no count HERE and is bounded by the deadline instead. Reading the
	// two as one ended the deadline for every ordinary failure in the build.
	Unbounded bool
}

// Escalates reports whether this verdict is one that buys a stronger tier. It
// is a method rather than a comparison at each call site because "does this
// count toward a lift" is the question the whole package exists to answer, and
// two spellings of it would be two answers.
func (v Verdict) Escalates() bool { return v.Action == ActionEscalate }

// Retries reports whether the caller should ask again, on the model it is on.
func (v Verdict) Retries() bool { return v.Action == ActionRetry }

// Hops reports whether the caller should ask the NEXT MODEL it has. It is a
// method for [Verdict.Escalates]'s reason: "did this model's budget run out with
// somewhere left to go" is one question, and two spellings of it at two call
// sites would be two answers.
func (v Verdict) Hops() bool { return v.Action == ActionHop }

// Compacts reports whether the caller should make the request smaller and ask
// the same question again. It is a method for [Verdict.Hops]'s reason: one
// spelling of "the request did not fit" for the whole build.
func (v Verdict) Compacts() bool { return v.Action == ActionCompact }

// EndsTurn reports whether this verdict is allowed to end a turn.
//
// A TRANSPORT VERDICT NEVER IS, whatever else it says. Giving up on a request is
// the end of the REQUEST; the turn it belonged to is the caller's business, and
// the measured failure this package was written for was a turn that ended
// because one 200 came back empty.
//
// AND NEITHER IS A REQUEST THAT CAN STILL BE MADE SMALLER. A [Shape] verdict
// whose move is [ActionCompact] has somewhere to go by definition — the same
// question, in fewer words — so it is not an ending either. A shape with no move
// left comes back as [Work] from its own policy and ends the turn like any other.
//
// BUT A VERDICT THAT ASKS FOR NOTHING ALWAYS IS, whatever class selected it.
// [ActionReport] is this package saying it has no move to sell; a caller that
// took that as "carry on" would loop on a failure nobody can act on but the
// person — an expired key answering every request the same way, forever.
func (v Verdict) EndsTurn() bool {
	if v.Compacts() {
		return false
	}
	if v.Action == ActionReport {
		return true
	}
	return v.Class != Transport
}

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
	// THE POLICY MAY HAND THE VERDICT TO ANOTHER CLASS, AND ONLY THE POLICY MAY.
	// The capability policy does it in exactly one place — a lifted tier that has
	// spent its ceiling has no cheaper answer left, so the honest verdict is the
	// work's ([capabilityPolicy.Decide]). Everything else is stamped with the
	// class that selected it, so a policy cannot silently disagree with its own
	// classification.
	if strings.TrimSpace(string(verdict.Class)) == "" {
		verdict.Class = class
	}
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
//  3. Then the REQUEST'S OWN SHAPE, before any status is read. A transcript that
//     did not fit the window is not a fact about a machine or about a model, and
//     it arrives under whatever status the router felt like using — so asking
//     the status first would file it as the wire or as the work depending on the
//     day.
//  4. Then WHO REFUSED, and only after that the status. A refusal that NAMED an
//     upstream is that upstream's and another endpoint may serve it, whatever
//     number it wore — this is the shape the measured 400s arrived in and the
//     shape nine out of ten measured 429s arrive in ([Evidence.Named]). Then the
//     status: 5xx is the wire by definition, and a 429 that named nobody is the
//     account's own ceiling, which is the wire with no machine to move to.
//     So is a ROUTING refusal (the account's own settings included): the router
//     emptied its endpoint set on a list or a setting, nobody was asked, and
//     somewhere else can serve it. So is a WITHDRAWN model, whose somewhere else
//     is another model rather than another machine. A 4xx that named nobody and
//     is none of those is the router reading OUR OWN BYTES and saying no, which
//     no endpoint will fix but a different request might: that is the shape.
//  5. Only then, a finding: a check that read the finished work and named gaps,
//     with the wire ruled out above. Whether that finding BUYS anything is the
//     capability policy's question and not this one.
//  6. Anything left is work.
func classOf(e Evidence) Class {
	if e.Passed {
		return Capability
	}
	if e.Overflow {
		return Shape
	}
	if e.TerminalTransport || e.Empty || e.Malformed || e.Timeout || e.Idle || e.Cut || e.Degenerate || e.Wire {
		return Transport
	}
	// A WITHDRAWN MODEL IS THE WIRE WITH ONE MOVE, and it is asked before the
	// status because the status it wears is somebody else's: the router answers
	// a model it no longer carries with the same 404 it answers an emptied set
	// with. The policy is what knows the move is the model rather than the
	// machine ([transportPolicy.Decide]).
	if e.Withdrawn {
		return Transport
	}
	// AND AN ACCOUNT THAT COULD NOT BE SERVED IS THE WIRE TOO, for the one
	// meaning of Transport that holds every time: it says nothing whatever about
	// the model or about the job. Its policy gives it no budget and no move
	// ([transportPolicy.Decide]) — the person has to act — but a task node that
	// died on it must not be charged for the job (#513).
	if e.Unserved {
		return Transport
	}
	// AND A SPENT SHAPE LADDER IS THE WIRE WITH ONE MOVE LEFT, for the reason
	// [Evidence.Spent] states: every shape of this request has been refused, which
	// is a fact about who can serve it and not about the job.
	if e.Spent {
		return Transport
	}
	switch {
	// A MACHINE THAT NAMED ITSELF IS ASKED BEFORE ANY STATUS, because the status
	// on a relayed refusal is the UPSTREAM'S and says nothing about what to do
	// with it: a 400, a 502 and a 429 relayed from one pool are one fact — that
	// pool said no — and the move for all three is the next pool. Asking the
	// number first filed the commonest of them, the 429, as the account's own
	// ceiling and bought ninety seconds of waiting on one machine ([Evidence.Named]
	// carries the measurement).
	case e.Named():
		return Transport
	case e.Status >= 500:
		return Transport
	// AND A 429 THAT NAMED NOBODY IS THE ACCOUNT'S OWN CEILING, which is the wire
	// too: it is a queue rather than a judgement about the model or the work. Its
	// moves are the wait it asked for and then another model, because there is no
	// machine to rotate to — every one of them is behind the same ceiling.
	case e.Status == 429:
		return Transport
	case e.Status >= 400 && (e.Routing || e.Account):
		return Transport
	case e.Status >= 400 && e.OurBytes:
		return Shape
	case e.Status >= 400:
		// A 4xx THIS PACKAGE WAS TOLD NOTHING STRUCTURAL ABOUT keeps the reading
		// it has always had. The transport decides [Evidence.OurBytes] at its own
		// door and sets it on every refusal it recognises, so reaching here means
		// a status arrived from a caller that has no refusal door — and the safe
		// answer for an unfamiliar failure is the one that takes no action.
		return Work
	}
	if e.Found || e.Refuted > 0 {
		return Capability
	}
	return Work
}
