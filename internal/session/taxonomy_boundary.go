package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// ── THE RESPONSE BOUNDARY ───────────────────────────────────────────────────
//
// This file is the ONE place in this package that asks what a bad response
// meant. Everything else — the turn loop, the errand ladder, the node runner,
// the gate — calls a function here and does what the verdict says.
//
// A STRUCTURAL TEST HOLDS THAT LINE (taxonomy_law_test.go). It fails if
// [taxonomy.Classify] is called anywhere else in the package, and it fails if
// either of the two functions that BUY A STRONGER MODEL — [Agent.nextNodeModel]
// and [Agent.repairModel] — is called from outside this file. That is not
// tidiness: the failure this whole mechanism exists for was three sites each
// answering the same question its own way, and a fourth site added next year
// would do it again unless something says no.
//
// ── WHAT WAS WRONG BEFORE ──
//
// A task worker rolled four consecutive HTTP 400s — "provider returned error",
// one endpoint after another, the arguments of a tool call arriving as
// unterminated JSON. The turn gave up, the node's run ended with a provider
// error, and task_run.go read that as THE MODEL being unable to answer and moved
// the node onto the next rung of the fallback chain: a new worker, a new
// journal, a model seven times the price, and no road back down. On three runs
// of a measured five-run comparison that was 57–82% of the whole bill; the run
// that never happened to roll four bad responses in a row cost $2.33.
//
// Nothing about who served a request is evidence about who was asked. That
// sentence is the mechanism.

// failureLimits is the boundary's three numbers, resolved once per agent from
// the person's profile (config.ResponseLimitsAt).
//
// ONCE, because resolving reads the profile file and this is asked on the
// failure path of every request — and because a number that could change between
// two attempts of one ladder is a ladder nobody can reason about afterwards.
func (a *Agent) failureLimits() taxonomy.Limits {
	a.limitsOnce.Do(func() {
		a.limits = config.ResponseLimitsAt(a.config.ProfileDir).Floored()
		// AND RESOLVING THEM PUBLISHES THE PERSON'S PATIENCE, because this is the
		// one place in the build that reads `response.attempts` and the two
		// places that build a deadline cannot read a profile at all (lane's
		// [lane.UsePatience], and internal/provider's dispatcher under it). It is
		// stated here rather than hidden inside the resolver so that "who told
		// the deadline how patient to be" has one answer and one line.
		lanes.UsePatience(a.limits.Patience)
	})
	return a.limits
}

// tallyFor is the [taxonomy.Tally] one node keeps, minted on first ask.
//
// It is keyed by node id on the OWNER rather than held on the node, so the
// graph's own struct stays what it is and a node nothing ever classified simply
// has no entry. Every worker this agent builds for that node — the run, each
// repair round — is handed the same pointer, which is what makes "the wire
// failures under this round" a fact the gate can read.
func (a *Agent) tallyFor(node *TaskNode) *taxonomy.Tally {
	if node == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tallies == nil {
		a.tallies = map[uint64]*taxonomy.Tally{}
	}
	tally, ok := a.tallies[node.id]
	if !ok {
		tally = &taxonomy.Tally{}
		a.tallies[node.id] = tally
	}
	return tally
}

// classify is the boundary itself: one call to [taxonomy.Classify], one journal
// line, one verdict back.
//
// THE LINE IS WRITTEN HERE AND NOWHERE ELSE, so a bench can count classes
// without knowing which site produced them, and so no caller can take a verdict
// and quietly not record it. `model` and `role` are what the row needs to be
// joinable against the call and error rows beside it (sessionfile.go).
func (a *Agent) classify(evidence taxonomy.Evidence, model, role string) taxonomy.Verdict {
	verdict := taxonomy.Classify(evidence, a.failureLimits())
	a.file.appendFailure(journalFailure{
		Class:    string(verdict.Class),
		Reason:   verdict.Reason,
		Action:   string(verdict.Action),
		Model:    strings.TrimSpace(model),
		Role:     strings.TrimSpace(role),
		Attempt:  evidence.Attempt,
		Status:   evidence.Status,
		Provider: strings.TrimSpace(evidence.Upstream),
		Refuted:  evidence.Refuted,
		SpentUSD: evidence.SpentUSD,
	})
	return verdict
}

// ── the wire ────────────────────────────────────────────────────────────────

// wireEvidence reads one failed request into [taxonomy.Evidence]: the
// transport's own facts, plus the three things only this package can see.
//
// NO CLASSIFICATION LIVES HERE, and that is the change this wave made. Every
// structural reading of a refusal — the status, who refused, whether a list
// emptied the set and whose, whether the model is still carried, whether the
// request fitted, whether the account could be served — is taken ONCE at the
// refusal door and handed over whole ([provider.Evidence]). What this adds is
// what the adapter cannot know: that a stream guard cut, that a deadline fired,
// and that a tool call's arguments did not survive.
//
// NO PATTERN LIST DECIDES ANYTHING. [isRetryable] is still asked, and it is
// still the harness's one list of what a bare socket failure looks like — but it
// sets a FACT ([taxonomy.Evidence.Wire]) rather than an outcome, which is the
// whole difference from the `isContextOverflow(errMsg) || !isRetryable(errMsg)`
// that used to return out of the turn loop forty lines after the verdict had
// been computed.
func wireEvidence(err error, attempt int) taxonomy.Evidence {
	if err == nil {
		return taxonomy.Evidence{Attempt: attempt}
	}
	evidence := provider.Evidence(err)
	evidence.Attempt = attempt
	message := err.Error()
	evidence.Message = clip(message, errorRowMessage)
	if refusal, ok := provider.RefusalFrom(err); ok {
		// THE UPSTREAM'S OWN WORDS ARE WHERE THE SHAPE IS. "Provider returned
		// error" is the router's sentence and says nothing; the raw body is
		// where `function.arguments must be valid JSON` and `unterminated
		// string` actually appear, which is the difference between a model that
		// cannot hold a tool and one endpoint in a pool mangling the stream.
		evidence.Malformed = malformedArguments(refusal.Raw) || malformedArguments(refusal.Message)
	}
	if _, ok := provider.CutFrom(err); ok {
		evidence.Cut = true
	}
	if errors.Is(err, context.DeadlineExceeded) || isDeadlineSentence(message) {
		evidence.Timeout = true
	}
	// AND THE LAST RESORT IS THE ANSWER THE HARNESS ALREADY GAVE. A transport
	// failure that carried no status, no cut and no deadline — a socket that hung
	// up, a name that would not resolve — is still the wire, and [isRetryable] is
	// where this build has always kept that list.
	if evidence.Status == 0 && !evidence.Cut && !evidence.Timeout &&
		!evidence.Overflow && isRetryable(message) {
		evidence.Wire = true
	}
	return evidence
}

// malformedArguments reports the one provider shape that reads as a capability
// failure and is not: a tool call whose arguments did not survive the stream.
//
// It is checked against the UPSTREAM'S RAW BODY rather than against the router's
// summary, and the two spellings are the two that were actually measured, from
// different endpoints serving the same model within seconds of each other.
func malformedArguments(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return false
	}
	return strings.Contains(raw, "must be valid json") ||
		strings.Contains(raw, "unterminated string") ||
		strings.Contains(raw, "invalid json in function arguments") ||
		(strings.Contains(raw, "function.arguments") && strings.Contains(raw, "json"))
}

// isDeadlineSentence catches the deadline that arrives as words rather than as a
// wrapped error — "read response: context deadline exceeded" off the adapter's
// own body read, which no errors.Is can see once it has been re-wrapped as text.
func isDeadlineSentence(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "context deadline exceeded") ||
		strings.Contains(message, "deadline exceeded")
}

// transportLadder is what a caller knows about the ladder ONE request is on, in
// the boundary's own vocabulary.
//
// It exists so that the loop hands the boundary FACTS and takes back a decision,
// rather than keeping a decision of its own beside the boundary's. Before it, the
// turn loop counted stream cuts, held its own three budgets for them, and hopped
// to the next model on its own authority — while the identical question about a
// refusal was answered by [taxonomy.Verdict] and answered differently, which is
// why one 502 and three 429s ended a turn that had two other models to ask.
type transportLadder struct {
	// attempt is which outright failure of this model this is, 1-based: a
	// refusal, a reset, a deadline. It does not count cut streams.
	attempt int
	// cuts is how many of this model's attempts were streams the guard ended,
	// this one included, and zero when this failure is not a cut.
	cuts int
	// degenerate says the cut was the reply ceasing to be language rather than
	// the stream going quiet, and rerouted says the cut actually took an
	// endpoint out of the routing underneath.
	degenerate bool
	rerouted   bool
	// watched says a person is steering this conversation and can see the
	// waiting ([Config.Interactive], which every worker door leaves unset). It
	// is what makes an unbounded wait patience rather than a hang
	// (taxonomy's [taxonomy.Evidence.Watched]).
	watched bool
	// oneMachine says the cuts under this step had no endpoint diversity to try
	// at all — a build with no router behind it and a set of one. It is the
	// opposite case to `rerouted` being false with a pool, and the boundary
	// answers it the opposite way (taxonomy's [taxonomy.Evidence.OneMachine]).
	oneMachine bool
	// fallback says the caller has a next model to ask. It is the whole
	// difference between moving on and giving up, and it is the caller's fact.
	fallback bool
	// outOfTime says the caller's own deadline is gone — the plan's give-up, in
	// the person's time — which is the whole of what bounds a failing request
	// (docs/design/recovery/DESIGN.md §4). It is the caller's fact for the same
	// reason `fallback` is: this package knows the clock it started, and the
	// boundary decides what running out of it MEANS.
	outOfTime bool
}

// mark writes the ladder onto the evidence the wire already answered for.
func (l transportLadder) mark(evidence *taxonomy.Evidence) {
	evidence.Attempt = l.attempt
	evidence.Cuts = l.cuts
	evidence.Degenerate = l.degenerate
	evidence.Rerouted = l.rerouted
	evidence.OneMachine = l.oneMachine
	evidence.Watched = l.watched
	evidence.FallbackAvailable = l.fallback
	evidence.OutOfTime = l.outOfTime
}

// readCallFailure is what the errand ladder and every caller with one model asks
// about one failed request: the plain shape, with nowhere else to go.
//
// A caller that HAS somewhere else to go asks [Agent.readLadderFailure] instead
// and gets [taxonomy.ActionHop] where this gets [taxonomy.ActionGiveUp]. The
// default is the conservative one on purpose: a caller that says nothing about a
// chain is a caller with no chain, and a move it could not make would be a
// verdict it had to ignore.
func (a *Agent) readCallFailure(err error, model, role string, attempt int) taxonomy.Verdict {
	return a.readLadderFailure(err, model, role, transportLadder{attempt: attempt})
}

// readLadderFailure is the same reading with the whole ladder on it. It
// classifies, journals, and records a transport failure against the piece of work
// this agent belongs to — which is what stops the gate three layers up from
// mistaking the wire for the model.
func (a *Agent) readLadderFailure(err error, model, role string, ladder transportLadder) taxonomy.Verdict {
	evidence := wireEvidence(err, ladder.attempt)
	ladder.mark(&evidence)
	return a.readWireEvidence(evidence, model, role)
}

// weighLadder is the classification WITHOUT the record: one failure read into a
// verdict, and nothing written down about it.
//
// IT EXISTS FOR ONE CALLER AND ONE REASON (loop.go). The turn's ladder cannot
// know whether it has time for another request until it knows what the verdict
// asks it to WAIT — and if it has not, the same failure has to be read again
// with that fact on it. Two reads are one failure, so the line is written once,
// afterwards, carrying the verdict that was actually acted on: a row per read
// would make a ladder of three read as four, and the first row would name a
// retry that never happened.
func (a *Agent) weighLadder(err error, ladder transportLadder) (taxonomy.Verdict, taxonomy.Evidence) {
	evidence := wireEvidence(err, ladder.attempt)
	ladder.mark(&evidence)
	return taxonomy.Classify(evidence, a.failureLimits()), evidence
}

// writeLadderVerdict is the other half of [Agent.weighLadder]: the journal line
// and the wire failure against the piece of work, written once the verdict is
// settled.
func (a *Agent) writeLadderVerdict(verdict taxonomy.Verdict, evidence taxonomy.Evidence, model, role string) {
	a.file.appendFailure(journalFailure{
		Class:    string(verdict.Class),
		Reason:   verdict.Reason,
		Action:   string(verdict.Action),
		Model:    strings.TrimSpace(model),
		Role:     strings.TrimSpace(role),
		Attempt:  evidence.Attempt,
		Status:   evidence.Status,
		Provider: strings.TrimSpace(evidence.Upstream),
		Refuted:  evidence.Refuted,
		SpentUSD: evidence.SpentUSD,
	})
	if verdict.Class == taxonomy.Transport {
		a.config.failures.Wire()
	}
}

// readWireEvidence is the last step both readings share: classify, journal, and
// record a transport failure against the piece of work this agent belongs to. It
// is a door of its own so that a caller holding the EVIDENCE as well as the
// verdict — [Agent.readErrandFailure] — reads one failure once rather than
// building the evidence twice.
func (a *Agent) readWireEvidence(evidence taxonomy.Evidence, model, role string) taxonomy.Verdict {
	verdict := a.classify(evidence, model, role)
	if verdict.Class == taxonomy.Transport {
		a.config.failures.Wire()
	}
	return verdict
}

// transportWords is the person's spelling of a wire failure — the same shape the
// journal names, said the way somebody watching a reply would say it.
//
// THE TWO SPELLINGS ARE NOT THE SAME SENTENCE AND MUST NOT BE. A journal line
// wants one grouping word per shape, so `the endpoint refused` is exactly right
// there and exactly wrong on a screen: `endpoint` is machinery, and no sentence a
// person reads in this build is allowed to use it. So the shapes are named once,
// in [taxonomy], and the words for them are written here.
func transportWords(verdict taxonomy.Verdict) string {
	switch verdict.Reason {
	case taxonomy.ReasonEmpty:
		return "nothing came back from the model"
	case taxonomy.ReasonMalformed:
		return "the model's tool call arrived broken"
	case taxonomy.ReasonTimeout:
		return "the model did not answer in time"
	case taxonomy.ReasonIdle:
		return "the model went quiet"
	case taxonomy.ReasonDegenerate:
		return "the reply lost its thread"
	case taxonomy.ReasonCut:
		return "the reply stopped part-way"
	case taxonomy.ReasonWire:
		return "the connection to the model dropped"
	case taxonomy.ReasonRefused:
		return "the model would not take the request"
	case taxonomy.ReasonPaced:
		return "we are being asked to slow down"
	case taxonomy.ReasonUnserved:
		return "the model could not be reached"
	case taxonomy.ReasonWithdrawn:
		return "that model is not being served any more"
	case taxonomy.ReasonUnauthorized:
		return "your key was not accepted for this model"
	case taxonomy.ReasonOverflow:
		return "this conversation got too long for the model"
	case taxonomy.ReasonTooBig:
		return "this conversation is too long for the model even after shortening it"
	case taxonomy.ReasonOurBytes:
		return "the request could not be sent as it was"
	}
	return "the request did not get through"
}

// transportKeptWords is the same shape said about a whole spent budget rather
// than about one attempt: what KEPT happening, which is what a person watching a
// step move to another model is owed.
//
// It is a second spelling of one list rather than a second list. The shapes are
// [taxonomy]'s, named once; this says them in the past-repeated tense because
// "the model would not take the request — finishing this one on X" reads as one
// bad minute, and the thing that actually happened was every attempt failing.
func transportKeptWords(verdict taxonomy.Verdict) string {
	switch verdict.Reason {
	case taxonomy.ReasonEmpty:
		return "nothing kept coming back from the model"
	case taxonomy.ReasonMalformed:
		return "the model's tool calls kept arriving broken"
	case taxonomy.ReasonTimeout:
		return "the model kept not answering in time"
	case taxonomy.ReasonIdle:
		return "the model kept going quiet"
	case taxonomy.ReasonDegenerate:
		return "the reply kept losing its thread"
	case taxonomy.ReasonCut:
		return "the reply kept stopping part-way"
	case taxonomy.ReasonWire:
		return "the connection to the model kept dropping"
	case taxonomy.ReasonRefused:
		return "the model kept turning the request away"
	case taxonomy.ReasonPaced:
		return "we kept being asked to slow down"
	case taxonomy.ReasonUnserved:
		return "the model could not be reached"
	case taxonomy.ReasonWithdrawn:
		return "that model is not being served any more"
	case taxonomy.ReasonUnauthorized:
		return "your key was not accepted for this model"
	case taxonomy.ReasonOverflow, taxonomy.ReasonTooBig:
		return "this conversation kept being too long for the model"
	case taxonomy.ReasonOurBytes:
		return "the request could not be sent as it was"
	}
	return "the request kept failing"
}

// readEmptyReply is what the turn loop asks about an HTTP 200 that carried no
// words and no tool calls.
//
// IT IS A TRANSPORT FAILURE AND THE TURN DOES NOT END ON IT. That is the whole
// change: the loop used to write one error row and stop, which on one measured
// run ended the entire thing eighteen minutes in. An endpoint that answered
// nothing is an endpoint that did not answer, and the answer to that is to ask
// again.
// AND IT IS BOUNDED BY THE CALLER'S CLOCK, NOT BY A COUNT OF EMPTY ANSWERS.
// `outOfTime` is the turn's own give-up, spent on going nowhere; the count that
// used to bound this was the person's `response.attempts` walked a second time,
// under a transport already bounded by the same deadline
// (docs/design/recovery/DESIGN.md §4).
func (a *Agent) readEmptyReply(model string, attempt int, outOfTime bool) taxonomy.Verdict {
	verdict := a.classify(taxonomy.Evidence{Empty: true, Attempt: attempt, OutOfTime: outOfTime}, model, "")
	if verdict.Class == taxonomy.Transport {
		a.config.failures.Wire()
	}
	return verdict
}

// readOverflow is what the turn loop asks about a request the provider said did
// not fit.
//
// IT IS A MOVE AND NOT AN ENDING, which is the whole of why it goes through the
// boundary at all. A transcript longer than the window says nothing about the
// wire, the model or the job — it is the REQUEST, and the answer is the same
// question in fewer words ([taxonomy.Shape], [taxonomy.ActionCompact]).
//
// `compacted` is whether this turn has already paid for that once, and it is the
// caller's fact because only the turn knows. The policy is what decides what to
// do with it: a second overflow after a compaction is a request that is not
// going to fit, and it comes back as work rather than as another round of the
// same move.
func (a *Agent) readOverflow(err error, model string, compacted bool) taxonomy.Verdict {
	evidence := wireEvidence(err, 1)
	if !evidence.Overflow {
		// NOT THIS SHAPE, AND NOT JOURNALED AS ANY OTHER. The caller asks this of
		// every failed request, so an ordinary refusal reaching here must leave no
		// row: the ladder below classifies and journals the same failure a few
		// lines later, and two rows for one failure is a ladder nobody can count.
		return taxonomy.Verdict{}
	}
	evidence.Compacted = compacted
	return a.classify(evidence, model, "")
}

// ── the node's model ────────────────────────────────────────────────────────

// movesForFailure is the gate on the ONE place a node's own model is changed
// because a run ended badly (task_run.go).
//
// It replaces a bare `terminalProviderFailure(runErr)` test, and the difference
// is the mechanism: a provider failure is still a provider failure, but it is
// now READ before it is acted on. A transport verdict says the endpoint was the
// suspect, and moving the node to a dearer model to answer for somebody else's
// bad minute is precisely the purchase this package exists to stop.
//
// It never BUYS a model for anything the taxonomy calls transport, and it writes
// the reason where a person watching the run can read it.
//
// ── AND IT NEVER REFUSES ONE A PERSON NAMED ────────────────────────────────
//
// What this gate exists to stop is a PURCHASE: moving a node to the next, often
// dearer, model in a chain to answer for somebody else's bad minute. A model
// chosen in the node's own room is the opposite act — nothing is being bought
// and nothing is being guessed, because the question "which model" was answered
// by the person watching. So the pick is asked FIRST and there is no reading of
// the class to reach.
//
// THAT IS NOT AN EXCEPTION BOLTED ONTO THE TRANSPORT RULE; it is the rule saying
// what it is about. Written the other way round — classify, then let a pick past
// a transport verdict — it would be two answers to "may this node move" that a
// later wave could drift apart. It reads the SAME fact the move itself reads
// ([TaskNode.standingModel]) against the SAME model the run used, so the gate
// and the move cannot disagree about whether there is anywhere to go.
//
// The measured case is exactly this one: a step grinding on a machine answering
// `temporarily rate-limited upstream` is a TRANSPORT failure however many times
// it happens, so a person who picked another model over it was told the choice
// was taken and then watched `staying on <the model they had just replaced>`.
func (a *Agent) movesForFailure(node *TaskNode, ranOn string, runErr error, log io.Writer) bool {
	if !terminalProviderFailure(runErr) {
		return false
	}
	if standing := node.standingModel(); standing != "" && !strings.EqualFold(standing, ranOn) {
		fmt.Fprintf(log, "the run ended on the connection rather than on the work: moving to %s, which you chose\n", standing)
		return true
	}
	// THE WORKER UNDER THIS HAS ALREADY SPENT WHATEVER IT HAD, which is what the
	// boundary is being told: a run that ended on the wire ended because its own
	// deadline did, so the reading is of a failure with nothing left rather than
	// of the first attempt of a ladder nobody is going to walk.
	evidence := wireEvidence(runErr, 1)
	evidence.OutOfTime = true
	verdict := a.classify(evidence, ranOn, "")
	if verdict.Class == taxonomy.Transport {
		// The wire failures are ALREADY on the node's tally: the worker that just
		// died carried the same pointer and recorded every one of them as it went
		// ([Agent.tallyFor], task_run.go). Counting them again here would be the
		// owner and the worker keeping two versions of one number.
		fmt.Fprintf(log, "the run ended on the connection rather than on the work — %s: staying on %s\n",
			verdict.Reason, ranOn)
		return false
	}
	return true
}

// diedOnTheWire says a run's error was the connection to the model and not a
// verdict about the request or the work: a transport failure that carried no
// status ([terminalProviderFailure] answers for those), was not a person's
// cancel, and reads as the wire (loop.go's [isRetryable]). It is the question
// the second worker and the ending both ask, so it is one function — and it
// lives here because [terminalProviderFailure] is the boundary's to ask
// (taxonomy_law_test.go).
// providerCouldNotServe answers the one question a failed landing's ending turns
// on: did the provider FAIL TO SERVE this request, or did it ANSWER it?
//
// ── UPSTREAM IS THE PROVIDER FAILING TO SERVE, NOT THE PROVIDER ANSWERING ───
//
// The two look alike from the call site — both arrive as an error, both end the
// node — and they are opposite news about the WORK. A service that was down, a
// route with no provider left on it, a limit still refusing after the call's own
// retries, an account that could not be served: none of them found anything out
// about the job, so counting one as work left undone spends a run's ceiling on
// somebody else's outage (#513, task_run.go's [TaskEndingUpstream]).
//
// BUT A MODEL REFUSING IS AN ANSWER, and so is a 4xx about what the request
// actually held — a body this run assembled that was malformed, too large, or
// could not be processed. Those are the provider answering, what it answered is
// about this work, and the work is still undone: they keep [TaskEndingError] and
// go on being named as left.
//
// SO IT IS THE VERDICT THAT ANSWERS, AND NOT A LIST OF STATUSES. It used to be
// `api.Status >= 500 || 404 || 429 || 401 || 403` written out here — the third
// of the three rules that each decided for themselves what a 404 meant
// (docs/design/recovery/DESIGN.md §2.6), and the one whose list drifted furthest
// from the other two. [taxonomy.Classify] already answers "was this the wire or
// was it an answer", from evidence the transport stamped once, so this asks it:
// the class the boundary gives a failure IS the distinction this function was
// written to draw.
//
// Anything the boundary does not recognise is the WORK — the safe side, because
// an unfamiliar failure keeps the meaning it has always had.
func providerCouldNotServe(err error) bool {
	if err == nil {
		return false
	}
	// A SPENT LADDER IS AN ANSWER, AND IT IS A TYPE RATHER THAN A STATUS. The
	// chain relaxed the request, dropped the ceiling and walked the models, and
	// what it hands back is a diagnosis for a person ([provider.RefusalError]) —
	// which is the same rule [provider.RoutingRefusal] states for the same
	// reason, and the one reading the class underneath it cannot carry, because
	// the refusal it wraps is a perfectly ordinary routing 404.
	var spent *provider.RefusalError
	if errors.As(err, &spent) {
		return false
	}
	return taxonomy.Classify(wireEvidence(err, 1), taxonomy.Limits{}.Floored()).Class == taxonomy.Transport
}

func diedOnTheWire(err error) bool {
	if err == nil || terminalProviderFailure(err) || errors.Is(err, context.Canceled) {
		return false
	}
	if _, cut := provider.CutFrom(err); cut {
		return true
	}
	// THE WIRE IS A FACT ON THE EVIDENCE AND NO LONGER A TEST HERE. Overflow is
	// the transport's structural answer ([provider.Evidence]) rather than a
	// regex over the sentence, and [isRetryable] is asked once, inside
	// [wireEvidence], where it sets [taxonomy.Evidence.Wire].
	return wireEvidence(err, 1).Wire
}

// escalateNodeModel is the only caller of [Agent.nextNodeModel] in this package,
// and the structural test says so.
func (a *Agent) escalateNodeModel(node *TaskNode, ranOn string) (string, bool) {
	return a.nextNodeModel(node, ranOn)
}

// failoverCheckerModel is where a CHECK goes when the model judging a node's
// work came back with neither word: the ADAPTER'S OWN CHAIN, the same one a
// conversation's turn hops along and the same one a node's run walks
// ([Agent.nextNodeModel]). One spelling of "the next model" for the whole
// binary — a list of this file's own would be the second answer to drift from
// it.
//
// IT IS NOT A PURCHASE, which is why the boundary lets it past rather than
// asking the taxonomy about it. What buys a stronger tier is a verdict about the
// WORK; this is the same question asked in another lane after one lane answered
// nothing at all, and no evidence about the deliverable has been read either
// way. It is bounded at one by its caller ([Agent.auditNode]).
//
// Empty is A MOVE THAT IS ABSENT rather than one that fails: an install with no
// chain, and `--one-model` — under which every text call this session makes
// rides the conversation's model by the door's own promise ([Config.OneModel]) —
// both ask the fresh checker on the model the check already had.
func (a *Agent) failoverCheckerModel(model string) (string, bool) {
	if a.config.OneModel {
		return "", false
	}
	options := a.fallbackModels(model)
	if len(options) == 0 {
		return "", false
	}
	return options[0], true
}

// ── the gate ────────────────────────────────────────────────────────────────

// readFinding is what the repair loop asks after a check has found gaps: does
// this finding buy a stronger tier, hold where it is, or end the work?
//
// The tally decides whether the finding is SEMANTIC at all — a round whose calls
// died on the wire had nothing for the check to pass — and the capability policy
// decides the rest: K findings on the same tier before a lift, a cap on what the
// lifted tier may cost this one piece of work, and the tier handed back the
// moment a check passes ([Agent.readPass]).
func (a *Agent) readFinding(node *TaskNode, log io.Writer) taxonomy.Verdict {
	tally := a.tallyFor(node)
	clean := tally.Refuted()
	evidence := tally.Evidence()
	evidence.Found = true
	if !clean {
		// The round is closed and its wire failures are spent, but the gate still
		// has to be told they happened — otherwise a finding with four dead calls
		// under it reads exactly like a finding with none.
		evidence.TransportSeen = 1
	}
	verdict := a.classify(evidence, node.runModel(), string(roles.RoleRepair))
	switch verdict.Action {
	case taxonomy.ActionEscalate:
		// THE TALLY IS NOT TOLD YET. A lift is recorded at the moment it is
		// actually bought ([Agent.repairTierModel]), never at the moment it is
		// decided — the caller may still have no round left to spend, and a tally
		// saying a tier was bought when nothing ran would put the cap and the
		// de-escalation both a step out of true.
		fmt.Fprintf(log, "the check found gaps again on the same model: buying one tier\n")
	case taxonomy.ActionHold:
		fmt.Fprintf(log, "sending the work back on the same model — %s\n", verdict.Reason)
	case taxonomy.ActionReport:
		fmt.Fprintf(log, "no more rounds are bought for this work — %s\n", verdict.Reason)
	}
	return verdict
}

// readPass is the other half, and the half that did not exist: a check that
// PASSED on a lifted tier is the evidence that the lift is no longer buying
// anything, so it is given back.
//
// Nothing in this build looked for that moment before, which is why every lift
// was permanent and why one bad minute at a provider became the price of a whole
// run.
func (a *Agent) readPass(node *TaskNode, log io.Writer) taxonomy.Verdict {
	tally := a.tallyFor(node)
	evidence := tally.Evidence()
	evidence.Passed = true
	verdict := a.classify(evidence, node.runModel(), string(roles.RoleRepair))
	if verdict.Action == taxonomy.ActionDeescalate {
		tally.Deescalate()
		fmt.Fprintf(log, "the check passed: the work goes back to %s\n", node.runModel())
	}
	tally.Passed()
	return verdict
}

// repairTierModel is the only caller of [Agent.repairModel] in this package, and
// the structural test says so.
//
// A HOLD RUNS THE ROUND ON THE MODEL THE WORK IS ALREADY ON. That is what makes
// "hold" a real answer rather than a refusal to act: the gaps still go back to a
// fresh worker in the same worktree with the finding in front of it — the
// cheapest work there is (repair_role.go) — it is simply not bought at the
// careful tier's price on the strength of a failure that was never about the
// model.
func (a *Agent) repairTierModel(node *TaskNode, verdict taxonomy.Verdict) string {
	tally := a.tallyFor(node)
	if !verdict.Escalates() && !tally.Escalated() {
		return strings.TrimSpace(node.runModel())
	}
	// HERE, because this is the line that buys it. [repair_role.go]'s cascade may
	// still floor to the model the work is already on — an install with no tiers,
	// a model somebody named for this node — and a tally that recorded a purchase
	// the ladder never made would hold a cap against money nobody spent.
	model := a.repairModel(node)
	if !strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(node.runModel())) {
		tally.Escalate()
	}
	return model
}

// billLift folds what a lifted round cost into the tally the cap is checked
// against. It is ignored while nothing is lifted ([taxonomy.Tally.Spend]), so
// the ordinary price of the work never counts toward a ceiling on the lift.
func (a *Agent) billLift(node *TaskNode, child *Agent) {
	if child == nil {
		return
	}
	a.tallyFor(node).Spend(child.Usage().CostUSD)
}

// ── the errands ─────────────────────────────────────────────────────────────

// readErrandFailure is what [Agent.callRole] asks about one errand that could
// not be answered.
//
// AN ERRAND'S TRANSPORT FAILURE IS DROPPED, DELIBERATELY AND OUT LOUD. Nobody
// typed the call and nobody is waiting on it, so a deadline on the naming of a
// session is not news a person needs — but it is news the FILE needs, because
// the measured version of this left a session with no title, no brief and no
// word anywhere of why. The row is the whole action.
// AND IT ASKS AS A LADDER, because it is one. `fallback` is the errand's own
// fact — a rung remains — which is what turns the transport budget being spent
// into [taxonomy.ActionHop] rather than [taxonomy.ActionGiveUp]
// ([Agent.readLadderFailure]). The evidence comes back beside the verdict because
// the ladder asks one question the verdict does not carry: see [errandWalksOn].
func (a *Agent) readErrandFailure(err error, role roles.Role, model string,
	ladder transportLadder,
) (taxonomy.Verdict, taxonomy.Evidence) {
	evidence := wireEvidence(err, ladder.attempt)
	ladder.mark(&evidence)
	return a.readWireEvidence(evidence, model, string(role)), evidence
}
