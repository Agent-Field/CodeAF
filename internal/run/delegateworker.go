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
// the run's folder (for a program that edits files, the person's folder itself,
// readied by internal/session's PrepareProgramFolder), its stdout is the
// records, and its terminal record is the ending. Every stage, step and ending
// is written to the task's action log (delegate.ActionsFile) the moment it is
// received, which is what the task page draws the program's work from; its
// `step` records are also what enter the trajectory, so the task page's step
// count is what the program said it did and not how many phases it announced;
// and the live step names the step of the program's process it is in.
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
// Each call's price reaches four books as it is metered ([delegateMeter]):
// the conversation's own, which folds the call whole — tokens, model and
// dollars — without writing a ledger row of its own (internal/session's
// beltFold); the run's live bank, which the supervisor holds to the ceiling;
// the task's spend rows, one per call, which the task page draws; and this
// machine's spending ledger, one row per call, exactly once, filed under the
// conversation and the task.
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
	// PlainFolder says the program works in its folder without git
	// (session.RunSpec.PlainFolder), so the program's line carries its own
	// flags for that (delegate.Delegate.PlainFolder).
	PlainFolder bool
	// Crew is the conversation's crew (session.RunSpec.Crew), which the
	// program's line carries in its own flags (delegate.Delegate.CrewFlags) so
	// it works on the models the person chose. Zero leaves it to its own.
	Crew delegate.Crew
	// Conversation is the id of the conversation the run belongs to
	// (session.RunSpec.Conversation), which every ledger row the program's
	// calls write names as its Root and its Session, beside the task's id, so
	// the conversation's spend and the spending page can say whose money it
	// was. Empty leaves the rows naming no conversation.
	Conversation string
	// OnCharge is told every priced call as it is metered
	// (session.RunSpec.OnCharge), for the conversation to fold the call's
	// tokens, model and dollars into its own books. Nil tells nobody.
	OnCharge func(session.RunCharge)
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
	// record is the program record as this run has written it so far: the
	// name, the ceiling and the instant the process was started, and the
	// stages once the hello has named them. It is kept here because the record
	// is written whole, twice — at the hello and when the process is gone — and
	// the second write must carry what the first one said.
	record delegate.ProgramRecord
	// reader is the program's own reader of its action log
	// (delegate.Delegate.Reader), told every record in the order it arrives, so
	// the live step can name the step of the program's process the record
	// served; stepped is whether any record has named one yet, and step the
	// word the live step reads now, which a record naming the same step again
	// does not write twice.
	reader  delegate.ActionReader
	stepped bool
	step    string
}

// remember writes one received record to the task's action log, stamped with
// the moment it arrived, and moves the live step to the step it served.
//
// THE LOG IS A RECORD, SO A DISK THAT REFUSES IT COSTS THE PAGE AND NEVER THE
// RUN, as the program record's does; and a child of ANOTHER BUILD is not this
// run's program, so nothing it says is written down as the program's.
func (s *delegateSink) remember(action delegate.Action) {
	if s.mismatch != "" {
		return
	}
	if strings.TrimSpace(s.taskDir) != "" {
		_ = delegate.AppendAction(s.taskDir, action)
	}
	s.live(action)
}

// live moves the live step for one received record.
//
// THE LIVE STEP IS THE STEP OF THE PROGRAM'S PROCESS IT IS IN, numbered after
// the last step recorded, so the row reads "senior-dev: explore" while the
// program explores and the count on the row stays the steps'. The step is the
// one the program's own reader of its log names for the record
// (delegate.Delegate.Present) — a stage can name one as well as a step — and a
// record that names none leaves the word standing.
//
// BEFORE ANY RECORD HAS NAMED A STEP, A STAGE IS SHOWN IN THE PROGRAM'S WORDS
// FOR A PERSON, NOT ITS STAGE'S NAME. A program that says what a person should
// read for its stages (delegate.Delegate's StageWords) is shown that word and
// no status beside it — a status is its machinery too — and a stage it gave no
// word keeps the word already shown. Only a program that said nothing is shown
// its own names, as it spelled them.
func (s *delegateSink) live(action delegate.Action) {
	if s.reader == nil {
		s.reader = s.worker.program.Reader()
	}
	if shown, ok := s.reader(action); ok && strings.TrimSpace(shown.Step) != "" {
		s.stepped = true
		if word := strings.TrimSpace(shown.Step); word != s.step {
			s.step = word
			_ = s.worker.store.SetLive(s.taskID, s.steps+1, s.name+": "+word)
		}
		return
	}
	if action.Kind != delegate.ActionStage || s.stepped {
		return
	}
	label := s.name + ": " + action.Stage
	if words := s.worker.program.StageWords; words != nil {
		word := strings.TrimSpace(words[action.Stage])
		if word == "" {
			return
		}
		label = s.name + ": " + word
	} else if action.Status != "" {
		label += " · " + action.Status
	}
	_ = s.worker.store.SetLive(s.taskID, s.steps+1, label)
}

func (s *delegateSink) Hello(h delegate.Hello) {
	if h.Protocol == delegate.ProtocolVersion {
		// THE PAGE LEARNS WHOSE CONVERSATION IT IS DRAWING, the stages the
		// program will move through and the ceiling its spend is read against,
		// the moment the program says hello — and keeps knowing after the run.
		// It is a record, so a disk that refuses it costs the page its heading
		// and never the run.
		s.record.Stages = h.Stages
		_ = delegate.WriteProgram(s.taskDir, s.record)
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

func (s *delegateSink) Stage(record delegate.StageRecord) {
	s.remember(delegate.StageAction(time.Now(), record))
}

func (s *delegateSink) Step(record delegate.StepRecord) {
	s.steps++
	if err := appendTrajectory(s.storeDir, s.taskID, Step{
		Kind:        trajectoryStepKind,
		Step:        s.steps,
		Command:     record.Command,
		Observation: observationHead(record.Observation),
	}); err != nil && s.lastErr == nil {
		s.lastErr = err
	}
	s.remember(delegate.StepAction(time.Now(), record))
}

func (s *delegateSink) Terminal(t delegate.Terminal) {
	s.terminal = &t
	s.remember(delegate.EndAction(time.Now(), t))
}

// delegateMeter is where the run's model API tells each charge as it is
// metered: the conversation's books, the run's live bank, the task's spend
// row, and the machine's spending ledger. It is called one charge at a time,
// in order.
type delegateMeter struct {
	ctx       context.Context
	store     *plandb.Store
	taskID    string
	taskDir   string
	role      string
	name      string
	workspace string
	ledger    string
	// conversation is the conversation the run belongs to, stamped on every
	// ledger row; onCharge folds each call into that conversation's books.
	conversation string
	onCharge     func(session.RunCharge)
}

// bank books one charge in all four places.
//
// THE CONVERSATION HEARS FIRST, BEFORE THE RUN'S BANK MOVES. The conversation
// folds each call whole — its tokens, its model, its dollars — as it is
// metered, and also folds whatever the run's total says it has not yet heard
// of (internal/session's beltFold); telling it the call before the total that
// holds the call is what keeps one dollar from being folded twice.
//
// THE LEDGER ROW IS WRITTEN HERE AND ONLY HERE. The conversation's fold writes
// no ledger row, exactly as it does for a bash worker whose own session wrote
// the rows — so each of the program's calls is on this machine's spending
// ledger once. The row is the worker seat's, because the program sits where
// the run's worker would, and it names whose work it was the way a task
// node's row does ([session.UsageLine.Root]): the conversation as its Root and
// its Session, the task as its Task. A row that named none of them was money
// the conversation's receipt and the spending page could not place — 94.9% of
// one day's spend on 2026-09-23 was senior-dev calls filed under nobody.
func (m *delegateMeter) bank(charge modelapi.Charge) {
	if m.onCharge != nil {
		m.onCharge(session.RunCharge{
			Model: charge.Model, TokensIn: charge.TokensIn, TokensOut: charge.TokensOut,
			Cached: charge.Cached, USD: charge.CostUSD,
		})
	}
	bankSpend(m.ctx, charge.Spent)
	if err := m.store.AddSpend(m.taskID, m.name, m.role, charge.CostUSD, charge.TokensIn, charge.TokensOut); err != nil {
		m.unstored(charge, err)
	}
	line := m.stamp(session.UsageLine{
		Model: charge.Model, Calls: 1, Input: charge.TokensIn, Output: charge.TokensOut, USD: charge.CostUSD,
		Reconciled: charge.Late,
	})
	session.RecordUsage(m.ledgerPath(), session.TagUsage(line, roles.RoleWorker, session.SeatWorker))
}

// unbilled keeps a call nobody could price on the ledger as the marker it is,
// with no invented money, filed under the same work as every priced row.
func (m *delegateMeter) unbilled(model string) {
	session.RecordUnbilledCall(m.ledgerPath(), session.TagUsage(m.stamp(session.UsageLine{Model: model}), roles.RoleWorker, session.SeatWorker))
}

// stamp names whose work a ledger row is: the workspace it was spent against,
// the task, and the conversation the task belongs to.
func (m *delegateMeter) stamp(line session.UsageLine) session.UsageLine {
	line.Workspace = m.workspace
	line.Task = strings.TrimPrefix(strings.TrimSpace(m.taskID), "t-")
	if conversation := strings.TrimSpace(m.conversation); conversation != "" {
		line.Root, line.Session = conversation, conversation
	}
	return line
}

// unstored says, in the task's own record folder, that a charge could not be
// written to the task's spend rows.
//
// A SPEND ROW THE STORE REFUSED IS NOT DROPPED IN SILENCE. The machine's
// ledger and the conversation's books already hold the charge, but the task
// page's figure is read from these rows, so a refusal makes the page read
// short; the line in delegate-stderr.log is where a person asking why finds
// the answer. It is written only after the model API has closed — a receipt
// that outlived even its wait, arriving after the program's process is gone —
// or on a store that failed outright, so it never interleaves with the
// program's own stderr.
func (m *delegateMeter) unstored(charge modelapi.Charge, err error) {
	if strings.TrimSpace(m.taskDir) == "" {
		return
	}
	file, openErr := os.OpenFile(filepath.Join(m.taskDir, delegateStderrName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openErr != nil {
		return
	}
	defer file.Close()
	model := strings.TrimSpace(charge.Model)
	if model == "" {
		model = "a model"
	}
	_, _ = fmt.Fprintf(file, "codeaf: a charge of $%.6f for a call on %s is not in this task's spend rows, because the task's record refused it (%v); the machine's spending ledger has it\n",
		charge.CostUSD, model, err)
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
	// THE PROGRAM'S OWN CLOCK: the instant its process was started and the
	// instant it was gone, both zero on every road out of here that never
	// started one. The ending line carries them, so the trajectory holds the
	// same pair the program record does.
	var started, ended time.Time
	end := func(steps int, reason, result string) {
		_ = appendTrajectory(storeDir, task.ID, Step{
			Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Result: result, Reason: reason,
			StartedAt: started, EndedAt: ended,
		})
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
		ctx: ctx, store: w.store, taskID: task.ID, taskDir: taskDir, role: role,
		// The spend row's "model" column carries the program's name, because
		// that is what spent the money; the ledger row names the model that
		// answered.
		name:      "delegate/" + w.program.Name,
		workspace: w.workspace, ledger: w.setup.Ledger,
		conversation: w.setup.Conversation, onCharge: w.setup.OnCharge,
	}
	api, err := modelapi.Open(modelapi.Config{
		TaskDir:      taskDir,
		CompleterFor: w.completerFor(),
		Serves:       w.setup.Serves,
		Seat:         w.setup.Seat,
		Ceiling:      w.cost,
		Bank:         meter.bank,
		Unbilled:     meter.unbilled,
		// THE LIVE STEP GOES WITH THE PROCESS, EVEN WHILE ITS LAST PRICE IS
		// OWED. The API's close waits for a cut call's receipt after the program
		// has exited, and a row reading "implement · running" through that wait
		// would claim a present that is over.
		Settling: func(int) { _ = w.store.ClearLive(task.ID) },
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
	sink := &delegateSink{worker: w, taskID: task.ID, storeDir: storeDir, taskDir: taskDir, name: w.program.Name, stop: stop,
		record: delegate.ProgramRecord{Name: w.program.Name, CeilingUSD: w.cost}}
	brief := strings.TrimSpace(task.Description)
	if brief == "" {
		brief = strings.TrimSpace(task.Title)
	}
	started = time.Now()
	sink.record.StartedAt = started
	result, err := delegate.Run(launchCtx, delegate.Launch{
		Name: w.program.Name,
		Bin:  exe,
		Args: delegate.ChildArgs(w.program, w.workspace, brief, delegate.Ceilings{CostUSD: w.cost, Hours: w.elapsed.Hours()},
			delegate.RunFacts{Plain: w.setup.PlainFolder, Crew: w.setup.Crew}),
		// NO KEY REACHES THE PROGRAM (delegate.ChildEnv): the API's address and
		// token are the whole of what it is given.
		Env:        delegate.ChildEnv(api.API()),
		Dir:        w.workspace,
		StderrPath: filepath.Join(taskDir, delegateStderrName),
		Grace:      w.setup.Grace,
	}, sink)
	// THE INSTANT THE PROCESS WAS GONE, and not the instant its stdout drained
	// ([delegate.Result.ExitedAt] says why; a shell run reads it the same way).
	ended = result.ExitedAt(started, time.Now())
	// THE RECORD IS WRITTEN AGAIN NOW, WHOLE, AND WHETHER OR NOT A HELLO CAME. A
	// program that died before it said hello is still a program this run
	// started, and its page and its row need its times as much as a finished
	// one's do. A child of ANOTHER BUILD is the one exception: it was never this
	// run's program, and it is not written down as one.
	if sink.mismatch == "" {
		sink.record.EndedAt = ended
		_ = delegate.WriteProgram(taskDir, sink.record)
	}
	// The program has exited: its API goes with it, so nothing it left behind
	// can spend, and the calls that were still running write their last turn.
	// THE CLOSE WAITS FOR THE RECEIPTS STILL OWED (modelapi's Server.Close): the
	// call a stop or the ceiling cut in the middle is priced about twenty
	// seconds later, and it has to reach the task's spend rows, the run's total
	// read just below and the conversation's books while all three are open.
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
	var reason string
	switch t.Status {
	case delegate.StatusPass:
		report.Verdict = t.Verdict()
		end(sink.steps, "finished: "+t.Message, report.Result)
		return report, nil
	case delegate.StatusBudget:
		reason = w.program.Name + " stopped on its own ceiling: " + t.Message
	case delegate.StatusCrashed:
		reason = w.program.Name + " crashed: " + t.Message
	default:
		// `fail`, and any word this build does not know, is work that does not
		// stand: the run reads it as incomplete.
		reason = w.program.Name + " did not finish: " + t.Message
	}
	end(sink.steps, reason, report.Result)
	return report, &ProgramEndedError{Status: t.Status, Reason: reason, Result: report.Result}
}

// ProgramEndedError is a program's own ending when it did not finish: the
// status word its terminal record carried, the sentence the task keeps, and
// its account in full. The run carries it to the session whole
// ([Summary.Program]), which draws the row from the fact rather than from the
// generic "ran and did not finish" — the row that said only that, over an hour
// of work that had submitted a change and said exactly why it would not
// stand, told a person nothing they could act on.
type ProgramEndedError struct {
	// Status is the terminal record's word: fail, budget, crashed, or one
	// this build does not know.
	Status string
	// Reason is the one sentence: `senior-dev did not finish: …`.
	Reason string
	// Result is the program's account: its message, what its model claimed
	// and what it observed ([delegateResult]).
	Result string
}

func (e *ProgramEndedError) Error() string { return e.Reason }

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
