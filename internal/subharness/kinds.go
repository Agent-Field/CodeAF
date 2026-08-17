package subharness

import (
	"fmt"
	"sort"
	"strings"
)

// THE NODE-KIND REGISTRY: the whole vocabulary a sub-harness program is written
// in, and the one place a kind is described.
//
// It is a LIBRARY IN THE BINARY, not generated code. A file may arrange these
// nine kinds in any shape the ladders allow, and it may not introduce a tenth —
// which is what makes a registered harness a thing that can be read, approved
// and re-run rather than a program that has to be trusted.
//
// Every kind states four things here and nowhere else:
//
//   - what it IS, in the one line the preview card and /harness both print;
//   - which dynamism rung it costs (subharness.go's [Dynamism.Allows]);
//   - whether it NESTS, which is what the depth check walks;
//   - and how one is checked, which is [kindInfo.validate].
//
// A tenth kind is a row in this table plus a case in the runner (exec.go). It is
// deliberately not less than that: a kind the validator knows and the runner
// does not is a program that passes review and stalls.

// Kind names one node kind.
type Kind string

const (
	// KindAgentLoop is a worker with a brief and a bounded number of turns: the
	// ordinary unit of work, oriented by a model and this harness's whitelist.
	KindAgentLoop Kind = "agent.loop"
	// KindToolCall is one tool, called with fixed arguments. No model, no turn,
	// no judgement — the deterministic half of a program.
	KindToolCall Kind = "tool.call"
	// KindParallelSplit runs its lanes at once and joins them.
	KindParallelSplit Kind = "parallel.split"
	// KindParallelJoin is the split's other end. IT IS NEVER WRITTEN IN A FILE:
	// the lanes are inside the split, so the join has no arguments of its own
	// and nothing to be written about. It exists as a kind because it exists in
	// the TRACE — the run records the moment the lanes landed as a node of its
	// own, so a trace DAG has the diamond in it that the program's shape implies
	// (run.go). Validation refuses it in a program for exactly that reason.
	KindParallelJoin Kind = "parallel.join"
	// KindBranch picks one arm by a condition over what the last node produced.
	KindBranch Kind = "branch"
	// KindLoopUntil repeats its body until a condition holds or the rounds run
	// out. It is the only loop, and it is bounded in the file.
	KindLoopUntil Kind = "loop.until"
	// KindHumanGate stops and asks a person. It is the one node that can end a
	// run because somebody said no.
	KindHumanGate Kind = "human.gate"
	// KindVerify climbs one rung of the verification ladder against what came
	// before it.
	KindVerify Kind = "verify"
	// KindSubharnessCall runs another registered harness here, at a pinned
	// version or at whatever is current.
	KindSubharnessCall Kind = "subharness.call"
	// KindTrigger is what starts the harness when a person does not. It is
	// declarative: it may only appear as the FIRST node of a program, and
	// running one is a no-op that records what the run was started by.
	KindTrigger Kind = "trigger"
)

// kindInfo is one row of the registry.
type kindInfo struct {
	kind Kind
	// blurb is the one line. It is written for a person reading a card, not for
	// a schema.
	blurb string
	// needs is the dynamism rung a program pays to use this kind.
	needs Dynamism
	// nests says this kind carries child steps, which is what the depth and the
	// count walks follow.
	nests bool
	// writable says a file may contain this kind. Only parallel.join is not.
	writable bool
	// validate checks one node of this kind, in the words a builder can act on.
	// The harness is passed for the whitelist and the ladders.
	validate func(h *Harness, n Node) error
}

var kindRegistry = []kindInfo{
	{
		kind: KindAgentLoop, blurb: "a worker with a brief, bounded in turns",
		needs: DynFixed, writable: true,
		validate: func(h *Harness, n Node) error {
			if strings.TrimSpace(n.Prompt) == "" {
				return fmt.Errorf("needs a prompt")
			}
			if n.MaxTurns < 0 {
				return fmt.Errorf("max_turns cannot be negative")
			}
			if n.MaxTurns > MaxTurns {
				return fmt.Errorf("max_turns %d is over the ceiling of %d", n.MaxTurns, MaxTurns)
			}
			return whitelisted(h, n.Tools)
		},
	},
	{
		kind: KindToolCall, blurb: "one tool, with fixed arguments",
		needs: DynFixed, writable: true,
		validate: func(h *Harness, n Node) error {
			if strings.TrimSpace(n.Tool) == "" {
				return fmt.Errorf("needs a tool")
			}
			return whitelisted(h, []string{n.Tool})
		},
	},
	{
		kind: KindParallelSplit, blurb: "lanes at once, joined",
		needs: DynWidth, nests: true, writable: true,
		validate: func(h *Harness, n Node) error {
			switch {
			case len(n.Lanes) < 2:
				return fmt.Errorf("needs at least two lanes")
			case len(n.Lanes) > MaxLanes:
				return fmt.Errorf("has %d lanes, over the ceiling of %d", len(n.Lanes), MaxLanes)
			case !n.Join.Valid():
				return fmt.Errorf("join %q is neither all nor first", n.Join)
			}
			// The cap is the dynamism rung's own number, and width is the rung
			// this kind costs — so a harness that granted itself width 3 may not
			// then open four lanes.
			if h.Cap > 0 && len(n.Lanes) > h.Cap {
				return fmt.Errorf("has %d lanes, over this harness's cap of %d", len(n.Lanes), h.Cap)
			}
			seen := map[string]bool{}
			for _, lane := range n.Lanes {
				name := strings.TrimSpace(lane.Name)
				if name == "" {
					return fmt.Errorf("every lane needs a name")
				}
				if seen[name] {
					return fmt.Errorf("two lanes are both called %q", name)
				}
				seen[name] = true
				if len(lane.Steps) == 0 {
					return fmt.Errorf("lane %q is empty", name)
				}
			}
			return nil
		},
	},
	{
		kind: KindParallelJoin, blurb: "where the lanes land",
		needs: DynWidth, writable: false,
		validate: func(*Harness, Node) error { return nil },
	},
	{
		kind: KindBranch, blurb: "one arm, chosen by a condition",
		needs: DynBranch, nests: true, writable: true,
		validate: func(h *Harness, n Node) error {
			if len(n.Cases) == 0 {
				return fmt.Errorf("needs at least one case")
			}
			for at, one := range n.Cases {
				if err := ValidCondition(one.When); err != nil {
					return fmt.Errorf("case %d: %w", at+1, err)
				}
				if len(one.Steps) == 0 {
					return fmt.Errorf("case %d (%s) is empty", at+1, one.When)
				}
			}
			return nil
		},
	},
	{
		kind: KindLoopUntil, blurb: "repeat until it holds, bounded",
		needs: DynBranch, nests: true, writable: true,
		validate: func(h *Harness, n Node) error {
			if len(n.Steps) == 0 {
				return fmt.Errorf("has an empty body")
			}
			if err := ValidCondition(n.Until); err != nil {
				return fmt.Errorf("until: %w", err)
			}
			switch {
			case n.Max < 0:
				return fmt.Errorf("max cannot be negative")
			case n.Max > MaxRounds:
				return fmt.Errorf("max %d is over the ceiling of %d", n.Max, MaxRounds)
			case h.Cap > 0 && n.Max > h.Cap:
				return fmt.Errorf("max %d is over this harness's cap of %d", n.Max, h.Cap)
			}
			return nil
		},
	},
	{
		kind: KindHumanGate, blurb: "stop and ask a person",
		needs: DynFixed, writable: true,
		validate: func(_ *Harness, n Node) error {
			if strings.TrimSpace(n.Prompt) == "" {
				return fmt.Errorf("needs a prompt — what is the person being asked")
			}
			return nil
		},
	},
	{
		kind: KindVerify, blurb: "climb one rung of the ladder",
		needs: DynFixed, writable: true,
		validate: func(h *Harness, n Node) error {
			if !n.Rung.Valid() {
				return fmt.Errorf("rung %q is not on the ladder (%s)", n.Rung, rungWords())
			}
			rung := n.Rung.Or(RungAccept)
			// The harness's own rung is the ceiling. A node that checks HARDER
			// than the harness was approved to check is a node the person did not
			// agree to pay for — the top rungs are a second worker or a person.
			ceiling := h.Verify.Or(RungAccept)
			height, _ := rung.Height()
			top, _ := ceiling.Height()
			if height > top {
				return fmt.Errorf("verifies at %s, above this harness's ladder of %s", rung, ceiling)
			}
			if rung != RungAccept && rung != RungHuman && strings.TrimSpace(n.Check) == "" {
				return fmt.Errorf("needs a check — the command to run or the question to ask")
			}
			return nil
		},
	},
	{
		kind: KindSubharnessCall, blurb: "run another harness here",
		needs: DynRecursive, writable: true,
		validate: func(h *Harness, n Node) error {
			if err := ValidName(n.Call); err != nil {
				return err
			}
			if n.Call == h.Name {
				// Direct self-call. The registry cannot see the whole cycle from
				// here — the other entries are on disk — but it can see this one,
				// and the runner's depth cap catches the rest.
				return fmt.Errorf("calls itself; use loop.until for repetition")
			}
			if n.CallVersion < 0 {
				return fmt.Errorf("call_version cannot be negative")
			}
			return nil
		},
	},
	{
		kind: KindTrigger, blurb: "what starts this when nobody does",
		needs: DynFixed, writable: true,
		validate: func(_ *Harness, n Node) error {
			if !n.On.Valid() {
				return fmt.Errorf("on %q is not a trigger (%s)", n.On, triggerWords())
			}
			if n.On != TriggerHosted && strings.TrimSpace(n.Spec) == "" {
				return fmt.Errorf("trigger %s needs a spec — the cadence, the path, or the command", n.On)
			}
			return nil
		},
	},
}

// lookup finds a kind's row.
func lookup(kind Kind) (kindInfo, bool) {
	for _, info := range kindRegistry {
		if info.kind == kind {
			return info, true
		}
	}
	return kindInfo{}, false
}

// kindDynamism is the rung a kind costs.
func kindDynamism(kind Kind) (Dynamism, bool) {
	info, ok := lookup(kind)
	if !ok {
		return "", false
	}
	return info.needs, true
}

// Kinds is the registry as a person reads it: every kind, its line, and the
// dynamism rung it costs. It is what /harness prints when somebody asks what a
// program may be made of, and what the builder's tool description is generated
// from — one table, every reader.
func Kinds() []KindDoc {
	docs := make([]KindDoc, 0, len(kindRegistry))
	for _, info := range kindRegistry {
		if !info.writable {
			continue
		}
		docs = append(docs, KindDoc{Kind: info.kind, Blurb: info.blurb, Needs: info.needs, Nests: info.nests})
	}
	return docs
}

// KindDoc is one registry row, published.
type KindDoc struct {
	Kind  Kind
	Blurb string
	Needs Dynamism
	Nests bool
}

// KnownKind reports whether a word names a kind at all.
func KnownKind(kind Kind) bool {
	_, ok := lookup(kind)
	return ok
}

// children is a node's nested steps, flattened, in the order they run. It is the
// one walk over nesting: the counter, the depth check, the id check and the card
// all use it, so a kind that grows children grows them for every reader at once.
func children(n Node) []Node {
	var out []Node
	for _, one := range n.Cases {
		out = append(out, one.Steps...)
	}
	out = append(out, n.Else...)
	out = append(out, n.Steps...)
	for _, lane := range n.Lanes {
		out = append(out, lane.Steps...)
	}
	return out
}

// whitelisted checks a list of tool names against the harness's whitelist.
//
// AN EMPTY WHITELIST IS NO TOOLS. It is the one place in this package where the
// permissive reading would be the dangerous one: a harness that forgot to say
// what it needs would otherwise be handed the whole belt by omission, and the
// tool list is the single sentence a person reads on the card to decide what
// this thing can reach.
func whitelisted(h *Harness, tools []string) error {
	for _, want := range tools {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		if !h.allows(want) {
			if len(h.Tools) == 0 {
				return fmt.Errorf("uses %q but this harness has no tool whitelist", want)
			}
			return fmt.Errorf("uses %q, which is not in this harness's tools (%s)",
				want, strings.Join(h.Tools, ", "))
		}
	}
	return nil
}

// allows reports whether a tool is on this harness's whitelist.
func (h *Harness) allows(tool string) bool {
	tool = strings.TrimSpace(tool)
	for _, listed := range h.Tools {
		if strings.TrimSpace(listed) == tool {
			return true
		}
	}
	return false
}

// permits is the two-ladder question asked once: may a harness at these rungs
// contain this kind at all.
func (h *Harness) permits(kind Kind) error {
	info, ok := lookup(kind)
	if !ok {
		return fmt.Errorf("%q is not a node kind (%s)", kind, kindWords())
	}
	if !info.writable {
		return fmt.Errorf("%q is recorded by a run, never written in a program", kind)
	}
	if !h.Dynamism.Allows(kind) {
		return fmt.Errorf("%q needs dynamism %s; this harness is %s",
			kind, info.needs, h.Dynamism.Or(DynFixed))
	}
	return nil
}

// ── the words, for error messages ───────────────────────────────────────────

func kindWords() string {
	words := make([]string, 0, len(kindRegistry))
	for _, info := range kindRegistry {
		if info.writable {
			words = append(words, string(info.kind))
		}
	}
	sort.Strings(words)
	return strings.Join(words, ", ")
}

func rungWords() string {
	words := make([]string, 0, len(rungOrder))
	for _, rung := range rungOrder {
		words = append(words, string(rung))
	}
	return strings.Join(words, " < ")
}

func triggerWords() string {
	words := make([]string, 0, len(triggerOrder))
	for _, kind := range triggerOrder {
		words = append(words, string(kind))
	}
	return strings.Join(words, ", ")
}
