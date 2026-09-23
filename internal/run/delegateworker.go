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
//
// ── ITS ONLY ROAD TO A MODEL IS THIS RUN'S MODEL API ────────────────────────
//
// Before the program starts, the worker opens the run's model API
// (internal/provider/modelapi) on this machine's loopback and hands the child
// its address and token and nothing else (delegate.ChildEnv): no key reaches
// the program. Every call it makes goes through the conversation's own
// completer, is refused at the run's dollar ceiling before it is made, and is
// written to the task's conversation log as one turn. The API is closed the
// moment the program has exited, and the token dies with it.
//
// ── MONEY IS METERED BY THE API, NEVER REPORTED BY THE PROGRAM ──────────────
//
// Each call's price reaches three books as it is metered ([delegateMeter]):
// the run's live bank, which the supervisor holds to the ceiling and the
// conversation's status line reads; the task's spend rows, one per call, which
// the task page draws; and this machine's spending ledger, one row per call,
// exactly once — the conversation folds the run's total into its own meter
// without writing a ledger row of its own (internal/session's addFoldedUsage).
// The program's terminal record may still carry its own reading of what it
// spent; that figure is kept on the record and never banked.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
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
	// from outside whatever the program does with them, and the model API
	// refuses a call made past the dollar one.
	cost    float64
	elapsed time.Duration
}

// DelegateSetup is how a delegated run starts its program's process and serves
// it models.
type DelegateSetup struct {
	// Exe is codeaf's own executable, which the program runs as. Empty is this
	// process's own; a test names a script that speaks the records.
	Exe string
	// Grace overrides the launch's SIGTERM grace, for a test.
	Grace time.Duration
	// CompleterFor answers the funnel a call on a model goes out through: the
	// conversation's own completer (session.RunSpec.CompleterFor), so a
	// program's calls take the road the conversation's own do. Nil is a run
	// with no model road, whose API answers every call with that sentence.
	CompleterFor func(model string) session.Completer
	// Serves answers whether this conversation's services can take a call on a
	// model (session.RunSpec.Serves); nil answers yes for every model.
	Serves func(model string) bool
	// Seat is the run's own work seat ([WorkSeat]): the model a call is
	// answered on when the one the program asked for cannot be reached here.
	Seat string
	// Ledger is the spending ledger the calls are written to. Empty is this
	// machine's own (session.UsageLedgerPath); a test names a file of its own.
	Ledger string
	// Keepalive overrides the model API's keepalive interval, for a test.
	Keepalive time.Duration
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
// the program record the task page reads. Its methods run on the reader's
// goroutine and none of them waits on anything but the store's own lock.
type delegateSink struct {
	worker   *DelegateWorker
	taskID   string
	storeDir string
	taskDir  string
	name     string
	steps    int
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
		// THE PAGE LEARNS WHOSE CONVERSATION IT IS DRAWING, and the stages the
		// program will move through, the moment the program says them — and
		// keeps knowing after the run. It is a record, so a disk that refuses it
		// costs the page its heading and never the run.
		_ = delegate.WriteProgram(s.taskDir, delegate.ProgramRecord{Name: s.name, Stages: h.Stages})
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

// delegateMeter is where the run's model API tells each charge as it is
// metered: the run's live bank, the task's spend row, and the machine's
// spending ledger. It is called one charge at a time, in order.
type delegateMeter struct {
	ctx       context.Context
	store     *plandb.Store
	taskID    string
	role      string
	name      string
	workspace string
	ledger    string
}

// bank books one charge in all three places.
//
// THE LEDGER ROW IS WRITTEN HERE AND ONLY HERE. The conversation that started
// the run folds the run's total into its own meter through the fold door,
// which writes no ledger row, exactly as it does for a bash worker whose own
// session wrote the rows — so each of the program's calls is on this machine's
// spending ledger once. The row is the worker seat's, because the program sits
// where the run's worker would.
func (m *delegateMeter) bank(charge modelapi.Charge) {
	bankSpend(m.ctx, charge.Spent)
	_ = m.store.AddSpend(m.taskID, m.name, m.role, charge.CostUSD, charge.TokensIn, charge.TokensOut)
	line := session.UsageLine{
		Model: charge.Model, Calls: 1, Input: charge.TokensIn, Output: charge.TokensOut, USD: charge.CostUSD,
		Reconciled: charge.Late, Workspace: m.workspace,
	}
	session.RecordUsage(m.ledgerPath(), session.TagUsage(line, roles.RoleWorker, session.SeatWorker))
}

// unbilled keeps a call nobody could price on the ledger as the marker it is,
// with no invented money.
func (m *delegateMeter) unbilled(model string) {
	session.RecordUnbilledCall(m.ledgerPath(), session.TagUsage(session.UsageLine{Model: model, Workspace: m.workspace}, roles.RoleWorker, session.SeatWorker))
}

func (m *delegateMeter) ledgerPath() string {
	if strings.TrimSpace(m.ledger) != "" {
		return m.ledger
	}
	return session.UsageLedgerPath()
}

// Run starts the program and reads it to its ending. The Report's Result is
// the ending in words a person reads; Steps is what the program said it did;
// USD is what the model API metered, and nothing the program said about it.
func (w *DelegateWorker) Run(ctx context.Context, task plandb.Task) (Report, error) {
	storeDir := filepath.Dir(w.store.Path())
	taskDir := plandb.TaskDir(storeDir, task.ID)
	if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryBeginKind, ExitsRecorded: true}); err != nil {
		return Report{}, fmt.Errorf("stamp the trajectory opening line: %w", err)
	}
	end := func(steps int, reason, result string) {
		_ = appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Result: result, Reason: reason})
	}
	exe := w.setup.Exe
	if exe == "" {
		self, err := os.Executable()
		if err != nil {
			reason := fmt.Sprintf("find codeaf's own executable to run %s: %v", w.program.Name, err)
			end(0, reason, "")
			return Report{}, errors.New(reason)
		}
		exe = self
	}
	role, err := w.store.RoleOf(task.ID)
	if err != nil {
		role = plandb.RoleWork
	}
	meter := &delegateMeter{
		ctx: ctx, store: w.store, taskID: task.ID, role: role,
		// The spend row's "model" column carries the program's name, because
		// that is what spent the money; the ledger row names the model that
		// answered.
		name:      "delegate/" + w.program.Name,
		workspace: w.workspace, ledger: w.setup.Ledger,
	}
	api, err := modelapi.Open(modelapi.Config{
		TaskDir:      taskDir,
		CompleterFor: w.completerFor(),
		Serves:       w.setup.Serves,
		Seat:         w.setup.Seat,
		Ceiling:      w.cost,
		Bank:         meter.bank,
		Unbilled:     meter.unbilled,
		// NOBODY IS READING THE PROGRAM'S CALLS AS THEY ARRIVE: it is a task's
		// worker, and the person is in their conversation or away from it.
		Role:      lanes.RoleLeafUnattended,
		Node:      w.program.Name,
		Keepalive: w.setup.Keepalive,
	})
	if err != nil {
		reason := fmt.Sprintf("open %s's model API: %v", w.program.Name, err)
		end(0, reason, "")
		return Report{}, errors.New(reason)
	}
	// THE TOKEN DIES WITH THE RUN, on every path out of this function; the
	// ordinary path closes it the moment the program has exited, below.
	defer func() { _ = api.Close() }()

	launchCtx, stop := context.WithCancel(ctx)
	defer stop()
	sink := &delegateSink{worker: w, taskID: task.ID, storeDir: storeDir, taskDir: taskDir, name: w.program.Name, stop: stop}
	brief := strings.TrimSpace(task.Description)
	if brief == "" {
		brief = strings.TrimSpace(task.Title)
	}
	result, err := delegate.Run(launchCtx, delegate.Launch{
		Name: w.program.Name,
		Bin:  exe,
		Args: delegate.ChildArgs(w.program, w.workspace, brief, delegate.Ceilings{CostUSD: w.cost, Hours: w.elapsed.Hours()}),
		// NO KEY REACHES THE PROGRAM (delegate.ChildEnv): the API's address and
		// token are the whole of what it is given.
		Env:        delegate.ChildEnv(api.API()),
		Dir:        w.workspace,
		StderrPath: filepath.Join(taskDir, delegateStderrName),
		Grace:      w.setup.Grace,
	}, sink)
	// The program has exited: its API goes with it, so nothing it left behind
	// can spend, and the calls that were still running write their last turn.
	_ = api.Close()
	// THE LIVE STEP GOES WITH THE PROCESS, whatever the ending: a row that still
	// read "implement · running" after the program was gone would be a claim
	// about a present that is over.
	_ = w.store.ClearLive(task.ID)

	report := Report{Steps: sink.steps, USD: api.Spent()}
	if sink.lastErr != nil {
		end(sink.steps, "the record failed: "+sink.lastErr.Error(), "")
		return report, sink.lastErr
	}
	if sink.mismatch != "" && ctx.Err() == nil {
		end(sink.steps, sink.mismatch, "")
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
		end(sink.steps, reason, "")
		return report, err
	}
	// THE CEILING, NOT A CRASH. A program the model API refused at the run's
	// dollar ceiling ends however it ends — senior-dev, whose own sum of its
	// answers' costs never reached the figure it was given, ends as `crashed` —
	// but what stopped it was the limit a person set, and the run says so. The
	// supervisor's own ledger has reached the same ceiling, so the run ends on
	// its cost limit; this is the worker's half, the words the task keeps.
	if t := result.Reading.Terminal; api.RefusedAtCeiling() > 0 && (t == nil || t.Status != delegate.StatusPass) {
		reason := fmt.Sprintf("%s reached the run's dollar ceiling of $%.2f", w.program.Name, w.cost)
		if t != nil {
			report.Result = delegateResult(w.program, *t)
			if message := strings.TrimSpace(t.Message); message != "" {
				reason += ": " + w.program.Name + " said " + message
			}
		}
		end(sink.steps, reason, report.Result)
		return report, errors.New(reason)
	}
	if errors.Is(err, delegate.ErrNoTerminal) {
		reason := fmt.Sprintf("%s exited %d without a terminal record", w.program.Name, result.ExitCode)
		if result.Reading.LastStage != "" {
			reason += "; its last stage was " + result.Reading.LastStage
		}
		end(sink.steps, reason, "")
		return report, errors.New(reason)
	}
	if err != nil {
		end(sink.steps, err.Error(), "")
		return report, err
	}
	t := *result.Reading.Terminal
	report.Result = delegateResult(w.program, t)
	switch t.Status {
	case delegate.StatusPass:
		end(sink.steps, "finished: "+t.Message, report.Result)
		return report, nil
	case delegate.StatusBudget:
		reason := w.program.Name + " stopped on its own ceiling: " + t.Message
		end(sink.steps, reason, report.Result)
		return report, errors.New(reason)
	case delegate.StatusCrashed:
		reason := w.program.Name + " crashed: " + t.Message
		end(sink.steps, reason, report.Result)
		return report, errors.New(reason)
	default:
		// `fail`, and any word this build does not know, is work that does not
		// stand: the run reads it as incomplete.
		reason := w.program.Name + " did not finish: " + t.Message
		end(sink.steps, reason, report.Result)
		return report, errors.New(reason)
	}
}

// completerFor is the setup's completer factory in the model API's own
// words, each completer marked so a call keeps the program's own cache
// lineage (session.WithOwnCacheLineage): a program's conversations are its
// own, and the conversation's key stamped over them would put every one of
// them on the conversation's warm instance.
func (w *DelegateWorker) completerFor() func(model string) modelapi.Completer {
	if w.setup.CompleterFor == nil {
		return nil
	}
	return func(model string) modelapi.Completer {
		completer := w.setup.CompleterFor(model)
		if completer == nil {
			return nil
		}
		return ownLineage{completer}
	}
}

// ownLineage is a completer whose calls keep the cache key already on their
// context.
type ownLineage struct{ inner session.Completer }

func (c ownLineage) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	return c.inner.CompleteWithMessages(session.WithOwnCacheLineage(ctx), messages, options...)
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
