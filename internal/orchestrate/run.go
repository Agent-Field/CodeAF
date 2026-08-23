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
	// Planner NAMES the model behind the [Planner] interface, for a surface to
	// draw beside the gauge ([Snapshot.Planner]). Nothing here reads it: which
	// model thinks is the caller's decision and this package only carries the
	// word, so an empty one costs a surface a segment and costs the run nothing.
	Planner string

	// OnNote carries one planner note, in the order the planner wrote them.
	OnNote func(text string)
	// OnFuel is the gauge's early warning, fired ONCE when spend crosses
	// [WarnMark]. It is not a running meter: a surface that wants the figure
	// at any other moment reads it off [Orchestrator.Snapshot].
	OnFuel func(f Fuel)
	// OnPause fires when the tank is empty and the run has stopped launching.
	// The answer comes back through [Orchestrator.Resolve].
	OnPause func(f Fuel)
	// OnNodes carries the crystallized graph every time any of it moves — a node
	// launched, a node landed, a node the planner added or a person cancelled. It
	// is the same slice [Snapshot.Nodes] carries, published at the same moment and
	// for a reader that cannot poll: the session registers a run's nodes as a task
	// FAMILY so the roster has a tree to draw (session's orchestrate.go), and a
	// registration driven by polling would be a clock asking a run whether
	// anything happened.
	//
	// IT IS THE WHOLE GRAPH AND NOT THE NODE THAT MOVED, because a publish is not
	// one transition: [Orchestrator.launch] starts up to a lane's worth at once,
	// and an amendment can add four nodes and cancel one. What moved is a
	// difference the reader already has to compute against what it drew last, so
	// this hands it the state rather than a guess at the event.
	//
	// The slice is the SNAPSHOT'S OWN and is read-only to the callback: writing
	// through it would edit what the next [Orchestrator.Snapshot] hands back.
	OnNodes func(nodes []NodeStatus)
}

// New builds a run. Nothing starts until Run is called.
func New(goal string, planner Planner, exec Executor, opts Options) *Orchestrator {
	lanes := opts.Lanes
	if lanes <= 0 {
		lanes = DefaultLanes
	}
	goal = strings.TrimSpace(goal)
	model := strings.TrimSpace(opts.Planner)
	return &Orchestrator{
		goal:         goal,
		planner:      planner,
		plannerModel: model,
		exec:         exec,
		lanes:        lanes,
		onNote:       opts.OnNote,
		onFuel:       opts.OnFuel,
		onPause:      opts.OnPause,
		onNodes:      opts.OnNodes,
		index:        map[string]*NodeStatus{},
		fuel:         Fuel{Cap: opts.Cap},
		gate:         make(chan string, 1),
		halt:         make(chan struct{}),
		// THE SNAPSHOT IS SEEDED, not left zero. A surface may poll before the
		// opening planner call returns — that call takes seconds and the page
		// opens immediately — and the two things that are true from construction
		// are the goal and who is thinking about it. Every later publish writes
		// the same two back.
		snap: Snapshot{Goal: goal, Planner: model},
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
	// EVERY CALL THE RUN MAKES RIDES A CONTEXT OF THE RUN'S OWN, hung off the
	// caller's. It is what [Orchestrator.Cancel] cuts, and it is the whole reason
	// a stop can mean "stop spending now": a cancel that only stopped LAUNCHING
	// would leave four child agents and a planner billing against a tank nobody
	// is watching any more.
	work, halt := context.WithCancel(ctx)
	defer halt()
	// halted is read ONCE and then dropped, because a closed channel is ready
	// forever and a select that kept picking it would spin. A nil channel blocks,
	// which is exactly what a run that has already been cut wants.
	halted := o.halt
	if o.wasStopped() {
		// Stopped before it began: there is no frontier to walk and no partial
		// trace to write up, and paying for an opening plan nobody will read
		// would be the first thing the stop was meant to prevent.
		o.finish("")
		return o.Snapshot(), nil
	}

	o.publish()
	// The opening call is the one moment the run legitimately waits on the
	// planner: there is no work to overlap it with.
	opening, err := o.planner.Plan(work, o.view())
	if err != nil {
		if o.wasStopped() {
			// Cancelled while the opening call was out. The error is the stop
			// arriving, not a run that failed, and it settles like one.
			o.finish("")
			return o.Snapshot(), nil
		}
		return o.Snapshot(), fmt.Errorf("orchestrate: the opening plan failed: %w", err)
	}
	o.absorb(work, thought{amendment: opening})

	var (
		completions = make(chan completed, o.lanes)
		thoughts    = make(chan thought, o.lanes)
		running     int
		thinking    int
	)
	for {
		running += o.launch(work, completions)
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
			o.think(work, thoughts)
		case landed := <-thoughts:
			thinking--
			o.absorb(work, landed)
		case answer := <-o.gate:
			o.openGate(answer)
			// A RESUMED RUN THINKS ONCE, whatever the frontier looks like.
			// Somebody has just decided to spend more on this; ending the run
			// because the last completion happened to arrive with nothing
			// pending behind it would answer that decision with a synthesis.
			if o.worthThinking() {
				thinking++
				o.think(work, thoughts)
			}
		case <-halted:
			// SOMEBODY STOPPED THE RUN. The states were already moved by
			// [Orchestrator.Cancel] — a pending node stops the instant the key is
			// pressed — and what is left to do here is cut the work: every node in
			// flight and every planner call loses its context on this line.
			//
			// The loop keeps turning afterwards rather than returning, so that the
			// cut nodes' completions come home: what they cost is metered, and what
			// they were is on the snapshot rather than left saying "running".
			halt()
			halted = nil
		case <-ctx.Done():
			// The window ran out, or the session left. The run SETTLES rather
			// than simply stopping being read: a snapshot left saying "running"
			// forever is a room drawing work that nothing is doing.
			o.note("the run ended early: " + ctx.Err().Error())
			o.finish("")
			return o.Snapshot(), ctx.Err()
		}
	}
	return o.synthesize(work), nil
}

// Cancel ends the run on a person's word, and it means STOP SPENDING NOW.
//
// It is the harder half of a pair the fuel gate already has one of. "stop" at
// the gate is a decision taken with nothing in flight — the tank emptied, the
// running nodes landed, and the question was asked afterwards. This is the same
// decision taken mid-run, so it has to do the thing the gate never had to: cut
// the context every node and every planner call is on, and throw away what they
// had got to. A node's partial output is not a result — it is a half-answer to
// a question nobody is waiting for any more — so it is dropped rather than
// digested ([Orchestrator.land]).
//
// WHAT IS KEPT IS THE TRACE. Every node that finished keeps its digest, the
// notes stay, the tank keeps what it metered, and the snapshot says Stopped.
// What does not happen is the synthesis: it is one more model call, and
// somebody who has just said stop is not asking to pay for it (see
// [Orchestrator.synthesize]).
//
// A PENDING NODE STOPS INSTANTLY, and it stops IN PLACE — marked [Cancelled],
// still in the graph — which is where this parts company with the planner's own
// cancel. An amendment DELETES the node it no longer wants (amend.go's
// dropLocked), because that is the planner changing its mind about work nobody
// has seen; this is a person ending work that is on their screen, and a chip
// that vanished under them would be the run denying it was ever asked for.
//
// It is IDEMPOTENT, and cancelling a run that has already settled does nothing
// at all.
func (o *Orchestrator) Cancel() {
	// The halt closes FIRST and exactly once. It is what a run already in flight
	// is watching, and a second press must never be the press that closes a
	// closed channel.
	o.halting.Do(func() { close(o.halt) })

	o.mu.Lock()
	if o.done || o.stopped {
		o.mu.Unlock()
		return
	}
	o.stopped = true
	// A GATE STILL UP IS A QUESTION ABOUT A MOMENT THAT HAS PASSED. The person
	// answered it by stopping, so it comes down here rather than being left for a
	// surface to draw over a run that is already over.
	o.paused = false
	for _, node := range o.nodes {
		if node.State == Queued || node.State == Ready {
			node.State = Cancelled
		}
	}
	o.mu.Unlock()

	o.publish()
	o.note(StoppedWord)
}

// StoppedWord is the one sentence this package says about a stopped run,
// wherever the stop came from. It is exported because the session says it back
// to the person ([session.Agent.Cancel]) and two spellings of one decision is
// one spelling too many.
const StoppedWord = "stopped; what finished is kept"

// wasStopped reports whether somebody has cancelled this run.
func (o *Orchestrator) wasStopped() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stopped
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
	if running > 0 {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	// A STOPPED RUN DOES NOT WAIT ON ITS PLANNER. Cancel cut the context every
	// call in flight is on, so a thought still out is one that will never be
	// delivered — [Orchestrator.think] drops it on ctx.Done — and counting it
	// would park the loop forever on a goroutine that has already given up.
	if o.stopped {
		return true
	}
	if thinking > 0 {
		return false
	}
	if o.finishing {
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
//
// A CUT NODE IS NOT A FAILURE AND KEEPS NOTHING. On a stopped run an error is
// the stop arriving — the context died under the node — so it lands [Cancelled]
// with its digest dropped, because whatever it managed to say is half an answer
// to a question nobody is waiting for. A node that finished CLEANLY before the
// cut reached it keeps its result: it is a fact, exactly as one that finished a
// second earlier would be.
func (o *Orchestrator) land(done completed) {
	o.mu.Lock()
	node, known := o.index[done.id]
	if known {
		node.Cost += done.cost
		switch {
		case o.stopped && done.err != nil:
			node.State, node.Digest, node.Err = Cancelled, "", ""
		case done.err != nil:
			node.State = Failed
			node.Digest = strings.TrimSpace(done.digest)
			node.Err = done.err.Error()
		default:
			node.State = Done
			node.Digest = strings.TrimSpace(done.digest)
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
	// THE ONE NODE THIS PACKAGE MINTS ITSELF, and it is named the same way every
	// other node is: from its id, by [NodeTitle]. No model is in this loop, so
	// there is nobody to ask for a name and nothing to cut one out of.
	closing := Node{ID: SynthesisID, Goal: synthesisGoal(brief)}
	closing.Title = NodeTitle(closing)
	answer, cost, err := o.exec.Exec(ctx, closing, results)
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
//
// ONE STOP WORD EVERYWHERE. "stop" never travels down the gate's channel: it is
// [Orchestrator.Cancel], because a run ended at the gate and a run ended from
// its page are one decision, and two settling paths for one decision is how two
// runs come to leave two different traces.
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
	if answer == GateStop {
		o.Cancel()
		return nil
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
//
// THE LOCK IS DROPPED BEFORE THE CALLBACK, on the rule mu is declared with: a
// hook belongs to whoever wired it, it may fan out to watchers, and a snapshot
// poll must never wait on somebody else's channel. What it is handed is the copy
// this call just built, so a reader gets the graph exactly as it was published
// rather than as it is by the time it looks.
func (o *Orchestrator) publish() {
	o.mu.Lock()
	nodes := make([]NodeStatus, 0, len(o.nodes))
	for _, node := range o.nodes {
		nodes = append(nodes, *node)
	}
	o.snap = Snapshot{
		Goal:    o.goal,
		Planner: o.plannerModel,
		Nodes:   nodes,
		Fuel:    o.fuel,
		Notes:   append([]string(nil), o.notes...),
		Steer:   append([]string(nil), o.steer...),
		Paused:  o.paused,
		Done:    o.done,
		Stopped: o.stopped,
		Answer:  o.answer,
	}
	o.mu.Unlock()
	if o.onNodes != nil {
		o.onNodes(nodes)
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
	// plannerModel is the word [Options.Planner] carried in, republished on
	// every snapshot and read by nothing here.
	plannerModel string
	exec         Executor
	lanes        int
	onNote       func(string)
	onFuel       func(Fuel)
	onPause      func(Fuel)
	onNodes      func([]NodeStatus)

	// gate carries the one answer a paused run is waiting for. It is buffered
	// to one so [Orchestrator.Resolve] never blocks a surface's goroutine, and
	// a second answer to the same pause is refused rather than queued.
	gate chan string

	// halt is CLOSED rather than sent on, because a stop is not an answer one
	// reader takes — it is a fact every part of the run has to see, whether it is
	// the loop parked on a select or a caller reading [Orchestrator.Cancel]
	// twice. halting is what makes closing it idempotent.
	halt    chan struct{}
	halting sync.Once

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
