package subharness

// The library-in-binary. Every kind here names machinery this binary already
// has: agent.loop is the session loop, tool.call is the tool call, human.gate
// is the ask the head already knows how to raise. A sub-harness arranges them;
// it never adds a tenth thing.
//
// Each kind carries the lowest dynamism rung a harness may declare and still
// contain it. That mapping is the enforcement point for the whole autonomy
// spectrum, so it is written once, here, beside the kind it bounds:
//
//	fixed      agent.loop, tool.call, verify, human.gate, trigger
//	branch     branch, loop.until          — the run picks a path
//	width      parallel.split, parallel.join — the run picks how many
//	recursive  subharness.call             — the run enters another harness
//
// meta and selfmod sit above these and unlock nothing new by themselves; they
// are rungs a harness declares when its agent.loop nodes rewrite their own
// briefs (meta) or when it may save a new version of itself (selfmod), and
// both are refused by anything that reads a lower rung.
const (
	KindAgentLoop      = "agent.loop"
	KindToolCall       = "tool.call"
	KindParallelSplit  = "parallel.split"
	KindParallelJoin   = "parallel.join"
	KindBranch         = "branch"
	KindLoopUntil      = "loop.until"
	KindHumanGate      = "human.gate"
	KindVerify         = "verify"
	KindSubharnessCall = "subharness.call"
	KindTrigger        = "trigger"
)

// The join modes. `all` is a barrier — every incoming edge must have run —
// and it is the default because a join that fires on a partial set is a
// deliberate choice, not a convenience.
const (
	JoinAll   = "all"
	JoinAny   = "any"
	JoinFirst = "first"
)

var joinModes = []string{JoinAll, JoinAny, JoinFirst}

// The trigger sources. `source.command` is the one that carries a payload: the
// shell command whose output is the trigger's reading.
const (
	TriggerHosted  = "hosted"
	TriggerIdle    = "idle"
	TriggerWatch   = "watch"
	TriggerCommand = "source.command"
)

var triggerSources = []string{TriggerHosted, TriggerIdle, TriggerWatch, TriggerCommand}

func init() {
	Register(Kind{
		Name:   KindAgentLoop,
		Desc:   "a session loop oriented by a brief, a model, and a slice of the whitelist",
		MinDyn: DynFixed,
		Specs: []spec{
			{name: "brief", required: true, about: "what this node is for, in enough words that somebody who read only this node could do the job"},
			{name: "model", about: "the model to run; empty means the session's own"},
			// A comma-separated slice of the harness whitelist. Validate holds
			// it to that whitelist; the kind only holds its shape.
			{name: "tools", about: "comma-separated, a SUBSET of the whitelist"},
			{name: "max_turns", max: MaxTurns, about: "how many turns the loop may take"},
		},
	})
	Register(Kind{
		Name:   KindToolCall,
		Desc:   "one whitelisted tool, called with fixed arguments",
		MinDyn: DynFixed,
		Specs: []spec{
			{name: "tool", required: true, about: "must be on the whitelist"},
			{name: "args", about: "the literal {{input}} means the previous step's output"},
		},
	})
	Register(Kind{
		Name:   KindVerify,
		Desc:   "a check at a rung of the verification ladder",
		MinDyn: DynFixed,
		Specs: []spec{
			// Empty means the harness's own rung. A node may name a different
			// one; Validate refuses a node that reaches above the harness.
			{name: "ladder", words: verifyLadder, about: "empty means the harness's own rung; it may name a LOWER rung, never a higher one"},
			{name: "check", about: "what is checked, as a sentence a judge can act on"},
		},
	})
	Register(Kind{
		Name:   KindHumanGate,
		Desc:   "a stop, with a question, until a person answers",
		MinDyn: DynFixed,
		Specs: []spec{
			{name: "ask", required: true, about: "the question"},
		},
	})
	Register(Kind{
		Name:   KindBranch,
		Desc:   "one successor, chosen at runtime by a reading",
		MinDyn: DynBranch,
		Specs: []spec{
			{name: "when", required: true, about: "a condition (see below)"},
		},
	})
	Register(Kind{
		Name:   KindLoopUntil,
		Desc:   "the same node again until a condition holds, bounded by rounds",
		MinDyn: DynBranch,
		Specs: []spec{
			{name: "until", required: true, about: "a condition"},
			{name: "max_rounds", max: MaxRounds, def: DefaultRounds, about: "how many re-readings are allowed"},
		},
	})
	Register(Kind{
		Name:   KindParallelSplit,
		Desc:   "fan out to a runtime-chosen width, bounded",
		MinDyn: DynWidth,
		Specs: []spec{
			{name: "width", required: true, max: MaxWidth, about: "how many lanes may open"},
			{name: "over", about: "what the lanes are over"},
		},
	})
	Register(Kind{
		Name:   KindParallelJoin,
		Desc:   "gather a split back into one thread",
		MinDyn: DynWidth,
		Specs: []spec{
			{name: "mode", words: joinModes, about: "default all is a barrier: every incoming edge must have run"},
		},
	})
	Register(Kind{
		Name:   KindSubharnessCall,
		Desc:   "another sub-harness, at a version pointer",
		MinDyn: DynRecursive,
		Specs: []spec{
			{name: "name", required: true, about: "another harness's name"},
			// 0, the default, means whatever the head pointer is at call time.
			// A pinned integer means that page and only that page.
			{name: "version", max: MaxVersion, about: "0 or absent means its head"},
		},
	})
	// The trigger's specs are named because its law is the specs plus one
	// conditional: declaring them twice would be the drift this file exists
	// to prevent.
	triggerSpecs := []spec{
		{name: "source", required: true, words: triggerSources, about: "what starts the run"},
		{name: "command", about: "the shell command whose output is the reading; REQUIRED when source is source.command"},
		// What a watch watches or how long idle must last, in the words
		// of whoever hosts that mode. This package insists there is one
		// and never reads it (trigger.go).
		{name: "spec", about: "what a watch watches or how long idle must last"},
		// A hosted trigger's allowed-args whitelist, comma-separated on
		// the same terms agent.loop's `tools` is. Empty means the command
		// takes none: an empty whitelist grants nothing, exactly as the
		// tool whitelist does.
		{name: "args", about: "a hosted command's allowed-argument list"},
	}
	Register(Kind{
		Name:   KindTrigger,
		Desc:   "what starts a run: hosted, idle, watch, or a source command",
		MinDyn: DynFixed,
		Specs:  triggerSpecs,
		Valid: func(f Fields) error {
			if err := def(triggerSpecs...)(f); err != nil {
				return err
			}
			if f.Get("source") == TriggerCommand && f.Get("command") == "" {
				return errRequired("command", "source is "+TriggerCommand)
			}
			return nil
		},
	})
}
