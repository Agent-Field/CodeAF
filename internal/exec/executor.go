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
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Input is one upstream result routed into a task.
//
// This is the dependency list finally doing its runtime job. A node receives
// exactly the results of the nodes it declared and nothing else, so the edges
// that were argued over during planning are the same edges that decide what an
// agent can see.
// Title names the producer. It is not decoration: the block these are rendered
// into is headed "results from earlier work, which you already have and must
// not gather again", and an untitled entry reads as an anonymous claim about
// what has already been done.
//
// There was once a Summary field here as well, written by the scheduler and
// read by nothing. It is gone rather than rendered: a field that describes an
// input but never reaches the agent is a claim about what the agent knows that
// is simply untrue.
type Input struct {
	Title     string
	Result    string
	Artifacts []string
}

// Task is one leaf, ready to run.
type Task struct {
	NodeID int
	// StoreNodeID is the durable provenance anchor used when a background job
	// requests promotion. One-shot execution leaves it empty.
	StoreNodeID string
	Title       string
	Goal        string // the whole plan's goal, for orientation
	Brief       string // the self-contained instruction: what the job is
	Contract    string // the working method: how this kind of job is done well
	Inputs      []Input
	OutputHint  string // suggested artifact path when the deliverable is a file
	// ImagePaths are user-supplied inputs attached to the initial leaf turn.
	ImagePaths []string
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
	// Control is polled at the same between-turn boundary as Steer. It is
	// deliberately cooperative: a model/tool turn already in flight lands,
	// then the claim owner releases through the store CAS path.
	Control func() ControlAction

	// control is installed by the scheduler so its watchdog can tear down a
	// Toolbox even when the executor goroutine itself is abandoned.
	control *leafControl
}

type ControlAction string

const (
	ControlNone   ControlAction = ""
	ControlPause  ControlAction = "pause"
	ControlCancel ControlAction = "cancel"
)

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
	// Steered counts the user's mid-flight lines this run actually read. It is
	// the difference between a redirection delivered to a mailbox and one
	// delivered to a mind, and it is zero on every leaf nobody steered.
	Steered int
	Usage     Usage
	Stop      StopReason
	// Exhausted is what ran out, when something did. It is separate from Stop
	// because the two answer different questions and the common case makes them
	// disagree: a leaf whose budget runs out is told to land, it lands, and it
	// ends StopDone — truthfully, because it did stop asking for tools. Reading
	// that as an ordinary finish was how the whole continuation subsystem came
	// to be dead on its designed path, and how a truncated partial posted as a
	// finished deliverable. Stop stays the honest answer to "how did the loop
	// end"; Exhausted answers "was it still working when it was told to stop",
	// which is what continuation and rating both actually need. Empty means
	// nothing ran out.
	Exhausted StopReason
	Elapsed   time.Duration
	// Promote is the executor's explicit verdict that a reflex needs the normal
	// compiled path. Text remains the useful partial discovered before stopping.
	Promote bool
	// ServiceRequests are live ownership leases requested through job.keep.
	// The resident must adopt or stop every lease before settling the leaf.
	ServiceRequests []ServiceRequest

	// Verdict is the same ending seen from the other side. Stop is written for a
	// person reading the run; Verdict is written for whatever learns from it, and
	// the two part company in exactly the case that matters — a leaf that
	// produced text and stopped because it was out of budget reads as "budget"
	// and grades as a failure.
	Verdict provider.Verdict

	// Ran is the tail of what the leaf actually did: the last calls it made,
	// in order, with the arguments clipped. ToolCalls already counted them and
	// a count settles nothing — the question a reader of a finished job
	// actually has is whether the check the deliverable claims to have run
	// appears anywhere in the run. The trace file answers that too, but it is
	// a file in the workspace holding every turn's prose; this is the same
	// evidence in memory, bounded, and already beside the text it is used to
	// check. It is a tail and not a transcript: absence in it is evidence, not
	// proof, and whatever reads it must say so.
	Ran []string
}

// ranLimit and ranArgumentBytes bound the record. Forty calls is well past the
// length of any single verification pass and small enough to hand to a judge
// whole; the argument clip keeps a command recognisable without carrying a
// pasted file into someone else's context.
const (
	ranLimit         = 40
	ranArgumentBytes = 200
)

// record appends one executed call to the bounded tail.
func (o *Outcome) record(call ai.ToolCall, failed bool) {
	line := strings.TrimSpace(call.Function.Name + " " + snip(strings.TrimSpace(call.Function.Arguments), ranArgumentBytes))
	if failed {
		line += "  → error"
	}
	o.Ran = append(o.Ran, line)
	if len(o.Ran) > ranLimit {
		o.Ran = o.Ran[len(o.Ran)-ranLimit:]
	}
}

// StopReason says how the loop ended. It is recorded rather than inferred
// because "the model finished" and "we cut it off" produce identical-looking
// output and mean opposite things about whether the result can be trusted.
type StopReason string

const (
	StopDone      StopReason = "done"      // the model stopped asking for tools
	StopTurnCap   StopReason = "turn-cap"  // ran out of iterations; a runaway backstop
	StopBudget    StopReason = "budget"    // ran out of tokens; the leaf was too expensive
	StopDeadline  StopReason = "deadline"  // ran out of wall clock
	StopError     StopReason = "error"     // the provider failed in a way we could not absorb
	StopPromote   StopReason = "promote"   // a reflex discovered that it is a job
	StopPaused    StopReason = "paused"    // user hold observed between turns
	StopCancelled StopReason = "cancelled" // user cancellation observed between turns

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

// Overran reports that the leaf still had work in hand when its resources ran
// out — the condition the continuation subsystem exists for. It reads both
// fields because a leaf can arrive here two ways: cut off outright (Stop), or
// told to land and complying (Exhausted). Only the resource endings count; a
// deadline is a fact about the clock rather than about work left undone, and a
// user pause or cancel is a decision rather than an overrun.
func (o *Outcome) Overran() bool {
	return o.Stop == StopBudget || o.Stop == StopTurnCap || o.Exhausted == StopBudget
}

func (o *Outcome) String() string {
	return fmt.Sprintf("%s in %d turns, %d tool calls, $%.4f", o.Stop, o.Turns, o.ToolCalls, o.Usage.Cost)
}
