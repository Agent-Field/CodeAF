package run

// A PROGRAM CODEAF CARRIES IS ONE MORE WORKER KIND. A program that does a
// whole task on its own — senior-dev first (internal/delegate,
// docs/design/delegate/PROTOCOL.md) — is seated on a task exactly where the
// bash worker is: it reads the same context for its limits, banks its dollars
// into the same account, publishes the same live step, appends to the same
// trajectory, and comes home with the same Report. Nothing above the factory
// knows which kind ran.
//
// What differs is inside: there is no model turn here. The program runs as a
// child process of codeaf's own executable (`codeaf <name> run --json …`) in
// the run's working copy, its stdout is the records, and its terminal record is
// the ending. Its stages feed the live step only; its `step` records are what
// enter the trajectory, so the task page's step count is what the program said
// it did and not how many phases it announced.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// delegateStderrName is the file a delegate's stderr is kept in, in the task's
// own record folder beside the trajectory, because stderr is where a program
// says why it could not start and a person opening the task should find it.
const delegateStderrName = "delegate-stderr.log"

// DelegateWorker runs one program as the worker of one task.
type DelegateWorker struct {
	store     *plandb.Store
	workspace string
	program   delegate.Delegate
	setup     DelegateSetup
	// cost and elapsed are the run's ceilings, handed to the program on its
	// command line so it cuts itself before the run has to. They are the
	// factory's copy of the run's Limits: the supervisor enforces the same two
	// from outside whatever the program does with them.
	cost    float64
	elapsed time.Duration
}

// DelegateSetup is how a delegated run starts its program's process.
type DelegateSetup struct {
	// Exe is codeaf's own executable, which the program runs as. Empty is this
	// process's own; a test names a script that speaks the records.
	Exe string
	// Grace overrides the launch's SIGTERM grace, for a test.
	Grace time.Duration
}

// NewDelegateWorker builds the worker. cost and elapsed are the run's
// ceilings, zero for none.
func NewDelegateWorker(store *plandb.Store, workspace string, program delegate.Delegate, setup DelegateSetup, cost float64, elapsed time.Duration) *DelegateWorker {
	return &DelegateWorker{store: store, workspace: workspace, program: program, setup: setup, cost: cost, elapsed: elapsed}
}

// DelegateFactory is the run's WorkerFactory for a delegated run: the root task
// is the program's, and every other task the run seats — the review round's
// check, and nothing else, because a delegated run is a run of one task —
// falls to the factory it wraps, which is the crew's.
func DelegateFactory(store *plandb.Store, workspace string, program delegate.Delegate, setup DelegateSetup, limits Limits, rest WorkerFactory) WorkerFactory {
	return func(task plandb.Task) Worker {
		if task.ID == store.RootID() {
			return NewDelegateWorker(store, workspace, program, setup, limits.CostUSD, limits.Elapsed)
		}
		if rest == nil {
			return nil
		}
		return rest(task)
	}
}

// delegateSink is the delegate.Sink one run of the worker hands the launch: it
// turns the stream into the store's live step, the trajectory's step lines and
// the run's spend bank. Its methods run on the reader's goroutine and none of
// them waits on anything but the store's own lock.
type delegateSink struct {
	worker   *DelegateWorker
	ctx      context.Context
	taskID   string
	storeDir string
	name     string
	steps    int
	usd      float64
	lastErr  error
	terminal *delegate.Terminal
	// stop ends the program early, and mismatch says why: the child spoke
	// another protocol than this build's, which means codeaf was rebuilt while
	// this conversation's engine was running and its child is the new build.
	stop     context.CancelFunc
	mismatch string
}

func (s *delegateSink) Hello(h delegate.Hello) {
	if h.Protocol == delegate.ProtocolVersion {
		return
	}
	// TWO BUILDS, ONE RUN. Nothing a newer child writes can be trusted to mean
	// what this parent reads it as, so the run is stopped before it spends and
	// the person is told the one thing that fixes it.
	s.mismatch = fmt.Sprintf("codeaf was rebuilt while this conversation was open (its %s speaks version %d of the records, this one reads %d); restart codeaf to run %s",
		s.name, h.Protocol, delegate.ProtocolVersion, s.name)
	if s.stop != nil {
		s.stop()
	}
}

func (s *delegateSink) Stage(stage, status string) {
	// THE LIVE STEP IS THE PROGRAM'S PHASE, numbered after the last step
	// recorded, so the row reads "senior-dev: implement · running" while the
	// program is inside that phase and the count on the row stays the steps'.
	label := s.name + ": " + stage
	if status != "" {
		label += " · " + status
	}
	_ = s.worker.store.SetLive(s.taskID, s.steps+1, label)
}

func (s *delegateSink) Spend(usd float64) {
	if usd > s.usd {
		s.usd = usd
	}
	bankSpend(s.ctx, s.usd)
}

func (s *delegateSink) Step(command, observation string) {
	s.steps++
	if err := appendTrajectory(s.storeDir, s.taskID, Step{
		Kind:        trajectoryStepKind,
		Step:        s.steps,
		Command:     command,
		Observation: observationHead(observation),
	}); err != nil && s.lastErr == nil {
		s.lastErr = err
	}
}

func (s *delegateSink) Terminal(t delegate.Terminal) { s.terminal = &t }

// Run starts the program and reads it to its ending. The Report's Result is
// the ending in words a person reads; Steps is what the program said it did;
// USD is the higher of what it streamed and what its terminal record said.
func (w *DelegateWorker) Run(ctx context.Context, task plandb.Task) (Report, error) {
	storeDir := filepath.Dir(w.store.Path())
	if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryBeginKind, ExitsRecorded: true}); err != nil {
		return Report{}, fmt.Errorf("stamp the trajectory opening line: %w", err)
	}
	launchCtx, stop := context.WithCancel(ctx)
	defer stop()
	sink := &delegateSink{worker: w, ctx: ctx, taskID: task.ID, storeDir: storeDir, name: w.program.Name, stop: stop}
	brief := strings.TrimSpace(task.Description)
	if brief == "" {
		brief = strings.TrimSpace(task.Title)
	}
	exe := w.setup.Exe
	if exe == "" {
		self, err := os.Executable()
		if err != nil {
			return Report{}, fmt.Errorf("find codeaf's own executable to run %s: %w", w.program.Name, err)
		}
		exe = self
	}
	result, err := delegate.Run(launchCtx, delegate.Launch{
		Name: w.program.Name,
		Bin:  exe,
		Args: delegate.ChildArgs(w.program, w.workspace, brief, delegate.Ceilings{CostUSD: w.cost, Hours: w.elapsed.Hours()}),
		// NO KEY REACHES THE PROGRAM (delegate.ChildEnv).
		Env:        delegate.ChildEnv(delegate.ModelAPI{}),
		Dir:        w.workspace,
		StderrPath: filepath.Join(plandb.TaskDir(storeDir, task.ID), delegateStderrName),
		Grace:      w.setup.Grace,
	}, sink)
	// THE LIVE STEP GOES WITH THE PROCESS, whatever the ending: a row that still
	// read "implement · running" after the program was gone would be a claim
	// about a present that is over.
	_ = w.store.ClearLive(task.ID)

	usd := sink.usd
	if t := result.Reading.Terminal; t != nil {
		if total, ok := t.CostUSD(); ok && total > usd {
			usd = total
		}
	}
	if usd > 0 {
		// The spend row is the task page's own figure. The role is read off the
		// store as the bash worker reads it; the "model" column carries the
		// delegate's name, because that is what spent the money.
		role, err := w.store.RoleOf(task.ID)
		if err != nil {
			role = plandb.RoleWork
		}
		_ = w.store.AddSpend(task.ID, "delegate/"+w.program.Name, role, usd, 0, 0)
	}
	report := Report{Steps: sink.steps, USD: usd}

	end := func(reason, result string) {
		_ = appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: sink.steps, Result: result, Reason: reason})
	}
	if sink.lastErr != nil {
		end("the record failed: "+sink.lastErr.Error(), "")
		return report, sink.lastErr
	}
	if sink.mismatch != "" && ctx.Err() == nil {
		end(sink.mismatch, "")
		return report, errors.New(sink.mismatch)
	}
	if result.Stopped {
		// THE RUN'S OWN ENDING CUT THIS PROGRAM: the context is what ended it, so
		// the error is the context's own and the supervisor records the cut. A
		// terminal the program wrote inside the grace still names the reason.
		reason := "stopped by the run"
		if t := result.Reading.Terminal; t != nil && t.Message != "" {
			reason += ": " + w.program.Name + " said " + t.Message
		}
		end(reason, "")
		return report, err
	}
	if errors.Is(err, delegate.ErrNoTerminal) {
		reason := fmt.Sprintf("%s exited %d without a terminal record", w.program.Name, result.ExitCode)
		if result.Reading.LastStage != "" {
			reason += "; its last stage was " + result.Reading.LastStage
		}
		end(reason, "")
		return report, errors.New(reason)
	}
	if err != nil {
		end(err.Error(), "")
		return report, err
	}
	t := *result.Reading.Terminal
	report.Result = delegateResult(w.program, t)
	switch t.Status {
	case delegate.StatusPass:
		end("finished: "+t.Message, report.Result)
		return report, nil
	case delegate.StatusBudget:
		reason := w.program.Name + " stopped on its own ceiling: " + t.Message
		end(reason, report.Result)
		return report, errors.New(reason)
	case delegate.StatusCrashed:
		reason := w.program.Name + " crashed: " + t.Message
		end(reason, report.Result)
		return report, errors.New(reason)
	default:
		// `fail`, and any word this build does not know, is work that does not
		// stand: the run reads it as incomplete.
		reason := w.program.Name + " did not finish: " + t.Message
		end(reason, report.Result)
		return report, errors.New(reason)
	}
}

// delegateResult is the ending in words: the deliverable for a program that
// lands text, and for one that lands a tree the program's message with the
// claim and the observation as two sentences, kept apart because the
// program's model and the program itself are two witnesses.
func delegateResult(m delegate.Delegate, t delegate.Terminal) string {
	if !m.LandsTree() {
		if deliverable := t.Deliverable(); deliverable != "" {
			return deliverable
		}
	}
	parts := []string{strings.TrimSpace(t.Message)}
	if claim := t.Claim(); claim != "" {
		parts = append(parts, m.Name+"'s model said: "+claim)
	}
	if observed := t.Observed(); observed != "" {
		parts = append(parts, m.Name+" observed: "+observed)
	}
	if reason := t.Reason(); reason != "" && reason != t.Message {
		parts = append(parts, reason)
	}
	return strings.Join(nonEmpty(parts), ". ")
}

func nonEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimRight(strings.TrimSpace(p), "."))
		}
	}
	return out
}
