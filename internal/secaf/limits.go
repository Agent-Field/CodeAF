package secaf

// Each of sec's agents is bounded by what its own work takes.
//
// sec-af gave every agent one cap, fifty turns and thirty minutes
// (SEC_AF_MAX_TURNS, the harness's wait). The owner's whole-repository audit of
// a mid-sized Rust repository on 2026-10-05 measured what each agent actually
// used, and the figures below are that measurement with headroom: about half
// again over the longest session seen, more where a session was cut at the
// old cap. An agent with no measurement keeps the old cap.
//
//	agent              sessions  turns (median, longest)  longest time
//	hunt scan               11    38, 50 (cut)              10m18s
//	hunt enrich             67     7, 30                    11m18s
//	prove tracer            22     9, 30                     4m28s
//	prove sanitization      22     7, 27                     4m10s
//	prove exploit           22    12, 40                    10m40s
//	recon config scanner     1    50 (cut)                   3m43s
//	recon architecture       1    32                         2m42s
//	recon data flow          1    33                         3m25s
//	recon dependencies       1    12                         2m14s
//	recon security context   1    11                           15s
//	dedup (chain)            1    16                        10m00s
//
// A person's --max-turns or --session-wall at a shell is one figure for every
// agent, and overrides the table.

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/agentsession"
	"github.com/Agent-Field/codeaf/internal/agentsession/appx"
)

// agentLimits is each agent's bounds, by the name its scratch folder starts
// with after `secaf-` (every agent makes one with os.MkdirTemp, and the name is
// the agent's own constant, where its label names the file it is looking at).
// The first prefix that matches wins.
var agentLimits = []struct {
	kind   string
	limits agentsession.Limits
}{
	{"hunt-scan", agentsession.Limits{Turns: 75, Wall: 20 * time.Minute}},
	{"hunt-enrich", agentsession.Limits{Turns: 45, Wall: 20 * time.Minute}},
	{"prove-tracer", agentsession.Limits{Turns: 45, Wall: 10 * time.Minute}},
	{"prove-sanitization", agentsession.Limits{Turns: 40, Wall: 10 * time.Minute}},
	{"prove-exploit", agentsession.Limits{Turns: 60, Wall: 20 * time.Minute}},
	{"recon-config-scanner", agentsession.Limits{Turns: 75, Wall: 15 * time.Minute}},
	{"recon-architecture", agentsession.Limits{Turns: 50, Wall: 10 * time.Minute}},
	{"recon-data-flow", agentsession.Limits{Turns: 50, Wall: 10 * time.Minute}},
	{"recon-dependencies", agentsession.Limits{Turns: 30, Wall: 10 * time.Minute}},
	{"recon-security-context", agentsession.Limits{Turns: 30, Wall: 5 * time.Minute}},
	{"dedup", agentsession.Limits{Turns: 30, Wall: 20 * time.Minute}},
}

// limitsFor is the bounds sec gives the agent a session was started for: the
// person's own figure when they set one, else the agent's from the table, and
// zero (the App's default) for an agent the table does not name.
func limitsFor(o options) func(appx.HarnessOptions) agentsession.Limits {
	return func(opts appx.HarnessOptions) agentsession.Limits {
		own := agentLimitsOf(opts.Cwd)
		if o.maxTurns > 0 {
			own.Turns = o.maxTurns
		}
		if o.sessionWall > 0 {
			own.Wall = o.sessionWall
		}
		return own
	}
}

// agentLimitsOf is the table's bounds for the agent whose scratch folder is
// cwd, or zero for one it does not name.
func agentLimitsOf(cwd string) agentsession.Limits {
	name, ours := strings.CutPrefix(filepath.Base(strings.TrimSpace(cwd)), "secaf-")
	if !ours {
		return agentsession.Limits{}
	}
	for _, row := range agentLimits {
		if strings.HasPrefix(name, row.kind+"-") {
			return row.limits
		}
	}
	return agentsession.Limits{}
}

// defaultAgentTurns and defaultAgentWall are sec-af's own one cap, kept for
// every agent the table does not name.
const (
	defaultAgentTurns = 50
	defaultAgentWall  = 30 * time.Minute
)

// sessionsAtOnce and callsAtOnce are how many agent sessions and single model
// calls run at once across the audit: sec-af's own fan-outs multiply — four
// hunters each enriching five locations — and the old harness capped its
// processes at eight for the same reason. --sessions sets the first.
const (
	sessionsAtOnce = 8
	callsAtOnce    = 8
)

// retries is how many times a model call that failed on the way is sent
// again, past the model API's own ladder of services.
const retries = 2

// sessionPolicy is how every one of sec's agent sessions reads and answers.
// These are the figures sec-af's harness ran on, now said here as sec's own
// (agentsession has no defaults; its Policy says why).
var sessionPolicy = agentsession.Policy{
	// The old harness's own count of asks for an answer of the right shape.
	FollowUps: 2,
	// About a hundred thousand tokens, which every model a person is likely to
	// seat holds with room for the answer.
	ContextChars: 400_000,
	AnswerNow:    "Stop reading now and give your answer from what you have found, in the form the system message asks for.",
	Tools: agentsession.ToolLimits{
		ReadLines: 400, ReadBytes: 48 << 10, ReadLineRunes: 2000,
		ListEntries: 400, GlobMatches: 300,
		// A file past two megabytes is a bundle, a dump or a dataset, and its
		// lines are not code to trace.
		GrepMatches: 200, GrepFileBytes: 2 << 20, GrepLineRunes: 240,
	},
}
