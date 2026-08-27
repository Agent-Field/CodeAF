package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
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

// wireEvidence reads one failed request into [taxonomy.Evidence] using the
// helpers this package already has, and nothing else.
//
// NO PATTERN LIST LIVES HERE. [provider.RefusalFrom] already knows whether a 4xx
// named an upstream, [provider.CutFrom] already knows a guard cut, and
// [isRetryable] is already the harness's one answer to "is this shape worth
// asking again". A second reading of the same sentence in this file would be a
// second answer, and the first thing to drift.
func wireEvidence(err error, attempt int) taxonomy.Evidence {
	evidence := taxonomy.Evidence{Attempt: attempt}
	if err == nil {
		return evidence
	}
	message := err.Error()
	evidence.Message = clip(message, errorRowMessage)
	if refusal, ok := provider.RefusalFrom(err); ok {
		evidence.Status = refusal.Status
		evidence.Upstream = refusal.Provider
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
		!isContextOverflow(message) && isRetryable(message) {
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

// readCallFailure is what the turn loop and the errand ladder ask about one
// failed request. It classifies, journals, and records a transport failure
// against the piece of work this agent belongs to — which is what stops the gate
// three layers up from mistaking the wire for the model.
func (a *Agent) readCallFailure(err error, model, role string, attempt int) taxonomy.Verdict {
	verdict := a.classify(wireEvidence(err, attempt), model, role)
	if verdict.Class == taxonomy.Transport {
		a.config.failures.Wire()
	}
	return verdict
}

// readEmptyReply is what the turn loop asks about an HTTP 200 that carried no
// words and no tool calls.
//
// IT IS A TRANSPORT FAILURE AND THE TURN DOES NOT END ON IT. That is the whole
// change: the loop used to write one error row and stop, which on one measured
// run ended the entire thing eighteen minutes in. An endpoint that answered
// nothing is an endpoint that did not answer, and the answer to that is to ask
// again.
func (a *Agent) readEmptyReply(model string, attempt int) taxonomy.Verdict {
	verdict := a.classify(taxonomy.Evidence{Empty: true, Attempt: attempt}, model, "")
	if verdict.Class == taxonomy.Transport {
		a.config.failures.Wire()
	}
	return verdict
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
// It never returns true for anything the taxonomy calls transport, and it writes
// the reason where a person watching the run can read it.
func (a *Agent) movesForFailure(node *TaskNode, runErr error, log io.Writer) bool {
	if !terminalProviderFailure(runErr) {
		return false
	}
	verdict := a.classify(wireEvidence(runErr, a.failureLimits().TransportAttempts),
		node.runModel(), "")
	if verdict.Class == taxonomy.Transport {
		// The wire failures are ALREADY on the node's tally: the worker that just
		// died carried the same pointer and recorded every one of them as it went
		// ([Agent.tallyFor], task_run.go). Counting them again here would be the
		// owner and the worker keeping two versions of one number.
		fmt.Fprintf(log, "the run ended on the connection rather than on the work — %s: staying on %s\n",
			verdict.Reason, node.runModel())
		return false
	}
	return true
}

// escalateNodeModel is the only caller of [Agent.nextNodeModel] in this package,
// and the structural test says so.
func (a *Agent) escalateNodeModel(node *TaskNode) (string, bool) {
	return a.nextNodeModel(node)
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
	if !clean {
		// The round is closed and its wire failures are spent, but the gate still
		// has to be told they happened — otherwise a finding with four dead calls
		// under it reads exactly like a finding with none.
		evidence.TransportSeen = 1
	}
	verdict := a.classify(evidence, node.runModel(), string(roleRepair))
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
	verdict := a.classify(evidence, node.runModel(), string(roleRepair))
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
func (a *Agent) readErrandFailure(err error, role roles.Role, model string, attempt int) taxonomy.Verdict {
	return a.readCallFailure(err, model, string(role), attempt)
}
