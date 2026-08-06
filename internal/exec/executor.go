// Package exec runs the leaves of a plan.
//
// A leaf is one unit of work sized for a single agent working alone and in
// order. This package is the boundary where structure becomes action: above it
// everything is a graph, below it is a loop with tools.
//
// The Executor interface exists before there is more than one implementation of
// it, because the interesting question is not how the general loop works but
// where a specialised one plugs in. A code reviewer, a coding harness, a
// retrieval-only worker — each is a different way to turn the same Task into
// the same Outcome, and the graph should not learn which one ran.
package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// Input is one upstream result routed into a task.
//
// This is the dependency list finally doing its runtime job. A node receives
// exactly the results of the nodes it declared and nothing else, so the edges
// that were argued over during planning are the same edges that decide what an
// agent can see.
type Input struct {
	Title     string
	Summary   string
	Result    string
	Artifacts []string
}

// Task is one leaf, ready to run.
type Task struct {
	NodeID     int
	Title      string
	Goal       string // the whole plan's goal, for orientation
	Brief      string // the self-contained instruction: what the job is
	Contract   string // the working method: how this kind of job is done well
	Inputs     []Input
	OutputHint string // suggested artifact path when the deliverable is a file
	// Reflex constrains the general loop to one obvious micro-action and gives
	// it an explicit promotion verdict when the assignment is larger than it
	// first appeared.
	Reflex bool

	// Steer, when set, is polled between turns for mid-flight guidance from
	// the user. Each returned line lands in the transcript as a user message
	// before the next model call, so a running worker can be redirected
	// without being killed. Nil (the default, and the whole one-shot path)
	// costs nothing.
	Steer func() []string
}

// Outcome is what came back.
//
// Text is the deliverable and is what flows into dependents. Artifacts are
// files left in the workspace; they are referenced by path rather than pasted,
// so a large output never lands in three dependents' contexts at once.
type Outcome struct {
	Text      string
	Artifacts []string
	Turns     int
	ToolCalls int
	Decayed   int // observations faded to stubs, a measure of how much context was reclaimed
	Usage     Usage
	Stop      StopReason
	Elapsed   time.Duration
	// Promote is the executor's explicit verdict that a reflex needs the normal
	// compiled path. Text remains the useful partial discovered before stopping.
	Promote bool

	// Verdict is the same ending seen from the other side. Stop is written for a
	// person reading the run; Verdict is written for whatever learns from it, and
	// the two part company in exactly the case that matters — a leaf that
	// produced text and stopped because it was out of budget reads as "budget"
	// and grades as a failure.
	Verdict provider.Verdict
}

// StopReason says how the loop ended. It is recorded rather than inferred
// because "the model finished" and "we cut it off" produce identical-looking
// output and mean opposite things about whether the result can be trusted.
type StopReason string

const (
	StopDone     StopReason = "done"     // the model stopped asking for tools
	StopTurnCap  StopReason = "turn-cap" // ran out of iterations; a runaway backstop
	StopBudget   StopReason = "budget"   // ran out of tokens; the leaf was too expensive
	StopDeadline StopReason = "deadline" // ran out of wall clock
	StopError    StopReason = "error"    // the provider failed in a way we could not absorb
	StopPromote  StopReason = "promote"  // a reflex discovered that it is a job

	// StopEmpty is the runaway-reasoning circuit breaker: a turn that spent most
	// of what the leaf had left and returned no visible text at all. It is
	// separate from StopError because nothing failed — the call succeeded and
	// was paid for in full — and separate from StopDone because nothing was
	// produced.
	StopEmpty StopReason = "empty"
)

// Usage is the running cost of one task.
type Usage struct {
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	Cost             float64 `json:"cost"`
}

func (u *Usage) merge(other Usage) {
	u.Calls += other.Calls
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	u.CachedTokens += other.CachedTokens
	u.Cost += other.Cost
}

// Executor runs one task to completion.
//
// Implementations must be safe for concurrent use: the scheduler runs many
// leaves at once against a single executor, which is the entire point of having
// built a graph.
type Executor interface {
	Skill() string
	Run(ctx context.Context, task Task) (*Outcome, error)
}

// Registry picks an executor by skill. Nodes carry no skill yet, so everything
// resolves to the general loop; the lookup exists so that adding a specialised
// worker later is a registration rather than a change to the scheduler.
type Registry struct {
	executors map[string]Executor
	fallback  Executor
}

func NewRegistry(fallback Executor) *Registry {
	return &Registry{executors: map[string]Executor{fallback.Skill(): fallback}, fallback: fallback}
}

// Register adds a specialised executor.
func (r *Registry) Register(executor Executor) { r.executors[executor.Skill()] = executor }

// For returns the executor for a skill, falling back to the general one. An
// unknown skill is served rather than refused: a plan that asks for a worker we
// do not have should still get its work done by the generalist.
func (r *Registry) For(skill string) Executor {
	if executor, ok := r.executors[skill]; ok {
		return executor
	}
	return r.fallback
}

func (o *Outcome) String() string {
	return fmt.Sprintf("%s in %d turns, %d tool calls, $%.4f", o.Stop, o.Turns, o.ToolCalls, o.Usage.Cost)
}
