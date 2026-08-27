package session

// THE PARENT THAT HANDED ITS WORK OUT, AND WHAT WAITING FOR IT COSTS.
//
// Every test here is written from ONE REAL RUN. A ten-issue job was split into
// three parts, all three were admitted before the parent's first request, and the
// parts really did work — ninety-four, two hundred and one, three hundred and
// fifty-nine seconds, four workers overlapping. The parent spent that window
// making thirty-one requests on a five second cadence about work it had already
// given away, and at 09:29:24 it announced `stopped: 6 steps without progress`
// and died — before its slowest part had finished. Nothing integrated. The batch
// did not finish.
//
// The counter was not wrong about what it saw. It was wrong about what it meant:
// a node between its parts' reports has nothing to write (the parts hold the
// work), nothing to learn (the answer is being made in three other worktrees) and
// no hand to reach for except the one that looks at what the parts are doing. Six
// of those and it read a parent doing the only thing available to it as a parent
// doing nothing.
//
// So the law these pin is one sentence: WAITING IS NOT WORKING, AND IT IS NOT
// SPINNING EITHER. A node with parts outstanding is parked — not asked, not
// counted, not clocked — and the turn that reads its reports reads all of them.

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// parkedParts is the graph hook these tests run under: every part the parent
// hands out sits there working until the test releases it, and then lands with
// the report it was given. The parent itself is left alone, which is what
// [newNest]'s own default does and for the same reason — a parent that landed the
// moment it started could never hand anything out.
func parkedParts(release <-chan struct{}, reports map[string]string, states map[string]TaskState) func(*TaskNode) {
	return func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		go func() {
			<-release
			title := node.title()
			state := TaskDone
			if want, ok := states[title]; ok {
				state = want
			}
			report := reports[title]
			if report == "" {
				report = title + " is done"
			}
			node.finish(report, nil, "", "")
			node.graph.complete(node, state)
		}()
	}
}

// handOutThree is the parent's first turn in the shape the run had it: three
// parts named, then a stretch of looking at the same thing over and over —
// which is all a node whose work is in three other worktrees can do — and then
// the last word of the turn.
//
// THE REPEATED READ IS THE POINT. `read` at the same path is a step that saves
// nothing, moves no worktree and teaches nothing new, which is exactly what the
// no-progress counter is built to catch. Against a threshold of 2 the old counter
// fired on the second of them and killed the parent where it stood.
func handOutThree(landed string) []step {
	steps := []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("p1", "propose_task", string(pieceArgs("arithmetic"))), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("p2", "propose_task", string(pieceArgs("currency"))), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("p3", "propose_task", string(pieceArgs("cli extras"))), nil
		},
	}
	for index := 0; index < 5; index++ {
		id := fmt.Sprintf("look-%d", index)
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "read", `{"path":"pyproject.toml"}`), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("three parts are out; nothing left for me until they report"), nil
	})
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(landed), nil
	})
	return steps
}

// runParent drives one node's whole run the way [Agent.workTaskNode] does, with
// the thresholds the test chose, and answers what stopped it.
func runParent(t *testing.T, nest *nest, limits taskLimits) (<-chan struct{}, *string) {
	t.Helper()
	stopped := new(string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, why, _ := runTaskChild(context.Background(), nest.node, nest.parent,
			"do the whole job", nest.node.config.Workspace, limits, nil, io.Discard)
		*stopped = why
	}()
	return done, stopped
}

// ── waiting is not spinning ─────────────────────────────────────────────────

// THE MEASURED FAILURE, PINNED. Three parts out, five identical reads against a
// no-progress threshold of two, and the parent must still be alive when its parts
// report — because every one of those steps was taken while the work was
// somewhere else.
func TestAParentWaitingOnItsPartsIsNotCountedAsStuck(t *testing.T) {
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: handOutThree("all three folded into one deliverable")}
	nest := newNest(t, completer, parkedParts(release, nil, nil))

	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 2})

	// The first turn runs to its end: three proposals, five repeated looks, one
	// closing sentence. Under the old counter the run was already over by the
	// fifth of these.
	waitRequests(t, completer, 9)
	waitQuiet(t, nest.node)
	select {
	case <-done:
		t.Fatalf("the parent ended while all three parts were still running: %q", *stopped)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parent never ended after its parts reported")
	}
	if *stopped != "" {
		t.Fatalf("the parent was stopped with %q, want a parent that waited to be left alone", *stopped)
	}
}

// AND IT IS NOT ASKED ANYTHING EITHER. The reports queue as they land, and the
// turn that reads them reads all three at once — which is the only turn whose
// brief, fold these into one deliverable, it can actually carry out. A parent
// re-entered after the first of three reports has two thirds of an answer and one
// honest thing to say about it, and every turn it spends saying so is money.
func TestAParentIsAskedNothingUntilEveryPartHasReported(t *testing.T) {
	first := make(chan struct{})
	rest := make(chan struct{})
	completer := &scriptedCompleter{steps: handOutThree("folded all three in")}
	nest := newNest(t, completer, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		gate := rest
		if node.title() == "arithmetic" {
			gate = first
		}
		go func() {
			<-gate
			node.finish(node.title()+" landed", nil, "", "")
			node.graph.complete(node, TaskDone)
		}()
	})

	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 6})
	waitRequests(t, completer, 9)
	waitQuiet(t, nest.node)
	asked := completer.requests()

	// ONE part reports. Nothing may be asked on the strength of it.
	close(first)
	time.Sleep(300 * time.Millisecond)
	if got := completer.requests(); got != asked {
		t.Fatalf("the parent was asked %d times after one of three parts reported, want it left parked at %d", got, asked)
	}
	// AND THE ROW SAYS WHAT IT IS DOING. A parked parent is not a row that has
	// gone quiet; it is a row waiting on its parts, and it says so.
	if !nest.parent.waitingOnItsPieces() {
		t.Fatal("the parent is not parked while two of its parts are still running")
	}
	if got := nest.parent.notice().Waiting; got != waitingOnItsParts {
		t.Fatalf("the parent's row says it is waiting on %q, want %q", got, waitingOnItsParts)
	}

	close(rest)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parent never ended after its parts reported")
	}
	if *stopped != "" {
		t.Fatalf("the parent was stopped with %q", *stopped)
	}
	// ONE integration turn, holding all three.
	if got := completer.requests(); got != asked+1 {
		t.Fatalf("the parent was asked %d times, want exactly one turn (%d) for the three reports together", got, asked+1)
	}
	last := userTextIn(completer.request(completer.requests() - 1))
	for _, want := range []string{"arithmetic landed", "currency landed", "cli extras landed"} {
		if !strings.Contains(last, want) {
			t.Fatalf("the integration turn reads %q, want %q in it", last, want)
		}
	}
	// AND THE HOLD IS OVER. A parent that is being asked something is not waiting.
	if nest.parent.waitingOnItsPieces() {
		t.Fatal("the parent is still parked after every part reported")
	}
}

// A PART THAT DID NOT FINISH IS STILL A REPORT, and the parent folds the others
// and says where the hole is. Nothing here is special-cased: the failed part's
// news rides the same queue in the same words the vocabulary already has.
func TestAParentIntegratesTheRestWhenOnePartFails(t *testing.T) {
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: handOutThree("two are in; the currency table is missing and I have said so")}
	nest := newNest(t, completer, parkedParts(release,
		map[string]string{"currency": "incomplete — the currency table was never written"},
		map[string]TaskState{"currency": TaskFailed}))

	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 2})
	waitRequests(t, completer, 9)
	waitQuiet(t, nest.node)
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parent never ended after its parts reported")
	}
	if *stopped != "" {
		t.Fatalf("the parent was stopped with %q, want a failed part to be news rather than an ending", *stopped)
	}
	last := userTextIn(completer.request(completer.requests() - 1))
	for _, want := range []string{"arithmetic is done", "cli extras is done", "incomplete — the currency table was never written"} {
		if !strings.Contains(last, want) {
			t.Fatalf("the integration turn reads %q, want %q in it", last, want)
		}
	}
}

// ── what the wait costs ─────────────────────────────────────────────────────

// THE CLOCK STOPS WITH THE NODE. The deadline bounds how long a node may WORK
// before somebody looks at whether it is getting anywhere; a stretch in which it
// made no request and took no step is not that. Left running, a parent whose
// parts took twenty minutes tripped the deadline checkpoint on its first step of
// integration and was audited for spinning while holding three finished reports
// it had not been given a chance to read.
func TestTheWaitOnItsPartsSpendsNoneOfTheParentsClock(t *testing.T) {
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: handOutThree("folded them in")}
	nest := newNest(t, completer, parkedParts(release, nil, nil))
	// If the deadline is ever reached the checkpoint asks whether the node is
	// working; this answers no, so a clock that kept running produces a stop with
	// this sentence in it rather than a live provider call.
	nest.session.config.TaskProgressCheck = func(string, []string) (bool, string) {
		return false, "it read the same file five times"
	}

	// A deadline far shorter than the wait: any clock that keeps running through
	// a park has expired several times over by the time the parts report.
	done, stopped := runParent(t, nest, taskLimits{
		maxSteps: 200, noProgress: 6, deadline: 150 * time.Millisecond,
	})
	waitRequests(t, completer, 9)
	waitQuiet(t, nest.node)
	time.Sleep(600 * time.Millisecond)

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parent never ended after its parts reported")
	}
	if *stopped != "" {
		t.Fatalf("the parent was stopped with %q, want the parked stretch not to have spent its deadline", *stopped)
	}
}

// AND THE HARNESS'S OWN CEILING NEVER STOOD OVER A NODE AT ALL, parked or
// working. Work inside a task already lives under its own step cap, its own
// no-progress counter and its own deadline, and a second meter over the top of
// those would be the harness governing the governed ([Agent.checkpoints]). This
// is the half of "the wait costs nothing" that is true by construction, and it is
// written down here so a change that quietly turned the ceiling on inside tasks
// is caught by the lane that would be hurt by it.
func TestTheCeilingNeverMetersANodeParkedOnItsParts(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.parent.park()
	defer nest.parent.unpark()
	if !nest.parent.waitingOnItsPieces() {
		t.Fatal("the node did not park")
	}
	if nest.node.checkpoints(context.Background(), userText("get on with it")) {
		t.Fatal("the harness ceiling meters a task node; a node has its own thresholds and no second one may stand over them")
	}
}

// ── the nursery law ─────────────────────────────────────────────────────────

// NO PART OUTLIVES THE COORDINATION IT WAS CUT OUT OF, and the order is the
// whole of it.
//
// A part's job row lives in its OWNER'S registry, and a part's owner is its
// parent's WORKER. So closing that worker reaches every part still running
// through [jobRegistry.shutdown] — which cancels a task job's context WITHOUT
// calling its explicit stop, because [job.signal] takes the `stop` handle and
// only [jobRegistry.kill] calls the other one. A part cut that way reads its own
// cancel as A PROCESS QUITTING, lands on the "paused — it resumes" road and
// returns nothing, so its state stays running for a recovery that is never
// coming. That is exactly what was measured: a part left `running` with a report
// saying it would resume, its lane never handed back, for the fifty minutes
// between its parent failing and the run hitting its wall.
//
// Both halves are pinned: a Close alone does not mark a node stopped, and the
// stop the parent takes first does.
func TestClosingAWorkerDoesNotMarkItsPartsStoppedAndTheParentMustDoItFirst(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]

	// HALF ONE: a job registry's shutdown cuts a task job's context and says
	// nothing about it. Nothing here may be relied on to end a part honestly.
	registry := nest.node.jobs
	if registry == nil {
		t.Fatal("the worker has no job registry to close")
	}
	cut := make(chan struct{})
	// The registry sends SIGTERM and then SIGKILL, and both reach the same handle
	// (jobs.go's [job.signal]), so the cancel a real node hands it is idempotent.
	var once sync.Once
	if _, err := registry.startTask(999, "a part of somebody's work", func() { once.Do(func() { close(cut) }) }, kid.markStopped); err != nil {
		t.Fatalf("startTask: %v", err)
	}
	registry.shutdown(0)
	select {
	case <-cut:
	case <-time.After(2 * time.Second):
		t.Fatal("the close never cancelled the part's context")
	}
	if kid.wasStopped() {
		t.Fatal("a Close now marks a task job stopped; the nursery law below is written on the assumption that it does not, so one of the two must move")
	}

	// HALF TWO: the parent stops its parts itself, before anything closes. The
	// part is MARKED, so it reads its ending as what it is — stopped, its branch
	// kept — settles, and hands its lane back.
	nest.graph.stopChildren(nest.parent.id)
	waitDoneNode(t, kid)
	if !kid.wasStopped() {
		t.Fatal("the parent's stop did not mark the part, so the part would read its cancel as a process quitting and wait for a recovery that is not coming")
	}
	if state := kid.stateNow(); !state.settled() {
		t.Fatalf("the part is %s after its parent stopped it, want it settled rather than left running forever", state)
	}
}

// AND A RUN WHOSE PROCESS IS GOING AWAY STILL HANDS ITS LANE BACK. The node's
// state is deliberately left alone — that is the whole mechanism a recovery reads
// — but the goroutine that was holding the lane is gone, and a slot booked
// against a node nothing is doing is the person's task.parallel cap quietly
// falling by one for every node that comes after ([TaskGraph.handBackLane]).
func TestAPausedRunHandsItsLaneBackSoTheQueueBehindItMoves(t *testing.T) {
	nest := newNest(t, nil, nil)
	started := make(chan uint64, 4)
	nest.graph.run = func(node *TaskNode) { started <- node.id }
	nest.graph.mu.Lock()
	nest.graph.limit = 1
	nest.graph.running = 1
	nest.parent.state = TaskRunning
	nest.graph.mu.Unlock()

	id := nest.graph.reserve()
	nest.graph.admit(id, taskSpec{title: "the next one", brief: "b", acceptance: "a", depth: 1})
	select {
	case <-started:
		t.Fatal("a second node started while the only lane was taken")
	case <-time.After(100 * time.Millisecond):
	}

	nest.graph.handBackLane(nest.parent)
	select {
	case got := <-started:
		if got != id {
			t.Fatalf("node %d started, want the one that was queued behind the lane (%d)", got, id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the queue behind a paused run never moved: its lane was never handed back")
	}
	// The paused node's own state is untouched, because that is what recovery
	// reads.
	if state := nest.parent.stateNow(); state != TaskRunning {
		t.Fatalf("the paused node is %s, want it left running for recovery to find", state)
	}
}

// ── the brief a parked parent has not read yet ──────────────────────────────

// A NODE WHOSE WORK WAS HANDED OUT BEFORE IT STARTED IS NOT ASKED FIRST EITHER.
// The parts were submitted on its behalf between the worker being built and its
// first request (task_divide_sketch.go), so they are already running: asking it
// anything now is buying a turn about waiting. The brief is queued instead, and
// the turn the last report starts is the one that reads it — one request holding
// the brief and every part's news at once.
func TestAParentHandedADivisionBeforeItStartedOpensOnTheReportsAndNotOnTheWait(t *testing.T) {
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the parts are in; here is the one deliverable"), nil
		},
	}}
	nest := newNest(t, completer, parkedParts(release, nil, nil))
	// The division that was drawn for it, put on its behalf before its first
	// request — which is what leaves a node with children and no turn yet.
	for _, title := range []string{"arithmetic", "currency"} {
		if _, _, err := nest.node.proposeTask(context.Background(), pieceArgs(title)); err != nil {
			t.Fatalf("propose_task: %v", err)
		}
	}

	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 6})
	// NOT ONE REQUEST while the parts run. Before this the node opened on its
	// brief and spent the whole window answering a question about work it had
	// already given away.
	time.Sleep(300 * time.Millisecond)
	if got := completer.requests(); got != 0 {
		t.Fatalf("the parent was asked %d times before any part reported, want none", got)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parent never ended after its parts reported")
	}
	if *stopped != "" {
		t.Fatalf("the parent was stopped with %q", *stopped)
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("the parent was asked %d times, want the one turn that holds the brief and both reports", got)
	}
	opening := userTextIn(completer.request(0))
	for _, want := range []string{"do the whole job", "arithmetic is done", "currency is done"} {
		if !strings.Contains(opening, want) {
			t.Fatalf("the one turn reads %q, want %q in it", opening, want)
		}
	}
}
