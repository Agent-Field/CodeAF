package praf

// Each of a review's agents runs under turn and time limits of its own,
// keyed on the label its reasoner gives its session (reasoners.Label*).
//
// EACH AGENT'S LIMIT IS ITS OWN, AND PR'S ALONE. A reviewer reading a
// dimension of the change and a lens choosing what to review do different
// amounts of reading, so one limit for all either cut the one or let the
// other wander; and the agent loop is shared with sec, so a limit tuned for
// its agents must never become review's. The owner's call (2026-10-06) is each
// program's own figures for each of its agents.
//
// THE FIGURES ARE A CHOICE OF DEPTH, NOT A MEASUREMENT. A live review's
// sessions read until their limit whatever it was, so the limit sets how
// long a review takes. A session at its limit is still made to answer, so a
// limit costs depth, never the answer.

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/agentsession"
	secappx "github.com/Agent-Field/codeaf/internal/agentsession/appx"
	"github.com/Agent-Field/codeaf/internal/praf/reasoners"
)

// agentLimits is each agent's turns and time.
var agentLimits = map[string]agentsession.Limits{
	reasoners.LabelIntake:         {Turns: 15, Wall: 5 * time.Minute},
	reasoners.LabelAnatomy:        {Turns: 30, Wall: 10 * time.Minute},
	reasoners.LabelPlan:           {Turns: 15, Wall: 5 * time.Minute},
	reasoners.LabelLens:           {Turns: 25, Wall: 10 * time.Minute},
	reasoners.LabelReviewer:       {Turns: 30, Wall: 15 * time.Minute},
	reasoners.LabelPostWorthiness: {Turns: 10, Wall: 5 * time.Minute},
	reasoners.LabelEvidence:       {Turns: 30, Wall: 10 * time.Minute},
	reasoners.LabelChallenge:      {Turns: 30, Wall: 10 * time.Minute},
	reasoners.LabelDeepen:         {Turns: 25, Wall: 10 * time.Minute},
	reasoners.LabelCrossRef:       {Turns: 25, Wall: 10 * time.Minute},
	reasoners.LabelObligations:    {Turns: 20, Wall: 10 * time.Minute},
	reasoners.LabelCoverage:       {Turns: 15, Wall: 5 * time.Minute},
}

// otherLimits is what a session no row names runs under.
var otherLimits = agentsession.Limits{Turns: 20, Wall: 10 * time.Minute}

// sessionsAtOnce and callsAtOnce are how many agent sessions and single model
// calls run at once across a review: pr-af's served node ran its reviewers
// eight at a time. --sessions sets the first.
const (
	sessionsAtOnce = 8
	callsAtOnce    = 8
)

// retries is how many times a model call that failed on the way is sent
// again, past the model API's own ladder of services.
const retries = 2

// sessionPolicy is how every one of review's agent sessions reads and answers.
// The figures are the ones its first live reviews ran on, said here as review's
// own: agentsession has no defaults, so that nothing tuned for sec's agents
// reaches review's, or the reverse.
var sessionPolicy = agentsession.Policy{
	// Asks for an answer of the right shape after one that was not.
	FollowUps: 2,
	// About a hundred thousand tokens, which every model a person is likely to
	// seat holds with room for the answer.
	ContextChars: 400_000,
	AnswerNow:    "Stop reading now and give your answer from what you have found, in the form the system message asks for.",
	Tools: agentsession.ToolLimits{
		// A line is cut at 2,000 characters, so the contexts a review writes
		// for its sessions are indented JSON (reasoners.writeContextFile).
		ReadLines: 400, ReadBytes: 48 << 10, ReadLineRunes: 2000,
		ListEntries: 400, GlobMatches: 300,
		// A file past two megabytes is a bundle, a dump or a dataset, and its
		// lines are not code to trace.
		GrepMatches: 200, GrepFileBytes: 2 << 20, GrepLineRunes: 240,
	},
}

// limitsFor is the run's per-agent limits, with the person's --max-turns and
// --session-wall, when set, in place of every agent's own.
func limitsFor(o options) func(secappx.HarnessOptions) agentsession.Limits {
	return func(session secappx.HarnessOptions) agentsession.Limits {
		limits, ok := agentLimits[session.Label]
		if !ok {
			limits = otherLimits
		}
		if o.maxTurns > 0 {
			limits.Turns = o.maxTurns
		}
		if o.sessionWall > 0 {
			limits.Wall = o.sessionWall
		}
		return limits
	}
}
