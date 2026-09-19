package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// The outcome words are the exit ladder's own (cmd/codeaf/envelope.go, the
// Short column every headless verb prints beside its exit codes), so a caller
// of this package and a reader of the process's exit code say the same thing
// about the same ending. The fifth rung — needed an answer — has no home here
// yet: nothing in the loop asks a question.
type Outcome string

const (
	OutcomeDone       Outcome = "done"
	OutcomeCannotRun  Outcome = "could not be run at all"
	OutcomeIncomplete Outcome = "ran and did not finish"
	OutcomeLimit      Outcome = "a limit you set stopped it"
)

// passInterval is how long the loop idles between passes when no worker has
// returned. It exists for work the workers did through the store: a worker
// that added tasks with its own store writes makes them launchable without
// anything in this process having to come back first.
const passInterval = 300 * time.Millisecond

// defaultStaleAfter is how long a claim may go untouched before a pass takes
// it over, when the caller named no window on Limits. Five minutes is long
// enough that a slow step never looks like a dead process and short enough
// that a run whose owner died is picked up within a working session.
const defaultStaleAfter = 5 * time.Minute

// maxWakes is how many times one composite task's worker is launched again to
// integrate its children's landings. A parent that keeps spawning more children
// every time it is woken would otherwise wake forever; at the cap the run stops
// waking it and closes it the way the store's own auto-completion would have,
// with whatever result it last reported. Four is the ordinary depth of a
// re-plan over a fold — split, integrate, a gap, a second fold — with one spare.
const maxWakes = 4

// workerReturn is one finished worker, on its way from its goroutine back to
// the loop.
type workerReturn struct {
	task   plandb.Task
	report Report
	err    error
}

// Supervisor is the launch loop over one plan store. It claims ready leaves
// as the task's own agent — the name its finish command answers to — and
// records the process that holds the claim beside it, so a run is owned per
// process: a claim a dead process left behind is released and taken over by
// the next one. It starts a worker for each claim under a slot bound and
// writes every worker's ending back into the store. The root task is the
// supervisor's own: no worker completes it, and it is completed once every
// other task in the store is terminal.
type Supervisor struct {
	// Owner is the process that holds this run's claims: "<hostname>:<pid>",
	// so each claim names the process answerable for it and a claim nobody
	// touches reads stale. It is set from the running process when the
	// supervisor is built; a test sets it to stand in for a second process
	// sharing one store.
	Owner string

	store     *plandb.Store
	workspace string
	slots     int
	limits    Limits
	factory   WorkerFactory

	// staleAfter is how long a claim may go untouched before this pass takes
	// it over, resolved from Limits (or its default) at the top of Run, so
	// every pass reads the same window.
	staleAfter time.Duration

	// finished carries worker endings back to the loop, buffered at the slot
	// bound plus the root's seat: a worker whose run has already ended deposits
	// its return and exits rather than blocking on a reader that is gone.
	// inFlight is the count of workers running, and the counters beside it are
	// the run's memory — nodes counts every worker launched, steps sums what
	// those workers reported; all of them belong to the loop goroutine and are
	// touched by no one else. cancels maps a running task to the context
	// cancel that ends its worker, so a pass can end one worker's context
	// without touching the rest.
	finished chan workerReturn
	// workers counts the goroutines launch has started and not yet seen end.
	// It is the run's one promise to its caller — NO WORKER OUTLIVES ITS RUN —
	// and drain is its only reader: every road out of Run waits on it before
	// answering, so the store, the working copy and the process are the
	// caller's alone the moment the run is over.
	workers        sync.WaitGroup
	inFlight       int
	spent          float64
	nodes          int
	steps          int
	rootResult     string
	rootFailed     bool
	limitHit       bool
	dispatchedRoot bool
	cancels        map[string]context.CancelFunc
	// wakes counts how many times each composite task's worker has been launched
	// again to integrate a landing of its children, and reported names the child
	// ids a task's worker has already been woken with, so a wake fires once per
	// landing generation (and once more only when a wake adds children).
	// lastReport keeps the report of a parent whose ending is being held for a
	// wake: absorb writes nothing into the store for such a return, so without
	// it the parent's own words would exist nowhere and the ending the run
	// writes at the cap would carry an empty result. All three belong to the
	// loop goroutine like the counters beside them.
	wakes      map[string]int
	reported   map[string]map[string]bool
	lastReport map[string]string
	// checkOf maps a check task's id to the leaf it reads, for the review
	// round: a check whose result does not hold leaves its sentence as a note
	// on the leaf it names here. It is written when the check is added and read
	// when the check's own ending lands, both on the loop goroutine, so it
	// belongs to the loop's memory like the counters beside it.
	checkOf map[string]string
}

// NewSupervisor builds a run over store. The workspace is the run's own
// working copy, carried for the worker seat and the landing that follow this
// loop; slots bounds how many workers run at once (a slot count below one
// runs one at a time, which keeps a misconfigured run alive rather than
// dead); limits bound the run's cost and, per task, its steps.
func NewSupervisor(store *plandb.Store, workspace string, slots int, limits Limits, factory WorkerFactory) *Supervisor {
	if slots < 1 {
		slots = 1
	}
	return &Supervisor{
		Owner:     ownerName(),
		store:     store,
		workspace: workspace,
		slots:     slots,
		limits:    limits,
		factory:   factory,
		finished:  make(chan workerReturn, slots+1),
		cancels:   make(map[string]context.CancelFunc),
		checkOf:   make(map[string]string),
	}
}

// Run drives the run to one of the outcome words and returns it. Each pass
// reads the ready set, claims every ready leaf whose ancestors are not
// cancelled, and starts one worker per claim, bounded by slots. A pass runs
// when a worker returns and on the idle timer, so a task added through the
// store while the loop waits is launched without waiting for anything else.
//
// NO WORKER OUTLIVES ITS RUN. The word this answers is the caller's licence to
// close the store, land the working copy or drop the process, so every road
// out of the loop ends the workers still in flight through their contexts and
// waits for each of them before answering — [Supervisor.drain], on all four
// roads. The road that needs it most is the tree's completion: the run's own
// row is written the moment its last descendant lands, and a coordinator still
// in its turn is a worker the completed tree left behind, with a spend row and
// a trajectory ending still to write.
//
// The run is over when the root task is terminal, when the cost limit has
// been crossed and every worker already launched has come home, or when the
// context ends. A run that ends on anything but the root's own completion
// leaves the store as it stands — an open root is a run a later pass can
// pick up — which is why the limit and the context end no store writes of
// their own.
func (s *Supervisor) Run(ctx context.Context) Outcome {
	if ctx.Err() != nil {
		return OutcomeCannotRun
	}
	rootID := s.store.RootID()
	if s.store.Task(rootID) == nil {
		return OutcomeCannotRun
	}
	s.spent = 0
	s.nodes = 0
	s.steps = 0
	s.rootResult = ""
	s.rootFailed = false
	s.limitHit = false
	s.dispatchedRoot = false
	s.inFlight = 0
	s.cancels = make(map[string]context.CancelFunc)
	s.checkOf = make(map[string]string)
	s.wakes = make(map[string]int)
	s.reported = make(map[string]map[string]bool)
	s.lastReport = make(map[string]string)
	s.staleAfter = s.limits.StaleAfter
	if s.staleAfter <= 0 {
		s.staleAfter = defaultStaleAfter
	}
	// TAKE-OVER BEFORE THE FIRST PASS: a claim a dead process left behind is
	// released here, so the ready set the first pass reads can offer it again
	// with no pass of waiting.
	s.releaseStale()

	timer := time.NewTimer(passInterval)
	defer timer.Stop()
	for {
		if outcome := s.pass(ctx, rootID); outcome != "" {
			s.drain()
			return outcome
		}
		select {
		case ret := <-s.finished:
			s.inFlight--
			s.absorb(ret)
		case <-timer.C:
			timer.Reset(passInterval)
		case <-ctx.Done():
			// The caller's wall: workers still out there were handed this
			// context and end with it. Their endings are absorbed so their
			// writes land before the word is returned, and the drain below is
			// what makes the word true — the wall is the one road that reads
			// the endings rather than dropping them, because the run it stops
			// is one a later pass picks up from the store.
			for s.inFlight > 0 {
				ret := <-s.finished
				s.inFlight--
				s.absorb(ret)
			}
			s.drain()
			return OutcomeIncomplete
		}
	}
}

// pass takes one look at the store, launches what is ready, and answers the
// run's outcome word when the run is over — an empty word means it is not.
func (s *Supervisor) pass(ctx context.Context, rootID string) Outcome {
	root := s.store.Task(rootID)
	if root == nil {
		return OutcomeCannotRun
	}
	s.endCancelledWorkers()
	// TAKE-OVER, EVERY PASS: refresh this process's own claims so they never
	// read stale, then hand back any claim whose process has stopped touching
	// it, so the ready read below offers it again. Our own claims are fresh
	// from the touch; a live claim held by another process is fresh from that
	// process's own touch and is left alone.
	s.touchClaims()
	s.releaseStale()
	if terminalStatus(root.Status) {
		// A ROOT WORKER CAN WRITE DONE BEFORE ITS GOROUTINE RETURNS. Its return
		// is what seats the review, so do not accept the stored ending first.
		if root.Status != plandb.StatusDone || s.inFlight == 0 {
			return s.outcomeForRoot(root.Status)
		}
	}

	if s.inFlight == 0 && (s.rootFailed || s.limitHit) {
		// Nothing of ours is running and the run cannot complete itself: the
		// root's own worker failed, or the cost counter has reached its limit.
		// The word says which; the store keeps whatever the run reached.
		if s.limitHit {
			return OutcomeLimit
		}
		return OutcomeIncomplete
	}
	if s.inFlight < s.slots && !s.dispatchedRoot {
		// The root is the first worker of the run. It is claimed by the runtime
		// in the store, so it needs no claim here — only a seat.
		s.dispatchedRoot = true
		s.launch(ctx, *root, "")
	}
	if !s.limitHit {
		for _, ready := range s.store.ReadySet().Runnable {
			if s.inFlight >= s.slots {
				break
			}
			task := *ready
			if task.ID == rootID || s.hasCancelledAncestor(task) {
				continue
			}
			// THE AGENT NAME IS THE TASK'S OWN ID, the store's naming trick:
			// the ending written for this task can only come from the worker
			// it was handed to, because ownership is checked against this
			// exact name. The process that holds the claim is named beside it,
			// so a claim this run abandons reads stale for another process.
			if _, err := s.store.Claim(task.ID, task.ID, s.Owner); err != nil {
				// Not ours anymore — another writer claimed, cancelled or
				// finished it between the read and the claim. The next pass
				// sees the store as it now stands.
				continue
			}
			s.launch(ctx, task, "")
		}
	}
	// WAKING A PARKED TASK COMES BEFORE WAKING A PARENT, so a task this sweep
	// starts is registered as running and the composite sweep below leaves it
	// alone.
	s.launchWaits(ctx, rootID)
	// WAKING A PARENT IS THE PASS'S LAST LAUNCH: with the ready leaves out of
	// the way, every composite task whose children have landed and whose worker
	// has not been woken with them is started again to integrate them.
	s.launchWakes(ctx, rootID)
	return ""
}

// launch starts one worker in its own goroutine. The task's context is the
// run's with a cancel of its own, recorded so a pass can end this one worker
// — a task the store has cancelled — without ending the run; it carries the
// step cap and, when wake is not empty, the resume clause a woken parent opens
// with. The goroutine reports back on the channel and ends.
func (s *Supervisor) launch(ctx context.Context, task plandb.Task, wake string) {
	worker := s.factory(task)
	taskCtx, cancel := context.WithCancel(ctx)
	if strings.TrimSpace(wake) != "" {
		taskCtx = WithWakeClause(taskCtx, wake)
	}
	s.cancels[task.ID] = cancel
	s.inFlight++
	s.nodes++
	// THE GOROUTINE IS COUNTED BEFORE IT STARTS, so a drain that begins the
	// moment after this launch cannot miss it and answer before it ends.
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		report, err := Report{}, error(nil)
		if worker == nil {
			err = errors.New("no worker for task " + task.ID)
		} else {
			report, err = worker.Run(WithStepsPerTask(taskCtx, s.limits.StepsPerTask), task)
		}
		s.finished <- workerReturn{task: task, report: report, err: err}
	}()
}

// drain ends every worker still in flight and waits for each of them to
// return: NO WORKER OUTLIVES ITS RUN. The run's word is the caller's licence
// to close its store and take its working copy away, and a worker goroutine
// still running at that moment writes its spend row, its trajectory ending or
// even a completion through a handle that is gone — the nil-database panic a
// run that answered while a worker was alive died on. Every road out of Run
// calls this, so the moment the outcome is answered is a moment at which the
// run holds nothing alive.
//
// THE CONTEXT GOES FIRST. A worker in the middle of a step ends at its next
// one, which is what bounds the wait however long the step was going to take;
// a worker whose context a pass already ended — a cancelled task, the caller's
// wall — is in the map no more and has only to be waited for. The endings are
// dropped rather than absorbed: the run is over, the store already carries the
// ending that stands, and the return of a worker the run outlived has no
// reader — the same reading absorb makes of a worker whose task the store
// cancelled under it.
func (s *Supervisor) drain() {
	for id, cancel := range s.cancels {
		cancel()
		delete(s.cancels, id)
	}
	s.workers.Wait()
}

// absorb writes one worker's ending into the store and keeps the run's
// counters true. A worker that came home well is done with its result; one
// that came home with an error fails with that error as the reason. Either
// way the task ends terminal, because a task whose worker has returned and
// that stays open would stall the whole run: nothing else can make it
// terminal, and the run would wait on it forever.
func (s *Supervisor) absorb(ret workerReturn) {
	// A RETURN FOR A TASK THE STORE ALREADY ENDED is written as nothing. The
	// ending the store carries — a cancellation that landed while the worker
	// ran, which the same write cleared the claim and cascaded down — is the
	// one that stands, and no Done or Fail of this run may speak over it. The
	// report is dropped whole, spend included: a worker stopped mid-flight
	// hands back no account this run counts. The completion check below still
	// runs, because the cancellation may be the write that finished the tree.
	delete(s.cancels, ret.task.ID)
	if ret.report.Waiting {
		// A PARKED RETURN IS NOT AN ENDING. The worker called `plandb wait`; the
		// store already released the claim and flagged the task waiting, and the
		// task stays open until the run wakes it ([launchWaits]). Nothing is
		// written over the store's own park, and the spend the worker made is
		// still the run's.
		s.spent += ret.report.USD
		s.steps += ret.report.Steps
		if s.limits.CostUSD > 0 && s.spent >= s.limits.CostUSD {
			s.limitHit = true
		}
		s.completeTree()
		return
	}
	if task := s.store.Task(ret.task.ID); task == nil || task.Status == plandb.StatusCancelled {
		s.completeTree()
		return
	}
	// A WORKER THAT FINISHED ITS OWN TASK IN THE STORE needs no ending written
	// for it: the store carries the completion and its result. The run still
	// counts what the worker spent, and the root's own report is still the
	// run's result.
	endedByStore := ret.err == nil && s.store.Task(ret.task.ID).Status == plandb.StatusDone
	s.spent += ret.report.USD
	s.steps += ret.report.Steps
	if s.limits.CostUSD > 0 && s.spent >= s.limits.CostUSD {
		s.limitHit = true
	}
	if ret.task.ID == s.store.RootID() {
		// The harness owns the root, and the store refuses worker writes to it.
		// Its worker's report is kept for the completion; its error is what
		// stops the run from ever completing, not a row.
		if ret.err != nil {
			s.rootFailed = true
		} else {
			s.rootResult = ret.report.Result
			// THE CHILDLESS ROOT IS A LEAF, and it is checked like any other. If
			// its worker already wrote the ending, the store preserves that result
			// and moves the root back to waiting on the check; CompleteRoot writes
			// the final word after the check lands. A root with children answers
			// plan and is not checked ([addReviewCheck] reads the shape fresh).
			s.addReviewCheck(ret.task, ret.report.Result)
		}
	} else {
		switch {
		case endedByStore:
			// THE WORKER WROTE ITS OWN ENDING, so the store's word stands and
			// nothing here speaks over it. The run counts the spend above and the
			// tree is checked below. THE REVIEW ROUND STILL READS IT: a worker's
			// own `plandb done` is the ending every real leaf writes, so the check
			// and a check's finding both run here, or the round would never fire on
			// real work.
			delete(s.lastReport, ret.task.ID)
			s.reviewLanded(ret.task, ret.report.Result)
		case ret.err == nil && s.waitsForWake(ret.task.ID):
			// A PARENT'S RESULT IS WRITTEN AFTER ITS CHILDREN LAND, NOT BEFORE.
			// The worker dispatched and ended its turn to wait on what it handed
			// out; this return is not an ending, so nothing is written and the
			// task stays with the run — held by this process's own claim — to be
			// woken once every child it dispatched has landed.
			//
			// THE WAIT DOES NOT TURN ON THE CHILDREN STILL BEING OPEN. Children
			// go out into slots and land when they land, so a set of short
			// children is often terminal before their own parent's turn ends;
			// writing this report would make the parent's first word its result
			// with nobody having integrated anything, which is the one bug this
			// law exists for. A landing the parent has not been woken with is
			// not in its report yet, so the ending waits either way.
			s.lastReport[ret.task.ID] = ret.report.Result
		case ret.err == nil:
			// THE REVIEW ROUND: a work-seat leaf that lands done is checked
			// once. The check is added BEFORE the completion is written, so the
			// leaf's parent cannot auto-complete past it in the same write and
			// the run's root waits on the open check like on any other child.
			delete(s.lastReport, ret.task.ID)
			s.addReviewCheck(ret.task, ret.report.Result)
			_, err := s.store.Done(ret.task.ID, ret.task.ID, ret.report.Result, nil, nil)
			if err == nil {
				s.recordCheckFinding(ret.task, ret.report.Result)
				break
			}
			ret.err = fmt.Errorf("write the completion: %w", err)
			fallthrough
		default:
			// A Done that could not be written is retried as a Fail: the worker
			// came home and its task must end. A refusal here means the store
			// already ended the task — a cancel that arrived while the worker
			// ran, most likely — and the store's word is the one that stands.
			_, err := s.store.Fail(ret.task.ID, ret.task.ID, ret.err.Error())
			if task := s.store.Task(ret.task.ID); err != nil && (task == nil || !terminalStatus(task.Status)) {
				// The store could not record the ending at all. Nothing in this
				// process can fix a write that failed twice; the task stays open
				// and the run stays with it, which is the honest reading.
				return
			}
		}
	}
	s.completeTree()
}

// addReviewCheck spawns the review round's one check task for a work-seat leaf
// that is about to land done. It is a no-op unless the run's limits turn the
// review round on, unless the leaf is a check itself — a check is never itself
// checked — and unless the leaf is a leaf at all: a task with children answers
// plan and its end is the store's own bookkeeping, which nothing reads.
//
// THE CHECK IS ADDED BEFORE THE COMPLETION IS WRITTEN. A parent whose children
// are all terminal auto-completes in the same write the completion lands in, so
// a check added afterwards would find its parent already done and could not be
// put under it. Added first, the parent counts the open check among its children
// and the run's root waits on it like on any other child (the store's own
// CanFinish says so, and completeTree reads the same shape).
//
// THE CHECK CARRIES THE LEAF'S OWN WORDS: the acceptance its description holds
// and the result it just reported, so the check worker reads what to prove the
// claim against and what the claim was. It carries no dependency at all, so it
// is ready the moment it exists. Its id is minted here and mapped back to the
// leaf, because that is what a "does not hold" finding names when it lands.
func (s *Supervisor) addReviewCheck(leaf plandb.Task, result string) {
	if !s.limits.ReviewRound || leaf.Role == plandb.RoleCheck {
		return
	}
	// A TASK WITH CHILDREN ANSWERS PLAN: it is a coordinator, its end is the
	// store's own bookkeeping, and no check reads it. The shape is read FRESH,
	// because a leaf that split during its turn is a coordinator by the time it
	// lands, while the task handed here was cloned before the split.
	if len(childIDs(s.store.Tasks(), leaf.ID)) > 0 {
		return
	}
	id := s.store.NextID()
	spec := plandb.TaskSpec{
		ID:          id,
		Title:       checkTitlePrefix + leaf.Title,
		Description: descriptionWithChecks("Acceptance: "+leaf.Description+"\n\nResult: "+result, leaf.Checks),
		Checks:      append([]string(nil), leaf.Checks...),
		ParentID:    leaf.ParentID,
		Role:        plandb.RoleCheck,
	}
	var err error
	if leaf.ID == s.store.RootID() && s.store.Task(leaf.ID).Status == plandb.StatusDone {
		_, err = s.store.AddRootCheck(spec)
	} else {
		_, err = s.store.AddMany([]plandb.TaskSpec{spec})
	}
	if err != nil {
		// A check the store would not admit does not replace the task's earned
		// ending. The terminal-root case has its explicit store seam above; all
		// other refusals keep the existing outcome and result unchanged.
		return
	}
	s.checkOf[id] = leaf.ID
}

// reviewLanded is the review round's own half for a task whose ending the store
// already carries — the `plandb done` a real worker writes on itself. The leaf
// is checked exactly as it would be had this run written the ending, and a
// check's finding is recorded exactly as it would be, so the round fires on real
// work and not only on the endings the supervisor writes itself.
func (s *Supervisor) reviewLanded(task plandb.Task, result string) {
	s.addReviewCheck(task, result)
	s.recordCheckFinding(task, result)
}

// recordCheckFinding turns a check's "does not hold" into work. A check's result
// begins "holds:" or "does not hold:" and closes with one sentence; the second is
// the finding, so it is left on the checked leaf in the check's own voice —
// author "check" — AND it is made into a fix task under the checked leaf's
// parent, the coordinator that owns the work.
//
// A FINDING IS WORK, NOT A REMARK. The note alone left the coordinator to notice
// a sentence nobody read; the fix task is the repair, and the run is not over
// until it lands. The fix carries the leaf's acceptance, the finding and the
// leaf's own result, and depends on nothing, so it is ready at once.
//
// ONE ROUND ONLY: a finding on a fix task is a note and no second fix task, so a
// run cannot loop.
func (s *Supervisor) recordCheckFinding(check plandb.Task, result string) {
	if check.Role != plandb.RoleCheck {
		return
	}
	const doesNotHold = "does not hold:"
	text := strings.TrimSpace(result)
	if !strings.HasPrefix(text, doesNotHold) {
		return
	}
	leaf := s.checkOf[check.ID]
	sentence := strings.TrimSpace(strings.TrimPrefix(text, doesNotHold))
	if leaf == "" || sentence == "" {
		return
	}
	checked := s.store.Task(leaf)
	if checked == nil {
		return
	}
	_, _ = s.store.AddNote(leaf, "check", sentence)
	if strings.HasPrefix(checked.Title, fixTitlePrefix) {
		return
	}
	id := s.store.NextID()
	_, _ = s.store.AddMany([]plandb.TaskSpec{{
		ID:          id,
		Title:       fixTitlePrefix + checked.Title,
		Description: fixDescription(checked, sentence),
		Checks:      append([]string(nil), checked.Checks...),
		ParentID:    checked.ParentID,
	}})
}

// The two prefixes the review round mints: the check it adds under a leaf, and
// the fix it adds under a leaf's parent when the check does not hold. They are
// named once so the title a check carries and the title a fix is recognised by
// cannot drift apart.
const (
	checkTitlePrefix = "check: "
	fixTitlePrefix   = "fix: "
)

// fixDescription is the work order the review round writes for a fix task: the
// acceptance the checked leaf was held to, the finding's one sentence, and the
// leaf's own result — everything a fresh worker needs to make the unmet
// requirement hold.
func fixDescription(leaf *plandb.Task, finding string) string {
	base := "Acceptance: " + leaf.Description + "\n\nFinding: " + finding + "\n\nResult: " + leaf.Result
	return descriptionWithChecks(base, leaf.Checks)
}

// descriptionWithChecks keeps the historical description byte-for-byte when a
// task declares no checks. Declared checks follow the acceptance and result as
// worker-readable lines while the same commands remain structured on the node.
func descriptionWithChecks(base string, checks []string) string {
	if len(checks) == 0 {
		return base
	}
	return base + "\n\nChecks:\n" + strings.Join(checks, "\n")
}

// completeTree finishes the run once every task but the root has ended: the
// one write the loop makes on a tree that finished under it, whole or not.
// CompleteRoot marks the root done when the whole tree did and failed when
// any part of it did not, with the root's own result the one its worker
// reported. A root whose own worker failed stays open — the run cannot
// complete itself, and a later pass may pick it up.
func (s *Supervisor) completeTree() {
	if s.rootFailed {
		return
	}
	// THE ROOT IS NOT COMPLETED WHILE A WAKE IS STILL OWED IT: its children
	// have landed but its own worker has not yet been run again to integrate
	// them, or that waking worker is still out. The result the root reports
	// after that wake is the one the run carries, so closing it now would keep
	// the first turn's word as the run's answer — the bug this law exists for.
	if s.rootAwaitingWake() {
		return
	}
	if s.treeTerminal() {
		_ = s.store.CompleteRoot(s.rootResult)
	}
}

// rootAwaitingWake answers whether the root still owes a wake: a landing of its
// children it has not been given, or a waking worker of its own still running.
func (s *Supervisor) rootAwaitingWake() bool {
	rootID := s.store.RootID()
	if _, running := s.cancels[rootID]; running {
		return true
	}
	tasks := s.store.Tasks()
	for _, task := range tasks {
		if task.ID == rootID {
			return needsWake(tasks, task, s.cancels, s.wakes, s.reported)
		}
	}
	return false
}

// treeTerminal answers whether every task but the root has ended — the one
// condition under which the run may complete its root. The root itself is
// read separately; its completion is never a worker's to write.
func (s *Supervisor) treeTerminal() bool {
	rootID := s.store.RootID()
	for _, task := range s.store.Tasks() {
		if task.ID != rootID && !terminalStatus(task.Status) {
			return false
		}
	}
	return true
}

// launchWakes starts the worker of every composite task whose children have all
// landed and whose worker has not yet been woken with them — THE WOKEN-PARENT
// LAW. A parent's result is written after its children land, not before, so a
// parent that dispatched and waited is started again in its own seat, with a
// resume clause naming what every child did, to integrate, verify, add more
// work if something is missing, and report. A woken parent that adds children
// is woken again when they land; the run wakes a task at most maxWakes times,
// because a parent that keeps spawning would otherwise never stop. The root is
// no exception: it is woken like any parent, and the report it gives after the
// wake is the run's Result.
//
// WAKES COUNT AS WORKERS: each one comes from the same factory, takes a slot and
// a node, and its spend is counted like any other seat's.
func (s *Supervisor) launchWakes(ctx context.Context, rootID string) {
	if s.limitHit {
		return
	}
	// THE PLAN IS READ ONCE for the whole sweep: every helper below answers out
	// of this one snapshot rather than re-reading the store per task.
	tasks := s.store.Tasks()
	for _, taskp := range tasks {
		task := *taskp
		if !task.Composite || terminalStatus(task.Status) {
			continue
		}
		if _, running := s.cancels[task.ID]; running {
			continue
		}
		if !childrenAllTerminal(tasks, task.ID) {
			continue
		}
		if s.wakes[task.ID] >= maxWakes {
			// THE CAP. The run stops waking this parent and ends it the way the
			// store's own auto-completion would have: the root is closed by
			// completeTree with the last report it gave; every other parent is
			// closed here, because the store leaves a held composite open.
			if task.ID != rootID {
				s.closeAtCap(task)
			}
			continue
		}
		if !needsWake(tasks, &task, s.cancels, s.wakes, s.reported) {
			continue
		}
		if s.inFlight >= s.slots {
			return
		}
		// THE WOKEN-PARENT LAW: every non-root launch holds the task under its
		// own agent id, so wait and done work identically on a first turn and a wake.
		//
		// A CLAIM THE STORE REFUSES DOES NOT CANCEL THE WAKE. A parent that is
		// never woken holds its family open until the wall, which is worse than
		// one launched as it was before this law: it can still answer in words,
		// and the run closes it on that.
		if task.ID != rootID && task.ClaimedBy == "" {
			if claimed, err := s.store.ClaimWake(task.ID, task.ID, s.Owner); err == nil {
				task = *claimed
			}
		}
		s.wakes[task.ID]++
		s.reported[task.ID] = childIDs(tasks, task.ID)
		s.launch(ctx, task, wakeClause(tasks, task))
	}
}

// launchWaits starts the worker of a task that parked itself with `plandb
// wait` once ITS WAIT IS OVER — the one road that brings it back, because the
// park flag ([Task.Waiting]) keeps the task off the ordinary ready frontier.
//
// A PARKED TASK WAKES ONCE, WHEN ITS WAIT IS OVER, and [waitMoved] is the one
// definition of what that means: nothing it waited on is open any more (every
// child terminal, every dependency done), or a child or dependency ended failed
// or cancelled since it parked so the parent can re-plan at once. A child being
// claimed, started or noted while a sibling still runs is not a reason, which is
// what keying the wake on [Store.Changed] alone cost: a launch per move, each a
// model call on the most expensive seat to read a plan that had not changed.
//
// THE PARK FLAG IS CLEARED IN THE STORE FIRST, so the task comes back to life
// through the store's own write and a launch this pass cannot make — no slot,
// another writer took it — is made on a later one.
func (s *Supervisor) launchWaits(ctx context.Context, rootID string) {
	if s.limitHit {
		return
	}
	tasks := s.store.Tasks()
	for _, taskp := range tasks {
		task := *taskp
		if !task.Waiting {
			continue
		}
		if _, running := s.cancels[task.ID]; running {
			continue
		}
		moved := waitMoved(tasks, &task, s.store)
		if len(moved) == 0 {
			continue
		}
		if s.inFlight >= s.slots {
			return
		}
		// A READY LEAF IS CLAIMED, the same claim every dispatch makes, so its
		// finish command still answers the ownership check. A composite is not
		// claimable and needs none — the root among them, which the run holds
		// without claiming like any other coordinator.
		if !task.Composite {
			if _, err := s.store.Claim(task.ID, task.ID, s.Owner); err != nil {
				continue
			}
		}
		woken, err := s.store.Wake(task.ID)
		if err != nil {
			continue
		}
		// The park flag is already cleared, so a refused claim still launches:
		// see [Supervisor.launchWakes] for why a wake is never dropped on it.
		if task.Composite && task.ID != rootID {
			if claimed, err := s.store.ClaimWake(task.ID, task.ID, s.Owner); err == nil {
				woken = claimed
			}
		}
		if task.Composite {
			// A WAIT RELAUNCH OF A COMPOSITE IS A WAKE AND IS COUNTED AS ONE:
			// it fires on the same fact [launchWakes] fires on — the children
			// landing — so it spends the same wake budget, and the cap and the
			// held-parent bookkeeping cannot disagree about how many times the
			// coordinator has been run. WHAT IT RECORDS AS REPORTED IS THE CHILD
			// SUBSET IT NAMES, not every child: a wake on one child's failure
			// leaves the siblings still running unreported, so the parent is
			// woken again when they land, while a wake when the whole set has
			// settled records the whole set.
			s.wakes[task.ID]++
			seen := s.reported[task.ID]
			if seen == nil {
				seen = map[string]bool{}
				s.reported[task.ID] = seen
			}
			for _, movedTask := range moved {
				if movedTask.ParentID == task.ID {
					seen[movedTask.ID] = true
				}
			}
		}
		s.launch(ctx, *woken, waitClause(moved))
	}
}

// waitMoved answers what a parked task's wake should carry, and nil when it
// should stay parked. THE LAW IS THAT A PARKED TASK WAKES ONCE, WHEN ITS WAIT
// IS OVER. "Over" is one of two facts and nothing else:
//
//   - nothing it waited on is open any more: every child of it is terminal and
//     every dependency is done ([Store.OpenWaits] empty). The clause then names
//     the whole settled set — each one's title, status and result — which is
//     what the parent integrates; or
//   - a child or a dependency ended failed or cancelled since it parked, so the
//     parent can re-plan at once rather than wait for the siblings that are
//     still running. The clause names that one.
//
// A child merely being claimed, started, noted or otherwise touched while a
// sibling still runs is NOT a reason, and neither is a dependency moving
// without landing: THE PARK IS A WAIT ON SOMETHING, and it is over when that
// something has finished, not when it has stirred.
func waitMoved(tasks []*plandb.Task, task *plandb.Task, store *plandb.Store) []*plandb.Task {
	waited := map[string]bool{}
	for _, dep := range task.Dependencies {
		waited[dep.TaskID] = true
	}
	for _, candidate := range tasks {
		if candidate.ParentID == task.ID {
			waited[candidate.ID] = true
		}
	}
	if len(waited) == 0 {
		return nil
	}
	all := map[string]*plandb.Task{}
	for _, candidate := range tasks {
		all[candidate.ID] = candidate
	}
	// THE WAIT IS OVER WHEN NOTHING IT WAITED ON IS OPEN. The clause names the
	// whole set, in the store's own admission order, so the parent reads every
	// child and dependency that settled and not only the last one to land.
	if len(store.OpenWaits(task.ID)) == 0 {
		var moved []*plandb.Task
		for _, candidate := range tasks {
			if waited[candidate.ID] {
				moved = append(moved, candidate)
			}
		}
		return moved
	}
	// OTHERWISE ONLY A FAILURE EARNS THE EARLY RETURN: a child or dependency
	// that ended failed or cancelled since the park, and has not been reported
	// to the worker yet.
	var moved []*plandb.Task
	for _, id := range store.Changed(task.WaitedAt) {
		candidate := all[id]
		if candidate == nil || !waited[id] {
			continue
		}
		if !candidate.UpdatedAt.After(task.WaitedAt) {
			continue
		}
		if candidate.Status == plandb.StatusFailed || candidate.Status == plandb.StatusCancelled {
			moved = append(moved, candidate)
		}
	}
	return moved
}

// waitClause is the sentence a woken parked task opens on: what it waited on,
// and what each of those has done. It is the wait's other half — a worker
// parked because it could not go on, and this names the finished work that lets
// it. THE WAIT'S LAW IS A PARKED TASK WAKES ONCE, WHEN ITS WAIT IS OVER
// ([waitMoved]); the clause carries every task that settled, not only the last
// one to land, so a parent woken on the last of three children reads all three.
func waitClause(moved []*plandb.Task) string {
	var b strings.Builder
	b.WriteString("what you waited on has finished, so your parked task is running again: ")
	for i, task := range moved {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s %q is now %s", task.ID, task.Title, task.Status)
		if result := strings.TrimSpace(task.Result); result != "" {
			b.WriteString(": ")
			b.WriteString(resultOrNoResult(result))
		}
	}
	return b.String()
}

// needsWake answers whether a composite task owes a wake: its children have all
// landed, at least one of them has not been reported to its worker yet, no worker
// of its own is running, and it is under the wake cap. A task nobody has worked
// is not held and the store closes it without a wake, so it never gets here.
func needsWake(tasks []*plandb.Task, task *plandb.Task, cancels map[string]context.CancelFunc, wakes map[string]int, reported map[string]map[string]bool) bool {
	if task == nil || !task.Composite || terminalStatus(task.Status) {
		return false
	}
	if _, running := cancels[task.ID]; running {
		return false
	}
	if wakes[task.ID] >= maxWakes {
		return false
	}
	seen := reported[task.ID]
	children, unreported := 0, false
	for _, child := range tasks {
		if child.ParentID != task.ID {
			continue
		}
		children++
		if !terminalStatus(child.Status) {
			return false
		}
		// A CHECK'S LANDING WAKES NOBODY. A check reviews work its parent has
		// already been told about; its finding reaches the parent as a `fix:`
		// task, which is work and does wake it when it lands, or as a note.
		// Waking a parent for the check itself was the turn that left a root
		// finished alone standing `ready` forever: reopened for its one check,
		// then owed a wake with nothing to integrate (do_engine_test.go).
		if child.Role == plandb.RoleCheck {
			continue
		}
		if !seen[child.ID] {
			unreported = true
		}
	}
	return children > 0 && unreported
}

// childIDs is the set of a task's direct children, the generation a wake reports.
func childIDs(tasks []*plandb.Task, id string) map[string]bool {
	set := map[string]bool{}
	for _, child := range tasks {
		if child.ParentID == id {
			set[child.ID] = true
		}
	}
	return set
}

// childrenAllTerminal answers whether a task has children and every one of them
// has ended. A composite with no children is not one whose landings can wake it.
func childrenAllTerminal(tasks []*plandb.Task, id string) bool {
	has, all := false, true
	for _, child := range tasks {
		if child.ParentID != id {
			continue
		}
		has = true
		if !terminalStatus(child.Status) {
			all = false
			break
		}
	}
	return has && all
}

// waitsForWake answers whether a worker's return is a WAIT rather than an
// ending: its task is a composite the store has not ended, and either a child of
// it has not landed yet, or a child has landed that its worker has not been woken
// with. The second half is the one a race needs: children are dispatched into
// slots and land whenever they land, so asking only whether any of them is still
// open lets a set of short children finish before their parent's own turn ends —
// and the parent's first word becomes its result with nobody having integrated
// anything, which is the bug the woken-parent law exists for. The root needs no
// such help at its own ending, because completeTree asks rootAwaitingWake before
// it writes; every other parent is written here, so here is where the law has to
// hold.
//
// THE ANSWER IS A SUBSET OF WHAT launchWakes FIRES ON, which is what keeps the
// run from holding a task open that no wake will close: a task the store already
// ended is not waiting, a task whose every landing has been reported is writing
// its ending now, and a task at the cap is closed by closeAtCap on the pass its
// last child lands in.
func (s *Supervisor) waitsForWake(id string) bool {
	// ONE READ OF THE PLAN, the way the wake sweep reads it: the task and its
	// children come out of the same snapshot, so the two halves of the question
	// cannot be answered about two different moments.
	tasks := s.store.Tasks()
	var task *plandb.Task
	for _, candidate := range tasks {
		if candidate.ID == id {
			task = candidate
			break
		}
	}
	if task == nil || !task.Composite || terminalStatus(task.Status) {
		return false
	}
	for _, child := range tasks {
		if child.ParentID == id && !terminalStatus(child.Status) {
			return true
		}
	}
	return needsWake(tasks, task, s.cancels, s.wakes, s.reported)
}

// closeAtCap ends a composite the run has woken its fill of times, the way the
// store's own auto-completion would have written it: done when every child
// finished, failed when one did not. It is reached only for a parent this run is
// holding open, since the store leaves a held composite alone.
//
// A COMPOSITE WHOSE CHILDREN ALL LANDED DONE IS NEVER CLOSED FAILED. The truth
// is read fresh from the store — not from the wake sweep's snapshot, which a
// child may have landed past — so the cap can only close the composite the way
// the store's own auto-completion would: done, with the parent's own last report
// when the run kept one (a held parent carries no result in the store, because
// every return of its worker was a wait that wrote nothing), and failed only
// for a child that did not finish, named in the reason.
func (s *Supervisor) closeAtCap(task plandb.Task) {
	fresh := s.store.Tasks()
	allDone, offender := true, (*plandb.Task)(nil)
	for _, child := range fresh {
		if child.ParentID != task.ID {
			continue
		}
		if child.Status != plandb.StatusDone {
			allDone = false
			if offender == nil {
				offender = child
			}
		}
	}
	if allDone {
		result := task.Result
		if kept := s.lastReport[task.ID]; strings.TrimSpace(kept) != "" {
			result = kept
		}
		_, _ = s.store.Done(task.ID, task.ID, result, nil, nil)
		return
	}
	reason := "the wake cap was reached with a child that did not finish"
	if offender != nil {
		reason = fmt.Sprintf("the wake cap was reached with child %q %s", offender.ID, offender.Status)
	}
	_, _ = s.store.Fail(task.ID, task.ID, reason)
}

// wakeClause is the resume clause a woken parent's worker opens with: what every
// child it dispatched reported, verbatim, and the instruction to integrate and
// verify the combined result. It is the same sentence for the root and for every
// other parent — the law does not turn on who owns the run.
func wakeClause(tasks []*plandb.Task, task plandb.Task) string {
	var b strings.Builder
	b.WriteString("every child you dispatched has landed. Integrate their results, verify the combined result in the workspace, add more children if something is missing, and report.")
	for _, child := range tasks {
		if child.ParentID != task.ID {
			continue
		}
		fmt.Fprintf(&b, "\n- child %s %q is %s: %s", child.ID, child.Title, child.Status, resultOrNoResult(child.Result))
	}
	return b.String()
}

// resultOrNoResult is what a wake clause says for a child that reported nothing:
// the phrase stands where the result would, so the clause never reads as a
// missing half-sentence.
func resultOrNoResult(result string) string {
	if strings.TrimSpace(result) == "" {
		return "(no result)"
	}
	return result
}

// ownerName is the identity a run's claims are held under: "<hostname>:<pid>",
// so a claim names the process answerable for it and a claim nobody touches
// reads stale. A machine that cannot say its own name still claims — the pid
// alone tells two processes apart.
func ownerName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

// touchClaims refreshes the seen-at stamp of every task this process holds,
// so a live claim never reads as stale however long its worker runs. The
// store writes only when something is held and moves the seen-at stamp alone,
// so a heartbeat is never mistaken for work the plan did.
func (s *Supervisor) touchClaims() {
	_, _ = s.store.TouchClaims(s.Owner)
}

// releaseStale hands back every claim whose owner has stopped touching it, so
// the ready set offers those tasks again. The stale scan leaves the root out
// — the run itself is nobody's take-over — and a claim this process is
// actively working is left alone: the touch above just refreshed it, and a
// worker in flight must not lose its task under it. A live claim held by
// another process is left alone for the same reason — that process keeps
// touching it.
func (s *Supervisor) releaseStale() {
	for _, task := range s.store.StaleClaims(s.staleAfter) {
		if _, running := s.cancels[task.ID]; running {
			continue
		}
		_, _ = s.store.Release(task.ID, task.ClaimedBy)
	}
}

// endCancelledWorkers ends the context of every running worker whose task
// the store has ended out from under it. A failure or cancellation written by
// somebody else is the task's terminal word; the loop cancels the context on
// its next pass, frees the slot when the worker returns, and absorb preserves
// that word instead of writing the late return over it. The ancestor walk is
// the belt over a row this handle read before a cancellation landed.
func (s *Supervisor) endCancelledWorkers() {
	for id, cancel := range s.cancels {
		task := s.store.Task(id)
		// FAILED AND CANCELLED, NEVER DONE. A worker that finishes writes its own
		// `done` and then returns; a pass landing between the two would cancel a
		// worker that ended well, its return would carry the cancellation as an
		// error, and absorb would skip the review round on work that completed.
		ended := task != nil && (task.Status == plandb.StatusCancelled || task.Status == plandb.StatusFailed)
		if task == nil || ended || s.hasCancelledAncestor(*task) {
			cancel()
			delete(s.cancels, id)
		}
	}
}

// hasCancelledAncestor walks the containment chain and answers whether any
// ancestor of the task was cancelled. The store cascades a cancellation to
// descendants in the same write, so a ready leaf under a cancelled ancestor
// should not exist — this is the belt over the window between the ready read
// and the claim, where a person's cancellation could land.
func (s *Supervisor) hasCancelledAncestor(task plandb.Task) bool {
	for parent := s.store.Task(task.ParentID); parent != nil; parent = s.store.Task(parent.ParentID) {
		if parent.Status == plandb.StatusCancelled {
			return true
		}
	}
	return false
}

// terminalStatus is the store's terminal ladder, read here because the
// outcome turns on it as much as the store's own laws do.
func terminalStatus(status plandb.Status) bool {
	return status == plandb.StatusDone || status == plandb.StatusFailed || status == plandb.StatusCancelled
}

// outcomeForRoot reads the run's word off the root's own ending: done when
// the run completed whole; the cannot-run word when the run itself was
// cancelled before anything of it started — no worker dispatched, none
// running, so nothing was spent and there is nothing to read; and the
// incomplete word every other way — failed leaves, or a cancelled run that
// had already begun.
func (s *Supervisor) outcomeForRoot(status plandb.Status) Outcome {
	if status == plandb.StatusDone {
		return OutcomeDone
	}
	if status == plandb.StatusCancelled && !s.dispatchedRoot && s.inFlight == 0 {
		return OutcomeCannotRun
	}
	return OutcomeIncomplete
}

// Spec is what a caller hands Start: the plan store the run lives in, the
// working copy its workers share, the run's own words, and the factory that
// resolves every task — the root's included — into the seat that runs it.
type Spec struct {
	// Store is the run's plan, opened by the caller and shared with the run's
	// other writers. The root task it was seeded with is the run itself.
	Store *plandb.Store
	// Workspace is the run's own working copy, carried for the worker seat
	// and the landing that follow this loop.
	Workspace string
	// Title and Brief are the run's own words. The store writes a root task's
	// title nowhere but its own open, so the title is the caller's to seed
	// there; the brief is Start's to put down — on a root opened without a
	// description it becomes the root task's description, which is the
	// assignment the root worker reads.
	Title string
	Brief string
	// Slots bounds how many workers run at once, and Limits bound the run's
	// cost and its per-task steps. Both pass through to the supervisor as
	// given.
	Slots  int
	Limits Limits
	// Factory makes the worker for every task the run dispatches. Start holds
	// no seat of its own: the root's worker comes from here like the rest.
	Factory WorkerFactory
}

// Summary is what a run came to, in the figures a headless caller prints
// beside its exit code: the outcome word off the same ladder the envelope
// speaks, the root's result where a deliverable goes, the run's size — every
// worker launched, every step its workers reported — what they cost, and the
// wall the run took.
type Summary struct {
	Outcome Outcome
	// Result is the root's own result: what the run's last worker reported
	// when the tree finished whole, and empty whenever it did not.
	Result  string
	Nodes   int
	Steps   int
	USD     float64
	Seconds float64
}

// Start is the one door a caller runs a plan through: it puts the run's
// words on the store's root task, runs the supervisor over the store to one
// outcome word, and answers what came of it. The context is the run's wall —
// workers end with it — and nothing here needs a worker of its own: every
// seat, the root's included, comes from the spec's factory.
//
// IT DOES NOT ANSWER WHILE A WORKER IT STARTED IS ALIVE: the supervisor drains
// every goroutine it launched before its outcome comes back here, so a caller
// may close its store — or the process — on the line after this returns and
// nothing of the run is left to write into it.
func Start(ctx context.Context, spec Spec) (Outcome, Summary) {
	started := time.Now()
	if spec.Store == nil || spec.Factory == nil {
		// A door with no store to run over, or no seat to run a task in, is a
		// run that could not begin: the first rung of the ladder, nothing
		// attempted and nothing spent.
		return OutcomeCannotRun, Summary{Outcome: OutcomeCannotRun}
	}
	store := spec.Store
	// THE RUN'S WORDS GO ON ITS ROOT TASK before anything launches, so the
	// root worker reads its assignment from the store the way every other
	// worker does. A store opened with a description on the root keeps it; a
	// root opened bare takes the brief here — the store's one write onto a
	// running task's description — and a resume of the same run finds the
	// words already there and writes nothing.
	if root := store.Task(store.RootID()); root != nil && strings.TrimSpace(root.Description) == "" && strings.TrimSpace(spec.Brief) != "" {
		if _, err := store.Amend(root.ID, spec.Brief); err != nil {
			return OutcomeCannotRun, Summary{Outcome: OutcomeCannotRun}
		}
	}
	supervisor := NewSupervisor(store, spec.Workspace, spec.Slots, spec.Limits, spec.Factory)
	outcome := supervisor.Run(ctx)
	result := supervisor.rootResult
	// THE TERMINAL ROOT'S STORED RESULT IS DELIVERABLE even when its worker return lands after the supervisor stops absorbing returns.
	if root := store.Task(store.RootID()); strings.TrimSpace(result) == "" && root != nil && root.Status == plandb.StatusDone {
		result = root.Result
	}
	return outcome, Summary{
		Outcome: outcome,
		Result:  result,
		Nodes:   supervisor.nodes,
		Steps:   supervisor.steps,
		USD:     supervisor.spent,
		Seconds: time.Since(started).Seconds(),
	}
}
