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
		Valid: def(
			spec{name: "brief", required: true},
			spec{name: "model"},
			// A comma-separated slice of the harness whitelist. Validate holds
			// it to that whitelist; the kind only holds its shape.
			spec{name: "tools"},
			spec{name: "max_turns", max: MaxTurns},
		),
	})
	Register(Kind{
		Name:   KindToolCall,
		Desc:   "one whitelisted tool, called with fixed arguments",
		MinDyn: DynFixed,
		Valid: def(
			spec{name: "tool", required: true},
			spec{name: "args"},
		),
	})
	Register(Kind{
		Name:   KindVerify,
		Desc:   "a check at a rung of the verification ladder",
		MinDyn: DynFixed,
		Valid: def(
			// Empty means the harness's own rung. A node may name a different
			// one; Validate refuses a node that reaches above the harness.
			spec{name: "ladder", words: verifyLadder},
			spec{name: "check"},
		),
	})
	Register(Kind{
		Name:   KindHumanGate,
		Desc:   "a stop, with a question, until a person answers",
		MinDyn: DynFixed,
		Valid: def(
			spec{name: "ask", required: true},
		),
	})
	Register(Kind{
		Name:   KindTrigger,
		Desc:   "what starts a run: hosted, idle, watch, or a source command",
		MinDyn: DynFixed,
		Valid: func(f Fields) error {
			base := def(
				spec{name: "source", required: true, words: triggerSources},
				spec{name: "command"},
			)
			if err := base(f); err != nil {
				return err
			}
			if f.Get("source") == TriggerCommand && f.Get("command") == "" {
				return errRequired("command", "source is "+TriggerCommand)
			}
			return nil
		},
	})
	Register(Kind{
		Name:   KindBranch,
		Desc:   "one successor, chosen at runtime by a reading",
		MinDyn: DynBranch,
		Valid: def(
			spec{name: "when", required: true},
		),
	})
	Register(Kind{
		Name:   KindLoopUntil,
		Desc:   "the same node again until a condition holds, bounded by rounds",
		MinDyn: DynBranch,
		Valid: def(
			spec{name: "until", required: true},
			spec{name: "max_rounds", max: MaxRounds},
		),
	})
	Register(Kind{
		Name:   KindParallelSplit,
		Desc:   "fan out to a runtime-chosen width, bounded",
		MinDyn: DynWidth,
		Valid: def(
			spec{name: "width", required: true, max: MaxWidth},
			spec{name: "over"},
		),
	})
	Register(Kind{
		Name:   KindParallelJoin,
		Desc:   "gather a split back into one thread",
		MinDyn: DynWidth,
		Valid: def(
			spec{name: "mode", words: joinModes},
		),
	})
	Register(Kind{
		Name:   KindSubharnessCall,
		Desc:   "another sub-harness, at a version pointer",
		MinDyn: DynRecursive,
		Valid: def(
			spec{name: "name", required: true},
			// 0, the default, means whatever the head pointer is at call time.
			// A pinned integer means that page and only that page.
			spec{name: "version", max: MaxVersion},
		),
	})
}
