package praf

// Each of a review's agents runs under turn and time limits of its own,
// keyed on the label its reasoner gives its session (reasoners.Label*).
//
// THE FIGURES ARE MEASURED, AND EACH AGENT'S IS ITS OWN. A reviewer reading
// a dimension of the change and a lens choosing what to review do different
// amounts of reading, so one limit for all either cut the one or let the
// other wander; the owner's call (2026-10-06) is each agent's own, as sec's
// are. A session at its limit is still made to answer, so a limit costs
// depth, never the answer.

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
