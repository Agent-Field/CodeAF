package orchestrate

// THE RUN: a scheduler that never thinks, beside a planner that never works.
//
// Everything here is arranged around one refusal — EXECUTION NEVER WAITS ON
// THOUGHT. A node's needs are the only launch gate, they are checked in code,
// and the moment they are met the node goes. The planner is called once at the
// start and once per completion, on a goroutine of its own, and what it answers
// lands on the frontier whenever it lands. A planner that takes forty seconds
// to say NOOP costs the run nothing, because the run was never stopped.
//
// THE FOUR LAWS THE LOOP IS WRITTEN AGAINST.
//
//   - THE COMMITMENT LAW. An amendment adds and cancels PENDING nodes. A node
//     that is running was launched on a judgement somebody already made, and a
//     planner allowed to retract that judgement mid-flight would leave work in
//     the world with nothing to attach it to (amend.go holds the check).
//   - ONE TANK. Every model call anywhere in the run — the nodes, the planner,
//     the synthesis — bills against one fuel cap (fuel.go). At the cap the
//     scheduler stops launching, in-flight nodes finish, and Run BLOCKS at the
//     gate until somebody answers it.
//   - DIGESTS ONLY. Dependents and the planner see a node's condensed output,
//     never its artifact. The planner is the one big-context call in the system
//     and it stays small on purpose.
//   - THE RUN ALWAYS SETTLES. A planner that never says done, a node whose
//     needs failed, a graph that ran out of launchable work — each of those
//     ends the loop and goes to synthesis over what is actually there. The one
//     way to hang is a person who never answers the gate, which is a person's
//     decision and is resumable.

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// DefaultLanes is how many nodes run at once when the caller names no number.
// It is small because a node is small: parallelism here is node COUNT, and a
// width that outruns the provider's pacing buys queueing, not speed.
const DefaultLanes = 4

// SynthesisID is what the closing call is called in the trace. It is not a
// node — it is never scheduled, never cancellable, and its cost is the run's
// rather than any node's — and it has an id at all so that a surface drawing
// the run's spend has a name for the last thing it paid for.
const SynthesisID = "synthesis"

// Options is everything about a run that is not the goal, the planner or the
// executor: the tank, the width, and the three lanes a run talks on.
//
// The callbacks are how this package says things without knowing what a
// surface is. They are called with no lock held and they must not block: the
// session's own emit (orchestrate.go) fans out to watchers on buffered
// streams, which is the shape they are written for.
type Options struct {
	// Cap is the fuel tank in dollars. Zero is NO CAP — a run nobody bounded —
	// and it is the caller's decision, not a default this package invents.
	Cap float64
	// Lanes bounds how many nodes execute at once; zero is [DefaultLanes].
	Lanes int

	// OnNote carries one planner note, in the order the planner wrote them.
	OnNote func(text string)
	// OnFuel is the gauge's early warning, fired ONCE when spend crosses
	// [WarnMark]. It is not a running meter: a surface that wants the figure
	// at any other moment reads it off [Orchestrator.Snapshot].
	OnFuel func(f Fuel)
	// OnPause fires when the tank is empty and the run has stopped launching.
	// The answer comes back through [Orchestrator.Resolve].
	OnPause func(f Fuel)
}

// New builds a run. Nothing starts until Run is called.
func New(goal string, planner Planner, exec Executor, opts Options) *Orchestrator {
	lanes := opts.Lanes
	if lanes <= 0 {
		lanes = DefaultLanes
	}
	return &Orchestrator{
		goal:    strings.TrimSpace(goal),
		planner: planner,
		exec:    exec,
		lanes:   lanes,
		onNote:  opts.OnNote,
		onFuel:  opts.OnFuel,
		onPause: opts.OnPause,
		index:   map[string]*NodeStatus{},
		fuel:    Fuel{Cap: opts.Cap},
		gate:    make(chan string, 1),
	}
}

// completed is one node's return from the executor, on its way back to the
// loop that launched it.
type completed struct {
	id     string
	digest string
	cost   float64
	err    error
}

// thought is one planner call's return. err is not a failure of the run: a
// planner that could not answer is a NOOP with a note (see [Orchestrator.absorb]).
type thought struct {
	amendment Amendment
	err       error
}

// Run walks the frontier until there is nothing left to walk, then synthesizes.
//
// The error is the OPENING call's, and only ever that one: a run whose first
// planner call failed has no frontier and nothing to do, and there is no
// partial trace worth handing back. Every later failure — a node that broke, a
// planner that answered nonsense, a synthesis that would not come back — is a
// fact IN the snapshot rather than a reason to lose it.
func (o *Orchestrator) Run(ctx context.Context) (Snapshot, error) {
	o.publish()
	// The opening call is the one moment the run legitimately waits on the
	// planner: there is no work to overlap it with.
	opening, err := o.planner.Plan(ctx, o.view())
	if err != nil {
		return o.Snapshot(), fmt.Errorf("orchestrate: the opening plan failed: %w", err)
	}
	o.absorb(ctx, thought{amendment: opening})

	var (
		completions = make(chan completed, o.lanes)
		thoughts    = make(chan thought, o.lanes)
		running     int
		thinking    int
	)
	for {
		running += o.launch(ctx, completions)
		if o.settled(running, thinking) {
			break
		}
		select {
		case done := <-completions:
			running--
			o.land(done)
			if !o.worthThinking() {
				continue
			}
			// THE PLANNER IS FIRED AND NOT AWAITED. The next iteration launches
			// whatever this completion unblocked, and the amendment lands on the
			// frontier whenever it arrives — possibly after two more nodes have
			// already finished, which is fine: it is amending a frontier, not
			// approving one.
			thinking++
			o.think(ctx, thoughts)
		case landed := <-thoughts:
			thinking--
			o.absorb(ctx, landed)
		case answer := <-o.gate:
			o.openGate(answer)
			// A RESUMED RUN THINKS ONCE, whatever the frontier looks like.
			// Somebody has just decided to spend more on this; ending the run
			// because the last completion happened to arrive with nothing
			// pending behind it would answer that decision with a synthesis.
			if o.worthThinking() {
				thinking++
				o.think(ctx, thoughts)
			}
		case <-ctx.Done():
			// The window ran out, or the session left. The run SETTLES rather
			// than simply stopping being read: a snapshot left saying "running"
			// forever is a room drawing work that nothing is doing.
			o.note("the run ended early: " + ctx.Err().Error())
			o.finish("")
			return o.Snapshot(), ctx.Err()
		}
	}
	return o.synthesize(ctx), nil
}

// think puts one planner call in flight. The send is guarded because a thought
// can outlive the loop: the context can die while several calls are out, and a
// goroutine parked on a channel nobody will read again is a leak for the life
// of the process.
func (o *Orchestrator) think(ctx context.Context, out chan<- thought) {
	go func() {
		amendment, err := o.planner.Plan(ctx, o.view())
		select {
		case out <- thought{amendment: amendment, err: err}:
		case <-ctx.Done():
		}
	}()
}

// worthThinking reports whether this completion is worth a planner call.
//
// IT IS A FUEL DECISION and not a scheduling one. A run that has its DonePlan,
// or that somebody stopped, or that is parked at the gate is a run where the
// only thing an amendment could change is a frontier nothing will launch from
// — and the call would bill the tank for it. Nothing is lost by skipping it:
// launching is driven by the needs a completion met, never by the answer.
func (o *Orchestrator) worthThinking() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return !o.finishing && !o.stopped && !o.paused
}

// settled reports whether the loop is over. It is the whole termination
// argument in one function, and every branch of it is a way a run ENDS rather
// than hangs.
//
// PAUSED IS DELIBERATELY NOT A WAY OUT. A run at the gate has work it is
// allowed to do and no money to do it with, and the loop falls through to a
// select with nothing but the gate and the context in it — which is exactly
// "Run blocks until somebody answers".
func (o *Orchestrator) settled(running, thinking int) bool {
	if running > 0 || thinking > 0 {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped || o.finishing {
		return true
	}
	if o.paused {
		return false
	}
	// Nothing in flight, nothing thinking, and nothing that could be launched:
	// what is left is blocked behind a node that failed or was never added, and
	// no further completion is coming to unblock it.
	for _, node := range o.nodes {
		if node.State != Queued && node.State != Ready {
			continue
		}
		if o.metLocked(node) {
			// It could have gone: the lanes were full when launch last looked,
			// and something is running that will free one.
			return false
		}
	}
	return true
}

// launch starts every node whose needs are met, up to the lane cap, and
// answers with how many it started.
//
// READY IS A REAL STATE and not a step this skips. A node whose needs are met
// and whose lane has not come up is a different fact from one still waiting on
// its dependencies, and a surface drawing the frontier says so.
func (o *Orchestrator) launch(ctx context.Context, out chan<- completed) int {
	o.mu.Lock()
	if o.paused || o.finishing || o.stopped {
		o.mu.Unlock()
		return 0
	}
	slots := o.lanes
	for _, node := range o.nodes {
		if node.State == Running {
			slots--
		}
	}
	type start struct {
		node Node
		deps []NodeStatus
	}
	var starts []start
	for _, node := range o.nodes {
		if node.State != Queued && node.State != Ready {
			continue
		}
		if !o.metLocked(node) {
			continue
		}
		node.State = Ready
		if slots <= 0 {
			continue
		}
		slots--
		node.State = Running
		starts = append(starts, start{node: node.Node, deps: o.dependsLocked(node)})
	}
	o.mu.Unlock()
	if len(starts) > 0 {
		o.publish()
	}
	for _, each := range starts {
		go func(n Node, deps []NodeStatus) {
			digest, cost, err := o.exec.Exec(ctx, n, deps)
			out <- completed{id: n.ID, digest: digest, cost: cost, err: err}
		}(each.node, each.deps)
	}
	return len(starts)
}

// metLocked reports whether every one of a node's needs is Done. A need that
// FAILED is not met and never will be — the node stays queued, the planner is
// told what failed, and it is the planner's business whether to replace the
// dead prerequisite or cancel what waited on it.
func (o *Orchestrator) metLocked(node *NodeStatus) bool {
	for _, need := range node.Needs {
		got, known := o.index[need]
		if !known || got.State != Done {
			return false
		}
	}
	return true
}

// dependsLocked is what a node is handed: its needs' statuses, in the order it
// named them. Digests only — the executor never sees an upstream artifact.
func (o *Orchestrator) dependsLocked(node *NodeStatus) []NodeStatus {
	deps := make([]NodeStatus, 0, len(node.Needs))
	for _, need := range node.Needs {
		if got, known := o.index[need]; known {
			deps = append(deps, *got)
		}
	}
	return deps
}

// land records one node's outcome and bills what it cost.
//
// A FAILURE IS A FACT, NOT AN END. The error is kept on the node, travels into
// the next planner View, and nothing cascades from it here: what a dead node
// means for the rest of the graph is a judgement, and judgements are the
// planner's.
func (o *Orchestrator) land(done completed) {
	o.mu.Lock()
	node, known := o.index[done.id]
	if known {
		node.Cost += done.cost
		node.Digest = strings.TrimSpace(done.digest)
		if done.err != nil {
			node.State = Failed
			node.Err = done.err.Error()
		} else {
			node.State = Done
		}
	}
	o.mu.Unlock()
	o.Charge(done.cost)
	o.publish()
}

// synthesize is the run's last call: one executor turn over every node's
// digest, grounded in node ids.
//
// A STOPPED RUN IS NOT SYNTHESIZED. "stop" is somebody saying the run is over
// and they do not want to pay for one more call; the partial trace is the
// answer, and it is already in the snapshot.
func (o *Orchestrator) synthesize(ctx context.Context) Snapshot {
	o.mu.Lock()
	// Past here the run is closing: the tank still meters, but there is nothing
	// left to hold back, so it no longer stops at the gate (fuel.go).
	o.finishing = true
	stopped := o.stopped
	brief := ""
	if o.plan != nil {
		brief = strings.TrimSpace(o.plan.Brief)
	}
	results := make([]NodeStatus, 0, len(o.nodes))
	for _, node := range o.nodes {
		if node.State == Done || node.State == Failed {
			results = append(results, *node)
		}
	}
	o.mu.Unlock()

	if stopped || len(results) == 0 {
		o.finish("")
		return o.Snapshot()
	}
	if brief == "" {
		// The run ended without a DonePlan — the frontier simply ran out, or the
		// person said "finish". The goal is the brief, which is what the planner
		// would have written anyway.
		brief = "Answer the goal from the work that was done: " + o.goal
	}
	answer, cost, err := o.exec.Exec(ctx, Node{ID: SynthesisID, Goal: synthesisGoal(brief)}, results)
	o.Charge(cost)
	if err != nil {
		o.note(fmt.Sprintf("the synthesis could not be written: %v", err))
		o.finish("")
		return o.Snapshot()
	}
	o.finish(strings.TrimSpace(answer))
	return o.Snapshot()
}

// synthesisGoal is the brief plus the one rule the run adds to it: every claim
// names the node it came from. A synthesis that cannot cite is a paragraph
// somebody has to re-derive the run to check.
func synthesisGoal(brief string) string {
	return brief + "\n\nYou have been handed each node's digest and its id. " +
		"Ground every claim in the id it came from, inline, like (n3). " +
		"A claim no node's digest supports does not go in."
}

// finish closes the run: the answer, if there is one, and the Done flag every
// surface reads.
func (o *Orchestrator) finish(answer string) {
	o.mu.Lock()
	o.done = true
	o.answer = answer
	o.mu.Unlock()
	o.publish()
}

// ── steering, the gate, and what a surface reads ────────────────────────────

// Steer appends one line of the person's own instruction. It is carried on
// EVERY later View rather than delivered once: a planner call that arrived
// while the person was typing must not be the reason their correction is never
// seen, and steering outranks the plan.
func (o *Orchestrator) Steer(text string) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	o.mu.Lock()
	o.steer = append(o.steer, text)
	o.mu.Unlock()
	o.publish()
}

// Resolve answers the fuel gate: "topup:<dollars>", "finish", or "stop".
//
// It refuses an answer to a question nobody asked, and an answer to a question
// already answered, because both are a surface's bug and neither is something
// the run can act on quietly.
func (o *Orchestrator) Resolve(answer string) error {
	answer = strings.TrimSpace(strings.ToLower(answer))
	switch {
	case answer == GateFinish, answer == GateStop:
	case strings.HasPrefix(answer, GateTopup+":"):
		if _, err := topupAmount(answer); err != nil {
			return err
		}
	default:
		return fmt.Errorf("orchestrate: %q is not an answer to the fuel gate (topup:<dollars>, finish, stop)", answer)
	}
	o.mu.Lock()
	paused := o.paused
	o.mu.Unlock()
	if !paused {
		return fmt.Errorf("orchestrate: this run is not at the gate")
	}
	select {
	case o.gate <- answer:
		return nil
	default:
		return fmt.Errorf("orchestrate: the gate is already answered")
	}
}

// Snapshot returns the latest published state. It is safe for concurrent
// reads: what it hands back was built under the lock and is never written
// afterwards.
func (o *Orchestrator) Snapshot() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.snap
}

// Goal is what the run was asked for, for a surface that has the run and not
// the sentence that started it.
func (o *Orchestrator) Goal() string { return o.goal }

// publish rebuilds the snapshot a surface polls. Every slice in it is a fresh
// copy, which is what makes [Orchestrator.Snapshot] lock-free for its reader.
func (o *Orchestrator) publish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	nodes := make([]NodeStatus, 0, len(o.nodes))
	for _, node := range o.nodes {
		nodes = append(nodes, *node)
	}
	o.snap = Snapshot{
		Goal:   o.goal,
		Nodes:  nodes,
		Fuel:   o.fuel,
		Notes:  append([]string(nil), o.notes...),
		Steer:  append([]string(nil), o.steer...),
		Paused: o.paused,
		Done:   o.done,
		Answer: o.answer,
	}
}

// view is what the planner is shown: the goal, what finished, what has not,
// the gauge, and every line the person has typed at the run.
func (o *Orchestrator) view() View {
	o.mu.Lock()
	defer o.mu.Unlock()
	v := View{Goal: o.goal, Fuel: o.fuel, Steer: append([]string(nil), o.steer...)}
	for _, node := range o.nodes {
		switch node.State {
		case Done, Failed:
			v.Results = append(v.Results, *node)
		default:
			v.Frontier = append(v.Frontier, *node)
		}
	}
	return v
}

// note records one line and says it out loud, exactly once.
func (o *Orchestrator) note(text string) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	o.mu.Lock()
	o.notes = append(o.notes, text)
	o.mu.Unlock()
	o.publish()
	if o.onNote != nil {
		o.onNote(text)
	}
}

// Orchestrator is one adaptive run. Construct it with [New]; everything below
// mu is written by the loop and read by whoever is watching.
type Orchestrator struct {
	goal    string
	planner Planner
	exec    Executor
	lanes   int
	onNote  func(string)
	onFuel  func(Fuel)
	onPause func(Fuel)

	// gate carries the one answer a paused run is waiting for. It is buffered
	// to one so [Orchestrator.Resolve] never blocks a surface's goroutine, and
	// a second answer to the same pause is refused rather than queued.
	gate chan string

	// mu guards everything below. It is never held across a planner call, an
	// executor call, or a callback: those are the three things that take
	// seconds, and a snapshot poll must never wait on any of them.
	mu    sync.Mutex
	nodes []*NodeStatus
	index map[string]*NodeStatus
	notes []string
	steer []string
	fuel  Fuel
	// warned is the 80% note, fired once per tank: a top-up that leaves the
	// spend back under the mark re-arms it (fuel.go).
	warned bool
	// paused is the gate. finishing is a run on its way to synthesis — a
	// DonePlan, or a person who said "finish" — and stopped is a run that will
	// not synthesize at all.
	paused    bool
	finishing bool
	stopped   bool
	done      bool
	plan      *DonePlan
	answer    string
	// snap is the published copy; the room polls it.
	snap Snapshot
}
