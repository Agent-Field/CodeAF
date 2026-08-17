// Package subharness is the sub-harness registry: durable, versioned programs
// over a fixed set of node kinds, built in conversation and run by name.
//
// ── WHAT A SUB-HARNESS IS, AND WHAT IT IS NOT ──
//
// The word already meant something in this codebase and it still does: exec's
// subharness is the WORKER a leaf runs on — linear, swe, bare — and that seam is
// untouched by this package (cmd/aforge/subharness.go). This one is the other
// half of the same word: not who does the work, but the SHAPE the work is done
// in. A sub-harness here is a program — an agent loop, then a check, then a
// branch, then a person — that somebody built once, named, and can run again.
//
// It is a REGISTRY ENTRY and not generated code. Every node kind this package
// knows is in the binary (kinds.go), a file only ever arranges them, and the
// arrangement is data all the way down. That is the same law craft's workflows
// live under and it is here for the same reason: a file that could introduce a
// new kind of step would be a file that could introduce a new kind of failure,
// and nothing on disk should be able to do that.
//
// ── THE FOUR THINGS AN ENTRY HOLDS ──
//
//   - IDENTITY. A name, a line about what it is for, whoever wrote it, and an
//     integer version. The version is a POINTER, not a hash: v1, v2, v3, bumped
//     by [Store.Save] on every accepted edit, so "run triage at v2" is a
//     sentence somebody can say and a thing the disk can answer.
//   - PROGRAM. An ordered list of nodes, some of which nest (branch cases, loop
//     bodies, parallel lanes). Written flat it reads as numbered steps, which is
//     what the preview card draws (card.go).
//   - BOUNDS. The tool whitelist, the verification rung this harness climbs to,
//     and the dynamism rung with its integer cap. All three are ceilings a file
//     may tighten and never loosen — [Clamp] enforces that at parse time so a
//     run can trust every number it reads.
//   - EVIDENCE. Tests, and the run history: every execution writes a trace DAG
//     under harnesses/<name>/run/<ts>.json (run.go). A harness with no runs
//     behind it is a proposal; a harness with fifty is a measured thing.
//
// ── WHY THE LADDERS ARE ARGUMENTS AND NOT MODES ──
//
// Verification and dynamism are both spectra, and the Agent-Field skill this
// adopts states them as spectra rather than as flags: a harness does not "have
// verification", it climbs to a rung — accept, schema, invariants, loop, report,
// rederive, adversarial, human — and it does not "allow dynamism", it is allowed
// up to one — fixed, branch, width, metaprompt, recursive, selfmod. A rung is
// comparable, so a node may be checked against the harness's ceiling with an
// integer comparison instead of a table of special cases, and the whole question
// "may this program do that" reduces to two of them ([Harness.permits]).
package subharness

import (
	"fmt"
	"strings"
	"time"
)

// The ceilings. A file may declare tighter numbers than these and never looser.
// They are small on purpose: a sub-harness is a shape somebody has to be able to
// read on one card before approving it, and a program past these bounds is a
// plan, which belongs to the planner.
const (
	// MaxNodes bounds the whole program, nesting included.
	MaxNodes = 48
	// MaxDepth bounds nesting — a branch inside a loop inside a lane is three,
	// and past four nobody can hold the shape in their head.
	MaxDepth = 4
	// MaxLanes bounds one parallel.split. Width multiplies cost linearly and
	// silently, which is why it is its own dynamism rung as well as a number.
	MaxLanes = 8
	// MaxRounds bounds one loop.until. Two rounds catches an honest miss; past
	// five the loop is wrong or the task is, and a person should look.
	MaxRounds = 10
	// DefaultRounds is what a loop.until that declares nothing gets.
	DefaultRounds = 3
	// MaxTurns bounds one agent.loop node, and DefaultTurns is the silent
	// answer. They are the leaf's own budget, stated here because the file may
	// ask for less and must not be able to ask for more.
	MaxTurns     = 40
	MaxTurnsSoft = 12
	DefaultTurns = 8
	// MaxCallDepth bounds subharness.call nesting. Recursion is a dynamism rung
	// and this is its floor of last resort: a cycle through the registry that
	// slipped past [Validate] still cannot run forever.
	MaxCallDepth = 3
	// MaxNameLen bounds a harness name, which is a filename and a word people
	// type.
	MaxNameLen = 48
)

// Harness is one registry entry: everything harnesses/<name>.hjson holds.
type Harness struct {
	// Name is the identity and the filename stem. It is lower-case, and the
	// characters are the ones a filename and a command line agree about
	// ([ValidName]).
	Name string `json:"name"`
	// Description is the one line the panel and the card show.
	Description string `json:"description,omitempty"`
	// Author is whoever built it — a person's handle, or the model's word for
	// itself when it was built in conversation.
	Author string `json:"author,omitempty"`
	// Version is the pointer: 1 for a first registration, bumped by
	// [Store.Save] on every accepted edit. It is never written by a builder —
	// the store owns it, because two surfaces editing the same harness must not
	// be able to both call themselves v4.
	Version int `json:"version"`
	// Program is the ordered list of nodes. Nesting lives inside the nodes that
	// nest (branch, loop.until, parallel.split).
	Program []Node `json:"program"`
	// Tools is the whitelist: the only tools any tool.call or agent.loop node in
	// this program may reach. Empty means NO TOOLS, not every tool — a harness
	// that forgot to say what it needs must not be handed the belt.
	Tools []string `json:"tools,omitempty"`
	// Verify is the highest rung this harness climbs to. A verify node may name
	// a rung at or below it and never above.
	Verify Rung `json:"verify,omitempty"`
	// Dynamism is how much shape this program is allowed to grow at runtime, and
	// Cap is that rung's integer bound: the loop ceiling, the lane count, the
	// recursion depth. One number, because the rung says which of those three it
	// is about.
	Dynamism Dynamism `json:"dynamism,omitempty"`
	Cap      int      `json:"cap,omitempty"`
	// Tests are the acceptance cases carried with the entry. They are data, not
	// code: an input, and what the final output must contain.
	Tests []Test `json:"tests,omitempty"`

	// Updated is when this version was written. It is stamped by the store.
	Updated time.Time `json:"updated,omitempty"`
}

// Test is one acceptance case: what to run it on, and what must come back.
type Test struct {
	Name   string `json:"name"`
	Input  string `json:"input,omitempty"`
	Expect string `json:"expect"`
}

// Node is one step. It is ONE struct rather than a kind per type, and every
// field says which kinds it belongs to, because the file is read by a model as
// often as by a person and one shape with empty fields is easier to write
// correctly than nine shapes to choose between. Which fields a kind requires,
// and which it refuses, is the registry's own table (kinds.go).
type Node struct {
	// ID names this node inside its program. It is what the trace records and
	// what a person points at when they say "it stalls at check".
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// Note is the one line the card shows when the node's own fields would read
	// worse than a sentence. Optional everywhere.
	Note string `json:"note,omitempty"`

	// agent.loop: what to ask, on what, with which of the harness's tools, for
	// how many turns.
	Prompt   string   `json:"prompt,omitempty"`
	Model    string   `json:"model,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	MaxTurns int      `json:"max_turns,omitempty"`

	// tool.call: which tool, and the arguments verbatim.
	Tool string         `json:"tool,omitempty"`
	Args map[string]any `json:"args,omitempty"`

	// branch: the cases in order, and what happens when none match.
	Cases []Case `json:"cases,omitempty"`
	Else  []Node `json:"else,omitempty"`

	// loop.until: the body, the condition that ends it, and how many rounds it
	// is worth.
	Until string `json:"until,omitempty"`
	Max   int    `json:"max,omitempty"`
	Steps []Node `json:"steps,omitempty"`

	// parallel.split: the lanes, and how the join settles.
	Lanes []Lane   `json:"lanes,omitempty"`
	Join  JoinMode `json:"join,omitempty"`

	// human.gate: what the person is being asked, and whether declining offers
	// them the run itself rather than only a no ([GateAnswer]).
	Escalate bool `json:"escalate,omitempty"`

	// verify: which rung, and what the check actually is — a command when the
	// rung runs one, a question when the rung asks one.
	Rung  Rung   `json:"rung,omitempty"`
	Check string `json:"check,omitempty"`

	// subharness.call: whose program to run here, and at which version. Zero is
	// "whatever is registered now", which is the honest default for a call that
	// wants the current shape rather than a frozen one.
	Call        string `json:"call,omitempty"`
	CallVersion int    `json:"call_version,omitempty"`

	// trigger: what starts this harness, and the detail that names it — the
	// cadence for idle, the path for watch, the command for source.command.
	On   TriggerKind `json:"on,omitempty"`
	Spec string      `json:"spec,omitempty"`
}

// Case is one arm of a branch: a condition (predicate.go) and what to do when it
// holds.
type Case struct {
	When  string `json:"when"`
	Steps []Node `json:"steps"`
}

// Lane is one arm of a parallel.split.
type Lane struct {
	Name  string `json:"name"`
	Steps []Node `json:"steps"`
}

// ── the verification ladder ─────────────────────────────────────────────────

// Rung is one step of the verification ladder, lowest first. It is an ARGUMENT
// to a harness rather than a mode of one: work is not verified or unverified, it
// is checked to a height, and the height is what a person is agreeing to when
// they approve the card.
type Rung string

const (
	// RungAccept takes the worker's word for it. It is the honest name for no
	// verification at all, and naming it is what makes the rest a ladder.
	RungAccept Rung = "accept"
	// RungSchema checks the SHAPE of what came back — a field is present, the
	// text parses, the list is non-empty.
	RungSchema Rung = "schema"
	// RungInvariants checks properties that must hold of any correct answer,
	// whatever the answer is.
	RungInvariants Rung = "invariants"
	// RungLoop runs the real check — the build, the test, the linter — and hands
	// its failure back as feedback for another bounded round.
	RungLoop Rung = "loop"
	// RungReport asks the worker to account for its own work in a form somebody
	// else can audit.
	RungReport Rung = "report"
	// RungRederive has the answer derived a second time, independently, and
	// compares.
	RungRederive Rung = "rederive"
	// RungAdversarial has a second worker try to REFUTE the answer rather than
	// reproduce it.
	RungAdversarial Rung = "adversarial"
	// RungHuman is a person. It is the top of the ladder because it is the only
	// rung that cannot be automated away, and it costs the most of the only
	// budget that does not refill.
	RungHuman Rung = "human"
)

// rungOrder is the ladder itself. Index is height.
var rungOrder = []Rung{
	RungAccept, RungSchema, RungInvariants, RungLoop,
	RungReport, RungRederive, RungAdversarial, RungHuman,
}

// Height is where a rung sits, and false for a word that names no rung.
func (r Rung) Height() (int, bool) {
	for at, known := range rungOrder {
		if known == r {
			return at, true
		}
	}
	return 0, false
}

// Valid reports whether this is a rung. The empty rung is valid and means
// RungAccept: a harness that says nothing about verification is a harness that
// takes the worker's word, and saying so out loud is better than a nil that
// means something else in each caller.
func (r Rung) Valid() bool {
	if r == "" {
		return true
	}
	_, ok := r.Height()
	return ok
}

// Or is this rung, or the fallback when it is empty.
func (r Rung) Or(fallback Rung) Rung {
	if r == "" {
		if fallback == "" {
			return RungAccept
		}
		return fallback
	}
	return r
}

// Rungs is the ladder, for help text and for a picker.
func Rungs() []Rung { return append([]Rung(nil), rungOrder...) }

// ── the dynamism ladder ─────────────────────────────────────────────────────

// Dynamism is how much shape a program may grow while it runs. Like [Rung] it is
// comparable, and the comparison is the whole enforcement: a kind belongs to a
// rung, and a program may use a kind only if the harness was granted that rung
// or higher ([Dynamism.Allows]).
type Dynamism string

const (
	// DynFixed is a straight line: loops, tools, gates, checks, and nothing
	// whose shape depends on what happened.
	DynFixed Dynamism = "fixed"
	// DynBranch lets the program choose a path — branch and loop.until — but
	// never how WIDE it is.
	DynBranch Dynamism = "branch"
	// DynWidth adds parallel.split: the program decides how many things happen
	// at once, bounded by Cap.
	DynWidth Dynamism = "width"
	// DynMetaprompt lets a node's instruction be written at runtime rather than
	// in the file. The shape is fixed; the words are not.
	DynMetaprompt Dynamism = "metaprompt"
	// DynRecursive adds subharness.call: this program may reach other programs,
	// bounded by [MaxCallDepth].
	DynRecursive Dynamism = "recursive"
	// DynSelfmod is a harness that may register a new version of itself. It is
	// the top of the ladder and it is the one rung nothing in this package acts
	// on by itself: a self-modification still goes through [Store.Save], which
	// still bumps a version, and the person still sees the card.
	DynSelfmod Dynamism = "selfmod"
)

var dynOrder = []Dynamism{DynFixed, DynBranch, DynWidth, DynMetaprompt, DynRecursive, DynSelfmod}

// Height is where a rung sits, and false for a word that names none.
func (d Dynamism) Height() (int, bool) {
	for at, known := range dynOrder {
		if known == d {
			return at, true
		}
	}
	return 0, false
}

// Valid reports whether this is a rung; empty is DynFixed for the reason the
// empty [Rung] is RungAccept.
func (d Dynamism) Valid() bool {
	if d == "" {
		return true
	}
	_, ok := d.Height()
	return ok
}

// Or is this rung, or the fallback when it is empty.
func (d Dynamism) Or(fallback Dynamism) Dynamism {
	if d == "" {
		if fallback == "" {
			return DynFixed
		}
		return fallback
	}
	return d
}

// Allows reports whether a harness at this rung may contain that kind. It is the
// one place the two ladders meet, and it is deliberately a lookup rather than a
// switch per caller: adding a kind means giving it a rung in [kindRegistry], and
// nothing else in the package has to learn about it.
func (d Dynamism) Allows(kind Kind) bool {
	need, known := kindDynamism(kind)
	if !known {
		return false
	}
	have, ok := d.Or(DynFixed).Height()
	if !ok {
		return false
	}
	needHeight, ok := need.Height()
	if !ok {
		return false
	}
	return have >= needHeight
}

// Dynamisms is the ladder, for help text and for a picker.
func Dynamisms() []Dynamism { return append([]Dynamism(nil), dynOrder...) }

// ── the small enumerations ──────────────────────────────────────────────────

// JoinMode is how a parallel.split settles.
type JoinMode string

const (
	// JoinAll waits for every lane. It is the default because a split whose
	// result depends on which lane won is a race somebody has to have asked for.
	JoinAll JoinMode = "all"
	// JoinFirst takes the first lane to finish and abandons the rest.
	JoinFirst JoinMode = "first"
)

func (j JoinMode) Valid() bool { return j == "" || j == JoinAll || j == JoinFirst }

// Or is this mode, or JoinAll.
func (j JoinMode) Or() JoinMode {
	if j == "" {
		return JoinAll
	}
	return j
}

// TriggerKind is what starts a harness that is not started by a person.
type TriggerKind string

const (
	// TriggerHosted is somebody else's schedule — a cron, a routine, a webhook.
	TriggerHosted TriggerKind = "hosted"
	// TriggerIdle fires when the session has been quiet; Spec is the cadence.
	TriggerIdle TriggerKind = "idle"
	// TriggerWatch fires when a file or a command's output changes; Spec names
	// it, in the words the watch tool uses (internal/session's tools_watch.go).
	TriggerWatch TriggerKind = "watch"
	// TriggerCommand fires from a command's exit; Spec is the command.
	TriggerCommand TriggerKind = "source.command"
)

var triggerOrder = []TriggerKind{TriggerHosted, TriggerIdle, TriggerWatch, TriggerCommand}

func (t TriggerKind) Valid() bool {
	for _, known := range triggerOrder {
		if known == t {
			return true
		}
	}
	return false
}

// Triggers is the list, for help text.
func Triggers() []TriggerKind { return append([]TriggerKind(nil), triggerOrder...) }

// ── identity ────────────────────────────────────────────────────────────────

// ValidName says whether a word may name a harness. The rule is the intersection
// of three things it has to survive: a filename, a shell word, and a sentence
// somebody says out loud. Lower case, digits, dash and dot, starting with a
// letter — and no dot-dot, because the name becomes a path.
func ValidName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("a harness needs a name")
	case len(name) > MaxNameLen:
		return fmt.Errorf("the name %q is longer than %d characters", name, MaxNameLen)
	case strings.Contains(name, ".."):
		return fmt.Errorf("the name %q contains ..", name)
	}
	for at, char := range name {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9', char == '-', char == '.':
			if at == 0 {
				return fmt.Errorf("the name %q must start with a letter", name)
			}
		default:
			return fmt.Errorf("the name %q may only hold lower-case letters, digits, - and .", name)
		}
	}
	return nil
}
