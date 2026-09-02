package main

// THE ADAPTIVE RUN, on this rig — `-orchestrate`.
//
// THE QUESTION IT ASKS: a sub-harness is a graph a model DESIGNED, once, before
// any of the work existed. An adaptive run never designs a graph: a planner
// amends a live frontier on every completion, and the graph is whatever that
// leaves behind. So — does planning against results already in hand find
// parallelism the static design could not see, and does it pay for the extra
// planner calls it costs?
//
// The two halves are the contract's own (internal/orchestrate):
//
//	PLANNER   internal/orchestrate/prompt.md + this rig's chat client. Called
//	          once at the start and once per node completion; answers with an
//	          Amendment, salvaged and strictly decoded exactly as the designer's
//	          envelope is.
//	EXECUTOR  one agent turn per node: the run's goal, the node's own goal, the
//	          digests of the ids it needs. It writes its artifact and then its
//	          own DIGEST, which is the only part anything else in the run sees.
//
// THE DRIVER IS LOCAL AND SAYS SO. internal/orchestrate is a contract skeleton
// at this commit — Orchestrator has a Snapshot and no Run — so the scheduler
// below is written to the contract's stated semantics rather than imported:
// no wave barriers, a node launches the instant its needs are all Done, the
// planner is called on every completion and execution NEVER waits for it,
// amendments touch pending nodes only, one fuel tank meters every model call
// including the planner's. When Run lands, this file is what should be deleted
// rather than the thing that taught it something.
//
// Two things this rig does not exercise, said here rather than left to be
// noticed: STEERING is always empty (nobody is at the keyboard, so law 8 is
// untested), and `worktree: true` is recorded and reported and
// nothing isolates anything — the worktree path comes from the session
// (Config.WorktreePath) and this rig has no session; the goals it is validated
// against write no code, so the gap costs nothing here and would cost
// everything in the room.

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// The rig's two halves ARE the contract's two interfaces, asserted here so the
// day Run lands this driver is handed to it rather than ported into it.
var (
	_ orchestrate.Planner  = (*driver)(nil)
	_ orchestrate.Executor = (*driver)(nil)
)

// The adaptive-run goals. They are a separate table from the designer's because
// they ask a different question: not "does this decompose" — every goal does —
// but "does the decomposition CHANGE once the first results are in". Both of
// these have a shape whose second half is only knowable after the first half
// ran: you cannot write the adversarial check on a pick before you know which
// one was picked.
var orchGoals = []struct{ key, note, text string }{
	{
		key:  "o1",
		note: "four approaches · three dimensions · a pick · an attack on the pick",
		text: "Compare four approaches to local-first sync for a notes app (CRDT, OT, periodic full-sync, git-based) on conflict UX, offline latency, implementation risk; pick one, adversarially check the pick.",
	},
	{
		key:  "o2",
		note: "enumerate · a property set per class · boundaries · review the plan",
		text: "Design the test strategy for a payment webhook handler: enumerate failure classes, properties per class, boundary conditions, adversarially review the plan.",
	},
}

func pickOrch(which string) []struct{ key, note, text string } {
	var out []struct{ key, note, text string }
	lower := strings.ToLower(strings.TrimSpace(which))
	for _, goal := range orchGoals {
		if lower == "all" || lower == goal.key {
			out = append(out, goal)
		}
	}
	if len(out) == 0 && strings.TrimSpace(which) != "" {
		out = append(out, struct{ key, note, text string }{"own", "yours", which})
	}
	return out
}

// ── the run ─────────────────────────────────────────────────────────────────

// nodeRun is one node and everything the run learned about it. Output is the
// whole artifact, kept ONLY so this rig can print it: dependents and the planner
// are handed Digest and nothing else, which is the contract's whole reason for
// having a digest.
type nodeRun struct {
	orchestrate.NodeStatus
	Output string
	Began  time.Time
	Ended  time.Time
	Forced []string // needs this rig added for a write-scope collision
	// Unmet is the ids this node's GOAL names and its `needs` does not reach:
	// a node briefed to read work that nothing will hand it. See [driver.unmet].
	Unmet []string
}

// finished is the state after which a node is a fact and an amendment may not
// touch it.
func (n *nodeRun) finished() bool {
	return n.State == orchestrate.Done || n.State == orchestrate.Failed
}

// completion is one node coming back off the wire.
type completion struct {
	id     string
	digest string
	output string
	cost   float64
	tokens int
	err    error
	// truncated is finish=length: the node ran out of completion budget. It
	// matters more here than it would elsewhere, because the DIGEST is written
	// LAST — so a node that ran long delivers its work and loses the only part
	// of it the rest of the run will ever read.
	truncated bool
}

// planned is one planner turn coming back. It arrives on a channel rather than
// being applied where it was computed because the loop below owns the graph and
// nothing else may write it.
type planned struct {
	amendment orchestrate.Amendment
	cost      float64
	tokens    int
	elapsed   time.Duration
	err       error
}

type driver struct {
	chat *chatClient
	goal string

	planTokens  int
	nodeTokens  int
	maxParallel int
	gate        string

	// The graph and the tank. Only the run loop writes these.
	nodes  []*nodeRun
	fuel   orchestrate.Fuel
	notes  []string
	steer  []string
	paused bool
	done   bool
	answer string

	// What the report is made of.
	plannerRuns int
	plannerNoop int
	plannerFail int
	cancelled   []orchestrate.Cancel
	maxRunning  int
	truncated   int
	warned80    bool
	refused     []string // amendments' illegal parts, told back to the planner

	completions chan completion
	plans       chan planned
	inflight    bool // a planner turn is out; only one at a time
	owed        int  // completions whose planner turn has not been made yet
}

// oneOrchestrate is the whole mode for one goal.
func (d *driver) run(ctx context.Context) {
	d.completions = make(chan completion, 64)
	d.plans = make(chan planned, 64)

	// THE START CALL BLOCKS, and it is the only one that does. There is nothing
	// running for it to hold up, and a run whose first amendment is still in the
	// air has no frontier to schedule.
	section("call 0 · the planner, with nothing done")
	d.applyPlanned(d.planTurn(ctx, d.view(), nil))

	for {
		d.launch(ctx)
		if d.settled() {
			break
		}
		if !d.inflight && d.owed > 0 && !d.done {
			d.owed--
			d.inflight = true
			// The view and the refusals are taken HERE, in the loop that owns
			// them, and handed to the turn by value. A planner goroutine that
			// read the graph itself would be reading it while nodes land.
			view, refused := d.view(), d.refused
			d.refused = nil
			go func() { d.plans <- d.planTurn(ctx, view, refused) }()
		}
		select {
		case <-ctx.Done():
			fmt.Printf("\n!! interrupted — %d node(s) were still running\n", d.running())
			return
		case done := <-d.completions:
			d.complete(done)
			// EVERY completion is owed a planner turn (the contract says once
			// per completion). They are SERIALIZED rather than coalesced: two
			// planners reading the same frontier would both add the same work.
			// Serializing costs nothing in wall-clock — the nodes keep running
			// underneath, which is the law execution never blocks on thought.
			if !d.done {
				d.owed++
			}
		case answer := <-d.plans:
			d.inflight = false
			d.applyPlanned(answer)
		}
	}
	if still := d.running(); still > 0 {
		fmt.Printf("  · the planner said done with %d node(s) still running — they are abandoned, unmetered\n", still)
	}
	d.atTheGate(ctx)
}

// settled is the end of the loop: the planner said done, or there is nothing
// running, nothing launchable, and no thought outstanding.
func (d *driver) settled() bool {
	if d.done {
		return true
	}
	if d.running() > 0 || d.inflight || d.owed > 0 {
		return false
	}
	if d.paused {
		return true
	}
	return len(d.launchable()) == 0
}

func (d *driver) running() int {
	total := 0
	for _, n := range d.nodes {
		if n.State == orchestrate.Running {
			total++
		}
	}
	return total
}

// launchable is the frontier the SCHEDULER acts on, and it is the whole of the
// scheduler's thinking: a node whose needs are all Done, with no wave, no
// barrier and nothing asked of the planner.
func (d *driver) launchable() []*nodeRun {
	byID := d.index()
	var out []*nodeRun
	for _, n := range d.nodes {
		if n.State != orchestrate.Queued && n.State != orchestrate.Ready {
			continue
		}
		ready := true
		for _, need := range n.Needs {
			dep, ok := byID[need]
			if !ok || dep.State != orchestrate.Done {
				ready = false
				break
			}
		}
		if ready {
			out = append(out, n)
		}
	}
	return out
}

// launch starts everything it may, right now. The two things that stop it are
// the parallel clamp this rig pays for and the fuel gate.
func (d *driver) launch(ctx context.Context) {
	d.strand()
	if d.paused {
		return
	}
	ready := d.launchable()
	for at, n := range ready {
		if d.fuel.Spent >= d.fuel.Cap {
			// THE GATE: in-flight nodes finish, nothing new is launched.
			if !d.paused {
				d.paused = true
				fmt.Printf("  ⏸ FUEL — $%.4f of a $%.2f tank is spent; %d node(s) in flight will finish and nothing new launches\n",
					d.fuel.Spent, d.fuel.Cap, d.running())
			}
			return
		}
		if d.running() >= d.maxParallel {
			// READY is real and the planner should see it: needs met, waiting
			// only for a slot this rig is paying for. The contract's own State
			// has the rung; a run that never used it would show the planner a
			// node "queued" and invite it to wonder what is missing.
			for _, waiting := range ready[at:] {
				waiting.State = orchestrate.Ready
			}
			return
		}
		n.State = orchestrate.Running
		n.Began = time.Now()
		fmt.Printf("  ▶ %-18s %s\n", n.ID, clip(oneLine(n.Goal), 96))
		if now := d.running(); now > d.maxRunning {
			d.maxRunning = now
		}
		node, deps := n.Node, d.digestsFor(n)
		go func() { d.completions <- d.execNode(ctx, node, deps) }()
	}
}

// strand fails the nodes whose needs can never be met. A node waiting on a
// failed or cancelled id is not queued, it is stuck, and a run that left it
// there would hang with nothing running.
func (d *driver) strand() {
	byID := d.index()
	for _, n := range d.nodes {
		if n.State != orchestrate.Queued {
			continue
		}
		for _, need := range n.Needs {
			dep, ok := byID[need]
			switch {
			case !ok:
				n.State, n.Err = orchestrate.Failed, fmt.Sprintf("needs %q, which was cancelled", need)
			case dep.State == orchestrate.Failed:
				n.State, n.Err = orchestrate.Failed, fmt.Sprintf("needs %q, which failed", need)
			default:
				continue
			}
			fmt.Printf("  ✗ %-18s %s\n", n.ID, n.Err)
			break
		}
	}
}

func (d *driver) index() map[string]*nodeRun {
	byID := make(map[string]*nodeRun, len(d.nodes))
	for _, n := range d.nodes {
		byID[n.ID] = n
	}
	return byID
}

// digestsFor is the CONTEXT CONTRACT, made of code: a node is handed the digest
// of every id it needs and nothing else — not their artifacts, not the goal
// statements of nodes it does not need, not the planner's notes.
func (d *driver) digestsFor(n *nodeRun) []orchestrate.NodeStatus {
	byID := d.index()
	var out []orchestrate.NodeStatus
	for _, need := range n.Needs {
		if dep, ok := byID[need]; ok {
			status := dep.NodeStatus
			status.Digest = dep.Digest
			out = append(out, status)
		}
	}
	return out
}

func (d *driver) complete(done completion) {
	byID := d.index()
	n, ok := byID[done.id]
	if !ok {
		return
	}
	n.Ended = time.Now()
	n.Cost = done.cost
	d.burn(done.cost)
	if done.err != nil {
		n.State, n.Err = orchestrate.Failed, done.err.Error()
		fmt.Printf("  ✗ %-18s %s · %v\n", n.ID, n.Ended.Sub(n.Began).Round(time.Millisecond), done.err)
		return
	}
	n.State = orchestrate.Done
	n.Digest = done.digest
	n.Output = done.output
	cut := ""
	if done.truncated {
		d.truncated++
		cut = " · TRUNCATED at the token cap, so its digest is a fallback"
	}
	fmt.Printf("  ✓ %-18s %s · $%.4f · %d tokens · digest %d bytes%s\n",
		n.ID, n.Ended.Sub(n.Began).Round(time.Millisecond), done.cost, done.tokens, len(done.digest), cut)
}

// burn meters one model call against the run's one tank and raises the 80% note
// exactly once.
func (d *driver) burn(cost float64) {
	d.fuel.Spent += cost
	if !d.warned80 && d.fuel.Cap > 0 && d.fuel.Spent >= 0.8*d.fuel.Cap && d.fuel.Spent < d.fuel.Cap {
		d.warned80 = true
		fmt.Printf("  ⚠ FUEL — $%.4f of $%.2f is spent (%.0f%%); the planner is told to converge\n",
			d.fuel.Spent, d.fuel.Cap, 100*d.fuel.Spent/d.fuel.Cap)
	}
}

// ── the planner ─────────────────────────────────────────────────────────────

// Plan is orchestrate.Planner. The run loop calls planTurn instead, which is
// this plus what the turn cost — the number a fuel tank is made of and the
// interface has nowhere to put.
func (d *driver) Plan(ctx context.Context, v orchestrate.View) (orchestrate.Amendment, error) {
	out := d.planTurn(ctx, v, nil)
	return out.amendment, out.err
}

// planTurn is one planner turn: the law, the view, one Amendment. It reads only
// its arguments and the config, so it is safe to run while nodes land — which is
// the point, since it is launched into a goroutine and the run does not wait.
//
// ONE RETRY, with the refusal fed back. A planner that answered with prose or
// with a key nobody declared is a turn the tank already paid for, and asking it
// to fix exactly that is an order of magnitude cheaper than losing the amendment.
func (d *driver) planTurn(ctx context.Context, v orchestrate.View, refused []string) planned {
	began := time.Now()
	history := []message{
		{Role: "system", Content: orchestrate.PlannerPrompt},
		{Role: "user", Content: renderView(v, refused)},
	}

	out := planned{}
	for tries := 0; tries < 2; tries++ {
		salvaged, at, err := jsonReply(ctx, d.chat, history, d.planTokens)
		out.cost += at.spent
		out.tokens += at.tokens
		if err == nil {
			var amendment orchestrate.Amendment
			if err = strict(salvaged.JSON, &amendment); err == nil {
				out.amendment = amendment
				out.elapsed = time.Since(began)
				return out
			}
			err = fmt.Errorf("your reply is not an Amendment: %w", err)
		}
		if ctx.Err() != nil || tries == 1 {
			out.err, out.elapsed = err, time.Since(began)
			return out
		}
		history = append(history,
			message{Role: "assistant", Content: at.raw},
			message{Role: "user", Content: "That amendment was REFUSED:\n\n" + err.Error() +
				"\n\nReply with the whole amendment again — ONE JSON object with only the keys add, cancel, note, done. " +
				"An empty object {} is a legal answer."})
	}
	return out
}

// view is the planner's whole world, taken as a copy. Digests only: the planner
// is the one big-context call in the system and it stays small on purpose.
func (d *driver) view() orchestrate.View {
	v := orchestrate.View{Goal: d.goal, Fuel: d.fuel, Steer: append([]string(nil), d.steer...)}
	for _, n := range d.nodes {
		status := n.NodeStatus
		if n.finished() {
			v.Results = append(v.Results, status)
			continue
		}
		v.Frontier = append(v.Frontier, status)
	}
	return v
}

// renderView writes the view in the five sections PART ONE of the law describes,
// in that order. A section that grew here and not there is drift, and
// internal/orchestrate's prompt_test holds the names together.
func renderView(v orchestrate.View, refused []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "GOAL\n%s\n\n", v.Goal)

	left := v.Fuel.Cap - v.Fuel.Spent
	share := 0.0
	if v.Fuel.Cap > 0 {
		share = 100 * v.Fuel.Spent / v.Fuel.Cap
	}
	fmt.Fprintf(&b, "FUEL\n$%.4f spent of $%.2f — %.0f%% burned, $%.4f left\n\n", v.Fuel.Spent, v.Fuel.Cap, share, left)

	b.WriteString("RESULTS (done, digests only)\n")
	if len(v.Results) == 0 {
		b.WriteString("(nothing has finished yet)\n")
	}
	for _, n := range v.Results {
		if n.State == orchestrate.Failed {
			fmt.Fprintf(&b, "[%s] FAILED · %s\n  %s\n", n.ID, clip(oneLine(n.Goal), 110), firstOr(n.Err, "no reason given"))
			continue
		}
		fmt.Fprintf(&b, "[%s] %s\n  %s\n", n.ID, clip(oneLine(n.Goal), 110), firstOr(n.Digest, "(it produced nothing)"))
	}

	b.WriteString("\nFRONTIER\n")
	if len(v.Frontier) == 0 {
		b.WriteString("(empty — nothing is queued, ready or running)\n")
	}
	for _, n := range v.Frontier {
		needs := ""
		if len(n.Needs) > 0 {
			needs = " · needs " + strings.Join(n.Needs, ", ")
		}
		fmt.Fprintf(&b, "[%s] %s%s · %s\n", n.ID, stateWord(n.State), needs, clip(oneLine(n.Goal), 100))
	}

	b.WriteString("\nSTEERING (since your last call)\n")
	if len(v.Steer) == 0 {
		b.WriteString("(nothing — the person has said nothing since the goal)\n")
	}
	for _, said := range v.Steer {
		fmt.Fprintf(&b, "- %s\n", said)
	}

	// Refusals are not part of the View: they are this driver telling the planner
	// what its LAST amendment could not do. A planner not told would send the
	// same illegal edge again on the next completion.
	if len(refused) > 0 {
		b.WriteString("\nYOUR LAST AMENDMENT WAS PARTLY REFUSED\n")
		for _, why := range refused {
			fmt.Fprintf(&b, "- %s\n", why)
		}
	}
	return b.String()
}

func stateWord(s orchestrate.State) string {
	switch s {
	case orchestrate.Queued:
		return "queued"
	case orchestrate.Ready:
		return "ready"
	case orchestrate.Running:
		return "running"
	case orchestrate.Done:
		return "done"
	default:
		return "failed"
	}
}

// ── amendments ──────────────────────────────────────────────────────────────

// applyPlanned records what the turn cost and then applies what it said. Cancels
// go first: re-adding a cancelled id in the same amendment is how the commitment
// law lets a PENDING node be re-aimed, and there is no edit.
func (d *driver) applyPlanned(answer planned) {
	d.plannerRuns++
	d.burn(answer.cost)
	if answer.err != nil {
		d.plannerFail++
		fmt.Printf("  ! planner turn %d produced nothing usable · %s · %v\n", d.plannerRuns, answer.elapsed.Round(time.Millisecond), answer.err)
		return
	}
	a := answer.amendment
	if len(a.Add) == 0 && len(a.Cancel) == 0 && a.Done == nil && strings.TrimSpace(a.Note) == "" {
		d.plannerNoop++
		fmt.Printf("  · planner turn %d · NOOP · %s · $%.4f\n", d.plannerRuns, answer.elapsed.Round(time.Millisecond), answer.cost)
		return
	}
	ending := ""
	if a.Done != nil {
		ending = " · claims DONE"
	}
	fmt.Printf("  · planner turn %d · %s · $%.4f · +%d node(s), -%d%s\n",
		d.plannerRuns, answer.elapsed.Round(time.Millisecond), answer.cost, len(a.Add), len(a.Cancel), ending)
	if note := strings.TrimSpace(a.Note); note != "" {
		d.notes = append(d.notes, note)
		fmt.Printf("    note: %s\n", indentRest(wrap(note, 92), "          "))
	}
	d.cancel(a.Cancel)
	d.add(a.Add)
	if a.Done != nil {
		// A `done` is a VERDICT ON RESULTS, and these two shapes are not one:
		// a planner that adds seven nodes and finishes in the same breath has
		// written what it expects the run to conclude, which is law 1's
		// forbidden thing wearing law 9's key. Refusing it costs one turn and
		// saves the whole run — the alternative is a synthesis of nothing, with
		// the graph it just drew abandoned mid-launch.
		switch {
		case len(a.Add) > 0:
			d.refuse("your `done` travelled with `add`: a run cannot be finished and be adding the work at the same time, so the nodes landed and the done was dropped")
		case d.finishedCount() == 0:
			d.refuse("your `done` arrived with RESULTS empty: nothing has finished, so there is nothing to synthesise and nothing to cite")
		default:
			d.done = true
			d.answer = a.Done.Brief
		}
	}
}

func (d *driver) finishedCount() int {
	total := 0
	for _, n := range d.nodes {
		if n.finished() {
			total++
		}
	}
	return total
}

// cancel drops PENDING nodes. A running node is not touched and a finished one
// is a fact — the commitment law, and the refusal is told back to the planner
// because a planner that thinks it cancelled something plans as if it did.
func (d *driver) cancel(cancels []orchestrate.Cancel) {
	for _, c := range cancels {
		kept := d.nodes[:0]
		found := false
		for _, n := range d.nodes {
			switch {
			case n.ID != c.ID:
				kept = append(kept, n)
			case n.State == orchestrate.Queued || n.State == orchestrate.Ready:
				found = true
				d.cancelled = append(d.cancelled, c)
				fmt.Printf("    − %-16s cancelled: %s\n", c.ID, clip(oneLine(c.Reason), 88))
			default:
				kept = append(kept, n)
				found = true
				d.refuse(fmt.Sprintf("%q could not be cancelled: it is %s, and the commitment law does not touch a node that has started", c.ID, stateWord(n.State)))
			}
		}
		d.nodes = kept
		if !found {
			d.refuse(fmt.Sprintf("%q could not be cancelled: no node by that id is in the run", c.ID))
		}
	}
}

var slug = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// add is where an amendment meets the law. Every refusal here is told back on
// the next call rather than killing the run: a planner that wrote one bad edge
// out of five should land the four.
func (d *driver) add(adds []orchestrate.Node) {
	live := map[string]bool{}
	for _, n := range d.nodes {
		live[n.ID] = true
	}
	batch := map[string]bool{}
	for _, node := range adds {
		batch[node.ID] = true
	}
	var arrived []*nodeRun
	for _, node := range adds {
		switch {
		case !slug.MatchString(node.ID):
			d.refuse(fmt.Sprintf("the id %q is not a slug (lowercase letters, digits, hyphens), so the node was dropped", node.ID))
			continue
		case live[node.ID]:
			d.refuse(fmt.Sprintf("%q is already a node in this run: an id is unique for the whole run, and re-aiming a PENDING node means cancelling it in the same amendment", node.ID))
			continue
		case strings.TrimSpace(node.Goal) == "":
			d.refuse(fmt.Sprintf("%q has an empty goal, so nobody could work it", node.ID))
			continue
		}
		bad := ""
		for _, need := range node.Needs {
			if !live[need] && !batch[need] {
				bad = need
				break
			}
		}
		if bad != "" {
			d.refuse(fmt.Sprintf("%q needs %q, which is not a node in this run and is not being added by this amendment", node.ID, bad))
			continue
		}
		fresh := &nodeRun{NodeStatus: orchestrate.NodeStatus{Node: node, State: orchestrate.Queued}}
		fresh.Needs = append([]string(nil), node.Needs...)
		d.collide(fresh)
		if cycle := d.wouldCycle(fresh); cycle != "" {
			d.refuse(fmt.Sprintf("%q was dropped: its needs close a cycle (%s), and a cycle is a frontier nothing can launch", node.ID, cycle))
			continue
		}
		d.nodes = append(d.nodes, fresh)
		arrived = append(arrived, fresh)
		live[node.ID] = true
		where := ""
		if len(fresh.Needs) > 0 {
			where = " · needs " + strings.Join(fresh.Needs, ", ")
		}
		fmt.Printf("    + %-16s%s\n      %s\n", node.ID, where, indentRest(wrap(clip(oneLine(node.Goal), 260), 92), "      "))
		for _, forced := range fresh.Forced {
			fmt.Printf("      write-scope collision with %q — the scheduler serialized them\n", forced)
		}
	}
	// The unmet pass runs AFTER the batch, because a node may name a sibling
	// that was added below it in the same amendment and a check that ran inline
	// would not have seen it yet.
	for _, fresh := range arrived {
		if fresh.Unmet = d.unmet(fresh); len(fresh.Unmet) > 0 {
			fmt.Printf("    ⚑ %-16s UNMET: its goal reads %s and its needs do not reach them — it launches at once, on nothing\n",
				fresh.ID, strings.Join(fresh.Unmet, ", "))
		}
	}
}

// unmet is the measurement this rig exists to make honest.
//
// The defect it catches is the one the law spends law 2's hardest paragraph on:
// the planner writes "synthesis needs all six" in the NOTE, leaves `needs` empty
// in the JSON, and the scheduler — which reads only `needs` — launches the
// synthesis at once, beside the six, reading nothing. It costs nothing, it fails
// no check, and it inflates max-concurrency by exactly the amount it is wrong,
// so a rig that did not count it would score the defect as the win.
//
// IT IS NOT TOLD BACK TO THE PLANNER and it is not repaired. Both would be this
// rig quietly fixing the law's job, and the number would then be measuring the
// repair. It is a floor, not a count: only ids carrying a hyphen are matched,
// because a one-word id like "synthesis" appears in prose that means nothing by
// it, and a false accusation here is worse than a missed one.
func (d *driver) unmet(fresh *nodeRun) []string {
	reaches := map[string]bool{}
	byID := d.index()
	var walk func(id string)
	walk = func(id string) {
		node, ok := byID[id]
		if !ok {
			return
		}
		for _, need := range node.Needs {
			if !reaches[need] {
				reaches[need] = true
				walk(need)
			}
		}
	}
	walk(fresh.ID)

	var out []string
	for _, other := range d.nodes {
		if other.ID == fresh.ID || reaches[other.ID] || !strings.Contains(other.ID, "-") {
			continue
		}
		if wholeWord(fresh.Goal, other.ID) {
			out = append(out, other.ID)
		}
	}
	return out
}

// wholeWord is containment that will not match "db-persistence" inside
// "db-persistence-check".
func wholeWord(text, word string) bool {
	for at := 0; ; {
		found := strings.Index(text[at:], word)
		if found < 0 {
			return false
		}
		found += at
		before := found == 0 || !isWordByte(text[found-1])
		end := found + len(word)
		after := end == len(text) || !isWordByte(text[end])
		if before && after {
			return true
		}
		at = found + 1
	}
}

func isWordByte(b byte) bool {
	return b == '-' || b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// collide is law 10 enforced by code rather than trusted to the planner: two
// nodes whose write scopes intersect are not independent, and the scheduler
// serializes them with an added edge (Node.WriteScope's own doc).
func (d *driver) collide(fresh *nodeRun) {
	if len(fresh.WriteScope) == 0 {
		return
	}
	held := map[string]bool{}
	for _, need := range fresh.Needs {
		held[need] = true
	}
	for _, other := range d.nodes {
		if other.State == orchestrate.Done || other.State == orchestrate.Failed || held[other.ID] {
			continue
		}
		if !overlaps(fresh.WriteScope, other.WriteScope) {
			continue
		}
		fresh.Needs = append(fresh.Needs, other.ID)
		fresh.Forced = append(fresh.Forced, other.ID)
		held[other.ID] = true
	}
}

// overlaps is path containment in either direction: "internal/x" and
// "internal/x/y.go" are the same collision as two identical paths.
func overlaps(a, b []string) bool {
	for _, one := range a {
		for _, two := range b {
			one, two := strings.Trim(one, "/"), strings.Trim(two, "/")
			if one == two || strings.HasPrefix(one, two+"/") || strings.HasPrefix(two, one+"/") {
				return true
			}
		}
	}
	return false
}

// wouldCycle walks the needs graph from the candidate. It names the path it
// found, because "a cycle" tells a planner nothing it can fix.
func (d *driver) wouldCycle(fresh *nodeRun) string {
	byID := d.index()
	byID[fresh.ID] = fresh
	var walk func(id string, seen []string) string
	walk = func(id string, seen []string) string {
		for at, was := range seen {
			if was == id {
				return strings.Join(append(seen[at:], id), " → ")
			}
		}
		node, ok := byID[id]
		if !ok {
			return ""
		}
		for _, need := range node.Needs {
			if found := walk(need, append(seen, id)); found != "" {
				return found
			}
		}
		return ""
	}
	return walk(fresh.ID, nil)
}

func (d *driver) refuse(why string) {
	d.refused = append(d.refused, why)
	fmt.Printf("    ! %s\n", indentRest(wrap(why, 90), "      "))
}

// ── the executor ────────────────────────────────────────────────────────────

// Exec is orchestrate.Executor. The run loop calls execNode, which is this plus
// the token count the report needs.
func (d *driver) Exec(ctx context.Context, n orchestrate.Node, deps []orchestrate.NodeStatus) (string, float64, error) {
	out := d.execNode(ctx, n, deps)
	return out.digest, out.cost, out.err
}

// execNode is one node: one agent turn, its needs' digests in front of it, and
// its own digest at the end of what it wrote.
//
// THE WORKER CONDENSES ITSELF. A second model call to summarise would be a
// second bill against the same tank for material the writer already has in
// hand, and the writer is the one who knows which numbers were load-bearing.
func (d *driver) execNode(ctx context.Context, node orchestrate.Node, deps []orchestrate.NodeStatus) completion {
	system := strings.Join([]string{
		"You are ONE NODE inside an adaptive run working on a person's goal. Other nodes are",
		"running beside you right now, on other parts of it.",
		"",
		"THE RUN'S GOAL: " + d.goal,
		"",
		"YOUR NODE'S GOAL, and the whole of what you are responsible for:",
		node.Goal,
		"",
		"Do your node's job and nothing else — other nodes do the other parts, and work you do",
		"out of turn is work that gets thrown away or contradicted. Answer with the WORK ITSELF:",
		"no preamble, no restating of the goal, no promises about what you are about to do.",
		"",
		"THEN END WITH A DIGEST. After your work, write a line reading exactly DIGEST: and then",
		"the condensed form of what you found — the claims and the numbers, restated tight, at",
		"most 200 words. The digest is the ONLY part of your answer anything else in this run",
		"will ever see: the planner reads it, and so does every node that depends on you.",
		"Anything you leave out of it is lost to the rest of the run.",
	}, "\n")

	var input strings.Builder
	if len(deps) == 0 {
		input.WriteString("Nothing you need has run — you are a first node. Work from the goal.")
	} else {
		input.WriteString("The digests of the nodes you need:\n")
		for _, dep := range deps {
			fmt.Fprintf(&input, "\n[%s] %s\n%s\n", dep.ID, clip(oneLine(dep.Goal), 110), firstOr(dep.Digest, "(it produced nothing)"))
		}
	}

	out, err := d.chat.complete(ctx, chatRequest{
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: input.String()},
		},
		MaxTokens: d.nodeTokens,
	})
	if err != nil {
		return completion{id: node.ID, err: err}
	}
	return completion{
		id:        node.ID,
		digest:    digestOf(out.Text),
		output:    out.Text,
		cost:      out.Cost,
		tokens:    out.Tokens,
		truncated: out.Finish == "length",
	}
}

// digestOf takes what the worker condensed. A worker that ignored the
// instruction is not failed for it — the tail of its answer is usually its
// conclusion, and a lost node is worse than a rough digest.
func digestOf(text string) string {
	text = strings.TrimSpace(text)
	if at := strings.LastIndex(text, "DIGEST:"); at >= 0 {
		if condensed := strings.TrimSpace(text[at+len("DIGEST:"):]); condensed != "" {
			return condensed
		}
	}
	if len(text) <= 1400 {
		return text
	}
	return "(no digest was written; this is the tail of the node's answer)\n…" + text[len(text)-1400:]
}

// ── the gate and the report ─────────────────────────────────────────────────

// atTheGate is what the absent person says when the tank is empty. The contract
// answers a pause with resume/topup/finish/stop; this rig has nobody to ask, so
// the flag answers, and `finish` buys ONE synthesis turn OVER the cap and says
// so rather than hiding the overage in the total.
func (d *driver) atTheGate(ctx context.Context) {
	if d.done || !d.paused || ctx.Err() != nil {
		return
	}
	if d.gate != "finish" {
		fmt.Printf("\n  ⏹ the tank is empty and the gate was answered `stop` — the run ends with no synthesis\n")
		return
	}
	fmt.Printf("\n  ⏭ the tank is empty and the gate was answered `finish` — one synthesis turn, OVER the cap\n")
	view := d.view()
	history := []message{
		{Role: "system", Content: orchestrate.PlannerPrompt},
		{Role: "user", Content: renderView(view, d.refused) +
			"\n\nTHE RUN IS OUT OF FUEL AND THE PERSON ANSWERED THE GATE WITH `finish`. Nothing more will be launched. " +
			"Answer with an amendment carrying ONLY `done`, and write the best synthesis the RESULTS above support — " +
			"every claim citing the node id it came from, and any part of the goal the results do not reach said plainly."},
	}
	salvaged, at, err := jsonReply(ctx, d.chat, history, d.planTokens)
	d.plannerRuns++
	d.burn(at.spent)
	if err != nil {
		fmt.Printf("  ! the synthesis turn produced nothing usable: %v\n", err)
		return
	}
	var amendment orchestrate.Amendment
	if err := strict(salvaged.JSON, &amendment); err != nil || amendment.Done == nil {
		fmt.Printf("  ! the synthesis turn did not answer with a `done`: %v\n", err)
		return
	}
	d.done, d.answer = true, amendment.Done.Brief
}

// report is the whole point of the rig: the numbers that say whether planning
// per completion bought anything.
func (d *driver) report(elapsed time.Duration, calls, tokens int, cost float64) {
	section("the run, measured")

	// Counted by state, not by subtraction: a run the planner ended early leaves
	// queued and running nodes behind, and "spawned minus failed" would report
	// every one of them as done.
	done, failed, unrun, cancelled := 0, 0, 0, len(d.cancelled)
	for _, n := range d.nodes {
		switch n.State {
		case orchestrate.Done:
			done++
		case orchestrate.Failed:
			failed++
		default:
			unrun++
		}
	}
	noopRate := 0.0
	if d.plannerRuns > 0 {
		noopRate = 100 * float64(d.plannerNoop) / float64(d.plannerRuns)
	}
	fmt.Printf("wall time          %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("nodes              %d spawned · %d done · %d failed · %d cancelled · %d never ran\n",
		len(d.nodes)+cancelled, done, failed, cancelled, unrun)
	unmet, unmetNodes := 0, 0
	for _, n := range d.nodes {
		if len(n.Unmet) > 0 {
			unmetNodes++
			unmet += len(n.Unmet)
		}
	}
	fmt.Printf("max concurrent     %d%s\n", d.maxRunning, honestConcurrency(unmetNodes))
	fmt.Printf("truncated nodes    %d — they hit the token cap before writing their digest, so what the run read of them is a fallback\n", d.truncated)
	fmt.Printf("unmet references   %d across %d node(s) — a goal that reads work its `needs` never asked for\n", unmet, unmetNodes)
	fmt.Printf("serial wall time   %s (the same nodes one after another)\n", d.serialWall().Round(time.Millisecond))
	fmt.Printf("planner            %d calls · %d NOOP (%.0f%%) · %d refused-and-retried · %d unusable\n",
		d.plannerRuns, d.plannerNoop, noopRate, len(d.refused), d.plannerFail)
	fmt.Printf("fuel               $%.4f spent of a $%.2f tank (%.0f%%)%s\n",
		d.fuel.Spent, d.fuel.Cap, 100*d.fuel.Spent/d.fuel.Cap, pausedNote(d.paused))
	fmt.Printf("bill               %d model calls · %d tokens · $%.4f\n", calls, tokens, cost)
	fmt.Printf("planner share      $%.4f of the tank went on thinking (%.0f%%)\n",
		d.fuel.Spent-d.nodeSpend(), 100*(d.fuel.Spent-d.nodeSpend())/max64(d.fuel.Spent, 0.000001))

	section("the graph, as it crystallized")
	for _, n := range d.nodes {
		needs := "—"
		if len(n.Needs) > 0 {
			needs = strings.Join(n.Needs, ", ")
		}
		took := ""
		if !n.Ended.IsZero() {
			took = n.Ended.Sub(n.Began).Round(time.Millisecond).String()
		}
		fmt.Printf("%-18s %-8s %-9s needs %s\n", n.ID, stateWord(n.State), took, needs)
	}
	if len(d.cancelled) > 0 {
		for _, c := range d.cancelled {
			fmt.Printf("%-18s %-8s %s\n", c.ID, "cancel", clip(oneLine(c.Reason), 80))
		}
	}

	if len(d.notes) > 0 {
		section("what the planner said as it went")
		for at, note := range d.notes {
			fmt.Printf("%2d. %s\n", at+1, indentRest(wrap(note, 74), "    "))
		}
	}

	section("every node's work, whole")
	for _, n := range d.nodes {
		fmt.Printf("── %s (%s)\n", n.ID, stateWord(n.State))
		if n.Err != "" {
			fmt.Printf("   ERROR %s\n\n", n.Err)
			continue
		}
		fmt.Println(indent(firstOr(n.Output, "(nothing)"), "   "))
		fmt.Println()
	}

	section("the synthesis")
	if !d.done {
		fmt.Println("the run never reached a `done` — there is no synthesis")
		return
	}
	fmt.Println(wrap(d.answer, 92))
	fmt.Println()
	fmt.Println(d.citations())
}

// citations measures law 9 against what the planner actually wrote: how much of
// the brief is grounded, and which nodes the run paid for and then never cited.
func (d *driver) citations() string {
	ids := map[string]bool{}
	for _, n := range d.nodes {
		if n.State == orchestrate.Done {
			ids[n.ID] = true
		}
	}
	cited := map[string]bool{}
	for id := range ids {
		if strings.Contains(d.answer, id) {
			cited[id] = true
		}
	}
	grounded, total := 0, 0
	for _, sentence := range sentences(d.answer) {
		total++
		for id := range ids {
			if strings.Contains(sentence, id) {
				grounded++
				break
			}
		}
	}
	share := 0.0
	if total > 0 {
		share = 100 * float64(grounded) / float64(total)
	}
	var uncited []string
	for id := range ids {
		if !cited[id] {
			uncited = append(uncited, id)
		}
	}
	sort.Strings(uncited)
	out := fmt.Sprintf("citations  %d of %d finished nodes are cited · %d of %d sentences carry an id (%.0f%%)",
		len(cited), len(ids), grounded, total, share)
	if len(uncited) > 0 {
		out += "\nuncited    " + strings.Join(uncited, ", ") + " — work the run paid for and the answer does not rest on"
	}
	return out
}

// serialWall is the same nodes, one after another: the number the wall time is
// worth comparing against, and the whole claim fan-out makes.
func (d *driver) serialWall() time.Duration {
	var total time.Duration
	for _, n := range d.nodes {
		if !n.Ended.IsZero() && !n.Began.IsZero() {
			total += n.Ended.Sub(n.Began)
		}
	}
	return total
}

func (d *driver) nodeSpend() float64 {
	total := 0.0
	for _, n := range d.nodes {
		total += n.Cost
	}
	return total
}

// honestConcurrency is the caveat that has to travel with the headline number:
// a node launched at once because its `needs` were empty when its goal says
// otherwise is width that was never real.
func honestConcurrency(unmetNodes int) string {
	if unmetNodes == 0 {
		return ""
	}
	return fmt.Sprintf(" — but %d node(s) had unmet references, so some of this width read nothing", unmetNodes)
}

func pausedNote(paused bool) string {
	if paused {
		return " · the run PAUSED at the gate"
	}
	return ""
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// sentences splits a brief roughly, on the punctuation a brief actually uses.
// It is a measure of grounding, not a parser, and it says so wherever it is
// printed.
func sentences(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		start := 0
		for at := 0; at < len(line); at++ {
			if line[at] != '.' && line[at] != '?' && line[at] != '!' {
				continue
			}
			if at+1 < len(line) && line[at+1] != ' ' {
				continue
			}
			if piece := strings.TrimSpace(line[start : at+1]); len(piece) > 20 {
				out = append(out, piece)
				start = at + 1
			}
		}
		if piece := strings.TrimSpace(line[start:]); len(piece) > 20 {
			out = append(out, piece)
		}
	}
	return out
}

// ── the mode ────────────────────────────────────────────────────────────────

// orchestrateMode is `-orchestrate`: the goals, each one run through the loop,
// and the totals.
func orchestrateMode(ctx context.Context, chat *chatClient, which, model string, opts driver) int {
	chosen := pickOrch(which)
	if len(chosen) == 0 {
		die("no goal to run")
	}
	fmt.Printf("harness-design -orchestrate · model %s · tank $%.2f per goal · at most %d nodes at once\n",
		model, opts.fuel.Cap, opts.maxParallel)
	fmt.Printf("planner law: %d bytes, %d lines · a LOCAL driver (internal/orchestrate has the contract, not Run yet)\n",
		len(orchestrate.PlannerPrompt), strings.Count(orchestrate.PlannerPrompt, "\n")+1)

	answered := 0
	for _, goal := range chosen {
		head(fmt.Sprintf("%s · %s", goal.key, goal.note), goal.text)
		d := opts
		d.chat, d.goal = chat, goal.text
		beforeCalls, beforeTokens, beforeCost := chat.spent()
		began := time.Now()
		d.run(ctx)
		elapsed := time.Since(began)
		afterCalls, afterTokens, afterCost := chat.spent()
		d.report(elapsed, afterCalls-beforeCalls, afterTokens-beforeTokens, afterCost-beforeCost)
		if d.done {
			answered++
		}
		if ctx.Err() != nil {
			break
		}
	}
	fmt.Printf("\n%s\ntotal  %s · %d of %d goals reached a synthesis\n", rule(), chat.bill(), answered, len(chosen))
	if answered < len(chosen) {
		return 1
	}
	return 0
}
