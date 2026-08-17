// Package subharness is the registry of sub-harnesses: named, durable programs
// written over a small fixed set of node kinds, stored as plain files under the
// state root, and pinned by an integer revision.
//
// A sub-harness is not a second engine and not generated code. It is data — an
// entry naming a program whose nodes are drawn from the kind registry in
// kinds.go, every one of which is a thing this binary already knows how to do:
// run an agent loop, call a tool, fan out and join, branch, repeat until,
// ask a human, verify, call another sub-harness, be triggered. The library is in
// the binary; the file only arranges it. That is what keeps a sub-harness
// reviewable: there is a closed list of what a node can be, and anything outside
// it fails to parse rather than becoming a new execution path nobody audited.
//
// Three facts about an entry are load-bearing everywhere else:
//
//   - The program is a DAG. Needs are edges, and a node with no needs is a root.
//     Cycles are refused at validation, so the only repetition a sub-harness can
//     express is the bounded one loop.until declares out loud.
//   - Tools are a whitelist. A tool.call may only name a tool the entry lists,
//     and an agent.loop may only narrow that list, never widen it. The whitelist
//     is the entry's blast radius, written where a reader looks first.
//   - Revision is an integer pointer, not a hash of the bytes. v1 and v2 are two
//     harnesses that happen to share a name, and a caller that pins a revision is
//     promised the thing it read — which is why Registry.Save refuses to rewrite
//     a revision in place.
package subharness

import (
	"fmt"
	"sort"
	"strings"
)

// Entry is one durable sub-harness: who it is, what it runs, what it may touch,
// how hard it checks itself, and how far it may deviate from what is written.
type Entry struct {
	Name     string `json:"name"`
	Desc     string `json:"desc,omitempty"`
	Author   string `json:"author,omitempty"`
	Revision int    `json:"revision"`

	// Nodes is the program: a DAG over the node kinds, in no required order.
	Nodes []Node `json:"nodes"`

	// Tools is the whitelist every tool-touching node is bounded by. Empty means
	// a harness that calls no tools at all, which is a real and useful shape —
	// not a harness that may call anything.
	Tools []string `json:"tools,omitempty"`

	// Verify is the default rung for verify nodes that name none. Empty leaves
	// each node to say its own, and a verify node with neither is refused: the
	// point of a ladder is that a rung was chosen.
	Verify Rung `json:"verify,omitempty"`

	// Dynamism bounds how far a run may depart from the written program.
	Dynamism Dynamism `json:"dynamism,omitempty"`

	// Tests are the entry's own acceptance cases, carried with it so a revision
	// bump can be judged against what the last revision was known to do.
	Tests []Test `json:"tests,omitempty"`
}

// Node is one step. Kind picks which of the payload pointers is read; the rest
// must be nil, so a node can never quietly be two things at once.
type Node struct {
	ID    string   `json:"id"`
	Kind  Kind     `json:"kind"`
	Needs []string `json:"needs,omitempty"`

	// Brief is the instruction the node carries, in the voice of whoever reads
	// it: a prompt for an agent.loop, a question for a human.gate, a claim for a
	// verify. Kinds that read no prose ignore it.
	Brief string `json:"brief,omitempty"`

	Loop    *AgentLoop `json:"loop,omitempty"`
	Call    *ToolCall  `json:"call,omitempty"`
	Split   *Split     `json:"split,omitempty"`
	Branch  *Branch    `json:"branch,omitempty"`
	Until   *LoopUntil `json:"until,omitempty"`
	Gate    *HumanGate `json:"gate,omitempty"`
	Check   *Check     `json:"check,omitempty"`
	Sub     *SubCall   `json:"sub,omitempty"`
	Trigger *Trigger   `json:"trigger,omitempty"`
}

// AgentLoop is the payload of an agent.loop node: a model, and the slice of the
// entry's whitelist this loop is allowed to reach.
type AgentLoop struct {
	// Model is a slot word (talk/work/boost) or a model word. Empty means the
	// job's model, resolved where the run is dispatched and not here.
	Model string `json:"model,omitempty"`
	// Tools narrows the entry whitelist. Empty means the whole of it.
	Tools []string `json:"tools,omitempty"`
	// MaxTurns bounds the loop. Zero means the dispatcher's default.
	MaxTurns int `json:"max_turns,omitempty"`
}

// ToolCall is one named call with literal arguments — the typed end of the
// autonomy spectrum, where nothing is decided at run time.
type ToolCall struct {
	Tool string            `json:"tool"`
	Args map[string]string `json:"args,omitempty"`
}

// Split fans the nodes that need it into parallel branches. Width is the cap on
// how many run at once; the matching parallel.join is what waits.
type Split struct {
	Width int `json:"width,omitempty"`
}

// Branch chooses one downstream node by a condition read from an upstream
// result. Else is taken when no case matches; empty Else means the run stops
// on that path rather than picking arbitrarily.
type Branch struct {
	// On names the node whose result is read.
	On    string       `json:"on"`
	Cases []BranchCase `json:"cases"`
	Else  string       `json:"else,omitempty"`
}

// BranchCase is one arm: a match against the watched result, and the node ID it
// hands control to.
type BranchCase struct {
	Match string `json:"match"`
	Goto  string `json:"goto"`
}

// LoopUntil repeats a span of nodes until a condition holds or the cap is spent.
// Max is mandatory at validation: an unbounded loop in a file is the one shape
// this package exists to make impossible.
type LoopUntil struct {
	Body  []string `json:"body"`
	Until string   `json:"until"`
	Max   int      `json:"max"`
}

// HumanGate stops the run for a person. Blocking gates hold the run; a
// non-blocking gate records the ask and carries on, which is how a harness asks
// for review it does not need to wait for.
type HumanGate struct {
	Blocking bool `json:"blocking"`
}

// Check is a verify node's payload: which rung of the ladder, and what it reads.
type Check struct {
	Rung Rung `json:"rung,omitempty"`
	// On names the nodes whose results are checked. Empty means this node's
	// direct needs, which is the ordinary case and stays unwritten.
	On []string `json:"on,omitempty"`
	// Script is a repo-relative executable for the rungs that run one. Exit 0
	// passes; anything else fails with its output as the reason.
	Script string `json:"script,omitempty"`
}

// SubCall runs another registered sub-harness as one node. Revision pins it: a
// zero revision follows whatever is current, which is convenient during
// authoring and a hazard in anything anyone depends on.
type SubCall struct {
	Harness  string `json:"harness"`
	Revision int    `json:"revision,omitempty"`
}

// Test is one acceptance case carried with the entry.
type Test struct {
	Name   string `json:"name"`
	Input  string `json:"input,omitempty"`
	Expect string `json:"expect,omitempty"`
}

// Rung is one step of the verification ladder, from cheapest to most expensive.
type Rung string

const (
	RungAccept      Rung = "accept"
	RungSchema      Rung = "schema"
	RungInvariants  Rung = "invariants"
	RungLoop        Rung = "loop"
	RungReport      Rung = "report"
	RungRederive    Rung = "rederive"
	RungAdversarial Rung = "adversarial"
	RungHuman       Rung = "human"
)

var rungs = map[Rung]bool{
	RungAccept: true, RungSchema: true, RungInvariants: true, RungLoop: true,
	RungReport: true, RungRederive: true, RungAdversarial: true, RungHuman: true,
}

// KnownRung reports whether a rung is on the ladder.
func KnownRung(r Rung) bool { return rungs[r] }

// Level is one step of the dynamism ladder: how much of the program a run may
// decide for itself.
type Level string

const (
	// LevelFixed runs exactly what is written.
	LevelFixed Level = "fixed"
	// LevelBranch lets the run pick arms.
	LevelBranch Level = "branch"
	// LevelWidth lets the run choose fan-out inside the cap.
	LevelWidth Level = "width"
	// LevelMetaprompt lets a node rewrite the briefs beneath it.
	LevelMetaprompt Level = "metaprompt"
	// LevelRecursive lets a run call sub-harnesses it chose rather than named.
	LevelRecursive Level = "recursive"
	// LevelSelfmod lets a run write the next revision of its own entry.
	LevelSelfmod Level = "selfmod"
)

var levels = map[Level]bool{
	LevelFixed: true, LevelBranch: true, LevelWidth: true,
	LevelMetaprompt: true, LevelRecursive: true, LevelSelfmod: true,
}

// Dynamism is the ladder rung plus its integer budget. Cap means whatever the
// level spends: branches taken, extra width, rewrites, recursive calls. Zero is
// the dispatcher's default, and it is never unlimited.
type Dynamism struct {
	Level Level `json:"level,omitempty"`
	Cap   int   `json:"cap,omitempty"`
}

// Validate is the whole contract an entry must meet before anything runs it or
// the registry stores it. It answers with the first fault it finds, named by
// node, because a file the author has to bisect is a file the author stops
// writing.
func (e Entry) Validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("subharness: entry has no name")
	}
	if err := ValidName(e.Name); err != nil {
		return err
	}
	if e.Revision < 1 {
		return fmt.Errorf("subharness %q: revision must be 1 or greater, got %d", e.Name, e.Revision)
	}
	if len(e.Nodes) == 0 {
		return fmt.Errorf("subharness %q: no nodes", e.Name)
	}
	if e.Verify != "" && !KnownRung(e.Verify) {
		return fmt.Errorf("subharness %q: unknown verify rung %q", e.Name, e.Verify)
	}
	if e.Dynamism.Level != "" && !levels[e.Dynamism.Level] {
		return fmt.Errorf("subharness %q: unknown dynamism level %q", e.Name, e.Dynamism.Level)
	}
	if e.Dynamism.Cap < 0 {
		return fmt.Errorf("subharness %q: dynamism cap must not be negative", e.Name)
	}

	byID := make(map[string]Node, len(e.Nodes))
	for _, n := range e.Nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return fmt.Errorf("subharness %q: a node has no id", e.Name)
		}
		if _, dup := byID[id]; dup {
			return fmt.Errorf("subharness %q: two nodes share the id %q", e.Name, id)
		}
		byID[id] = n
	}
	for _, n := range e.Nodes {
		for _, need := range n.Needs {
			if _, ok := byID[need]; !ok {
				return fmt.Errorf("subharness %q: node %q needs unknown node %q", e.Name, n.ID, need)
			}
			if need == n.ID {
				return fmt.Errorf("subharness %q: node %q needs itself", e.Name, n.ID)
			}
		}
		info, known := KindFor(n.Kind)
		if !known {
			return fmt.Errorf("subharness %q: node %q has unknown kind %q", e.Name, n.ID, n.Kind)
		}
		if err := info.Validate(e, n, byID); err != nil {
			return fmt.Errorf("subharness %q: node %q: %w", e.Name, n.ID, err)
		}
	}
	return acyclic(e, byID)
}

// acyclic walks the needs edges depth-first. It reports the node the cycle was
// entered at rather than the whole ring: the author needs a place to look, and
// the ring is obvious from there.
func acyclic(e Entry, byID map[string]Node) error {
	const (
		open = 1
		done = 2
	)
	state := make(map[string]int, len(byID))
	var walk func(id string) error
	walk = func(id string) error {
		switch state[id] {
		case done:
			return nil
		case open:
			return fmt.Errorf("subharness %q: node %q is part of a needs cycle", e.Name, id)
		}
		state[id] = open
		for _, need := range byID[id].Needs {
			if err := walk(need); err != nil {
				return err
			}
		}
		state[id] = done
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := walk(id); err != nil {
			return err
		}
	}
	return nil
}

// Node returns the node with an id, and whether the entry has one.
func (e Entry) Node(id string) (Node, bool) {
	for _, n := range e.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// Roots are the nodes nothing hands control to: where a run can begin.
//
// Depending on nothing and being a root are two different facts, and the
// difference is the trigger. A hosted trigger's entry node has no needs — it
// waits for no result, and the trigger does not precede it in the DAG — but the
// run does not start there, it arrives there when the command is typed. So a
// node is a root when nothing needs-edges into it AND no trigger enters it and
// no branch arm goes to it. Everything else is reached.
func (e Entry) Roots() []Node {
	reached := make(map[string]bool, len(e.Nodes))
	for _, n := range e.Nodes {
		if n.Trigger != nil {
			reached[n.Trigger.Entry] = true
		}
		if n.Branch != nil {
			for _, c := range n.Branch.Cases {
				reached[c.Goto] = true
			}
			reached[n.Branch.Else] = true
		}
	}
	roots := make([]Node, 0, 1)
	for _, n := range e.Nodes {
		if len(n.Needs) == 0 && !reached[n.ID] {
			roots = append(roots, n)
		}
	}
	return roots
}

// Allows reports whether a tool name is on the entry's whitelist.
func (e Entry) Allows(tool string) bool {
	tool = strings.TrimSpace(tool)
	for _, t := range e.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

// ValidName holds entry names to what can be a file name and a command word at
// once: lowercase letters, digits, dash and underscore. The registry path is
// plain — the name IS the file — so a name that needs escaping is a name that
// would eventually escape the directory.
func ValidName(name string) error {
	if name == "" {
		return fmt.Errorf("subharness: empty name")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("subharness: name %q may use only lowercase letters, digits, dash and underscore", name)
		}
	}
	return nil
}
