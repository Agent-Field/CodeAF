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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/verify"
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
	// Whole says Result above is the producer's material and not an account of
	// it: the files were read back and their text is in the block. It exists
	// because the sentence under that block is an instruction either way, and
	// the two instructions are opposites — "read them if you need the full
	// detail" is an invitation to go and get what the leaf is already holding.
	// False is the older and weaker claim, and is what every caller that does
	// not set this keeps.
	Whole bool
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

	// Fold says this leaf's material is entirely in Inputs above, whole, and
	// that its job is to assemble it — so the loop runs it as a fold: one model
	// call, and a second only if the first asked for a tool or produced nothing
	// deliverable. See foldTurns.
	//
	// It is set by whatever assembled this task, from two structural facts it
	// can check and this package cannot: that the plan gives this node nothing
	// to go and find (plan.Folds), and that every dependency arrived pushed
	// rather than clipped to a handle. The second is what makes the first safe —
	// a node told to assemble material it was only handed a pointer to has to go
	// and get it, whatever its plan says.
	Fold bool

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
	// It exists for the leaf that is long and whose insides are not nodes. A
	// linear leaf is a turn loop nobody watches and passes nil; a saved program
	// that runs a pipeline for forty minutes would otherwise be a spinner, and
	// the two alternatives to this are both worse — splicing its stages into
	// the graph would put work in the plan that nobody planned, and posting
	// them as thread messages would spend the person's attention on a running
	// pipeline. phase is the coarse thing being done, done/total are a count
	// when there is one, and latest is the short right-hand side. Nil-safe and
	// ignored when nil, so no existing caller pays anything for it.
	Progress func(phase string, done, total int, latest string)

	// Fault carries a recovered panic out to whoever can write it down.
	//
	// It exists because guard.Note's whole record is a line in a log file, and a
	// caught fault is a fact about the run that changes what the person watching
	// should expect. In the crashed run of 2026-08-28 a leaf faulted, was
	// escalated two seconds later, and the headless stream said `still waiting`
	// for eleven minutes; the operator read it as a hang. The surface that owns
	// the journal is the one that can journal it, so the leaf-running code
	// reports and the surface records. Nil-safe and ignored when nil, so a
	// caller with nowhere to write pays nothing.
	Fault func(error)

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

// Faulted reports one recovered panic, and reports nothing at all when the
// surface offered nowhere to write it. The nil check lives here for the same
// reason [Task.progress]'s does, and it matters more: every call site is inside
// a deferred recover, which is exactly where a second panic is unrecoverable.
func (t Task) Faulted(err error) {
	if t.Fault == nil || err == nil {
		return
	}
	t.Fault(err)
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
	// Folded counts the leaf's own aged assistant turns retired to pointers. It
	// is separate from Decayed because the two say different things about a run:
	// decay means the work produced more raw material than fits, fold means the
	// work produced more of its own prose than fits, and only the second is
	// evidence that a leaf was talking to itself.
	Folded int
	// Steered counts the user's mid-flight lines this run actually read. It is
	// the difference between a redirection delivered to a mailbox and one
	// delivered to a mind, and it is zero on every leaf nobody steered.
	Steered int
	Usage   Usage
	// PerTurn is Usage with its shape kept: one row per turn, summing to Usage
	// exactly. See meter.go for why a summed row alone cannot answer the
	// question anybody asks of it. Executors that do not meter turns leave it
	// empty, which reads as "no shape recorded" rather than as a leaf with no
	// turns.
	PerTurn []TurnUsage
	// Meter is the bound that actually landed this leaf, with its own two
	// numbers.
	//
	// THREE CEILINGS PRODUCED ONE SENTENCE. A leaf could be landed by its cost
	// grant, by an undiscounted token ceiling three times that grant, or by a
	// cumulative bound on prompt sent — and all three set Exhausted to
	// StopBudget, so the journal said "it ran out of its tokens" and the surface
	// printed the grant, which in the ink run of 2026-08-29 was a number the
	// leaf never came near. An autopsy could not tell which ceiling had fired,
	// and the first three readings of that run each blamed a different one.
	//
	// A MEASUREMENT THAT WAS NOT TAKEN IS A FACT ABOUT THE RUN (FAILSAFE.md's
	// sixth failure) and so is a measurement whose meter nobody can name. Empty
	// on a leaf that was not landed by a bound.
	Meter Meter
	Stop  StopReason
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
	// Mode names the shape the loop actually ran in, when it ran in one that is
	// not the open loop. It is written so a benchmark can assert that a fold
	// fired rather than inferring it from a turn count that any well-behaved
	// leaf might also have produced. Empty is the ordinary loop.
	Mode string
	// Promote is the executor's explicit verdict that a reflex needs the normal
	// compiled path. Text remains the useful partial discovered before stopping.
	Promote bool
	// SplitRequest is the leaf's own finding that it is holding more than one
	// agent's job, and its account of how the job divides. It is for the
	// settlement path that grows the graph: nil — every leaf that never asked,
	// which is every leaf outside swarm mode — is what keeps that path unentered
	// and this field free.
	//
	// It is separate from Promote because the two say opposite things about the
	// work. Promote says "this is bigger than the shape I was given, run it
	// again properly"; this says "this is several things, and here they are" —
	// the difference between an escalation and a division. Text remains the
	// partial the leaf produced before it stopped, and the parts consume it.
	SplitRequest *SplitRequest
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

	// Regressed names the project's own checks that passed before this work and
	// fail after it.
	//
	// It exists because a leaf's own new tests are the ONE signal that
	// structurally cannot see a regression: the leaf wrote them, so they test
	// what the leaf was thinking about and nothing else. Three graded runs in
	// the 2026-08-28 sweep shipped patches that deleted attributes their
	// repositories already had, every hidden test failed on setup, and from
	// inside the run there was no signal at all — one of them ran its own tests
	// thirty-three times and scored zero. Test COUNT correlates with nothing.
	// Two measured runs of the project's own command, before and after, are the
	// only thing that can see it, and that is what fills this in.
	//
	// NIL MEANS NO CLAIM — nobody looked, because the project declares no
	// verification entrypoint, or the leaf's wall could not afford the reading,
	// or the command would not run, or the tree never changed. Empty-non-nil is
	// not a distinction anything downstream needs and nothing writes one: this
	// field is nil or it is populated. A populated one is a finding the gate
	// raises itself, with no citation to weigh, because the person never had to
	// ask for their repository to keep working (docs/design/gate/SETTLEMENT.md
	// §4, FAILSAFE.md clause 2).
	Regressed []string

	// OwnFailing names the red checks that FIRST APPEARED AFTER THE BASELINE:
	// the ones this run wrote itself, and did not get passing.
	//
	// It is what Regressed used to swallow. A leaf whose own new checks are red
	// has not finished; a leaf that turned somebody else's check red has broken
	// the repository, and calling the first one the second is how happy-dom's
	// nemotron n1 run was failed for breaking checks it had written that hour
	// while the grader scored the same tree 9 of 9.
	//
	// Nil on every worker that cannot take two readings of the tree, which reads
	// as no claim.
	OwnFailing []string

	// Removed names the PUBLIC names this work deleted: a name the tree spelled
	// before the job's first change that the finished tree does not, in the
	// files the run's own record says it changed.
	//
	// It is Regressed's other half and it is the half a suite cannot see. A
	// check proves something is exercised; it never proves nothing else exists.
	// igel s11 deleted eight public class attributes off `Igel` and its own
	// reading of the finished tree came back BETTER — named 2 → 14, red 2 → 0 —
	// while all twenty-four hidden tests failed at setup on `Igel.results_path`.
	//
	// Nil on every worker that cannot take two readings of the tree, which reads
	// as no claim and never as nothing removed. See verify.Surface.
	Removed []string

	// Unbound names what this work READS that nothing in the tree binds: an
	// attribute a class reaches for and no code assigns, a binding an import
	// asks an in-tree module for that the module does not define.
	//
	// It is the question one step further back than Removed. That one compares
	// two readings and reports a name that USED to be there; this needs only the
	// tree as it stands, and it is the one measurement a run gets on a project
	// with no baseline at all. igel s14 imported `temp_post_req_data_path` from
	// a module it had just stopped binding it in, all twenty-four hidden tests
	// failed on `ImportError`, and the only thing the run could say was that its
	// own checks were red. See verify.UnboundReferences.
	//
	// Nil on every worker with no workspace to read, which reads as no claim.
	Unbound []string

	// Verification is the whole photograph the two readings above came out of:
	// the entrypoint that was run, the budget it was run on, and both readings'
	// complete rosters rather than only their red halves.
	//
	// Regressed is one subtraction over it. The delivery gate needs the other
	// two. WHICH CHECKS EXIST answers whether anything at all exercises a
	// behaviour the request asked for, and WHICH CHECKS STOPPED EXISTING is the
	// one thing that catches a worker deleting the test that was failing it —
	// neither question can be asked of a list of failures. The Taken flag is
	// what keeps the gate honest about a project that declares no verification:
	// nobody looked and nothing may be concluded, which is a different fact from
	// a suite that came back clean. See docs/design/gate/ACCEPTANCE.md.
	//
	// A zero value is a photograph nobody took, which is what every worker that
	// cannot take two readings leaves here.
	Verification verify.Reading

	// Account is the worker's structured account of the work itself: the files
	// it changed, the checks it ran, and what each one found. See [Account] for
	// why a leaf that reports only prose is expensive.
	//
	// It is a pointer and it is usually nil. Only a worker that can observe its
	// own change set and run its own verifier has anything to put here; every
	// other leaf leaves it unset, which reads as "no claim" — the same silence
	// Baseline uses, and for the same reason.
	Account *Account

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

// SplitPart is one of the jobs a leaf found inside its own assignment.
//
// Title is what the part is called where a person reads the graph. Summary is
// the one line that tells a planner what this part is FOR, so a division can be
// weighed against its siblings without opening any of them. Brief is the part's
// whole assignment, written to be handed to an agent that will read nothing
// else — which is the ownable-subject test in structural form: a brief that has
// to say "after the previous part finishes" is describing a phase, and a phase
// is not a part.
type SplitPart struct {
	Title   string
	Summary string
	Brief   string
}

// SplitRequest is a leaf saying, in the middle of its own run, that it is
// holding more than one agent's job.
//
// Until this existed a node could only grow the graph by failing first: it
// spent its whole budget, settled overrun, and the remainder was replanned
// afterwards — the same decision this carries, bought at the price of a wasted
// leaf. So the cheap version is the one the worker asks for, and the expensive
// one stays as the backstop for the leaf that never noticed.
//
// It is a request and not an instruction. What is done with it is the
// settlement path's business and it goes through the same governor every other
// way of growing a running job goes through, so a leaf that asks to divide into
// nine gets whatever the caps and the planner between them allow — including
// nothing, which delivers the partial exactly as a refused overrun does.
type SplitRequest struct {
	// Parts are the jobs the leaf believes it is holding, in no particular
	// order — a division exists to be run at the same time, and parts that have
	// an order are a sequence somebody has mislabelled.
	Parts []SplitPart
	// Evidence is what the leaf actually read or discovered that revealed the
	// division: the file it opened, the count it found, the shape of the thing
	// in front of it. It is here because a division decided from the assignment
	// alone is the division the build already made and refused; what makes this
	// one worth a round is that it was made against what is really there, and a
	// planner handed the request without the finding cannot tell the two apart.
	Evidence string
}

// Valid reports whether a request is one the growth path may act on: two or
// more parts, each of which names itself and carries an assignment.
//
// A one-part "division" is the split that only restates, which plan.WorthKeeping
// would refuse a round later at the price of a planning call — so it is refused
// here, for free. A part with no brief is a title with nothing behind it, and
// handing an agent one is how a node comes to be executed against its own name.
//
// It is nil-safe because every caller reaches it through a field that is nil on
// every leaf that never asked.
func (r *SplitRequest) Valid() bool {
	if r == nil || len(r.Parts) < 2 {
		return false
	}
	for _, part := range r.Parts {
		if strings.TrimSpace(part.Title) == "" || strings.TrimSpace(part.Brief) == "" {
			return false
		}
	}
	return true
}

// ranLimit and ranArgumentBytes bound the record. Forty calls is well past the
// length of any single verification pass and small enough to hand to a judge
// whole; the argument clip keeps a command recognisable without carrying a
// pasted file into someone else's context.
//
// These two stay absolute where every other bound in this package became a
// share of a window, and the exception is the reason rather than an oversight.
// The record is not this leaf's memory — it is a fixed-size artifact handed to
// SOMEBODY ELSE, a judge or a reconciler reading many nodes at once, and what
// bounds it is that reader's budget rather than this worker's model. Sizing it
// from the producer's window would let one long-context leaf eat the judge's
// whole prompt, which is the failure a relative bound is supposed to prevent,
// arriving from the other direction.
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

	// StopOverrun is a leaf that was landed for running far past what work of
	// its kind has ever cost on this machine. Nothing writes it any more; it is
	// kept because a leaf run recorded before it was retired still carries the
	// word on disk (internal/store/leafrun.go), and a reason a reader cannot
	// name is worse than one nothing produces. It is separate from StopBudget
	// because nothing ran out, and from StopError because nothing failed.
	StopOverrun StopReason = "overrun"

	// StopSplit is the cooperative ending: the leaf found mid-work that it was
	// holding several agents' jobs, said what they were, and stopped so they
	// could run instead of it.
	//
	// It is its own reason rather than one of the endings above because every
	// one of those would misreport it. StopDone says the work is finished and
	// it is not. StopPromote says the shape was wrong and the same work should
	// be run again properly, which is an escalation and not a division.
	// StopBudget and StopOverrun say a resource ran out, and nothing did — the
	// grant was untouched and the leaf gave it back.
	//
	// It deliberately does NOT make Outcome.Overran true. Overran is the
	// question "was there work left when the resources ran out", and the
	// continuation subsystem it gates exists to replan a remainder from an
	// exhaustion. A cooperative split has its own path with its own reason in
	// the growth journal, and letting both fire on one settlement would spend
	// two rounds on one decision.
	StopSplit StopReason = "split"

	// StopStuckTimeout is recorded when three identical timeouts of the same
	// command end the round: the command does not finish here, and repeating it
	// further would only burn the wall. It is separate from StopDeadline because
	// the clock still has time, and from StopError because the provider did not
	// fail — the command is simply not one that returns inside the timeout the
	// tool runs it with.
	StopStuckTimeout StopReason = "stuck-timeout"

	// MaxIdenticalTimeouts is how many identical timed-out tool calls (same
	// command text, same timeout) the loop tolerates before ending the round.
	MaxIdenticalTimeouts = 3
)

// OutOfRoom reports that this ending is a leaf THAT WAS STILL WORKING when
// something it could not argue with stopped it: its tokens, its turns, or its
// clock.
//
// It is a method on the reason rather than a rule at each reader because the
// answer travels: the settlement asks it of an Outcome, and the scheduler asks
// it of an ExecResult that holds nothing but this string. Two spellings of one
// question is how a leaf came to be judged done on one side of a seam and cut
// off on the other.
//
// The empty reason answers false, and every caller that carries this across a
// seam says separately whether an ending was recorded at all — a StopReason is
// a string, and its zero value must never be readable as "it finished fine".
func (s StopReason) OutOfRoom() bool {
	switch s {
	case StopBudget, StopTurnCap, StopOverrun, StopDeadline:
		return true
	}
	return false
}

// Abandoned is the node watchdog's own ending: the executor was still inside a
// worker that had already run past every limit it was given, and the runner
// stopped waiting for it.
//
// It is a type rather than fmt.Errorf so that the fact it carries — the clock
// ran out, nothing about the work refused — survives the trip to whoever decides
// what happens next. A retry reading this by matching on the words "abandoned"
// would be a second, private answer to a question the ending already answers,
// and the first thing to go wrong with a second answer is that it disagrees.
// The message is unchanged from the sentence this replaced.
type Abandoned struct {
	// After is the watchdog it outlived.
	After time.Duration
}

func (a *Abandoned) Error() string {
	if a == nil {
		return ""
	}
	return fmt.Sprintf("executor did not return within %s; abandoned", a.After.Round(time.Second))
}

// Timeout satisfies the same interface net.Error uses, which is how a caller
// asks "was this the clock?" without knowing which layer answered.
func (a *Abandoned) Timeout() bool { return true }

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

// Mutator is a worker whose product is a change to the workspace itself rather
// than a message about it.
//
// It is a capability interface rather than a method on Executor because it is a
// fact about a minority of workers and every reader of it is optional: a build
// with only the generalist answers no to everything here and behaves exactly as
// it did. The distinction it draws is the one that decides whether a second
// attempt at a leaf is worth anything. A worker that produces prose can always
// produce better prose by being run again; a worker that produces a diff, whose
// diff has already landed and whose checks are already green, cannot — running
// it again re-executes a whole pipeline against a tree where the work is
// finished, which was measured at 23 model calls, zero edits and 80% of the
// leaf's spend.
//
// It is deliberately a question about the executor rather than a name: nothing
// here names a worker, so a mutating program is a registration and not an edit
// to the repair path.
type Mutator interface {
	// Mutates reports that this worker's deliverable is a change to the
	// workspace. It is a property of the worker and never of one run.
	Mutates() bool
}

// Mutates asks the question of any executor, including the ones that have never
// heard of it. Nil and non-mutating both answer false.
func Mutates(executor Executor) bool {
	mutator, ok := executor.(Mutator)
	return ok && mutator.Mutates()
}

// Registry picks an executor by subharness. Nearly every node carries none and
// resolves to the general loop; the lookup is what makes adding a specialised
// worker a registration rather than a change to the scheduler.
//
// IT IS ALSO THE SUBHARNESS REGISTRY, and there is deliberately only the one.
// The name→worker lookup with a fallback that this type has always been is
// exactly the shape a typed subharness needs, so the subharness contract GREW
// this rather than standing a second registry beside it (docs/SUBHARNESS-PRD.md
// §3). The two halves answer two different questions about the same names:
//
//   - executors/fallback below serve a LEAF that named a worker. [Registry.For]
//     degrades to the generalist for an unknown name, because a plan that asked
//     for a worker this build does not have should still get its work done.
//   - runners/bundles serve a PERSON or a program that named a subharness.
//     [Registry.Subharness] refuses an unknown name, because typing one that
//     does not exist and quietly getting a different one is worse than being
//     told. See runner.go for the lookup order across the layers.
type Registry struct {
	executors map[string]Executor
	fallback  Executor
	// runners is the compiled-in Go subharnesses, layer zero of the lookup.
	runners map[string]Runner
	// bundles is the stores in front of the registry, kept sorted by layer so
	// the lookup order lives in the [Layer] constants and nowhere else. The
	// store lane fills it through [Registry.UseBundles]; a build with no store
	// wired has none, and answers about compiled-in subharnesses only.
	bundles []bundleLayer
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
//
// A judged hand-back counts. The straggler was still working when it was told
// to land — that is the entire finding against it — so the question "is there
// work left here" is exactly the question the continuation subsystem exists to
// answer about it, and answering it is the "divide" arm of the judgement the
// hand-back was made to reach.
func (o *Outcome) Overran() bool {
	return o.Stop == StopBudget || o.Stop == StopTurnCap || o.Stop == StopOverrun ||
		o.Exhausted == StopBudget || o.Exhausted == StopOverrun
}

func (o *Outcome) String() string {
	return fmt.Sprintf("%s in %d turns, %d tool calls, $%.4f", o.Stop, o.Turns, o.ToolCalls, o.Usage.Cost)
}

// RanOutOfRoom reports that an error is a worker that was still working when
// the clock stopped it, and how long it was given.
//
// It exists so that "the ending was the clock" is asked of the TYPE rather than
// of the sentence, at every level that has to decide what happens next. The one
// that matters is the node's: an ending that is exhaustion is not a failure, so
// the node goes back on the queue with its record intact rather than being
// settled failed with a run's worth of work in it (see resident.Runner.runOne).
func RanOutOfRoom(err error) (time.Duration, bool) {
	var abandoned *Abandoned
	if errors.As(err, &abandoned) && abandoned != nil {
		return abandoned.After, true
	}
	return 0, false
}

// Requeued is the whole rule an exhausted node is judged by, and it is asked by
// both schedulers: the resident's, which drives `aforge do` and the chat, and
// the one-shot [Scheduler] that drives `aforge run`.
//
// AN ENDING THAT IS EXHAUSTION IS NOT A VERDICT ON THE WORK. The worker ran out
// of the room it was given; that is the queue's input and the growth governor's,
// so the node is offered again and the next claim carries on from the record
// this one left. The ink run of 2026-08-29 journaled exactly that sentence and
// then settled the node failed in the same second, over a twenty-six kilobyte
// patch and seventy-three minutes of unspent wall — because the sentence was in
// one place and the decision was in another.
//
// IT IS GATED ON THERE BEING SOMETHING TO RESUME FROM, which is what keeps it
// from being an unbounded retry: a claim that reads an empty record is the same
// cold start again, and an attempt that recorded not one turn before the clock
// stopped it has told us the only thing it is going to.
//
// The record is read through a function rather than passed in because reading it
// costs a query on the resident's side, and the question is only reached for an
// ending that is exhaustion in the first place — a run whose leaves fail for
// ordinary reasons must not pay for a record it will never consult. What comes
// back is the room the worker was given and how much of its work survived, which
// is what the release sentence and the stream line are both composed from.
func Requeued(err error, record func() int) (allowed time.Duration, recorded int, requeue bool) {
	allowed, spent := RanOutOfRoom(err)
	if !spent || record == nil {
		return 0, 0, false
	}
	if recorded = record(); recorded <= 0 {
		return 0, 0, false
	}
	return allowed, recorded, true
}

// Meter is one bound, named, with what it reached and what it allowed.
//
// It is a value rather than a sentence because the two readers want different
// things from it: the journal wants the figures so a later run can be compared
// with this one, and the person wants a line. Composing the line from the
// figures keeps the two from disagreeing, which is the whole of the defect it
// answers — the ink run of 2026-08-29 printed a grant of 150,000 beside a leaf
// that had been landed by a different ceiling at 240,000.
type Meter struct {
	// Name is the bound in one word, for a reader and for a grep. The four the
	// loop writes have constants because two files spell them and one of them
	// decides whether the figures are re-read at land time: see MeterCost.
	Name string `json:"name,omitempty"`
	// Reached and Allowed are the bound's own two numbers, in the bound's own
	// unit. Zero Allowed means the bound has no figure worth printing (a
	// structural detector rather than a ceiling).
	Reached int `json:"reached,omitempty"`
	Allowed int `json:"allowed,omitempty"`
	// Unit is what those numbers count, so a line can be composed without the
	// reader having to know which bound spells its allowance in what: "tokens",
	// "turns", "prompt tokens sent".
	Unit string `json:"unit,omitempty"`
}

// The bounds the loop writes down, spelled once.
//
// MeterCost and MeterDeadline are LIVE: both are read when the landing reserve
// is granted and both keep moving while the landing turns run, so what they
// held at the grant is not what the leaf actually reached. They are re-read at
// land time. MeterTurns and MeterNoProgress are counts of the loop itself and
// are already final at the moment they are written.
const (
	MeterCost       = "cost"
	MeterDeadline   = "deadline"
	MeterTurns      = "turns"
	MeterNoProgress = "no-progress"
)

// Named reports that a bound actually said something.
func (m Meter) Named() bool { return strings.TrimSpace(m.Name) != "" }

// Words is the bound in a person's own sentence, with its figures.
func (m Meter) Words() string {
	if !m.Named() {
		return ""
	}
	if m.Allowed <= 0 {
		return m.Name
	}
	unit := m.Unit
	if unit == "" {
		unit = "tokens"
	}
	return fmt.Sprintf("%s: %d of %d %s", m.Name, m.Reached, m.Allowed, unit)
}
