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
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
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
	// NodeKey is the identity everything this leaf writes is filed under: its
	// artifact bucket, its flight recorder, its spilled observations, its
	// background job logs.
	//
	// It exists because NodeID is not always unique. A headless run's NodeID is
	// its plan node's number, which is unique within the graph — that path sets
	// nothing here and keeps the numeric spelling it has always written. The
	// resident surface has no such number: it holds a store node, whose creation
	// sequence belongs to the whole splice, so a four-part job handed four
	// workers one bucket, one recorder and one set of spill names. Siblings run
	// concurrently by construction, so that is not a naming inelegance — it is
	// one worker's spilled observation overwritten by another's while a stub in
	// its context still points at the file.
	//
	// Empty falls back to NodeID, which is what keeps every existing headless
	// path byte-identical. See [Task.leafKey].
	NodeKey string
	// StoreNodeID is the durable provenance anchor used when a background job
	// requests promotion. One-shot execution leaves it empty.
	StoreNodeID string
	Title       string
	Goal        string // the whole plan's goal, for orientation
	Brief       string // the self-contained instruction: what the job is
	Contract    string // the working method: how this kind of job is done well
	// Spec is the same job as the object the planner authored, carried whole.
	// Brief and Contract above are two of its fields and remain what this
	// executor reads; the object is here for the worker that can be handed a
	// spec directly instead of prose reassembled at the boundary. An empty
	// Spec renders to the empty string and changes nothing.
	Spec   plan.Spec
	Inputs []Input
	// OutputHint is where a file goes if this work needs one. It is an
	// address, never an instruction: what a leaf owes is its final message,
	// and a path offered as though a document were expected is how a job came
	// to leave 07-pr-482-code-review.md, 70-read-diff.md and 144-synthesis.md
	// in a person's own directory.
	OutputHint string
	// Intermediate says this leaf's result is consumed by later work rather
	// than read by the person who asked. Its handoff is its final message, so
	// it is offered no deliverable path at all — OutputHint, when it carries
	// anything, names the run's own scratch.
	Intermediate bool
	// ImagePaths are user-supplied inputs attached to the initial leaf turn.
	ImagePaths []string
	// DocumentPaths are user-supplied documents already staged in the
	// workspace. The brief names them; this is the same fact in structural
	// form, and it is what arms the document reader before turn 1 instead of
	// making the leaf spend a turn asking for a tool it demonstrably needs.
	DocumentPaths []string
	// Reflex constrains the general loop to one obvious micro-action and gives
	// it an explicit promotion verdict when the assignment is larger than it
	// first appeared.
	Reflex bool

	// Subharness names the worker this leaf was routed to. It is carried on the
	// task rather than looked up again at dispatch because the choice was made
	// once, upstream, and journaled: the scheduler's job is to honour it, not to
	// re-decide it. Empty is the generalist, which is nearly every leaf.
	Subharness string

	// Steer, when set, is polled between turns for mid-flight guidance from
	// the user. Each returned line lands in the transcript as a user message
	// before the next model call, so a running worker can be redirected
	// without being killed. Nil (the default, and the whole one-shot path)
	// costs nothing.
	Steer func() []string
	// Share, when set, gives the worker a one-line channel to the rest of its
	// job: a discovery about the material, a pitfall, a decision siblings must
	// match. It is nil for a job with no siblings, so a single-worker errand
	// never pays the schema for a channel with nobody on the other end.
	Share func(line string) error
	// Board is the reading side of Share: polled at the same between-turn
	// boundary as Steer, it returns lines other workers on this job shared.
	// They land in the transcript in their own voice, never the user's — a
	// sibling's discovery is testimony, not instruction.
	Board func() []string
	// Control is polled at the same between-turn boundary as Steer. It is
	// deliberately cooperative: a model/tool turn already in flight lands,
	// then the claim owner releases through the store CAS path.
	Control func() ControlAction

	// Progress is within-node visibility: where the work has got to, said in a
	// way that replaces the last thing it said rather than adding to it.
	//
	// It exists for the worker whose leaf is long and whose insides are not
	// nodes. A linear leaf is a turn loop nobody watches and passes nil; a
	// subharness that runs a pipeline for forty minutes would otherwise be a
	// spinner, and the two honest alternatives to this — splicing its stages
	// into the graph, or posting them as thread messages — are the two things
	// docs/SUBHARNESSES.md forbids by name. phase is the coarse thing being
	// done, done/total are a count when there is one, and latest is the short
	// right-hand side. Nil-safe and ignored when nil, so no existing caller
	// pays anything for it.
	Progress func(phase string, done, total int, latest string)

	// control is installed by the scheduler so its watchdog can tear down a
	// Toolbox even when the executor goroutine itself is abandoned.
	control *leafControl
}

// leafKey is the one place the answer to "who is this leaf, for naming
// purposes?" is worked out, so no writer can pick a different one from a reader.
func (t Task) leafKey() string {
	if key := strings.TrimSpace(t.NodeKey); key != "" {
		return key
	}
	return strconv.Itoa(t.NodeID)
}

// progress reports one step of within-node progress, and reports nothing at all
// when the surface offered no channel. The nil check lives here rather than at
// every call site because a worker that has to remember it will forget it once,
// in the path that only runs when something has already gone wrong.
func (t Task) progress(phase string, done, total int, latest string) {
	if t.Progress == nil || strings.TrimSpace(phase) == "" {
		return
	}
	t.Progress(phase, done, total, latest)
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
	Usage   Usage
	Stop    StopReason
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

	// Baseline is what was already broken before this work began: the checks
	// that came back red, and were red in exactly the same places before the
	// leaf touched the workspace.
	//
	// It exists because the delivery gate was reading a suite's absolute state
	// as a verdict on the change, and a repository with one pre-existing red
	// test therefore convicted every correct patch that passed through it — a
	// measured, repeated way of throwing finished work away (audit-notes
	// §14.4.1). A worker that can tell the difference owes the judge the
	// difference in words, because the judge cannot rerun anything. Only a
	// worker that actually photographs the repository before it starts fills
	// this in; every other leaf leaves it empty, which reads as "no claim".
	Baseline []string

	// Calibration is what the worker noticed about its own fit for this job:
	// free-text sentences, in the worker's own voice, about whether the work sat
	// comfortably inside its envelope, under it, or at the top of it.
	//
	// It is deliberately prose rather than a number or an enum. The only reader
	// is the recalibration call that rewrites a subharness's three anchor
	// examples, and that reader is a model reading evidence — a "fit: 0.3" would
	// have to be invented at one end and interpreted at the other, and both
	// halves would be fiction. Nothing branches on it and nothing may; the
	// generalist emits none of it, so every existing profile record and every
	// existing prompt is exactly what it was.
	//
	// A worker writes these about ITSELF. "This sat under my envelope" is a fact
	// this executor is uniquely placed to observe; "the other worker should have
	// had it" is a judgement it is not, and the note says the first thing.
	Calibration []string
}

// Calibrate appends one self-observation, ignoring the empty ones so a caller
// can compose a note conditionally without guarding every call.
func (o *Outcome) Calibrate(note string) {
	if note = strings.TrimSpace(note); note != "" {
		o.Calibration = append(o.Calibration, note)
	}
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
	Subharness() string
	Run(ctx context.Context, task Task) (*Outcome, error)
}

// Registry picks an executor by subharness. Nearly every node carries none and
// resolves to the general loop; the lookup is what makes adding a specialised
// worker a registration rather than a change to the scheduler.
type Registry struct {
	executors map[string]Executor
	fallback  Executor
}

func NewRegistry(fallback Executor) *Registry {
	return &Registry{executors: map[string]Executor{fallback.Subharness(): fallback}, fallback: fallback}
}

// Register adds a specialised executor.
func (r *Registry) Register(executor Executor) { r.executors[executor.Subharness()] = executor }

// For returns the executor for a subharness, falling back to the general one.
// An unknown name is served rather than refused: a plan that asks for a worker
// we do not have should still get its work done by the generalist.
func (r *Registry) For(subharness string) Executor {
	if executor, ok := r.executors[subharness]; ok {
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
