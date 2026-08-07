// Package craft is aforge's learned know-how: workflows, skills, verifiers,
// and exemplars, versioned in a git repository the resident owns. A workflow
// is a reusable job-shape written by the distiller from experience — never by
// hand into the binary — and refined across versions whose survival is
// measured like any other belief.
//
// The execution model is deliberately not a second engine. A workflow COMPILES
// into an ordinary graph subtree: steps are leaves, needs are edges, fan-out
// unrolls at runtime through the revision sentinel, and a failed verifier
// earns another bounded round the same way — a sentinel re-splice. Because
// every step is a normal node in the event-sourced store, persistence, resume
// after a crash, cost accounting, steering, surgery, the load governor, and
// the provider rate limiter are all inherited rather than reimplemented. LLM
// access is the same in-process clients every other leaf uses, sharing one
// cache lineage per run.
//
// The control layer is data, not code: a workflow file may declare shape and
// bounds but can never loop unboundedly, recurse into another workflow, or
// grant itself more budget than the package ceilings below. Intelligence
// lives in the step briefs; the file only arranges them.
package craft

import "time"

// The safety rails. A workflow file may declare tighter bounds than these but
// never looser: Clamp enforces the ceilings at parse time so a compiled run
// can trust every number it reads.
const (
	// DefaultRunBudgetUSD bounds a run whose file declares nothing. Small on
	// purpose: a craft run is routine work, not an expedition, and the rail
	// pauses-and-asks rather than failing when it is reached.
	DefaultRunBudgetUSD = 2.50
	// MaxRunBudgetUSD is the most a file may grant itself. Larger ambitions
	// belong to the user, said in chat, not to a file the distiller wrote.
	MaxRunBudgetUSD = 10.0
	// DefaultWallClock bounds a run's total wall time; the sentinel stops
	// opening new rounds or fan-out past it, running leaves finish normally.
	DefaultWallClock = 30 * time.Minute
	// MaxWallClock is the ceiling a file may declare.
	MaxWallClock = 2 * time.Hour
	// DefaultFanCap bounds one for_each unroll; MaxFanCap is the ceiling a
	// file may declare. Fan-out multiplies cost linearly and silently, so the
	// default stays modest.
	DefaultFanCap = 8
	MaxFanCap     = 24
	// DefaultMaxRounds bounds verify-revise loops when the file is silent;
	// MaxRounds is the ceiling. Two rounds catches most honest misses; past
	// five the verifier is wrong or the task is, and a human should look.
	DefaultMaxRounds = 2
	MaxRounds        = 5
	// MaxSteps refuses pathological files at parse time. A shape bigger than
	// this is a plan, and plans belong to the planner.
	MaxSteps = 24
)

// Workflow is one learned job-shape, parsed from workflows/<name>.yaml in the
// craft repository. Version identity (git commit) is attached by the Repo at
// load time, never written in the file.
type Workflow struct {
	Name        string
	Description string
	Params      []Param
	Steps       []Step
	Limits      Limits

	// Commit is the craft-repo version this Workflow was loaded at. Empty for
	// an unsaved draft. Survival stats key on Name+Commit.
	Commit string
}

// Param is a named hole in the step briefs, filled at invocation from the
// user's request. Substitution syntax in briefs is {{name}}.
type Param struct {
	Name        string
	Description string
	Default     string
	Required    bool
}

// Step is one leaf-to-be. Exactly one of Brief or Verify drives it: a Brief
// step is agentic (an ordinary worker with the ordinary tools), a Verify step
// runs an executable check from the repo. Skill names a forged executable the
// worker should reach for; it is advice surfaced in the brief, not a harness
// change.
type Step struct {
	ID    string
	Brief string
	// Needs are step IDs whose results feed this step, compiled to ordinary
	// feeds_into edges.
	Needs []string
	// Model is a slot word (talk/work/boost) or a model word resolved through
	// the existing model-words machinery. Empty means the job's model.
	Model string
	// Skill names an executable in the craft repo's skills/ directory (also on
	// AFORGE_SKILLS_BIN). Advice, not enforcement.
	Skill string
	// ForEach unrolls this step over a list produced upstream.
	ForEach *ForEach
	// Verify makes this a checking step.
	Verify *Verify
}

// ForEach unrolls one step over items discovered at runtime — the compile
// plants a single planner leaf whose result the sentinel expands into
// siblings, capped by Fan.
type ForEach struct {
	// Source is "<stepID>" — the items are read from that step's result, one
	// per line of its declared list output.
	Source string
	// Fan caps the unroll; 0 means DefaultFanCap. Clamped to MaxFanCap.
	Fan int
}

// Verify runs an executable from the repo's verifiers/ directory against the
// step's inputs. Exit 0 is a pass. Anything else is a fail whose stdout/stderr
// become revision feedback.
type Verify struct {
	// Script is a repo-relative path under verifiers/.
	Script string
	// UntilPass, when set, buys bounded repair rounds on failure.
	UntilPass *UntilPass
}

// UntilPass is the only loop craft has, and it is not a loop in the graph: on
// a failed verify the revision sentinel re-splices the named steps once more,
// carrying the verifier's output as feedback, up to MaxRounds total attempts.
type UntilPass struct {
	// Revise names the step IDs to re-run on failure. Empty means the verify
	// step's direct needs.
	Revise []string
	// MaxRounds caps total verify attempts; 0 means DefaultMaxRounds. Clamped
	// to the package MaxRounds.
	MaxRounds int
}

// Limits are the file's declared bounds, clamped to the package ceilings.
// Zero values mean the defaults.
type Limits struct {
	CostUSD   float64
	WallClock time.Duration
}
