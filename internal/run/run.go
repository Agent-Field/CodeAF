package run

import (
	"context"
	"errors"
	"fmt"
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

// workerReturn is one finished worker, on its way from its goroutine back to
// the loop.
type workerReturn struct {
	task   plandb.Task
	report Report
	err    error
}

// Supervisor is the launch loop over one plan store. It claims ready leaves
// as the task's own agent, starts a worker for each under a slot bound, and
// writes every worker's ending back into the store. The root task is the
// supervisor's own: no worker completes it, and it is completed once every
// other task in the store is terminal.
type Supervisor struct {
	store     *plandb.Store
	workspace string
	slots     int
	limits    Limits
	factory   WorkerFactory

	// finished carries worker endings back to the loop, buffered at the slot
	// bound plus the root's seat: a worker whose run has already ended deposits
	// its return and exits rather than blocking on a reader that is gone.
	// inFlight is the count of workers running, and the counters beside it are
	// the run's memory — nodes counts every worker launched, steps sums what
	// those workers reported; all of them belong to the loop goroutine and are
	// touched by no one else. cancels maps a running task to the context
	// cancel that ends its worker, so a pass can end one worker's context
	// without touching the rest.
	finished       chan workerReturn
	inFlight       int
	spent          float64
	nodes          int
	steps          int
	rootResult     string
	rootFailed     bool
	limitHit       bool
	dispatchedRoot bool
	cancels        map[string]context.CancelFunc
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
		store:     store,
		workspace: workspace,
		slots:     slots,
		limits:    limits,
		factory:   factory,
		finished:  make(chan workerReturn, slots+1),
		cancels:   make(map[string]context.CancelFunc),
	}
}

// Run drives the run to one of the outcome words and returns it. Each pass
// reads the ready set, claims every ready leaf whose ancestors are not
// cancelled, and starts one worker per claim, bounded by slots. A pass runs
// when a worker returns and on the idle timer, so a task added through the
// store while the loop waits is launched without waiting for anything else.
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

	timer := time.NewTimer(passInterval)
	defer timer.Stop()
	for {
		if outcome := s.pass(ctx, rootID); outcome != "" {
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
			// writes land before the word is returned.
			for s.inFlight > 0 {
				ret := <-s.finished
				s.inFlight--
				s.absorb(ret)
			}
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
	if terminalStatus(root.Status) {
		return s.outcomeForRoot(root.Status)
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
		s.launch(ctx, *root)
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
			// exact name.
			if _, err := s.store.Claim(task.ID, task.ID); err != nil {
				// Not ours anymore — another writer claimed, cancelled or
				// finished it between the read and the claim. The next pass
				// sees the store as it now stands.
				continue
			}
			s.launch(ctx, task)
		}
	}
	return ""
}

// launch starts one worker in its own goroutine. The task's context is the
// run's with a cancel of its own, recorded so a pass can end this one worker
// — a task the store has cancelled — without ending the run; it carries the
// step cap. The goroutine reports back on the channel and ends.
func (s *Supervisor) launch(ctx context.Context, task plandb.Task) {
	worker := s.factory(task)
	taskCtx, cancel := context.WithCancel(ctx)
	s.cancels[task.ID] = cancel
	s.inFlight++
	s.nodes++
	go func() {
		report, err := Report{}, error(nil)
		if worker == nil {
			err = errors.New("no worker for task " + task.ID)
		} else {
			report, err = worker.Run(WithStepsPerTask(taskCtx, s.limits.StepsPerTask), task)
		}
		s.finished <- workerReturn{task: task, report: report, err: err}
	}()
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
	if task := s.store.Task(ret.task.ID); task == nil || task.Status == plandb.StatusCancelled {
		s.completeTree()
		return
	}
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
		}
	} else {
		switch {
		case ret.err == nil:
			_, err := s.store.Done(ret.task.ID, ret.task.ID, ret.report.Result, nil, nil)
			if err == nil {
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
	if s.treeTerminal() {
		_ = s.store.CompleteRoot(s.rootResult)
	}
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

// endCancelledWorkers ends the context of every running worker whose task
// the store has ended out from under it. The store's cancel writes the
// ending under the task and everything under it and releases the claim in
// the same write — ClaimedBy goes with the cancelled row, and a Release
// against a cancelled task is refused — so the part left to the loop is the
// context: cancelled here, the worker stops at its next step, and the return
// it makes afterwards is dropped whole in absorb. The ancestor walk is the
// belt over a row this handle read before the ending landed.
func (s *Supervisor) endCancelledWorkers() {
	for id, cancel := range s.cancels {
		task := s.store.Task(id)
		if task == nil || task.Status == plandb.StatusCancelled || s.hasCancelledAncestor(*task) {
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
