package provider

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/paymentrefusal"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// ── ONE REFUSAL OBJECT, THREE READERS ───────────────────────────────────────
//
// A refusal used to be classified three times, by three pieces of code that had
// each been told a different half of the truth, and the class that mattered
// most fell through all three.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// A chat run on 2026-09-01 collected six HTTP 404s of one shape:
//
//	Providers serving <model>: digitalocean, streamlake, baidu, deepinfra,
//	but your request's provider.only preference permits only: coreweave
//
// That sentence means ONE thing — the machine this request demanded cannot
// serve this model — and it is the one refusal class that is certain about a
// lane. Nothing acted on it. The strike ([Client.refuseUpstream]) required
// `provider_name` in the error metadata, which the router omits precisely when
// it is answering for itself; the ladder's first rung was offered only for
// `require_parameters`, `ignore` and `max_price`, so a pinned request climbed
// six rungs still pinned to the lane that had said no; the frontier went on
// scoring the lane because nothing wrote the refusal back; and the surface drew
// the whole thing as `· slow · trying nextbit…`, a sentence about a wait. The
// same lane was chosen three separate times in one run.
//
// Three mirror-image defects and one mute surface, and every one of them is a
// reading of the same fact. So the fact is decided ONCE, here, and read three
// times:
//
//	(a) the strike        velocity.go's [Client.refuseUpstream]
//	(b) the ladder/walk   endpoints.go's [Client.relaxationPlan], hedge.go's walk
//	(c) the screen        [RescueNews] → internal/session → internal/tui3
//
// ── THE LANE COMES FROM OUR OWN REQUEST, NEVER FROM THEIR PROSE ─────────────
//
// The refusal above names the lane twice in English, and reading it out of
// there would work until the day the router rewords it — which is exactly the
// failure docs/design/failsafe/FAILSAFE.md rule 1 is about, and exactly what
// endpoints.go's phrase list was demoted to a hint for. THE LANE A REFUSAL IS
// ABOUT IS THE LANE OUR REQUEST DEMANDED, which this process wrote itself, knows
// exactly, and can read with no parsing at all ([Client.onlyLane]).

// refusalKind is what a refusal IS, decided from structure alone.
type refusalKind uint8

const (
	// refusalNone is everything this classifier has no opinion about: a
	// success, a transport error, a refusal on a model the catalog has never
	// heard of.
	refusalNone refusalKind = iota
	// refusalUpstream is the endpoint the ROUTER CHOSE saying no. Another
	// endpoint may well serve the same request, and the lane is paced rather
	// than written off ([APIError.FromUpstream]).
	refusalUpstream
	// refusalRouting is the ROUTER ITSELF saying nothing it can reach will
	// serve this request's shape (endpoints.go's [Client.routingRefusal]).
	refusalRouting
	// refusalPaced is a 429: the pool that answered is not judging this request,
	// it is saying its queue is full for now. It is a class of its own rather
	// than an absence, because it is acted on differently from every other
	// refusal — the lane is HELD for the wait the provider itself named rather
	// than written off ([Client.refuseLane]) — and because a fact that reaches
	// no reader at all is the defect this class closes.
	refusalPaced
	// refusalPayment is an authenticated account that cannot fund the request.
	// It is terminal for the whole account, not a wait and not one lane's fault.
	refusalPayment
	// refusalPlanPaused is a fixed-price window that will become usable again.
	// It is neither ordinary pacing nor terminal account failure, and it must
	// never earn an endpoint walk or a model hop.
	refusalPlanPaused
	// refusalNoPlan is a bound plan door that this key or model cannot use. It is
	// a fact about the door, not about one endpoint behind it.
	refusalNoPlan
)

// laneRefusal is one refusal as every reader of it needs it.
//
// ONE FIELD PER READER AND NO MORE: what happened, which machine it is about,
// whether that machine is finished for this model, whether any machine was asked
// at all, whose list emptied the set, and whether the MODEL is still carried. A
// field a reader has to interpret would be this classification happening a
// second time somewhere else.
//
// NONE OF THEM IS A DECISION, which is the line this wave drew. This object says
// what the wire said; what to DO about it is one function in one package
// ([taxonomy.Classify]), reached from the marks [markRefusal] leaves on the
// error. The ledger's own three answers — pace, strike, nothing — are the
// exception and stay here, because they are writes rather than moves.
type laneRefusal struct {
	Kind refusalKind
	// Lane is the machine the refusal is ABOUT, empty when it is about none.
	// For an upstream refusal it is the endpoint the router named; for a
	// routing refusal it is the endpoint our own request demanded.
	Lane string
	// Terminal says this lane cannot serve this model at all, so it is not
	// worth another second of anybody's deadline: the strike writes it out of
	// the serving set, the walk skips it, and the screen says refused rather
	// than slow.
	//
	// ONLY A ROUTING REFUSAL AGAINST A DEMANDED LANE IS TERMINAL. An upstream's
	// 4xx is that upstream's verdict on one request and it recovers; "the
	// router will not serve this model from that machine" is a fact about the
	// pairing, and asking again produces the identical 404 for nothing.
	Terminal bool
	// Unasked says NO MACHINE ANSWERED THIS. A list emptied the endpoint set
	// before the request reached one — this process's own vetoes, or the ignored
	// providers standing on the account.
	//
	// IT SEPARATES TWO THINGS `Terminal` ALONE CONFLATED, and the separation is
	// the point. The DEMAND is finished either way: a pairing nobody can use is
	// a pairing to stand down, so a person's pin is retired and they are told,
	// exactly as before. The MACHINE is not: it never got the request, so it has
	// said nothing, and striking it would file a list's verdict against a
	// machine and widen the very list that caused the refusal. So the pin path
	// reads `Terminal` and the ledger reads this.
	Unasked bool
	// Account says the ACCOUNT'S OWN SETTINGS keep the demanded machine from
	// every model — a fact about the account and the machine, not about this
	// model, which is why it is filed where every model's frontier reads it
	// (internal/lane's account.go) rather than on the per-model serving set. It
	// is only ever true beside Unasked: the machine was never asked.
	Account bool
	// Withdrawn says THE ROUTER NO LONGER CARRIES THIS MODEL — the sheet has no
	// row for it and the router answered for itself. It implicates no machine at
	// all, so the ledger does nothing with it; it is on this object because this
	// object is where every structural reading of a refusal is taken, and the
	// verdict downstream needs it to hop at once rather than spend a transport
	// budget on three more identical 404s ([Client.withdrawnModel], #838).
	Withdrawn bool
	// Reset is the vendor's stated reset value for a paused plan window. It is
	// carried from the one classification so the dispatcher and surface never
	// have to parse the refusal a second time.
	Reset string
	// AskedBare says the refused attempt went out with NO membership-narrowing
	// preference — no pin, no rescue's demand, no strike list — so the refusal
	// is evidence about the router's DEFAULT and may count toward a takeover
	// (routefirst.go's [Client.noteRouterRefusal]). One false is "the shape was
	// ours", the safest reading for a direct caller of [Client.laneRefusalFor]
	// — a hedge arm, which is a demand of one machine by construction — and it
	// is set true by the one classifier that holds the request's knobs
	// ([Client.refusalObject]).
	AskedBare bool
}

// struck reports whether there is a lane here for the ledger to WRITE OFF — a
// machine the next encode should route around because it said no.
//
// A MACHINE THAT WAS NEVER ASKED IS NOT ONE OF THEM, whatever this request
// demanded of it (see [laneRefusal.Unasked]).
//
// AND NEITHER IS A PACED ONE. A pool with a full queue has refused nothing; it
// has named a wait, and the wait is what the ledger holds it for
// ([laneRefusal.paced]). The two are separated here rather than at the door so
// that both transports read the same separation.
func (r laneRefusal) struck() bool {
	return (r.Kind == refusalUpstream || r.Kind == refusalRouting) && r.Lane != "" && !r.Unasked
}

// paced reports that this refusal is ONE MACHINE'S QUEUE being full, and that
// the wire named which machine.
//
// A 429 THAT NAMES NOBODY IS THIS ACCOUNT'S OWN CEILING and answers false: no
// lane is implicated, nothing is written, and the call waits it out exactly as
// it always did (retry.go). Asking another machine for an account-wide limit
// would multiply the traffic that earned it.
func (r laneRefusal) paced() bool {
	return r.Kind == refusalPaced && r.Lane != ""
}

// RescueNews is a rescue as a SURFACE may read it, narrowed from [laneRefusal]
// so that a status line never has to interpret a transport's vocabulary.
//
// It travels through [HedgeReport.OnHedgeStart] — see waitreport.go — and it is
// derived from the classifier rather than assembled by hand, so the word a
// person reads and the decision the ledger took cannot drift apart.
type RescueNews struct {
	// Alt is the machine this news is about: the one a rescue is going to, or
	// the one whose rescue has just died.
	Alt string
	// Reason is why a rescue went out, in the two words a surface draws:
	// [RescueSlow] when a lane was merely late, [RescueRefused] when a lane
	// said no. Empty is a rescue nobody classified, which draws as slow.
	Reason string
	// Failed retracts a claim rather than making one: the `trying X…` this
	// surface is showing is about a machine that has now failed, and a status
	// line that goes on promising it is lying about the present tense.
	Failed bool
}

// The two words a rescue is drawn with. They are constants because three
// packages spell them — the transport writes them, internal/session carries
// them, internal/tui3 draws them — and a word spelled in three places is a word
// that gets reworded in one.
const (
	// RescueSlow is a lane that was late. Something is being done about it.
	RescueSlow = "slow"
	// RescueRefused is a lane that said no. Nothing more will be asked of it.
	RescueRefused = "refused"
)

// news is this refusal as the surface reads it, for a rescue going to alt.
func (r laneRefusal) news(alt string) RescueNews {
	reason := RescueSlow
	if r.Kind == refusalRouting || r.Terminal {
		reason = RescueRefused
	}
	return RescueNews{Alt: alt, Reason: reason}
}

// nameServed stamps the machine that WAS SERVING onto a refusal that did not
// name one, and leaves every other refusal exactly as it arrived.
//
// IT IS THE ONE PLACE A SERVED NAME REACHES A REFUSAL, and it runs inside the
// refusal door ([Client.refuseUpstream]) rather than at a call site, so that
// EVERY reader downstream sees the same two facts about an in-stream refusal
// that it sees about an HTTP one: the status it wore and the machine it came
// from. `error.metadata.provider_name` is how the router names an upstream it
// is relaying for, and it omits the field when a refusal is delivered inside an
// already-open stream — the stream named its provider in the chunks instead. A
// reader that only has the error would therefore have read the same refusal as
// two different facts depending on which transport carried it: the ledger here,
// [RefusalFrom] and through it internal/taxonomy's Evidence, and the journal's
// error row.
//
// IT NEVER OVERWRITES A NAME THE ROUTER GAVE. The metadata is the router saying
// whose refusal this is; the served name is only this process's memory of who
// was answering, and it fills a silence rather than correcting a statement.
func nameServed(err error, served string) error {
	served = strings.TrimSpace(served)
	if served == "" {
		return err
	}
	if named, ok := RefusalFrom(err); ok && strings.TrimSpace(named.Provider) == "" {
		named.Provider = served
	}
	return err
}

// markRefusal stamps the classifier's STRUCTURAL ANSWERS onto the error a caller
// will read, so that what this transport worked out travels as typed facts
// rather than being re-derived from a sentence three packages away.
//
// It runs inside the refusal door beside [nameServed], for the same reason:
// every refusal a caller can see passes that door once, on either transport,
// and a mark written anywhere else would be a second place that decides it.
//
// THREE MARKS, AND ONE VERDICT DOWNSTREAM READS ALL OF THEM. `Routing` says a
// list emptied the set, `Account` says whose list it was, and `Withdrawn` says
// the model itself is gone — and internal/taxonomy turns the three into one
// action ([taxonomy.Classify]). Until this wave the same three facts were read
// by three unrelated rules that each grew their own exceptions: taxonomy's
// `classOf`, this file's `refusalKind`, and internal/session's
// `providerCouldNotServe`. Marking them here is what let the other two go.
func markRefusal(err error, refusal laneRefusal) {
	marked, ok := RefusalFrom(err)
	if !ok {
		return
	}
	if refusal.Kind == refusalRouting {
		marked.Routing = true
		marked.Account = refusal.Account
	}
	if refusal.Withdrawn {
		marked.Withdrawn = true
	}
	marked.Payment = refusal.Kind == refusalPayment
	marked.PlanPaused = refusal.Kind == refusalPlanPaused
	marked.PlanUnavailable = refusal.Kind == refusalNoPlan
}

// refusalObject is THE classifier. Everything this process does about a refusal
// is decided here, once, from the request that earned it.
//
// It is a method on the client because two of the three structural questions —
// is this a router at all, does the catalog know this model — are the client's
// own ([Client.routingRefusal] holds the whole argument).
func (c *Client) refusalObject(request *ai.Request, knobs callKnobs, err error) laneRefusal {
	if request == nil {
		return laneRefusal{}
	}
	model := c.modelFor(request)
	refusal := c.laneRefusalFor(model, c.onlyLane(model, knobs), err)
	// THE REFUSAL'S SHAPE IS THE KNOBS' OWN DEMAND, not the composed object's
	// ladder-membership answer ([providerPrefs.membershipNarrowing], which is
	// the ladder's first rung's question and counts require_parameters as
	// membership). The gate's law is about MACHINES: a refusal counts toward
	// the router's default only when nobody demanded machines — a bare ask —
	// and a demand is exactly a rescue's arm ([callKnobs.hedgeLane]) or a
	// choice with admitted names ([lanes.Choice.Only]). The lane-draw law
	// reads both off the knobs, once, in one place ([Client.withLaneChoice]).
	refusal.AskedBare = !demandedShape(knobs)
	return refusal
}

// demandedShape reports whether the attempt went out carrying a machine demand
// — a rescue's arm, or a choice (pin, takeover draw) with admitted names. It
// answers the GATE'S question ([laneRefusal.AskedBare]), which is narrower
// than the ladder's [providerPrefs.membershipNarrowing]: require_parameters is
// membership of the wire body, not of machines, and the ledger's own Ignore
// vetoes leave the pick to the router inside the set the vetoes leave.
func demandedShape(knobs callKnobs) bool {
	if knobs.hedgeLane != "" {
		return true
	}
	return knobs.laneChoice != nil && len(knobs.laneChoice.Only) > 0
}

// laneRefusalFor is the same classification asked by a caller that already
// knows which machine the request demanded.
//
// IT IS AN ENTRANCE AND NOT A SECOND CLASSIFIER. The race's walk holds an arm
// rather than a request — an arm IS its demanded lane (hedge.go's start) — so
// it answers the `only` question by construction and there is nothing for it to
// re-derive. Every rule about what a refusal means is here, once.
func (c *Client) laneRefusalFor(model, demanded string, err error) laneRefusal {
	refusal, ok := RefusalFrom(err)
	if !ok {
		return laneRefusal{}
	}
	body := []byte(refusal.Body)
	switch paymentrefusal.Classify(refusal.Status, body) {
	case paymentrefusal.Payment:
		return laneRefusal{Kind: refusalPayment, Terminal: true}
	case paymentrefusal.WindowExhausted:
		return laneRefusal{Kind: refusalPlanPaused, Reset: paymentrefusal.ResetAt(body)}
	case paymentrefusal.NoPlan:
		return laneRefusal{Kind: refusalNoPlan, Terminal: true}
	}
	demanded = strings.TrimSpace(demanded)
	if refusal.Status == http.StatusTooManyRequests {
		// A 429 IS PACING RATHER THAN A VERDICT ON THE REQUEST, and that half is
		// unchanged: the lane is held for the wait the provider itself named
		// rather than written off.
		//
		// WHAT IS NEW IS THAT IT IS A FACT ABOUT A MACHINE whenever the wire
		// named one — `error.metadata.provider_name`, or the name the stream
		// itself was serving under (client.go fills that in before it asks) —
		// and a fact about a machine belongs in the ledger the next encode
		// reads. This used to return nothing at all, which was right for the
		// account-wide case below and wrong for every 429 that named its pool:
		// see [Client.refuseLane] for the ninety-three seconds it cost.
		//
		// AND A POOL THAT NAMED NOBODY IS THE MACHINE WE DEMANDED, when the
		// request demanded exactly one. The ledger is keyed on WHO SERVED
		// (docs/design/recovery/DESIGN.md §3 clause 4), and the honest order of
		// answers to that is: the wire's own name, else the one machine this
		// request permitted, else nothing. A demand of one machine is the whole
		// serving set, so there is no other machine the queue could have been.
		pool := strings.TrimSpace(refusal.Provider)
		if pool == "" {
			pool = demanded
		}
		return laneRefusal{Kind: refusalPaced, Lane: pool}
	}
	// THE UPSTREAM'S OWN REFUSAL IS READ FIRST, because a named provider is the
	// router telling us it found something to try and that something said no —
	// which is the opposite class from an empty endpoint set, whatever status
	// the two arrive under.
	if refusal.FromUpstream() {
		return laneRefusal{Kind: refusalUpstream, Lane: refusal.Provider}
	}
	// A MODEL THE ROUTER HAS PUT DOWN IS ASKED BEFORE AN EMPTIED SET, because
	// the two arrive wearing the same 404 and very often the same sentence, and
	// only one of them has a machine anywhere behind it. Nothing is struck and
	// nothing is paced — there is no lane in this refusal — and what the mark
	// buys is downstream: the verdict hops the model at once instead of paying
	// the whole transport budget for three more identical refusals (#838).
	if c.withdrawnModel(model, refusal.Status, body) {
		// AND IT IS REMEMBERED, so the next turn hops before it sends rather than
		// paying the whole shape ladder to be told the same thing (withdrawn.go).
		c.withdrawn.noteWithdrawn(model)
		return laneRefusal{Withdrawn: true}
	}
	if !c.routingRefusal(model, refusal.Status, body) {
		return laneRefusal{}
	}
	// A REFUSAL ABOUT A LIST IMPLICATES NO MACHINE — the second half of the law
	// [velocityLedger.keepTheSetServable] enforces on the way out, standing here
	// on the way back. This sentence says a list emptied the set, so the demanded
	// lane was never asked and never answered; striking it would pace the
	// machine, write it out of the serving set, and widen the very list that
	// caused the refusal, so the next request is refused sooner.
	//
	// THE DEMAND IS STILL FINISHED, and that half is unchanged: a pairing this
	// account cannot use is one to stand down, so a person's pin is retired and
	// they are told (lanepin.go, issues #456 and #533). Only the machine is
	// spared, which is what [laneRefusal.Unasked] carries.
	//
	// AND A LIST MAY BE THE ACCOUNT'S OWN. "0 endpoints out of 1 requested …
	// Paid model training violation (account settings)" is the same fact as "all
	// providers have been ignored" — a list emptied the set and nobody answered —
	// and it was read as the demanded machine's own refusal, so that machine was
	// struck for one model and demanded again on the next (the 2026-09-10 race).
	// It is Unasked like any other list, and it is ALSO a fact about every model,
	// which is what Account carries to the ledger.
	return laneRefusal{
		Kind:     refusalRouting,
		Lane:     demanded,
		Terminal: demanded != "",
		Unasked:  listEmptied(body),
		Account:  demanded != "" && accountExcluded(body),
	}
}

// onlyLane is the ONE machine this request demanded on the wire, empty when it
// demanded none.
//
// IT READS THE FIELD RATHER THAN THE REASONS FOR IT. Three different decisions
// can put `provider.only` on a body — a rescue demanding the machine the
// primary is not on, a person's strict lane pin, and a choice already made for
// a streamed call — and enumerating them here would be a fourth place that has
// to be kept in step with the encoder. So the composed object the request
// actually went out with is asked ([Client.wirePreferences]), which is also why
// a request that has climbed the ladder's first rung answers empty: that rung
// takes the whole provider object off, so there was no demand, and the refusal
// that follows is about the model rather than about a machine.
//
// SEVERAL MACHINES IMPLICATE NONE. `only` with two names in it is a refusal
// about a SET, and striking one of them would be this process guessing which.
// Nothing in this build sends more than one today; this is what it means if
// something ever does.
//
// AND RE-DERIVING THE OBJECT IS EXACT, which it was not always. The chooser
// underneath it is a SAMPLED decision and two draws are two different answers
// (lanes.go's [Client.drawLaneChoice]), so while the encoder was free to draw
// one of its own this line re-derived a ranking that could differ from the one
// the wire had carried. `only` happened to survive that — it comes from a strict
// pin or a rescue's own demand, both facts rather than draws — but the safety
// was an accident of which field was being read. Every call now carries the ONE
// choice it was decided on ([Client.withLaneChoice]) and this composes the same
// object the body did, field for field.
func (c *Client) onlyLane(model string, knobs callKnobs) string {
	prefs := c.wirePreferences(model, knobs)
	// ONE NAME OR NO NAME, because a strike has to name the machine it takes
	// away. A demand listing two machines and refused as a whole says only that
	// the pair could not serve the model between them, and striking either on
	// that evidence would be striking on a guess — the same reason a refusal
	// that names no upstream at all is left to the breaker in #138 rather than
	// struck here. Nothing on the wire builds such a demand today: a person's
	// pin is one machine (lanes.go) and a rescue's demand is the single arm it
	// walked to, so this arm is a guard against a future caller rather than a
	// case anybody can reach.
	if prefs == nil || len(prefs.Only) != 1 {
		return ""
	}
	return strings.TrimSpace(prefs.Only[0])
}

// refuseServing writes a terminal refusal into the negative half of the serving
// set, so that the NEXT choice cannot pick the machine the wire has just
// refused (internal/lane's sheet.go).
//
// IT FOLDS THE MODEL ON THE WAY IN and never files under the spelling that
// happened to be on the wire ([laneModel]). The defect this closes had the bare
// id and the dated slug disagreeing about who serves a model; a refusal written
// under one and read under the other would rebuild that disagreement one layer
// down.
func (c *Client) refuseServing(model string, refusal laneRefusal) {
	if !refusal.Terminal || refusal.Unasked {
		// A machine that never got the request cannot have refused to serve it.
		return
	}
	lanes.RefuseServing(laneModel(model), refusal.Lane)
}

// ── THE ONE EXTRACTOR ───────────────────────────────────────────────────────

// Evidence reads one failed request into the vocabulary the response boundary
// reasons in, and DECIDES NOTHING.
//
// ── WHY IT IS HERE AND NOT AT THE CALL SITE ─────────────────────────────────
//
// A 404 used to be classified by three unrelated rules: internal/taxonomy's
// `classOf`, this file's `refusalKind`, and internal/session's
// `providerCouldNotServe`. Each was correct about the half it had been told,
// each grew a special case every time a wave met a new shape — #835 added
// `Evidence.Routing` to work around a fourth — and the class that mattered most
// fell through all of them: an account's privacy setting reached a person as
// `the request itself was refused`, with two other machines idle.
//
// So the split is the other way round now. THIS TRANSPORT KNOWS FACTS and says
// them: what status it wore, who refused, whether a list emptied the set, whose
// list it was, whether the model is still carried, whether the request fitted.
// ONE FUNCTION TURNS FACTS INTO A MOVE ([taxonomy.Classify]), and it is the only
// place in the build that may. A law test holds the line
// (internal/provider/classifier_law_test.go).
//
// The facts themselves were all decided at the refusal door, once, on the way
// past ([markRefusal], [apiError]); nothing is re-read from a sentence here.
// What is NOT on this evidence is everything the transport cannot see — whether
// a stream guard cut, how many attempts this model has had, whether the caller
// has another model — and those are the caller's to add before it classifies.
func Evidence(err error) taxonomy.Evidence {
	evidence := taxonomy.Evidence{TerminalTransport: terminalTransportFailureFrom(err)}
	if err == nil {
		return evidence
	}
	refusal, ok := RefusalFrom(err)
	if !ok {
		// A FAILURE THAT CARRIES NO REFUSAL STILL HAS ONE SHAPE WORTH READING.
		// Not every overflow arrives as an [APIError] — an SDK that wraps its own
		// body read hands back a sentence — and "the request did not fit" is the
		// one shape whose answer is an ACTION rather than an ending, so losing it
		// costs a person a turn that could have been compacted. It is the hint
		// arm of [overflowRefusal] and nothing else is read from the prose.
		evidence.Overflow = overflowRefusal(0, "", err.Error())
		return evidence
	}
	evidence.Status = refusal.Status
	evidence.Upstream = strings.TrimSpace(refusal.Provider)
	evidence.Overflow = refusal.Overflow
	// A SPENT LADDER IS NOT SOMEWHERE LEFT TO GO, which is [RoutingRefusal]'s own
	// rule and is asked through it rather than beside it: [RefusalError] wraps
	// the router's last refusal after the whole ladder has been climbed, and
	// reading THAT as an emptied set would send the caller round it again to be
	// told the same thing.
	evidence.Routing = RoutingRefusal(err)
	// AND A SPENT LADDER IS A BUDGET RATHER THAN A CLASS. [RoutingRefusal] answers
	// false for one on purpose — there is no machine and no shape left HERE — and
	// that is a different sentence from "nothing was learned", which is what the
	// boundary needs. Both are true at once and each is said once.
	var ladder *RefusalError
	evidence.Spent = errors.As(err, &ladder)
	evidence.Account = evidence.Routing && refusal.Account
	evidence.Withdrawn = refusal.Withdrawn
	evidence.PlanPaused = refusal.PlanPaused
	evidence.Unserved = accountUnserved(refusal.Status) || refusal.Payment || refusal.PlanUnavailable
	// OUR OWN BYTES ARE WHAT IS LEFT OVER, and the transport is the only layer
	// that can say so: it is the 4xx that named no upstream, is not a list, is
	// not an absent model and is not a key — which is [APIError.OurRequest]'s
	// whole argument, asked here once.
	evidence.OurBytes = !evidence.Withdrawn && !evidence.Unserved && refusal.OurRequest()
	return evidence
}

// accountUnserved reports the three generic statuses that mean THE ACCOUNT
// could not be served: no key, a key without permission, a balance that ran
// out. The vendor-specific payment and no-plan facts join it in [Evidence]
// through the marks from [markRefusal].
//
// IT IS THE ONE STATUS COMPARISON LEFT IN THIS BUILD OUTSIDE internal/taxonomy,
// and it is here rather than at a reader for exactly that reason: the law test
// forbids a call site choosing an action from a status, and the only way to keep
// that promise is for the transport to name the shape once. 402 is on it for
// completeness — the router answers a spent balance with one — and 429 is not,
// because pacing is a queue rather than an account.
func accountUnserved(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden:
		return true
	}
	return false
}
