package session

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// task_child_run.go is ONE NODE'S RUN, from the first request to the last report
// folded in.
//
// ── WHY IT IS A VALUE AND NOT A FUNCTION ──
//
// The run has fourteen facts that every phase of it reads and most of them
// write: what the node has changed, how many steps it has spent, how many of
// them added nothing, when its clock runs out, how many extensions it has been
// granted, what it has been seen to run. They used to live as locals in one
// five-hundred-line function with two closures over them, which is the same
// state with the same sharing and no name for any of the phases that touch it —
// so nothing could be read, tested or reasoned about on its own, and the four
// defects filed off the family audit were all one road with too many endings for
// anybody to hold in their head (#260).
//
// [childRun] is that state, named once, with the phases as methods on it. The
// sharing is EXACTLY what the closures had — a pointer receiver is what a
// closure over a local variable already was — so nothing about when a fact is
// written or read has moved. What is new is only that each phase can be pointed
// at.
//
// ── THE PHASES, IN THE ORDER THEY HAPPEN ──
//
//   - [childRun.open] submits the brief, or queues it for a node whose parts are
//     already out.
//   - [childRun.drain] consumes the node's own events, and [childRun.step] reads
//     one finished tool call: what it produced, whether it added anything,
//     whether the node is spinning.
//   - [childRun.checkpoint] is the WORKING or CIRCLING question, put to a reader
//     when a threshold fires.
//   - [childRun.landIfStopped] is the landing turn a stopped node is given.
//   - [childRun.foldParts] holds a node open while its parts run, and folds
//     their reports.

// childRun is everything one node's run knows about itself while it is running.
//
// The two contexts are NOT interchangeable and both are kept. `ctx` is the
// node's own, and it is what the progress reader and the landing turn are given
// so that tripping a threshold does not cancel the very turn that asks whether
// the work should carry on. `runCtx` is this run's, hung off `ctx` with a cancel
// of its own, and it is what the working turns ride — so [childRun.stop] ends
// the child the way `jobs kill` does, a cancelled turn with its partial work
// kept and its branch intact, while the caller is told WHICH threshold fired
// rather than being left to infer it from a context error that has three
// possible causes.
type childRun struct {
	ctx    context.Context
	runCtx context.Context
	stop   context.CancelFunc

	child  *Agent
	node   *TaskNode
	dir    string
	limits taskLimits
	room   *taskRoom
	log    io.Writer

	changed  []string
	seen     map[string]bool
	ledger   *progressLedger
	lastDirt string
	failure  error
	stopped  string
	steps    int
	idle     int
	// ranCheck says this node had been RUNNING its work — a build, a test, a
	// script — before anything was taken off its belt. It is what makes the
	// landing's "unverified" sentence a fact rather than a guess: a node that
	// never ran a check is not a node whose last edits went unchecked.
	ranCheck   bool
	extensions int
	deadline   time.Time
	evidence   []string
	// effects is what this node has PRODUCED, as opposed to what it has done
	// (effects.go). The evidence above is a narrative of calls and a
	// narrative of successful calls reads as work whatever the calls left
	// behind; this is the one fact under it — whether anything is different
	// now — and it is what the checkpoint is handed alongside the story.
	effects *effectLedger
	// reportedParts is monotone for this worker's division. A landing is
	// progress even when its note races the event drain below.
	reportedParts int
}

// runTaskChild submits the brief, consumes the node's own events internally,
// and enforces the two thresholds from the same stream.
//
// The events are NOT forwarded INTO THE CONVERSATION. A node's tool rows belong
// to its journal, not to the chat: a person watching a chat must not have forty
// of somebody else's greps scroll past. What is kept for the chat is the one
// thing the person needs to see afterwards — which files the node wrote — read
// off the edit and write calls as they end.
//
// They ARE forwarded into the node's own ROOM, which is the other half of that
// same rule: the events a chat must not carry are exactly the events somebody
// who walked into this node's page came to see (task_room.go). The publish is
// the first thing [childRun.drain] does with an event, so a watcher sees the
// child's narrative in the order it happened rather than in the order this side
// of the wall got round to counting it — and it runs for a nil room too, which
// is the ordinary case of a node nobody is watching.
//
// The three phases below are the whole of a run and they are in the only order
// they can be in: the node works, a node that was stopped gets its landing turn,
// and a node holding parts is held open until their reports are folded.
func runTaskChild(ctx context.Context, child *Agent, node *TaskNode, instruction, dir string, limits taskLimits, room *taskRoom, log io.Writer) ([]string, string, error) {
	if limits.deadline <= 0 {
		limits.deadline = taskDeadline
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	// ── THE DOOR SHUTS ON EVERY ROAD OUT, NOT JUST THE CLEAN ONE ──
	//
	// From the moment this function returns, this child never reads again — the
	// check and the landing are other hands — but it stays OPEN until the
	// caller's retire, which on a checked node is minutes away. A line steered
	// in during that window would still be TAKEN ([Agent.enqueueSteeredLine]
	// answers whether the agent is closed, not whether anybody will drain it),
	// echoed by the room as said, and closed over unread: the #273 swallow. The
	// tail loop below also withdraws at its own last read, which is earlier on
	// the ordinary road; this defer is for the roads the loop never takes — a
	// threshold stop, a cancelled context, a turn that errored — where the
	// swallow was otherwise alive and well. A caller that put somebody else in
	// the room restores them itself (task_audit.go's repair round).
	defer room.speaking(nil)

	run := &childRun{
		ctx:           ctx,
		runCtx:        runCtx,
		stop:          stop,
		child:         child,
		node:          node,
		dir:           dir,
		limits:        limits,
		room:          room,
		log:           log,
		seen:          map[string]bool{},
		ledger:        newProgressLedger(),
		effects:       newEffectLedger(),
		deadline:      time.Now().Add(limits.deadline),
		reportedParts: child.reportedChildren(),
	}
	if err := run.open(instruction); err != nil {
		return nil, "", err
	}
	run.landIfStopped()
	run.foldParts()
	return run.changed, run.stopped, run.failure
}

// open is THE FIRST REQUEST, OR NONE AT ALL.
//
// A node that was handed a drawn division before it started has ALREADY given
// its work away: the parts were submitted on its behalf between the worker
// being built and this line (task_divide_sketch.go), and they are running now.
// Asking it anything before their reports are in is buying a turn about
// waiting — which is exactly what was measured: thirty-one requests at a five
// second cadence, none of them able to write a line the parts were not already
// writing, ending in the no-progress counter killing the one node that could
// have folded them together.
//
// SO THE BRIEF IS QUEUED RATHER THAN ASKED. It sits on the steering queue with
// nobody having read it, [childRun.foldParts] parks, and the turn that reads it
// is the turn the last report starts — one request holding the brief, the
// drawing's own "AND THIS IS YOURS, ONCE THEIR REPORTS ARE IN"
// ([drawnDivision.afterParts]) and every part's news at once, which is the
// integration the division was drawn for.
//
// EVERY OTHER NODE OPENS EXACTLY AS IT ALWAYS DID. A node with no parts out —
// which is nearly all of them, including one that divides mid-run and is
// already talking when it does — submits its brief here and runs.
func (r *childRun) open(instruction string) error {
	if r.child.childrenOutstanding() {
		r.child.enqueueNote(briefNote(instruction))
		return nil
	}
	events, err := r.child.Submit(r.runCtx, instruction)
	if err != nil {
		return err
	}
	r.drain(events)
	return nil
}

// checkpoint puts the WORKING or CIRCLING question and answers whether the
// work carries on.
//
// `renew` is whether an answer of WORKING buys the node MORE — the next equal
// slice of the deadline and, with it, the next slice of the step budget
// ([taskMaxExtensions]). It is true for the two thresholds that are BOUNDS
// being met: the node has spent its steps or its hour, and carrying on means
// being granted another allowance. It is false for the threshold that is a
// FINDING about the work — a run of actions that produced nothing new — where
// carrying on means only "keep going on what you already have". A finding
// that handed out another two hundred steps would be a spin buying itself
// room, which is the opposite of what noticing it is for.
//
// IT ANSWERS NOTHING, because there is nothing left for a caller to decide: what
// it settles it settles on the run itself — [childRun.stopped] and the cancel it
// hangs off — and a returned yes-or-no would be a second copy of that, free to
// disagree with it.
func (r *childRun) checkpoint(threshold string, renew bool) {
	if r.node == nil {
		r.stopped = "stopped at " + threshold
		r.stop()
		return
	}
	owner := r.node.owner
	if owner == nil {
		owner = r.node.graph.home
	}
	if owner == nil {
		r.stopped = "stopped at " + threshold
		r.stop()
		return
	}
	// THE FACT GOES IN FRONT OF THE STORY. A reader handed twenty lines of
	// successful calls has to infer repetition from them and will not; handed
	// one sentence saying how many of them changed nothing, it has the finding
	// outright and can spend its reading on the working copy instead.
	asked := r.evidence
	if line := r.effects.sameEffectLine(); line != "" {
		asked = append([]string{line}, r.evidence...)
	}
	working, reason := owner.taskProgress(r.ctx, r.node, r.dir, asked, r.log)
	if working && !renew {
		// THE READER LOOKED AT THIS RUN AND SAID CARRY ON, so the run starts
		// again from here. Without the reset the next action would meet the
		// same count and ask the same question of the same reader, which is a
		// model call per step for as long as the node keeps going.
		r.effects.pardon()
		fmt.Fprintf(r.log, "checkpoint: working — carrying on\n")
		return
	}
	if working && r.extensions < taskMaxExtensions {
		r.extensions++
		r.deadline = r.deadline.Add(r.limits.deadline)
		fmt.Fprintf(r.log, "checkpoint: working — renewed %d of %d\n", r.extensions, taskMaxExtensions)
		return
	}
	if working {
		reason = "the work used all 4 extensions"
	}
	r.stopped = fmt.Sprintf("stopped at %s: %s", threshold, strings.TrimSpace(reason))
	r.stop()
}

// drain consumes one stream of the child's own events: it publishes every one of
// them into the node's room, prints the beginning of each call, and hands a
// finished call to [childRun.step].
//
// It keeps draining after a cancel, which is deliberate — the child is still
// finishing its batch and closing its stream, and this side of the wall must
// read that to the end rather than leaving a producer blocked on a channel
// nobody is taking from.
func (r *childRun) drain(events <-chan Event) {
	for event := range events {
		r.room.publish(event)
		switch event.Kind {
		case EventToolBegin:
			fmt.Fprintf(r.log, "· %s\n", event.Hint)
		case EventToolEnd, EventToolFailed:
			r.step(event)
		case EventError:
			r.failure = event.Err
		}
	}
}

// step reads ONE FINISHED TOOL CALL, which is the only unit available from out
// here: the child's model round-trips are inside its own loop, and this side of
// the wall sees the calls they produce.
//
// PROGRESS is any of the three halves of the job ([addedSomething]) — a
// SUCCESSFUL call that actually saved a file ([producedAFile], on EventToolEnd
// and never EventToolFailed, because an edit whose oldText did not match changed
// nothing and a node repeating it is the exact spin the counter exists to
// catch), a step that left the worktree different from how it found it
// ([worktreeMoved]), or a step that TAUGHT the node something it did not know
// ([taughtSomething]). What the counter kills is the fourth thing: the same call
// again, changing nothing, learning nothing.
//
// AND "TAUGHT" IS ASKED OF THE SHAPE IT WAS BUILT FOR. A reading taken over a
// deliverable that has just changed is information by construction, and so is a
// question the node has never asked whose answer brought something back; line
// novelty judges the third case, which is the same question asked again of work
// that has not moved (novelty.go's [progressLedger]). The unit is still the
// STEP and not the second: what the counter is spending is a model round-trip
// and a tool call, and six of those that added nothing is a spin whether they
// took eighteen seconds or eighteen minutes.
func (r *childRun) step(event Event) {
	r.steps++
	// AND WHETHER THIS NODE WAS RUNNING ITS WORK, which is not a
	// question about progress at all — it is what makes the landing's
	// "unverified" sentence below a fact rather than a guess
	// (withdrawn.go's [checkingTools]). A harness-made failure does not
	// count: an answer this side of the wall wrote is not the node
	// having run anything.
	if checkingTools[event.Tool] && !event.HarnessMade {
		r.ranCheck = true
	}
	// ALL THREE QUESTIONS ARE ASKED OF EVERY STEP, and each is
	// asked before any of them is read, because two of them RECORD
	// as they answer. The worktree fingerprint has to be refreshed
	// on the step that moved it whichever question noticed, or the
	// next step inherits a stale one and reads somebody else's dirt
	// as its own; the target has to be recorded even on a step that
	// was already progress for another reason, or the same call can
	// be spent twice. A short-circuiting `switch` did both wrong.
	//
	// THE THREE ARE READ IN ONE PLACE ([addedSomething]), because
	// they are one rule and because the ledger they keep has state
	// now — a step that moved the work arms the reading after it
	// (novelty.go's [progressLedger]) — and a rule with state that
	// is spelled out at its call site is a rule with two versions.
	moved := worktreeMoved(r.dir, &r.lastDirt)
	path, wrote := changedPath(event, r.dir)
	saved := wrote && event.Kind == EventToolEnd
	// ── AND THE JOURNAL RECORDS WHAT THE STEP PRODUCED ──
	//
	// The line the checkpoint will read used to be the hand and its
	// arguments and nothing else, which is a story about what the node
	// SET OUT to do. A story of successful calls reads as work whatever
	// the calls left behind, and that is the whole of the defect this
	// answers: a worker rewriting one file with the same bytes writes a
	// perfect narrative of editing. So every line now carries the one
	// fact a reader cannot get from an exit code — whether anything is
	// different because of the call (effects.go).
	//
	// IT IS RECORDED HERE, after the saving call's file is known,
	// because the effect of a call that saves something is the file and
	// not the sentence it answered with.
	effect, produced := effectPrintOf(event, r.dir, path, saved, moved, r.lastDirt)
	repeat := r.effects.saw(event.Tool, effect, produced)
	r.evidence = append(r.evidence, effectEvidence(event, repeat))
	if len(r.evidence) > 24 {
		r.evidence = r.evidence[len(r.evidence)-24:]
	}
	added := addedSomething(event, saved, moved, r.ledger)
	// AND THE RUN IS FOLDED IN BESIDE THE COUNTER, off the same
	// reading of the same step. It grows only where the counter was
	// reset by a step that changed nothing, which is the one shape
	// the counter cannot see ([effectLedger.counted]).
	r.effects.counted(event.Tool, added, repeat)
	partLanded := r.tookAPartsReport()
	if saved && !r.seen[path] {
		r.seen[path] = true
		r.changed = append(r.changed, path)
		// SAID OUT LOUD THE MOMENT IT IS TRUE. The node's leavings are
		// written once at the end; this is the same fact told while
		// another window could still act on it.
		r.node.noteWrote(path)
	}
	r.count(event, partLanded, added)
	// Named once. The loop keeps draining after the cancel — the child
	// is still finishing its batch and closing its stream — and a second
	// threshold tripping on the way out must not rewrite the reason the
	// node was stopped.
	if r.stopped != "" {
		return
	}
	r.trip()
}

// tookAPartsReport answers whether a part has reported since the last time this
// was asked, and moves the mark when one has.
//
// THE MARK IS MONOTONE AND IT IS READ RATHER THAN RECOMPUTED, because that is
// the whole of what it is for: tool events and model requests travel on separate
// lanes, so a fast part can report after a request was made but before the drain
// reaches that request's last tool event, and a count taken at this instant would
// mislabel the old step as a new idle one.
func (r *childRun) tookAPartsReport() bool {
	reports := r.child.reportedChildren()
	if reports <= r.reportedParts {
		return false
	}
	r.reportedParts = reports
	return true
}

// count is the no-progress counter's whole reading of one step: it is reset by
// anything that was progress, suspended by the two conditions under which the
// counter's claim is simply false, and otherwise raised by one.
func (r *childRun) count(event Event, partLanded, added bool) {
	switch {
	// A PART LANDING IS PROGRESS EVEN WHEN THIS EVENT IS OLD NEWS. Tool
	// events and model requests travel on separate lanes, so a fast part
	// can report after a request was made but before this drain reaches
	// that request's last tool event. Reading only
	// [Agent.childrenOutstanding] at this instant then mislabels the old
	// step as a new idle one. The monotone report count gives the landing
	// one exact place in the ledger, whether the part finished or failed.
	//
	// AND THAT RACE IS ALL IT IS FOR. A report the node was parked on has
	// already been paid for by [childRun.foldParts]'s own reset, and the mark is
	// squared up against every request made there, so a landing can buy
	// the counter one reset and never two.
	case partLanded:
		r.idle = 0
	// ── A STEP THE HARNESS FAILED IS THE HARNESS'S STEP ──
	//
	// It comes FIRST, ahead of every other reading, because it is not a
	// finding about the node at all: the tool never ran, the world never
	// answered, and the bytes the node read were written on this side of
	// the wall — a hand that was withdrawn, a door that refused the call
	// (withdrawn.go's [Event.HarnessMade]).
	//
	// Measured in SWE-Marathon s4: the landing pass took `bash` off a
	// worker's belt and the harness then answered eight retries with
	// "Unknown tool", each of which was a step that taught nothing, saved
	// nothing and moved no worktree — a counter reading them would have
	// been counting its own refusals against the model. NEITHER
	// DIRECTION: it is not progress either, so a harness failure cannot
	// launder a genuine spin by resetting the count. The step still
	// costs a step and still stands in the evidence, because the money
	// was really spent and the auditor should see what happened.
	case event.HarnessMade:
	case r.child.taskNewsOwed() > 0:
		// AND A REPORT WAITING FOR ITS READER SUSPENDS OLD STEPS. Until
		// the next request carries the queued note, an event reaching this
		// drain may still belong to the turn from before the report landed.
		// Counting those delayed events would spend the freshly reset
		// allowance before the parent had seen the news it is meant to
		// integrate. Once [Agent.drainSteering] carries the note it clears
		// this count, and genuine spinning over the fold is counted again.
	// EXPLORATION IS PROGRESS, and so is PRODUCTION. A research
	// node may never write until its final words; a build node may
	// spend its first dozen steps reading; a node making pictures
	// paints, looks at what it painted, and paints again without
	// ever calling edit or write. What stops a node is SPINNING —
	// the same target again, no new file, no new dirt — not the
	// absence of an edit (PMCoder's "reads saturated", not "reads
	// happened").
	case added:
		r.idle = 0
	case r.child.childrenOutstanding():
		// AND A NODE WHOSE WORK IS IN SOMEBODY ELSE'S HANDS IS NOT
		// SPINNING. The counter's whole claim is that a step which
		// taught nothing and saved nothing is a step the node had no
		// business taking — and that claim is false the moment the
		// work itself is elsewhere. A parent between its parts' reports
		// has nothing left to write (the parts hold it), nothing left
		// to learn (the answer is being made in three other
		// worktrees), and the only hands it can reach for are the ones
		// that look at what its parts are doing. Six of those and it
		// killed itself while every part was still working — the
		// division bought three workers and delivered nothing, because
		// the one node that could integrate them was gone.
		//
		// SUSPENDED AND NOT SWITCHED OFF. The moment the last report
		// lands the counter starts again from zero, so a parent that
		// spins over the FOLD is caught exactly as it always was. What
		// is still standing over this stretch is the step budget
		// ([taskLimits.maxSteps]) and the deadline, which are bounds on
		// spend rather than findings about the work.
	default:
		r.idle++
	}
}

// trip is the four thresholds, read in the one order they may be read in.
func (r *childRun) trip() {
	switch {
	case r.steps >= r.limits.maxSteps*(r.extensions+1):
		r.checkpoint(fmt.Sprintf("%d-step checkpoint", r.limits.maxSteps*(r.extensions+1)), true)
	case !time.Now().Before(r.deadline):
		r.checkpoint("deadline checkpoint", true)
	case r.idle >= r.limits.noProgress:
		r.stopped = fmt.Sprintf("stopped: %d steps without progress", r.limits.noProgress)
		r.stop()
	// ── AND THE STEPS THAT LOOK LIKE PROGRESS AND ARE NOT ──
	//
	// It comes LAST, under the counter, and that placement is the whole
	// of its scope. Everything the counter already catches — the same
	// question asked again, the same directory listed again — reaches
	// its threshold on the same step and is stopped there, exactly as
	// it always was. What is left underneath is the one shape the
	// counter is blind to by construction: a SUCCESSFUL SAVE, which
	// resets it ([addedSomething]) without anybody ever asking what
	// landed. Measured on 2026-08-31: one file rewritten twenty times
	// in twenty-two minutes for $7.99, every call clean, the counter at
	// zero throughout, and the step cap the only thing that ended it.
	//
	// NOTHING IS DECIDED HERE. The run length says WHEN TO ASK and
	// never what the answer is: the reader is handed the sentence
	// counting the run and the working copy it is standing in, and it
	// rules. A number in this file saying how many identical writes are
	// too many would be a number wrong for the next kind of work — the
	// reason the leash was fed a narrative in the first place is that
	// nobody could write that number down honestly.
	//
	// AND IT IS THE NODE'S OWN THRESHOLD IT BORROWS, not one of its
	// own. `no_progress` is already this harness's single answer to
	// "how many in a row is worth stopping over", it is on the wire so
	// a brief can raise it for work that is legitimately repetitive,
	// and a second number beside it would be the drift the
	// one-source-of-truth law forbids (CLAUDE.md's design laws).
	case r.effects.sameEffectRun() >= r.limits.noProgress:
		r.checkpoint("repeat checkpoint", false)
	}
}

// landIfStopped is THE LANDING TURN, and it happens after the active request has
// drained. It is a fresh turn so no request is killed mid-flight; the
// instruction forbids exploration and permits only writing the deliverable
// already in hand. A node that was not stopped, and one whose whole session was
// cut, get no such turn — there is nothing to land in the first case and nobody
// to land it in the second.
//
// THE LANDING BELT IS [savingTools] AND NOT A PAIR OF NAMES. A node
// whose deliverable is a picture, a piece of music or a video saves it
// with the verb that makes it, and a landing pass that admitted only
// write and edit took that verb away at the one moment the node was
// being ordered to produce — see [landingInstruction] for what the node
// then said. The instruction is generated FROM the belt, so a media verb
// the machine does not have is neither offered nor named.
//
// THE NARROWING IS A WITHDRAWAL AND SAYS SO (withdrawn.go). It used to be
// two slice headers swapped in place, which left the dispatcher unable to
// tell a hand that was TAKEN from a name that never existed — so a node
// reaching for its build was answered "Unknown tool: bash", retried eight
// times because eighteen bytes gave it no reason not to, and was then
// nudged for repeating itself. Going through [Agent.withdrawTools] records
// what went and what is left, under the same lock as the swap.
//
// ── AND WHAT IT SAVED IN THAT TURN WAS NEVER CHECKED ──
//
// Measured in SWE-Marathon s4: a worker that had been building and testing
// all run was landed, lost `bash` with the narrowing, and then edited two
// source files anyway — the last two things it did. They never compiled.
// The report said the work was done, the parent believed it, and the
// conversation discovered the breakage five minutes later.
//
// THREE FACTS MAKE THE SENTENCE TRUE, and it is written only when all
// three hold: the node had been running a check, the narrowing took that
// hand away, and it saved something afterwards. It says nothing about what
// the check was, what language the work is in, or whether the edits are
// good — only that nothing looked at them, which is the one thing the
// reader downstream cannot see for itself. The words are the vocabulary a
// landing already uses for work nobody could stand behind, and they lead
// the report, so a parent reading a stopped node's account is never handed
// unverified edits as finished ones ([Agent.landStopped]).
func (r *childRun) landIfStopped() {
	if r.stopped == "" || r.ctx.Err() != nil {
		return
	}
	restore := r.child.withdrawTools(landingBelt, landingWithdrawal)
	landing := landingInstruction(r.child.beltTools())
	savedInLanding := false
	if events, err := r.child.Submit(r.ctx, landing); err == nil {
		savedInLanding = r.drainLanding(events)
	}
	lostItsCheck := r.child.landingLostTheCheck()
	restore()
	if savedInLanding && r.ranCheck && lostItsCheck {
		r.stopped = withReport(r.stopped, unverifiedEdits)
	}
}

// drainLanding consumes the landing turn's events and answers whether the node
// saved anything in it.
//
// It is NOT [childRun.drain]: no step is counted, no threshold is read and no
// checkpoint is put, because the node has already been stopped and the turn
// exists only to let it write down what it is holding. What it keeps is the one
// thing the rest of the run keeps — which files were written — so a deliverable
// produced in the landing turn is on the ledger like any other.
func (r *childRun) drainLanding(events <-chan Event) bool {
	saved := false
	for event := range events {
		r.room.publish(event)
		if event.Kind != EventToolEnd {
			continue
		}
		if path, wrote := changedPath(event, r.dir); wrote && !r.seen[path] {
			r.seen[path] = true
			r.changed = append(r.changed, path)
			r.node.noteWrote(path)
		}
		// AND THE LANDING SAVED SOMETHING ONLY IF THE CALL DID
		// ([producedAFile]). The sentence three checks below is about work
		// nobody looked at, and a landing turn that only measured a clip
		// produced no work to look at.
		if producedAFile(event.Tool, event.Args) {
			saved = true
		}
	}
	return saved
}

// foldParts is THE NODE THAT HANDED PART OF ITS WORK OUT.
//
// A parent's turn ends the moment it has nothing left to say, and its
// sub-tasks are still working: the belt hands the id back immediately and
// tells it not to wait (task.go). So the turn ending is NOT the node ending.
// The runner holds it open, and every report that lands re-enters the model
// with it — the same turn a landing starts in a conversation, started here
// by the one who is reading it ([Agent.resumeTurn]).
//
// THE WAIT IS ON THE REPORT AND NEVER ON THE STATE. A node is settled a
// moment before its news is handed over ([Agent.deliverTaskNote]), and a
// waiter watching the state would stop waiting inside that moment and land
// its parent on a report nobody read.
//
// A tripped threshold or a cut context ends this exactly as it ends the
// turn above: the children are stopped with the parent (see
// [TaskGraph.stopChildren]) and their branches are kept.
//
// AND THE PERSON CAN STILL REACH IT WHILE IT WAITS. A parent parked on its
// pieces has no turn running, and [Agent.wakeLocked] declines inside a task —
// so a line steered into its room (task_room.go) has nothing of its own to
// land in. It is held on the same queue the reports ride and it is READ HERE:
// `held` keeps this loop from parking on top of somebody's words and from
// closing the child while they are still queued, and the resumeTurn below is
// the turn that puts them in front of the model. Without it the sentence sat
// there for as long as the slowest piece ran and was dropped outright when
// the last report came in, having been accepted with a note saying it had
// arrived.
func (r *childRun) foldParts() {
	for r.stopped == "" && r.runCtx.Err() == nil {
		// The generation is taken BEFORE the question, so a report landing
		// between the two closes the channel this select is about to wait on.
		news := r.child.taskNewsWait()
		// ── AND THE TWO FACTS ARE READ AS ONE ──
		//
		// A delivery writes the queue, then the fact, then the wake, and never in
		// any other order ([Agent.deliverTaskNote]) — and a PAIR of reads defeats
		// that order whichever way round it is taken, because the last part of a
		// division can land between them. Read owed first and this node takes its
		// integration turn holding every report and is then told about news that
		// turn already carried, which is one model turn spent on an empty request.
		// Read outstanding first and it leaves this loop with a report nobody has
		// read. One locked read answers both ([Agent.taskNewsStanding]).
		owed, working := r.child.taskNewsStanding()
		held := r.child.steeringHeld()
		if owed == 0 && !held && !working {
			// ── THE DOOR SHUTS BEFORE THE LOOP LEAVES ──
			//
			// From here the child never reads again — the check and the landing
			// are other hands — but it stays OPEN until [Agent.workTaskNode]'s
			// retire, which on a checked node is minutes away. A line steered in
			// during that window would still be TAKEN ([Agent.enqueueSteeredLine]
			// answers whether the agent is closed, not whether anybody will
			// drain it), echoed by the room as said, and then closed over: the
			// exact swallow the speaker's clearing at close exists to prevent
			// (task_room.go's [taskRoom.speaker]), happening in the gap before
			// close. So the speaker is withdrawn HERE, at the moment "nobody is
			// in there to read it" becomes true, and the queue is asked once
			// more: a line that raced the withdrawal — the room's own lock
			// orders the two, see [taskRoom.steerIn] — is answered by one more
			// turn instead of dying with the worker (#273).
			//
			// AND A CAUGHT LINE PUTS THE SPEAKER BACK. The turn that answers it
			// is a turn the worker is reading again: a follow-up steer must be
			// deliverable, and a part that lands while it runs must reach THIS
			// worker rather than being misrouted to the person's own
			// conversation ([Agent.deliverTaskNote] reads the speaker to decide
			// who is owed the report). The next pass through this gate takes
			// the speaker away again.
			r.room.speaking(nil)
			if !r.child.steeringHeld() {
				return
			}
			r.room.speaking(r.child)
		}
		if working && !held {
			r.park(news)
			continue
		}
		// ── AND A LANDING IS SPENT EXACTLY ONCE ──
		//
		// The reset in [childRun.park] is this node's whole payment for every
		// report that came in while it was parked. So the mark those reports are
		// counted against is squared up HERE, at the instant a request is made:
		// everything reported by now is news this request is carrying, and it has
		// been paid for. Left at its old value, the first step of the fold read the
		// same landings a second time through `partLanded` and was handed a free
		// step off news the node had already been paid for — one more step than a
		// threshold of two can afford, and the spin the fold is judged on went
		// uncounted.
		//
		// THE MARK IS TAKEN HERE AND NOT AT THE UNPARK, because a report and the
		// question "is anything still outstanding" are two reads at two instants:
		// the last of three parts can land between them, park the node one more
		// time and leave a mark one short. Taken against the request, there is no
		// gap — a report that lands after this line is a report this request could
		// not have carried, which is exactly the race `partLanded` is for.
		r.reportedParts = r.child.reportedChildren()
		next := r.child.resumeTurn(r.runCtx)
		if next == nil {
			return
		}
		r.drain(next)
	}
}

// park is WAITING, WHICH IS NOT WORKING AND IS NOT ASKED FOR EITHER.
//
// While ANY part is still out this node is PARKED: no request, no step, no
// counter, no clock. It used to be parked only in the gaps between reports
// and re-entered on each one, and the difference is a whole coordination
// pattern. A parent asked after the first of three reports has two thirds
// of an answer and one honest thing to say about it — that it is waiting —
// and every turn it spends saying so is money, transcript and, because the
// harness cannot tell waiting from spinning, six steps closer to being
// killed on the spot. The reports simply QUEUE instead ([Agent.enqueueNote]
// keeps them in the order they landed), and the turn that reads them reads
// all of them, which is the only turn whose brief — fold these into one
// deliverable — it can actually carry out.
//
// THE PERSON IS THE EXCEPTION AND THE ONLY ONE. A line steered into this
// node's room (task_room.go) is somebody at a keyboard waiting for an
// answer, and holding it for as long as the slowest part runs would be the
// room going silent on them. Whoever calls this has already asked, and takes the
// loop past the park while somebody's words are held.
//
// ── AND THE WAIT COSTS THE NODE NOTHING IT WOULD HAVE SPENT WORKING ──
//
// THE CLOCK IS PUSHED BY EXACTLY THE PARKED TIME. The deadline is a
// bound on how long this node may WORK before somebody looks at whether
// it is getting anywhere ([taskLimits.deadline]); a stretch in which it
// made no request and took no step is not that. Left running, it was a
// node whose parts took twenty minutes tripping the deadline checkpoint
// on its first step of integration and being audited for spinning while
// holding three finished reports it had not been given a chance to read.
//
// AND THE NO-PROGRESS COUNTER STARTS AGAIN FROM ZERO, because a report
// landing is the largest single thing this node can learn. Whatever it
// had accrued reaching for the only hands it had while it waited is
// spent, and the fold is judged on the fold.
//
// THE STEP BUDGET NEEDS NOTHING DONE TO IT: a step is one finished tool
// call and a parked node makes none. Nor does the harness's own ceiling,
// which never stands over a node at all ([Agent.checkpoints] refuses
// InTask) — so a park cannot move that meter either.
func (r *childRun) park(news <-chan struct{}) {
	// The lane goes back for exactly as long as the wait lasts
	// ([TaskGraph.park]).
	since := time.Now()
	r.node.park()
	select {
	case <-news:
	case <-r.runCtx.Done():
	}
	r.node.unpark()
	r.deadline = r.deadline.Add(time.Since(since))
	r.idle = 0
}
