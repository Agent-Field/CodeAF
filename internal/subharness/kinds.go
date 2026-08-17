package subharness

import (
	"fmt"
	"sort"
	"strings"
)

// The registry of node kinds. This closed list is the whole vocabulary a
// sub-harness file may use, and it is a registry rather than a switch statement
// for one reason: every reader — the validator, the authoring prompt, the
// documentation, whatever surface lists what a harness can be made of — must be
// looking at the same list. A kind that exists in the executor but not here is
// unreachable from a file, and a kind here that no executor serves fails loudly
// at dispatch instead of silently at parse.

// Kind names one node kind.
type Kind string

const (
	// KindAgentLoop is a model in a tool loop — the loop-end of the autonomy
	// spectrum, where what happens next is decided at run time.
	KindAgentLoop Kind = "agent.loop"
	// KindToolCall is one named call with literal arguments — the typed end,
	// where nothing is decided at run time.
	KindToolCall Kind = "tool.call"
	// KindParallelSplit fans its dependents out; KindParallelJoin waits for them.
	KindParallelSplit Kind = "parallel.split"
	KindParallelJoin  Kind = "parallel.join"
	// KindBranch picks one arm from an upstream result.
	KindBranch Kind = "branch"
	// KindLoopUntil repeats a named body under a mandatory cap.
	KindLoopUntil Kind = "loop.until"
	// KindHumanGate stops for a person.
	KindHumanGate Kind = "human.gate"
	// KindVerify runs one rung of the verification ladder.
	KindVerify Kind = "verify"
	// KindSubharnessCall runs another registered entry as one node.
	KindSubharnessCall Kind = "subharness.call"
	// KindTrigger is how a run starts: hosted on a source, on idle, on a watch,
	// or on a source's own command.
	KindTrigger Kind = "trigger"
)

// KindInfo is one registration: what the kind is for, whether it may stand at
// the head of a program, and what makes an instance of it well-formed.
type KindInfo struct {
	Kind Kind
	// Purpose is one line, written for whoever is choosing a kind — an author or
	// a model. It is the menu, built from the registry so a new kind cannot be
	// added without saying what it is for.
	Purpose string
	// Root says the kind may appear with no needs. Every kind may in principle;
	// this marks the ones that only make sense there.
	Root bool
	// Validate checks one node of this kind against the entry it lives in.
	Validate func(e Entry, n Node, byID map[string]Node) error
}

var kinds = map[Kind]KindInfo{
	KindAgentLoop: {
		Kind:     KindAgentLoop,
		Purpose:  "a model in a tool loop, with a model and a slice of the harness's tool whitelist; use it where the next step must be decided by reading the last one",
		Root:     true,
		Validate: validateAgentLoop,
	},
	KindToolCall: {
		Kind:     KindToolCall,
		Purpose:  "one whitelisted tool called with literal arguments; use it where the call is already known and a model would only be a way of retyping it",
		Root:     true,
		Validate: validateToolCall,
	},
	KindParallelSplit: {
		Kind:     KindParallelSplit,
		Purpose:  "fan the nodes that need this one out to run at once, up to a declared width",
		Root:     true,
		Validate: validateSplit,
	},
	KindParallelJoin: {
		Kind:     KindParallelJoin,
		Purpose:  "wait for every node it needs and carry their results forward as one",
		Validate: validateJoin,
	},
	KindBranch: {
		Kind:     KindBranch,
		Purpose:  "read one upstream result and hand control to one arm; the else arm is what happens when nothing matches",
		Validate: validateBranch,
	},
	KindLoopUntil: {
		Kind:     KindLoopUntil,
		Purpose:  "repeat a named body until a condition holds, never more than the declared maximum times",
		Validate: validateLoopUntil,
	},
	KindHumanGate: {
		Kind:     KindHumanGate,
		Purpose:  "put the question to a person; blocking holds the run, non-blocking records the ask and carries on",
		Validate: validateHumanGate,
	},
	KindVerify: {
		Kind:     KindVerify,
		Purpose:  "check what upstream produced at one rung of the ladder, from a bare accept to an adversarial or human read",
		Validate: validateCheck,
	},
	KindSubharnessCall: {
		Kind:     KindSubharnessCall,
		Purpose:  "run another registered sub-harness as one node, at a pinned revision or whatever is current",
		Root:     true,
		Validate: validateSubCall,
	},
	KindTrigger: {
		Kind:     KindTrigger,
		Purpose:  "how a run starts: hosted as a command on a source, on idle, on a watch, or on a source's own command",
		Root:     true,
		Validate: validateTrigger,
	},
}

// KindFor resolves a kind name to its registration.
func KindFor(k Kind) (KindInfo, bool) {
	info, ok := kinds[k]
	return info, ok
}

// Kinds returns every registered kind in a stable order, so a menu rendered
// twice reads the same twice.
func Kinds() []KindInfo {
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, string(k))
	}
	sort.Strings(names)
	list := make([]KindInfo, 0, len(names))
	for _, name := range names {
		list = append(list, kinds[Kind(name)])
	}
	return list
}

// KindMenu is the vocabulary as prose, built from the registry for whoever is
// writing or generating an entry.
func KindMenu() string {
	var menu strings.Builder
	menu.WriteString("Node kinds. A sub-harness is a DAG over exactly these:\n")
	for _, info := range Kinds() {
		fmt.Fprintf(&menu, "\n- %s — %s", info.Kind, info.Purpose)
	}
	menu.WriteString("\n")
	return menu.String()
}

// payload reports which payload pointers a node carries, so every validator can
// insist its own is present and no other is. A node that is two things at once
// is the fault most likely to survive a careless edit, because both halves look
// right on their own.
func payloads(n Node) []Kind {
	var set []Kind
	if n.Loop != nil {
		set = append(set, KindAgentLoop)
	}
	if n.Call != nil {
		set = append(set, KindToolCall)
	}
	if n.Split != nil {
		set = append(set, KindParallelSplit)
	}
	if n.Branch != nil {
		set = append(set, KindBranch)
	}
	if n.Until != nil {
		set = append(set, KindLoopUntil)
	}
	if n.Gate != nil {
		set = append(set, KindHumanGate)
	}
	if n.Check != nil {
		set = append(set, KindVerify)
	}
	if n.Sub != nil {
		set = append(set, KindSubharnessCall)
	}
	if n.Trigger != nil {
		set = append(set, KindTrigger)
	}
	return set
}

// onlyPayload is the shared half of every validator: whatever payload this kind
// reads, no other kind's payload may be set beside it.
func onlyPayload(n Node) error {
	for _, carried := range payloads(n) {
		if carried != n.Kind {
			return fmt.Errorf("carries a %s payload", carried)
		}
	}
	return nil
}

func known(byID map[string]Node, id string) bool {
	_, ok := byID[id]
	return ok
}

func validateAgentLoop(e Entry, n Node, _ map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	if n.Loop == nil {
		return nil
	}
	if n.Loop.MaxTurns < 0 {
		return fmt.Errorf("max_turns must not be negative")
	}
	// A loop may take a slice of the harness's whitelist and never a tool the
	// harness itself was not granted. Narrowing is the only direction: the entry
	// header is where a reader learns the blast radius, and a node that could
	// widen it would make that header a lie.
	for _, tool := range n.Loop.Tools {
		if !e.Allows(tool) {
			return fmt.Errorf("tool %q is not on the harness whitelist", tool)
		}
	}
	return nil
}

func validateToolCall(e Entry, n Node, _ map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	if n.Call == nil || strings.TrimSpace(n.Call.Tool) == "" {
		return fmt.Errorf("names no tool")
	}
	if !e.Allows(n.Call.Tool) {
		return fmt.Errorf("tool %q is not on the harness whitelist", n.Call.Tool)
	}
	return nil
}

func validateSplit(_ Entry, n Node, _ map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	if n.Split != nil && n.Split.Width < 0 {
		return fmt.Errorf("width must not be negative")
	}
	return nil
}

func validateJoin(_ Entry, n Node, _ map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	if len(n.Needs) == 0 {
		return fmt.Errorf("joins nothing: a join with no needs waits for nobody")
	}
	return nil
}

func validateBranch(_ Entry, n Node, byID map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	if n.Branch == nil {
		return fmt.Errorf("has no branch")
	}
	if !known(byID, n.Branch.On) {
		return fmt.Errorf("branches on unknown node %q", n.Branch.On)
	}
	if len(n.Branch.Cases) == 0 {
		return fmt.Errorf("has no cases")
	}
	for _, c := range n.Branch.Cases {
		if strings.TrimSpace(c.Match) == "" {
			return fmt.Errorf("a case matches nothing")
		}
		if !known(byID, c.Goto) {
			return fmt.Errorf("case %q goes to unknown node %q", c.Match, c.Goto)
		}
	}
	if n.Branch.Else != "" && !known(byID, n.Branch.Else) {
		return fmt.Errorf("else goes to unknown node %q", n.Branch.Else)
	}
	return nil
}

func validateLoopUntil(_ Entry, n Node, byID map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	if n.Until == nil {
		return fmt.Errorf("has no loop")
	}
	if len(n.Until.Body) == 0 {
		return fmt.Errorf("loops over nothing")
	}
	for _, id := range n.Until.Body {
		if !known(byID, id) {
			return fmt.Errorf("loops over unknown node %q", id)
		}
	}
	if strings.TrimSpace(n.Until.Until) == "" {
		return fmt.Errorf("has no until condition")
	}
	// The cap is mandatory and not defaulted. A default here would mean a file
	// could omit the one number that decides whether the loop ever stops, and
	// the reader of the file would have to know this package to find it.
	if n.Until.Max < 1 {
		return fmt.Errorf("max must be 1 or greater: a loop without a cap is the one shape a file may not express")
	}
	return nil
}

func validateHumanGate(_ Entry, n Node, _ map[string]Node) error {
	return onlyPayload(n)
}

func validateCheck(e Entry, n Node, byID map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	rung := e.Verify
	if n.Check != nil && n.Check.Rung != "" {
		rung = n.Check.Rung
	}
	if rung == "" {
		return fmt.Errorf("names no rung and the harness declares no default")
	}
	if !KnownRung(rung) {
		return fmt.Errorf("unknown verify rung %q", rung)
	}
	on := n.Needs
	if n.Check != nil && len(n.Check.On) > 0 {
		on = n.Check.On
	}
	if len(on) == 0 {
		return fmt.Errorf("checks nothing: give it needs or name what it reads")
	}
	for _, id := range on {
		if !known(byID, id) {
			return fmt.Errorf("checks unknown node %q", id)
		}
	}
	return nil
}

func validateSubCall(_ Entry, n Node, _ map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	if n.Sub == nil || strings.TrimSpace(n.Sub.Harness) == "" {
		return fmt.Errorf("names no harness")
	}
	if err := ValidName(n.Sub.Harness); err != nil {
		return err
	}
	if n.Sub.Revision < 0 {
		return fmt.Errorf("pinned revision must not be negative")
	}
	return nil
}
