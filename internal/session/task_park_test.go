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
	"path/filepath"
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

// waitParked waits for a node's runner to be ON A PARK LATER THAN `past` —
// waiting on its parts with no turn running — and answers that park's
// generation, so the caller can name it to the next wait. It fails rather than
// hanging. Pass 0 for the first park of the run, when there is no earlier one to
// be told apart from.
//
// It is the signal a test needs before it can say "and it did not ask", because
// the honest reading of a parent that has been given no time is neither yes nor
// no. A clock cannot answer that question on a machine carrying other work: the
// milliseconds pass whether or not the goroutine behind them has been given a
// processor.
//
// AND "PARKED" ALONE IS NOT THAT SIGNAL ONCE A REPORT HAS LANDED. A parent woken
// by one part unparks, reads it, finds another part outstanding and parks again,
// and the flag is true on both sides of that window — so a waiter looking only
// at the flag can return on the park the parent has NOT YET WOKEN FROM, and the
// assertion the caller makes one line later lands in the unpark → re-park gap
// and reads the wrong park. That was 2 runs in 40 of
// [TestAParentIsAskedNothingUntilEveryPartHasReported] under -race (#201).
//
// THE CURE IS THE GENERATION AND NOT A LONGER POLL. Sleeping until the flag
// "looks settled" would pass on a quiet machine and hide a parent that had
// genuinely stopped re-parking, which is the regression these tests exist to
// catch. Waiting past a NAMED park is an answer about identity: this is a park
// that had not been taken when the caller last looked.
func waitParked(t *testing.T, node *TaskNode, past uint64) uint64 {
	t.Helper()
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		// One reading, never two: the flag and the number are taken under the
		// same hold of the graph's lock ([TaskNode.parkStanding]).
		if parked, generation := node.parkStanding(); parked && generation > past {
			return generation
		}
		time.Sleep(time.Millisecond)
	}
	if past > 0 {
		t.Fatalf("the parent never parked again after park %d", past)
	}
	t.Fatal("the parent never parked on its parts")
	return 0
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
	completer := &scriptedCompleter{steps: handOutThree("folded all three in")}
	// THE PARTS ARE HELD AND LANDED ONE AT A TIME, each of them while the parent
	// is on its park. Landing the last two together raced the delivery against
	// the loop that reads it: a report is put on the parent's queue and its part
	// marked reported ([TaskNode.markNoted]) one instant before the news counter
	// moves ([Agent.postTaskNews]), so a runner that read "nothing outstanding,
	// one note owed" between those two instants took its integration turn — with
	// every report in it, correctly — and was then told about news that turn had
	// already carried, spending one more turn on nothing. Landing them against
	// the park closes the window: the parent is asleep, and the wake it comes
	// back on is the last thing the last delivery does.
	var partsMu sync.Mutex
	var parts []*TaskNode
	nest := newNest(t, completer, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		partsMu.Lock()
		parts = append(parts, node)
		partsMu.Unlock()
	})

	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 6})
	waitRequests(t, completer, 9)
	waitQuiet(t, nest.node)
	asked := completer.requests()

	partsMu.Lock()
	landing := append([]*TaskNode(nil), parts...)
	partsMu.Unlock()
	if len(landing) != 3 {
		t.Fatalf("%d parts were admitted, want the three that were handed out", len(landing))
	}
	// EVERY WAIT HERE NAMES THE PARK IT ALREADY SAW. A part is landed against a
	// park the parent is known to be on, and the assertions that follow wait for
	// the NEXT park — the one it took after reading that report — because the
	// flag is true on both sides of the unpark → re-park window and "parked"
	// alone would let the reads below land inside it.
	parked := waitParked(t, nest.parent, 0)
	land := func(part *TaskNode) {
		t.Helper()
		part.finish(part.title()+" landed", nil, "", "")
		part.graph.complete(part, TaskDone)
	}

	// ONE part reports. Nothing may be asked on the strength of it.
	land(landing[0])
	parked = waitParked(t, nest.parent, parked)
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

	land(landing[1])
	waitParked(t, nest.parent, parked)
	land(landing[2])
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

// AND THE FOLD STILL HAS TO MOVE. A report buys the parent a fresh allowance
// and a chance to read the news; it does not make every later step progress. Two
// repeated reads after the reports are inside the integration turn must still
// spend a threshold of two and stop the parent exactly as they would any other
// worker.
func TestAParentThatSpinsAfterItsPartReportsIsStillStopped(t *testing.T) {
	release := make(chan struct{})
	steps := handOutThree("")
	steps[len(steps)-1] = func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("fold-look-1", "read", `{"path":"pyproject.toml"}`), nil
	}
	steps = append(steps,
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("fold-look-2", "read", `{"path":"pyproject.toml"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("I am still looking at the same thing"), nil
		},
	)
	completer := &scriptedCompleter{steps: steps}
	nest := newNest(t, completer, parkedParts(release, nil, nil))

	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 2})
	waitRequests(t, completer, 9)
	waitQuiet(t, nest.node)
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the idle integration turn never stopped")
	}
	if want := "stopped: 2 steps without progress"; *stopped != want {
		t.Fatalf("the idle integration turn stopped with %q, want %q", *stopped, want)
	}
}

// ── what the wait costs ─────────────────────────────────────────────────────

// THE CLOCK STOPS WITH THE NODE. The deadline bounds how long a node may WORK
// before somebody looks at whether it is getting anywhere; a stretch in which it
// made no request and took no step is not that. Left running, a parent whose
// parts took twenty minutes tripped the deadline checkpoint on its first step of
// integration and was audited for spinning while holding three finished reports
// it had not been given a chance to read.
// THE WAIT IS MEASURED RATHER THAN SLEPT THROUGH. The run's deadline, its
// checks and the stretch a park gives back all read one clock
// ([childRun.now] → [Agent.taskClockNow]), which production leaves as the real
// one and this test drives by hand — so what is pinned is the causal law and not
// how much wall time nine scripted steps happened to take on a loaded machine.
// Timed with real sleeps it failed under a full-package run for exactly that
// reason: the setup itself spent the 150ms allowance before the park began.
func TestTheWaitOnItsPartsSpendsNoneOfTheParentsClock(t *testing.T) {
	release := make(chan struct{})
	// THE INTEGRATION TURN TAKES A TOOL STEP, because a step is where a run reads
	// its deadline ([childRun.trip]). A fold that only says a sentence never asks
	// the clock anything, so it cannot show whether the clock was right.
	steps := handOutThree("folded them in")
	steps[len(steps)-1] = func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("fold", "read", `{"path":"pyproject.toml"}`), nil
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("folded them in"), nil
	})
	completer := &scriptedCompleter{steps: steps}
	nest := newNest(t, completer, parkedParts(release, nil, nil))
	// If the deadline is ever reached the checkpoint asks whether the node is
	// working; this answers no, so a clock that kept running produces a stop with
	// this sentence in it rather than a live provider call.
	nest.session.config.TaskProgressCheck = func(string, []string) (bool, string) {
		return false, "it read the same file five times"
	}
	// The clock stands still through the setup, so every step below happens
	// inside the allowance whatever the machine is doing.
	clock := newFakeClock()
	nest.node.taskNow = clock.now
	nest.node.taskTimer = clock.timer

	done, stopped := runParent(t, nest, taskLimits{
		maxSteps: 200, noProgress: 6, deadline: 150 * time.Millisecond,
	})
	waitRequests(t, completer, 9)
	waitQuiet(t, nest.node)
	// AND THE ADVANCE HAPPENS INSIDE THE PARK, which is the only stretch this law
	// is about. Four times the whole allowance passes: a clock that kept running
	// through it has expired several times over by the time the parts report.
	waitParked(t, nest.parent, 0)
	clock.advance(600 * time.Millisecond)

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

// AND THE SAME TIME SPENT WORKING DOES END THE RUN. The park's deduction is a
// statement about waiting, not a way of making the deadline unreachable, so the
// companion to the law above is that a node which spends its allowance at its
// own work is stopped exactly as it always was.
func TestAWorkerThatSpendsItsAllowanceWorkingIsStillStopped(t *testing.T) {
	clock := newFakeClock()
	// Every step this node takes moves the clock, which is what working costs.
	var steps []step
	for index := 0; index < 6; index++ {
		id := fmt.Sprintf("look-%d", index)
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			clock.advance(80 * time.Millisecond)
			return toolResponse(id, "read", `{"path":"pyproject.toml"}`), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("that is everything"), nil
	})
	completer := &scriptedCompleter{steps: steps}
	nest := newNest(t, completer, nil)
	nest.session.config.TaskProgressCheck = func(string, []string) (bool, string) {
		return false, "it read the same file five times"
	}
	nest.node.taskNow = clock.now
	nest.node.taskTimer = clock.timer

	done, stopped := runParent(t, nest, taskLimits{
		maxSteps: 200, noProgress: 6, deadline: 150 * time.Millisecond,
	})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parent never stopped after spending its whole allowance working")
	}
	if *stopped == "" {
		t.Fatal("a parent that spent its deadline at its own work was not stopped")
	}
}

// fakeClock is a clock a test moves on purpose. Its two calls are locked because
// the run reads it from the runner's goroutine while the test writes it.
type fakeClock struct {
	mu     sync.Mutex
	at     time.Time
	timers map[chan time.Time]time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{at: time.Now()} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
	for ch, at := range c.timers {
		if !c.at.Before(at) {
			ch <- c.at
			delete(c.timers, ch)
		}
	}
}

func (c *fakeClock) timer(after time.Duration) (<-chan time.Time, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if after <= 0 {
		ch <- c.at
	} else {
		if c.timers == nil {
			c.timers = make(map[chan time.Time]time.Time)
		}
		c.timers[ch] = c.at.Add(after)
	}
	return ch, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.timers, ch)
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
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the parts are in; here is the one deliverable"), nil
		},
	}}
	// THE PARTS ARE HELD BY THE TEST AND LANDED ONE AT A TIME, which is what
	// makes the count below a fact about the program rather than about the
	// machine. Landing both at once has a race in it that belongs to neither
	// this claim nor this test: a part's report reaches the parent's queue and
	// marks the part reported ([TaskNode.markNoted]) BEFORE it bumps the news
	// counter ([Agent.postTaskNews]), and a runner that reads the pair inside
	// that gap — nothing outstanding, one report owed — takes its turn carrying
	// both reports and is then told about news it has already carried, so it
	// spends one more turn saying nothing. On a quiet machine the two goroutines
	// never landed inside each other's gap; on a loaded one they did, and this
	// test failed with two identical requests.
	var partsMu sync.Mutex
	var parts []*TaskNode
	nest := newNest(t, completer, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		partsMu.Lock()
		parts = append(parts, node)
		partsMu.Unlock()
	})
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
	//
	// The park is the SIGNAL that it reached the wait, and it replaces a
	// three-hundred-millisecond sleep that only ever meant "it has probably got
	// there by now" — which on a busy machine was a parent that had not started.
	parked := waitParked(t, nest.parent, 0)
	if got := completer.requests(); got != 0 {
		t.Fatalf("the parent was asked %d times before any part reported, want none", got)
	}

	// One at a time, each landed only once the parent is back on its park, so the
	// last report is the one that wakes it and every earlier one is already
	// counted when it does.
	partsMu.Lock()
	landing := append([]*TaskNode(nil), parts...)
	partsMu.Unlock()
	if len(landing) != 2 {
		t.Fatalf("%d parts were admitted, want the two that were proposed", len(landing))
	}
	for index, part := range landing {
		if index > 0 {
			// Past the park the previous landing was made against, so this one
			// really is going into a parent that has read what it was sent.
			parked = waitParked(t, nest.parent, parked)
		}
		part.finish(part.title()+" is done", nil, "", "")
		part.graph.complete(part, TaskDone)
	}
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

// ── the last two parts landing together ─────────────────────────────────────

// AND WHEN THE LAST TWO PARTS LAND AT THE SAME INSTANT IT IS STILL ONE TURN.
//
// The two tests above land their parts one at a time, against the parent's park,
// because landing them together used to race the delivery against the loop that
// reads it — and that race was a real defect and not a test premise. A delivery
// writes three things in a fixed order (the queue, the fact, the wake:
// [Agent.deliverTaskNote]) and the runner read two of them as two separate
// questions, owed first. A last part landing between those reads was seen as
// "nothing outstanding, one note owed": the parent took its integration turn
// carrying BOTH reports, correctly, and was then told about news that turn had
// already carried, so it came back for one more turn with an empty request.
// Measured as two byte-identical requests — a model turn's money, and a blank
// exchange in the room.
//
// THE LANDINGS ARE RELEASED FROM ONE CHANNEL, which is as close to the same
// instant as two goroutines get, and the run is repeated because what is being
// pinned is that no interleaving of the two deliveries can buy a second turn.
// The parent is on its park before either lands, so the only thing that varies
// between rounds is the order the two deliveries reach the runner in.
func TestTheLastTwoPartsLandingTogetherStillCostOneTurn(t *testing.T) {
	for round := 0; round < 25; round++ {
		t.Run(fmt.Sprintf("landing %d", round+1), func(t *testing.T) {
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse("both parts are in; here is the one deliverable"), nil
				},
			}}
			parts := make(chan *TaskNode, 2)
			nest := newNest(t, completer, func(node *TaskNode) {
				if node.parent == 0 {
					return
				}
				parts <- node
			})
			for _, title := range []string{"arithmetic", "currency"} {
				if answer, failed, err := nest.node.proposeTask(context.Background(), pieceArgs(title)); err != nil || failed {
					t.Fatalf("propose_task: %v, failed=%v: %s", err, failed, answer)
				}
			}

			done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 6})
			// THE PARK IS THE STARTING LINE. Landing a part before the runner
			// reaches its wait would be a different test — one about the brief
			// being submitted — and it would not touch the window this pins.
			waitParked(t, nest.parent, 0)

			// Parking observes admitted children, not their asynchronous runner
			// callbacks. Wait for both callbacks before releasing their landings.
			var landing []*TaskNode
			timer := time.NewTimer(10 * time.Second)
			defer timer.Stop()
			for len(landing) < 2 {
				select {
				case part := <-parts:
					landing = append(landing, part)
				case <-timer.C:
					t.Fatalf("%d child runners started, want two", len(landing))
				}
			}
			release := make(chan struct{})
			var landed sync.WaitGroup
			for _, part := range landing {
				landed.Add(1)
				go func(part *TaskNode) {
					defer landed.Done()
					<-release
					part.finish(part.title()+" is done", nil, "", "")
					part.graph.complete(part, TaskDone)
				}(part)
			}
			close(release)
			landed.Wait()

			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the parent never ended after both parts reported")
			}
			if *stopped != "" {
				t.Fatalf("the parent was stopped with %q", *stopped)
			}
			if got := completer.requests(); got != 1 {
				extra := ""
				if got > 1 {
					extra = fmt.Sprintf("\nthe second request reads:\n%s", userTextIn(completer.request(1)))
				}
				t.Fatalf("the parent was asked %d times, want the one turn that holds the brief and both reports%s", got, extra)
			}
			opening := userTextIn(completer.request(0))
			for _, want := range []string{"do the whole job", "arithmetic is done", "currency is done"} {
				if !strings.Contains(opening, want) {
					t.Fatalf("the one turn reads %q, want %q in it", opening, want)
				}
			}
		})
	}
}

// waitReported waits for one part's report to have been MARKED handed over —
// the middle of the three things a delivery does ([Agent.deliverTaskNote]) — and
// fails rather than hanging. It is the signal a test needs to say "the delivery
// has reached the window" without a sleep standing in for it.
func waitReported(t *testing.T, part *TaskNode) {
	t.Helper()
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		if part.reported() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the delivery of %q never marked the part reported", part.title())
}

// AND IT IS ONE TURN WHEN A PART AND A HAND LAND ON EACH OTHER'S HEELS.
//
// The round above lands two parts on one road and lets the machine choose the
// interleaving. This one crosses the TWO roads a piece of this node's work comes
// home by — a sub-task's report and a forked hand's — because the parent parked
// on them cannot tell those apart and must not have to: it asks one question of
// the pair ([Agent.taskNewsStanding]) and takes one integration turn holding
// both.
//
// It used to hold a window open by freezing the task store, because a delivery
// wrote a checkpoint after its mark and stopped there. That window is gone. What
// a delivery owes the disk is now written when the RECIPIENT'S RECORD holds the
// note ([durableDelivery]), so there is no file write inside the seam to suspend
// a delivery on, and the two facts the waiter reads are made under one lock with
// nothing slow between them.
func TestAPartAndAHandLandingTogetherStillCostOneTurn(t *testing.T) {
	var (
		here      *nest
		delivered sync.WaitGroup
	)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			// The request waits for the part's delivery to be over, so what the
			// count below finds is the pair being read as one fact rather than a
			// delivery that had not finished when the turn started.
			delivered.Wait()
			return textResponse("the part and the hand are in; here is the one deliverable"), nil
		},
		// A second step so that a parent which is asked twice is answered rather
		// than erroring, and the count below is the finding instead of a stall.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("there was nothing in that request"), nil
		},
	}}

	var partsMu sync.Mutex
	var parts []*TaskNode
	here = newNest(t, completer, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		partsMu.Lock()
		parts = append(parts, node)
		partsMu.Unlock()
	})
	// THE GRAPH KEEPS A CHECKPOINT, which every real session's does
	// ([runTaskChild] hands its graph a store built from the session file) and a
	// scripted one does not: the acknowledgement this run's landing earns is
	// written to it, on the drain, and that write is on this test's road.
	here.graph.store = newTaskStore(filepath.Join(t.TempDir(), "tasks.json"))

	// One part handed out, and one hand forked — the two roads a piece of this
	// node's work can be on, and the two roads a report comes home by.
	if _, _, err := here.node.proposeTask(context.Background(), pieceArgs("currency")); err != nil {
		t.Fatalf("propose_task: %v", err)
	}
	hand, _, err := here.node.jobs.startHand("hand 1", "the shorter path")
	if err != nil {
		t.Fatalf("starting the hand: %v", err)
	}

	done, stopped := runParent(t, here, taskLimits{maxSteps: 200, noProgress: 6})
	waitParked(t, here.parent, 0)
	if got := completer.requests(); got != 0 {
		t.Fatalf("the parent was asked %d times while its work was out, want none", got)
	}
	partsMu.Lock()
	landing := append([]*TaskNode(nil), parts...)
	partsMu.Unlock()
	if len(landing) != 1 {
		t.Fatalf("%d parts were admitted, want the one that was proposed", len(landing))
	}

	// The part reports first, on its own goroutine, exactly as a runner does.
	delivered.Add(1)
	go func() {
		defer delivered.Done()
		here.node.deliverTaskNote(landing[0], landing[0].attemptNow(), landing[0].resultTag(),
			"task 2 finished: currency\ncurrency is done")
	}()
	waitReported(t, landing[0])

	// And the hand comes home on the other road, which is the wake the parent
	// reads its pair on.
	here.node.handIsHome(hand, 0,
		forkArguments{Parts: []forkPart{{Role: "the shorter path"}}},
		forkResult{say: "the shorter path is done", outcome: forkDone})

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parent never ended after its part and its hand reported")
	}
	if *stopped != "" {
		t.Fatalf("the parent was stopped with %q", *stopped)
	}
	if got := completer.requests(); got != 1 {
		extra := ""
		if got > 1 {
			extra = fmt.Sprintf("\nthe second request reads:\n%s", userTextIn(completer.request(1)))
		}
		t.Fatalf("the parent was asked %d times, want the one turn that holds both reports%s", got, extra)
	}
	only := userTextIn(completer.request(0))
	for _, want := range []string{"do the whole job", "currency is done", "the shorter path is done"} {
		if !strings.Contains(only, want) {
			t.Fatalf("the one turn reads %q, want %q in it", only, want)
		}
	}
}
