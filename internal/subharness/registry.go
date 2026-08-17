// Package subharness is the registry of node kinds a sub-harness is built
// from, and the durable home of the sub-harnesses themselves.
//
// A sub-harness is a registry entry, not generated code: identity, a program
// that is a DAG over the node kinds below, a tool whitelist, a verification
// rung, a dynamism rung with its budget, and tests. The kinds are a
// library-in-binary — `agent.loop` is the session loop this binary already
// runs, `tool.call` is the tool call it already makes — so a saved harness is
// a small, inspectable arrangement of machinery that exists, and never a
// program the resident has to compile.
//
// Two ladders run through everything here, both adopted from the Agent-Field
// skill and both carried as an argument rather than a design:
//
//   - Verification: accept < schema < invariants < loop < report < rederive <
//     adversarial < human. How hard a run's output has to work to be believed.
//   - Dynamism: fixed < branch < width < meta < recursive < selfmod. How much
//     shape a run is allowed to decide for itself, with an integer Cap that is
//     the budget for deciding it.
//
// The dynamism rung is enforced against the program: a `fixed` harness may not
// contain a `branch` node, a harness that never declared `recursive` may not
// call another sub-harness. That is the whole point of writing the rung down —
// a harness cannot quietly become more autonomous than its author said.
//
// Version is a pointer. v1, v2, v3 are immutable pages under the harness's own
// directory, and opening a pointer again reads the same page it read before;
// see store.go. Design: docs/SUBHARNESS.md.
package subharness

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The integer caps. Every number a harness file may declare is bounded here
// rather than at the point of use, because the file is written by a model and
// the cost of a mistyped width is paid in fan-out, not in an error message.
const (
	// MaxWidth bounds one parallel.split. Wider than this is a job for the
	// planner, not for a node.
	MaxWidth = 16
	// MaxRounds bounds one loop.until. Past a handful of rounds the condition
	// is wrong or the work is, and either way another round will not find out.
	MaxRounds = 8
	// DefaultRounds is what a loop.until that declares nothing gets.
	DefaultRounds = 2
	// MaxTurns bounds one agent.loop's conversation with itself.
	MaxTurns = 64
	// MaxDynCap bounds the dynamism budget — the total number of runtime
	// decisions (extra loop rounds, minted width) a run may spend.
	MaxDynCap = 32
	// MaxNodes refuses a pathological program at parse time. A shape bigger
	// than this is a plan, and plans belong to the planner.
	MaxNodes = 64
	// MaxIdBytes is how long a node id may be. Node ids are read by people in
	// a trail and typed back by hand; past this they stop being either.
	MaxIdBytes = 48
	// MaxVersion bounds a version pointer. It is a cap on a field a file may
	// declare — a subharness.call pinned to v10000 is a typo, not a pin.
	MaxVersion = 9999
)

// Id is who a sub-harness is. Version is the pointer: 1, 2, 3, minted by the
// store, never chosen by the writer.
type Id struct {
	Name    string `json:"name"`
	Desc    string `json:"desc,omitempty"`
	Author  string `json:"author,omitempty"`
	Version int    `json:"version"`
}

// Fields are one node's arguments. They are strings on the wire — including
// the integers — so that a saved page round-trips byte for byte and the
// integer caps are enforced in one place (Int, below) rather than by whatever
// numeric type a decoder happened to choose.
type Fields map[string]string

// Int reads an integer field, returning fallback when the field is absent or
// blank. It is total on purpose: Validate has already refused every page whose
// integer fields do not parse, so the runner reads them without a second error
// path.
func (f Fields) Int(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(f[name]))
	if err != nil {
		return fallback
	}
	return value
}

// Get reads a field with its surrounding whitespace removed.
func (f Fields) Get(name string) string { return strings.TrimSpace(f[name]) }

// Node is one step of a program: an id nobody else in the program carries, a
// registered kind, and that kind's fields.
type Node struct {
	Id     string `json:"id"`
	Kind   string `json:"kind"`
	Fields Fields `json:"fields,omitempty"`
}

// Edge is a directed dependency, from node id to node id. Edges are ids and
// not indices because a program is edited by hand and by model, and an index
// silently means a different node the moment a line moves.
type Edge [2]string

// From names the edge's source node.
func (e Edge) From() string { return e[0] }

// To names the edge's target node.
func (e Edge) To() string { return e[1] }

func (e Edge) String() string { return e[0] + "->" + e[1] }

// Program is the DAG. Node order is the tie-break everywhere order matters —
// the entry search, a branch's default successor, the topological walk — so a
// program that is rewritten with its nodes in the same order runs the same way
// twice.
type Program struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges,omitempty"`
}

// Node finds a node by id.
func (p Program) Node(id string) (Node, bool) {
	for _, node := range p.Nodes {
		if node.Id == id {
			return node, true
		}
	}
	return Node{}, false
}

// Successors lists the ids an edge leads to from id, in edge order.
func (p Program) Successors(id string) []string {
	var out []string
	for _, edge := range p.Edges {
		if edge.From() == id {
			out = append(out, edge.To())
		}
	}
	return out
}

// Predecessors lists the ids that lead to id, in edge order.
func (p Program) Predecessors(id string) []string {
	var out []string
	for _, edge := range p.Edges {
		if edge.To() == id {
			out = append(out, edge.From())
		}
	}
	return out
}

// The verification ladder, weakest first. A rung is an argument to the harness
// and, per node, to a verify node — never a separate design.
const (
	VerifyAccept      = "accept"
	VerifySchema      = "schema"
	VerifyInvariants  = "invariants"
	VerifyLoop        = "loop"
	VerifyReport      = "report"
	VerifyRederive    = "rederive"
	VerifyAdversarial = "adversarial"
	VerifyHuman       = "human"
)

var verifyLadder = []string{
	VerifyAccept, VerifySchema, VerifyInvariants, VerifyLoop,
	VerifyReport, VerifyRederive, VerifyAdversarial, VerifyHuman,
}

// VerifyLadder returns the rungs in order, weakest first.
func VerifyLadder() []string { return append([]string(nil), verifyLadder...) }

// VerifyRung is a rung's height, or -1 for a word that is not on the ladder.
func VerifyRung(word string) int { return rung(verifyLadder, word) }

// Verify is how hard a run has to work to be believed.
type Verify struct {
	Ladder string `json:"ladder"`
}

// The dynamism ladder, least autonomous first.
const (
	DynFixed     = "fixed"
	DynBranch    = "branch"
	DynWidth     = "width"
	DynMeta      = "meta"
	DynRecursive = "recursive"
	DynSelfmod   = "selfmod"
)

var dynLadder = []string{DynFixed, DynBranch, DynWidth, DynMeta, DynRecursive, DynSelfmod}

// DynLadder returns the rungs in order, least autonomous first.
func DynLadder() []string { return append([]string(nil), dynLadder...) }

// DynRung is a rung's height, or -1 for a word that is not on the ladder.
func DynRung(word string) int { return rung(dynLadder, word) }

// Dyn is how much shape a run may decide for itself, and the budget it may
// spend deciding. Cap is a whole-run allowance, not a per-node one: an extra
// loop round costs a unit, a minted branch of width costs a unit, and a run
// that spends its last unit stops deciding and finishes on the shape it has.
type Dyn struct {
	Ladder string `json:"ladder"`
	Cap    int    `json:"cap,omitempty"`
}

// Test is one case the harness is expected to survive. It is held here, beside
// the program, so that a version pointer names the shape and the cases that
// justified it together.
type Test struct {
	Name   string `json:"name"`
	Input  string `json:"input,omitempty"`
	Expect string `json:"expect,omitempty"`
}

// Harness is one registry entry, whole. It is the unit the store versions and
// the unit the runner runs.
type Harness struct {
	Id        Id       `json:"id"`
	Program   Program  `json:"program"`
	Whitelist []string `json:"whitelist,omitempty"`
	Verify    Verify   `json:"verify"`
	Dyn       Dyn      `json:"dyn"`
	Tests     []Test   `json:"tests,omitempty"`
}

// Allows reports whether a tool is on the harness's whitelist. An empty
// whitelist allows nothing — a harness that wants a tool has to say which one.
func (h Harness) Allows(tool string) bool {
	for _, allowed := range h.Whitelist {
		if allowed == tool {
			return true
		}
	}
	return false
}

// Kind is one registered node kind. MinDyn is the lowest dynamism rung a
// harness may declare and still contain this kind; Valid is the kind's own law
// over its fields.
type Kind struct {
	Name   string
	Desc   string
	MinDyn string
	Valid  func(Fields) error
}

// kinds is the registry. It is populated once, from kinds.go's init, and read
// concurrently thereafter — Register is not for runtime callers, it is how the
// library declares itself.
var kinds = map[string]Kind{}

// Register adds a node kind. It panics on a duplicate or a malformed rung
// because both are programmer errors in this package's own init, discovered at
// process start rather than at the first parse.
func Register(k Kind) {
	if k.Name == "" {
		panic("subharness: node kind with no name")
	}
	if _, taken := kinds[k.Name]; taken {
		panic("subharness: node kind registered twice: " + k.Name)
	}
	if DynRung(k.MinDyn) < 0 {
		panic("subharness: node kind " + k.Name + " wants dynamism rung " + k.MinDyn)
	}
	kinds[k.Name] = k
}

// Lookup finds a registered kind.
func Lookup(name string) (Kind, bool) {
	k, found := kinds[name]
	return k, found
}

// Kinds lists the registry in name order.
func Kinds() []Kind {
	out := make([]Kind, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func rung(ladder []string, word string) int {
	for height, name := range ladder {
		if name == word {
			return height
		}
	}
	return -1
}

// spec is one field's law inside a kind: whether it must be present, the cap
// if it is an integer, and the closed set of words if it is one of those.
type spec struct {
	name     string
	required bool
	max      int
	words    []string
}

// def builds a kind's Valid from a table of field specs. Every kind's law is
// written this way so that the caps live next to the fields they bound, and so
// that an unknown field is an error rather than a silently ignored typo — a
// model that writes `rounds` where the kind says `max_rounds` should be told,
// not quietly given the default.
func def(specs ...spec) func(Fields) error {
	return func(f Fields) error {
		known := make(map[string]spec, len(specs))
		for _, s := range specs {
			known[s.name] = s
		}
		var unknown []string
		for name := range f {
			if _, ok := known[name]; !ok {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return fmt.Errorf("unknown field %s", strings.Join(quoted(unknown), ", "))
		}
		for _, s := range specs {
			value := strings.TrimSpace(f[s.name])
			if value == "" {
				if s.required {
					return fmt.Errorf("field %q is required", s.name)
				}
				continue
			}
			if s.max > 0 {
				number, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("field %q: %q is not an integer", s.name, value)
				}
				if number < 1 {
					return fmt.Errorf("field %q: %d is below 1", s.name, number)
				}
				if number > s.max {
					return fmt.Errorf("field %q: %d is past the cap of %d", s.name, number, s.max)
				}
			}
			if len(s.words) > 0 && rung(s.words, value) < 0 {
				return fmt.Errorf("field %q: %q is not one of %s", s.name, value, strings.Join(s.words, "|"))
			}
		}
		return nil
	}
}

// errRequired reports a field that is required only under a condition, which
// is the one shape the spec table cannot express.
func errRequired(name, because string) error {
	return fmt.Errorf("field %q is required when %s", name, because)
}

func quoted(words []string) []string {
	out := make([]string, len(words))
	for i, word := range words {
		out[i] = strconv.Quote(word)
	}
	return out
}
