package session

// Completion follows the main model's response and actual execution state.

import (
	"context"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// Bound saved decision prose without restricting generation.
	completionDecisionBytes = 600
	// These bounds belong to retained tool context, not completion or delegation.
	checkpointResultBytes  = 400
	checkpointDigestTokens = 5000
	checkpointDigestBytes  = checkpointDigestTokens * bytesPerToken
)

// This exact historical prefix is recognized when reopening old transcripts.
// New turns never ask a second model to certify completion.
const legacyBoundedCheckpointCarryOnLead = "[carry on] A reader of a bounded account of the work raised the observation below. " +
	"Check it against the actual current work and the person's request before changing anything. " +
	"Fix any confirmed gap. If the observation is mistaken or already satisfied, preserve the correct work, " +
	"explain the evidence briefly, and finish; do not invent a change to satisfy the observation.\n"

const checkpointStoppedNote = "stopping here · "

// journalDecision writes down what the goal owner decided, because a run that
// carried on and a run that stopped read identically in this file otherwise —
// which is the same gap [journalCeiling] was written to close one road over.
//
// A PERSON'S SESSION WRITES NOTHING HERE, and that is [Person]'s emptiness law
// reaching the journal: the interactive session must not be able to tell that
// any of this arrived, and a new line in somebody's transcript is something
// they can tell. What a person decided is in the conversation, where they said
// it.
func (a *Agent) journalDecision(decision Decision) {
	steward := a.steward()
	if steward == nil {
		return
	}
	a.mu.Lock()
	file := a.file
	a.mu.Unlock()
	if file == nil {
		return
	}
	budget := steward.Budget()
	file.appendPrincipal(journalPrincipal{
		Who:      "steward",
		Event:    "decided",
		Decision: string(decision.Verb),
		Reason:   decision.Reason,
		Brief:    clip(decision.Brief, completionDecisionBytes),
		WallMS:   budget.SpentWall.Milliseconds(),
		CostUSD:  budget.SpentUSD,
	})
}

// A transport failure or empty response is not model completion. The ordinary
// request error and retry path owns those outcomes.
func turnBroke(response *ai.Response) bool {
	if response == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(provider.FinishReason(response))) {
	case "error", "network_error":
		return true
	}
	return strings.TrimSpace(response.Text()) == "" && len(response.ToolCalls()) == 0
}

func (a *Agent) decideOverTheChecks(ctx context.Context, remains Remains) Decision {
	checks, stashed := a.terminalAudit(ctx)
	remains.Checks = checks
	// AND WHAT GIT IS HOLDING OUT OF THE TREE COMES BACK WITH THEM. The reading
	// handed in was assembled from the session's own ledgers, which know a path
	// was written and not whether the writing is still there; the stash is the
	// one account of that nobody in this session can take for themselves
	// ([Remains.Stashed]).
	remains.Stashed = stashed
	remains.WasFailing, remains.Unread, remains.BaselineRead = a.baselineRedChecksFor(checks)
	remains.WasFailingTests = a.baselineFailureNames()
	return a.who().Decide(remains)
}

func (a *Agent) journalAbsorbed(remains Remains) {
	lines := remains.absorbed()
	if len(lines) == 0 || a.steward() == nil {
		return
	}
	a.mu.Lock()
	file := a.file
	if a.absorbed == nil {
		a.absorbed = map[string]bool{}
	}
	fresh := lines[:0:0]
	for _, line := range lines {
		if !a.absorbed[line] {
			a.absorbed[line] = true
			fresh = append(fresh, line)
		}
	}
	a.mu.Unlock()
	if file == nil || len(fresh) == 0 {
		return
	}
	file.appendPrincipal(journalPrincipal{Who: "steward", Event: "absorbed", Kept: fresh})
}

func (a *Agent) turnIsWaitingOnItsOwnWork() bool {
	return len(a.jobsWorkingNow()) > 0
}

// continueAfterExecutionFacts returns actual outstanding execution evidence to
// an unattended run after the main model finishes its response.
// CRITICAL: MISSING FILE-CHANGE METADATA IS NOT UNFINISHED WORK. Shell actions,
// remote actions and read-only answers must not buy another model's approval.
// Do not restore a completion gate without preserving the ordinary-completion
// and simple-loop regressions and measuring quality, cost and time together.
func (a *Agent) continueAfterExecutionFacts(ctx context.Context, hub *eventHub, response *ai.Response) bool {
	if turnBroke(response) || a.turnIsWaitingOnItsOwnWork() || a.turnHandedItsAskOff() {
		return false
	}
	decision := a.decideRemains(ctx, response.Text())
	switch decision.Verb {
	case DecideDone:
		return false
	case DecideStop:
		hub.send(Event{Kind: EventNotice, Text: checkpointStoppedNote + decision.Reason})
		return false
	}
	hub.send(Event{Kind: EventNotice, Text: checkpointCarryOnNote})
	a.record(textMessage("user", checkpointCarryOnLead+decision.Brief))
	return true
}

const checkpointCarryOnNote = "the ask is not finished · carrying on rather than stopping here"

// decideRemains consults the principal once, after any explicitly declared
// checks. An intermediate success would erase repeated-failure state before
// the check result arrives, making a failed check retry forever.
func (a *Agent) decideRemains(ctx context.Context, said string) Decision {
	remains := a.remainsFor(said)
	a.journalAbsorbed(remains)
	// An existing stop wins before any declared command is started. Inspect
	// the stop facts without an intermediate Decide call: a provisional done
	// would erase the repeated-check failure that the final reading must keep.
	spent, why := a.who().Budget().Exhausted()
	stopped := false
	if steward := a.steward(); steward != nil {
		steward.mu.Lock()
		stopped = steward.stopped != ""
		steward.mu.Unlock()
	}
	var decision Decision
	if ctx.Err() == nil && !spent && !stopped && len(remains.unmet()) == 0 && remains.Acceptance != "" {
		decision = a.decideOverTheChecks(ctx, remains)
	} else {
		if err := ctx.Err(); err != nil {
			decision = stop(err.Error())
		} else if spent {
			decision = stopSpent(why)
		} else {
			decision = a.who().Decide(remains)
		}
	}
	a.journalDecision(decision)
	return decision
}

// The continuation contains concrete execution facts, not a new assignment.
const checkpointCarryOnLead = "[carry on] The execution results below remain unresolved. " +
	"Use the original request and actual results to decide what work remains. " +
	"Do not repeat successful actions just to satisfy a progress record.\n\n"
