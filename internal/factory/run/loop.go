package run

// The loop: the runner's state machine, persisted on the item.
//
// AN ITEM'S STREAM IS WRITTEN HERE AND NOWHERE ELSE. Every transition is one
// store.Update under the item's lock, and only then an [Event], so a surface
// that hears an event and reads the store always reads the move the event
// names. The money is the one exception, and only for Stream.Spent and
// Stream.Activity, which it writes through Annotate (money.go).
//
// One goroutine runs each item that holds a bench. What a door needs to
// reach that goroutine (its cancel, its steer channel, the answer it waits
// on, whether it is paused) is a control in one map under one mutex. A door
// that changes the item writes the store itself, under the control's write
// lock, so a stop can never be undone by a round that was just finishing.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// loopLogMost is how many lines a stream keeps, the newest last: the same
// window the floor keeps, so the stream view scrolls the same way.
const loopLogMost = 400

// habitEvery is how many clean approvals on one repo offer a habit, and the
// activity word a clean approval leaves on the item, which is what is counted.
// THE TWO WORDS ARE STORED (counted off the items' own activity, and written
// to disk), so they keep their old spelling; the screen says approve.
const (
	habitEvery         = 3
	activitySigned     = "signed off"
	activitySignedEdit = "signed off with changes"
)

// errLoopStopped is a write refused because the item was stopped under it.
var errLoopStopped = errors.New("factory run: the item was stopped")

// ErrAlreadyRunning marks a launch refused because the job is already under
// way. The text a person reads stays the job's own sentence ("#12 is already
// running"); this value is only how a door recognizes that refusal.
var ErrAlreadyRunning = errors.New("already running")

// alreadyRunningError keeps that sentence while Unwrap names the marker, so a
// status mapping can see the refusal without rewriting what the floor said.
type alreadyRunningError struct{ sentence string }

func (e alreadyRunningError) Error() string { return e.sentence }

func (e alreadyRunningError) Unwrap() error { return ErrAlreadyRunning }

// AlreadyRunning is the refusal for ref, in the job's own words.
func AlreadyRunning(ref string) error {
	return alreadyRunningError{ref + " is already running"}
}

// floorLoop is the runner's state: every item in flight, the benches and the
// queue for them, and what each item's stages have said so far.
type floorLoop struct {
	r *Runner

	mu    sync.Mutex
	ctls  map[int]*loopCtl
	bench map[int]int // bench number to the item on it
	queue []*loopCtl
	// results are each item's last result per phase index, and notes the
	// sentences its stages, gates and steers left, for the next job. They
	// outlive a control, so a send-back's round still reads what came before.
	results map[int]map[int]factory.StageResult
	notes   map[int][]string
	// extra are phases a send-back appended, by name, which are not in the
	// item's stages: their ask is kept here because a phase's note changes.
	extra map[int]map[string]factory.Stage

	// sayMu keeps each item's manager lines in order (manager.go): owed are
	// the lines a conversation could not take yet, and retrying says a retry
	// is already on its way.
	sayMu    sync.Mutex
	owed     map[int][]owedLine
	retrying map[int]bool
}

// loopCtl is one item's controls, reached by the doors.
type loopCtl struct {
	id     int
	ctx    context.Context
	cancel context.CancelFunc
	// cause cancels ctx with why; a person's Stop cancels it with
	// [factory.ErrPersonStopped] first ([StoppedHere]), and every other end
	// (a withdrawn lease, the engine closing) with none.
	cause           context.CancelCauseFunc
	spending        sync.WaitGroup
	steer           chan string
	modelSelections chan ModelSelection
	modelMu         sync.Mutex
	answers         chan loopAnswer
	// wmu holds a write and the check that the item was not stopped as one
	// step; askMu keeps one question at a time.
	wmu   sync.Mutex
	askMu sync.Mutex
	// pauseMu keeps pause writes and paid round admission in order without
	// taking floorLoop.mu inside a store callback.
	pauseMu sync.Mutex
	// reverify says the control runs the checks again, holding no bench.
	reverify bool
	// hearMu keeps one reading of the manager conversation at a time
	// (manager.go). phase, phaseAt and phaseSpent are the phase whose first
	// round started last, when, and what the item had spent then, for its
	// done line; only the item's own goroutine touches them.
	hearMu     sync.Mutex
	phase      int
	phaseAt    time.Time
	phaseSpent float64
	// inboxMu keeps one step's question at a time with the manager and the
	// person (inbox.go).
	inboxMu sync.Mutex

	// Guarded by floorLoop.mu.
	pending     string // the question the item waits on: "" is none
	paused      bool
	roundCancel context.CancelFunc
	// opened is the chat the round now running opened, and carry the chat a
	// pause cut short, which the next round continues rather than replaces.
	opened       string
	carry        string
	benchNo      int
	released     bool
	releasedDone chan struct{}
	// finishing says the final proof write is in progress, under floorLoop.mu.
	finishing bool
}

type loopAnswer struct {
	yes   bool
	words string
}

// loop is the runner's state machine, made on first use.
func (r *Runner) loop() *floorLoop {
	r.loopOnce.Do(func() {
		r.floor = &floorLoop{
			r:        r,
			ctls:     map[int]*loopCtl{},
			bench:    map[int]int{},
			results:  map[int]map[int]factory.StageResult{},
			notes:    map[int][]string{},
			extra:    map[int]map[string]factory.Stage{},
			owed:     map[int][]owedLine{},
			retrying: map[int]bool{},
		}
	})
	return r.floor
}

func (r *Runner) now() time.Time {
	if r.opts.Clock != nil {
		return r.opts.Clock()
	}
	return time.Now()
}

// Launch puts an item on a bench, or in the queue when every bench is taken.
func (r *Runner) Launch(id int) error {
	st := r.opts.Store
	if st == nil {
		return errors.New("factory run: no store")
	}
	lp := r.loop()
	it, err := st.Get(id)
	if err != nil {
		return err
	}
	if err := factory.CheckPersonStop(it); err != nil {
		return err
	}
	// A PAUSED ITEM'S LAUNCH IS ITS RUN: the one top-bar button resumes it.
	if done, err := r.resumePaused(it); done || err != nil {
		return err
	}
	if err := launchable(it); err != nil {
		return err
	}
	if r.opts.Rail != nil && r.opts.SpentToday != nil {
		if rail := r.opts.Rail(); rail > 0 && r.opts.SpentToday() >= rail {
			return fmt.Errorf("the day rail is %s and today's spend has reached it", loopUSD(rail))
		}
	}
	if err := strangerWrites(it); err != nil {
		if saveErr := st.Update(id, func(it *factory.Item) error {
			if launchable(*it) == nil {
				if refusal := strangerWrites(*it); refusal != nil {
					var denied WriteAccessRefusal
					if errors.As(refusal, &denied) {
						factory.StampFailure(it, denied.Failure(*it, r.now()))
					}
				}
			}
			return nil
		}); saveErr != nil {
			return saveErr
		}
		return err
	}
	lp.mu.Lock()
	if lp.ctls[id] != nil {
		lp.mu.Unlock()
		return AlreadyRunning(it.Ref())
	}
	ctx, cause := context.WithCancelCause(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: func() { cause(nil) }, cause: cause, steer: make(chan string, 16), modelSelections: make(chan ModelSelection, 1), answers: make(chan loopAnswer, 1), phase: -1}
	lp.ctls[id] = c
	lp.results[id] = map[int]factory.StageResult{}
	lp.notes[id] = nil
	delete(lp.extra, id)
	lp.mu.Unlock()

	// THE MANAGER IS MADE BEFORE THE RUN IS, and what the person told it
	// before this run is the run's brief: it goes on the item's notes, which
	// open every stage's brief (manager.go).
	talk, unmade := r.manage(it)
	brief := r.brief(talk, it.Heard)
	now := r.now()
	err = lp.move(c, "", EventQueued, "queued", func(it *factory.Item) error {
		if err := launchable(*it); err != nil {
			return err
		}
		if strings.TrimSpace(it.Talk) == "" && talk != "" {
			it.Talk = talk
		}
		for _, h := range brief {
			if !h.At.After(it.Heard) {
				continue
			}
			if words := strings.TrimSpace(h.Words); words != "" && !factory.StatusOnlyRequest(words) {
				it.Notes = append(it.Notes, words)
			}
			it.Heard = h.At
		}
		it.State = factory.StateQueued
		it.Failure = nil
		it.DoneHow = ""
		it.DoneAt = time.Time{}
		if it.Approval == nil || it.Approval.State != "approving" {
			it.Approval = nil
		}
		it.StopUndo = nil
		it.PublishStatus, it.Publishing = nil, false
		it.PublishError = ""
		it.Question, it.QKind = "", ""
		it.Proof, it.Policy = nil, nil
		if it.Stream != nil && begun(it.Stream) {
			factory.ClearJobRun(it)
		}
		it.Stream = freshStream(it, now)
		if strings.TrimSpace(it.RunOn) != "" {
			// An AgentField job's steps are its run's calls, made as they come.
			// The pipeline it was filed with goes too: a step left there would
			// be made into a phase with its stock words, and a run step of the
			// same name (`plan`) would wear them.
			it.Stages, it.Stream.Phases, it.AgentField = nil, nil, nil
		}
		loopSay(it, now, "said", "queued")
		loopSay(it, now, "fail", unmade)
		return nil
	})
	if err != nil {
		lp.forget(c)
		return err
	}
	lp.enqueue(c)
	return nil
}

const refuseStrangerWrite = factory.WriteAccessRefusalWords

// WriteAccessRefusal stops a stranger's job before a step that can write.
type WriteAccessRefusal struct {
	Step   string
	Reason string
	before *factory.Phase
}

func (e WriteAccessRefusal) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return refuseStrangerWrite
}

// Failure keeps prior rounds without counting the refusal as an attempt.
// Lookup retry details stay in memory; the saved refusal keeps its usual words.
func (e WriteAccessRefusal) Failure(it factory.Item, at time.Time) factory.Failure {
	attempts := 0
	var phases []factory.Phase
	if it.Stream != nil {
		phases = it.Stream.Phases
	}
	if e.before != nil {
		phases = []factory.Phase{*e.before}
	}
	for _, ph := range phases {
		if ph.Name == e.Step {
			attempts = ph.Round
			// A pending phase can already name its next, untried round.
			if ph.State == factory.PhasePending {
				attempts = max(0, attempts-1)
			}
			if ph.Failure != nil && ph.Failure.Detail == refuseStrangerWrite {
				attempts = ph.Failure.Attempts
			}
			break
		}
	}
	return factory.Failure{Step: e.Step, Code: factory.CodeToolError, Detail: refuseStrangerWrite, Attempts: attempts, Machine: it.WorktreeMachine, Next: factory.FailureStopped, At: at}
}

func strangerStage(it factory.Item, st factory.Stage) error {
	if st.CanWrite() {
		return CheckWriteAccess(it, st.Name)
	}
	return nil
}

func strangerWrites(it factory.Item) error {
	for _, st := range it.Stages {
		if runsIn(it, st) {
			if err := strangerStage(it, st); err != nil {
				return err
			}
		}
	}
	return nil
}

func (lp *floorLoop) allowRound(c *loopCtl, st factory.Stage) bool {
	it, err := lp.r.opts.Store.Get(c.id)
	if err == nil {
		err = lp.r.allowWrite(c.ctx, it, st)
	}
	if err != nil {
		lp.failStop(c, st.Name, err)
		return false
	}
	return true
}

// launchable says whether an item may be launched from where it stands: new,
// dismissed, or never put anywhere yet. A dropped job is refused with its way
// back ([factory.RefuseDropped]).
func launchable(it factory.Item) error {
	if err := factory.CheckPersonStop(it); err != nil {
		return err
	}
	if err := factory.RequireIssueJob(it); err != nil {
		return err
	}
	if err := factory.RefuseDropped(it); err != nil {
		return err
	}
	if it.State == factory.StateNeedsYou && it.QKind == factory.QKindAskFirst {
		return nil
	}
	switch it.State {
	case factory.StateNew, factory.StateDismissed, "":
		return nil
	case factory.StateLanded:
		if it.PublishStatus != nil && it.PublishStatus.State == "failed" {
			return nil
		}
		return fmt.Errorf("%s has landed · approve it, or request changes", it.Ref())
	case factory.StateShipped:
		return fmt.Errorf("%s has shipped", it.Ref())
	}
	return AlreadyRunning(it.Ref())
}

// loopPhases compiles an item's stages into its phases, in order: a stage
// that is on and fits the item is a pending phase. File conditions stay
// pending until their turn; other nonmatching stages are left out. The tail is re-read at each round: what has not started yet is
// compiled again from the item's stages as they stand then ([tailPhases]), so
// the manager's edit during a run (its own `factory_run`) reaches the stages
// after it.
func loopPhases(it factory.Item) []factory.Phase {
	var out []factory.Phase
	for _, s := range it.Stages {
		if !runsIn(it, s) {
			continue
		}
		out = append(out, factory.Phase{Name: s.Name, Kind: kindOf(s), State: factory.PhasePending, Note: s.Ask})
	}
	return out
}

// runsIn says whether stage s belongs in the run, retaining file conditions
// until their turn: on and fitting it, or
// the plan's approve step turned off while the rules the job started under
// guard paths ([forcedGate]).
func runsIn(it factory.Item, s factory.Stage) bool {
	return (s.On || forcedGate(it, s)) && (factory.FileCondition(s) || factory.Fits(s, it))
}

// forcedGate says whether s is the plan's approve step ([factory.PlanGateStage])
// turned off under rules that guard paths ([pipeline.GuardsPaths]). TURNING
// THE APPROVAL OFF DOES NOT TURN A RULE OFF: the step runs anyway, and passes
// on its own unless the plan touches what a rule guards ([floorLoop.approve]).
func forcedGate(it factory.Item, s factory.Stage) bool {
	if s.On || !factory.IsApprove(s) || !pipeline.GuardsPaths(it.StartRules) {
		return false
	}
	g := factory.PlanGateStage(it.Stages)
	return g >= 0 && it.Stages[g].Name == s.Name
}

func kindOf(s factory.Stage) factory.StageKind {
	if s.Kind == "" {
		return factory.StageChat
	}
	return s.Kind
}

// enqueue puts a control in the queue and starts whatever the benches allow.
func (lp *floorLoop) enqueue(c *loopCtl) {
	lp.mu.Lock()
	lp.queue = append(lp.queue, c)
	lp.mu.Unlock()
	lp.pump()
}

// pump hands free benches to the queue, first come first served. Benches 0
// is unbounded, and every item still carries the lowest bench number free.
func (lp *floorLoop) pump() {
	var start []*loopCtl
	lp.mu.Lock()
	for len(lp.queue) > 0 {
		if n := lp.r.opts.Benches; n > 0 && len(lp.bench) >= n {
			break
		}
		c := lp.queue[0]
		lp.queue = lp.queue[1:]
		if c.released || c.ctx.Err() != nil {
			continue
		}
		b := 1
		for lp.bench[b] != 0 {
			b++
		}
		lp.bench[b] = c.id
		c.benchNo = b
		start = append(start, c)
	}
	lp.mu.Unlock()
	for _, c := range start {
		go lp.drive(c)
	}
}

// release gives the item's bench back and forgets its control, once.
func (lp *floorLoop) release(c *loopCtl) {
	lp.mu.Lock()
	if c.released {
		lp.mu.Unlock()
		return
	}
	c.released = true
	if c.releasedDone != nil {
		close(c.releasedDone)
	}
	if c.benchNo > 0 && lp.bench[c.benchNo] == c.id {
		delete(lp.bench, c.benchNo)
	}
	if lp.ctls[c.id] == c {
		delete(lp.ctls, c.id)
	}
	for i, q := range lp.queue {
		if q == c {
			lp.queue = append(lp.queue[:i:i], lp.queue[i+1:]...)
			break
		}
	}
	lp.mu.Unlock()
	lp.pump()
}

// forget drops a control that never reached the queue.
func (lp *floorLoop) forget(c *loopCtl) {
	c.cancel()
	lp.release(c)
}

func (lp *floorLoop) ctl(id int) *loopCtl {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return lp.ctls[id]
}

// emit sends one event without ever holding the loop up.
func (lp *floorLoop) emit(id int, stage string, kind EventKind, text string) {
	ch := lp.r.opts.Events
	if ch == nil {
		return
	}
	select {
	case ch <- Event{Item: id, Stage: stage, Kind: kind, At: lp.r.now(), Text: text}:
	default:
	}
}

// write changes the item under the store's lock, unless the item was
// stopped first. move is write and then its event, as one step.
func (lp *floorLoop) write(c *loopCtl, change func(*factory.Item) error) error {
	return lp.move(c, "", "", "", change)
}

func (lp *floorLoop) move(c *loopCtl, stage string, kind EventKind, text string, change func(*factory.Item) error) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.ctx.Err() != nil {
		return errLoopStopped
	}
	if err := lp.r.opts.Store.Update(c.id, func(it *factory.Item) error {
		if err := factory.CheckPersonStop(*it); err != nil {
			return err
		}
		if kind != EventQueued && it.State == factory.StateNew && it.Stream != nil && !it.Stream.Ended.IsZero() {
			return errLoopStopped
		}
		if it.Stream == nil {
			it.Stream = &factory.Stream{}
		}
		return change(it)
	}); err != nil {
		return err
	}
	if kind != "" {
		lp.emit(c.id, stage, kind, text)
	}
	return nil
}

// loopSay appends one line to the item's stream, stamped by the runner.
func loopSay(it *factory.Item, at time.Time, tone, text string) {
	text = strings.TrimSpace(text)
	if it.Stream == nil || text == "" {
		return
	}
	it.Stream.Log = append(it.Stream.Log, factory.LogLine{At: at, Tone: tone, Text: text})
	if over := len(it.Stream.Log) - loopLogMost; over > 0 {
		it.Stream.Log = append([]factory.LogLine(nil), it.Stream.Log[over:]...)
	}
}

// say is one log line written on its own.
func (lp *floorLoop) say(c *loopCtl, tone, text string) {
	now := lp.r.now()
	_ = lp.write(c, func(it *factory.Item) error {
		loopSay(it, now, tone, text)
		return nil
	})
}

// drive is the item's goroutine: it takes the bench, runs each phase in
// order, and lands the item.
func (lp *floorLoop) drive(c *loopCtl) {
	defer lp.release(c)
	defer c.settleSpend()
	if c.reverify {
		lp.reverifyRun(c)
		return
	}
	if err := lp.write(c, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Stream.Bench = c.benchNo
		return nil
	}); err != nil {
		return
	}
	// A JOB RUN BY AN AGENTFIELD REASONER is handed to that run whole: its
	// steps are the run's own calls, written by Remote as they happen.
	if it, err := lp.r.opts.Store.Get(c.id); err == nil && strings.TrimSpace(it.RunOn) != "" {
		lp.driveRemote(c)
		return
	}
	// THE FIRST STAGE RUNS FIRST, with the item's stages as they stand. No
	// manager turn comes before it (Options says why, at ShapeWait).
	if lp.r.opts.Talk != nil {
		watching := make(chan struct{})
		defer close(watching)
		go lp.watchTalk(c, watching)
	}
	for {
		lp.retailTail(c)
		it, err := lp.r.opts.Store.Get(c.id)
		if err != nil || it.Stream == nil {
			return
		}
		if !lp.stopSpending(c, it) {
			return
		}
		i := nextPhase(it.Stream.Phases)
		if i >= 0 && it.Stream.Phases[i].Name == factory.FollowPRStep && it.PullRequest == nil {
			lp.land(c)
			return
		}

		if i < 0 {
			// A PIPELINE THAT ONLY RUNS ON AGENTFIELD ENDS AS ITS RUNS DO: it
			// made no change here for a person to approve, so there is no
			// proof to land on (a pull request its run opened is the job's).
			if factory.AgentFieldPipeline(it) {
				lp.shipAgentField(c)
				return
			}
			lp.land(c)
			return
		}
		if !lp.runPhase(c, i) {
			return
		}
	}
}

// nextPhase is the first phase not yet finished, or -1. A phase left failed
// is one a person said to skip, and is finished too.
func nextPhase(ph []factory.Phase) int {
	for i, p := range ph {
		if p.State != factory.PhaseDone && p.State != factory.PhaseSkipped && p.State != factory.PhaseFailed {
			return i
		}
	}
	return -1
}

// stageAt is the stage behind phase i: the item's stage of that name, or the
// stage a send-back made.
func (lp *floorLoop) stageAt(it factory.Item, i int) (factory.Stage, int) {
	ph := it.Stream.Phases[i]
	if j := factory.StageIndex(it.Stages, ph.Name); j >= 0 {
		return it.Stages[j], j
	}
	lp.mu.Lock()
	s, ok := lp.extra[it.ID][ph.Name]
	lp.mu.Unlock()
	if !ok {
		s = factory.Stage{Name: ph.Name, Kind: ph.Kind, Ask: ph.Note, Until: factory.UntilDone, On: true}
	}
	return s, -1
}

// ask parks the item on a question and waits for the person. The phase
// takes state (waiting, or failed with note when the stage could not go on),
// and the item needs you. It answers false when the item was stopped.
//
// said is the manager's line for the question (manager.go), "" for the plain
// `asking you: <question>`; the answer is said back to it as `answered: …`.
func (lp *floorLoop) ask(c *loopCtl, i int, pending, qkind, question string, state factory.PhaseState, note, said string) (loopAnswer, bool) {
	return lp.askIn(c.ctx, c, i, pending, qkind, question, state, note, said)
}

// askIn is [floorLoop.ask] that also stops waiting when ctx ends: a step's
// question (inbox.go) waits under its round's ctx, which a pause cuts. A
// question let go so is no longer pending, and an answer already on its way
// is kept for the step's next round ([floorLoop.keepAnswer]); the item still
// shows the question, which [Runner.Answer] takes after.
func (lp *floorLoop) askIn(ctx context.Context, c *loopCtl, i int, pending, qkind, question string, state factory.PhaseState, note, said string) (loopAnswer, bool) {
	c.askMu.Lock()
	defer c.askMu.Unlock()
	lp.mu.Lock()
	c.pending = pending
	lp.mu.Unlock()
	now := lp.r.now()
	name, round := "", 0
	err := lp.write(c, func(it *factory.Item) error {
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = question, qkind
		if i >= 0 && i < len(it.Stream.Phases) {
			ph := &it.Stream.Phases[i]
			name, round = ph.Name, ph.Round
			setPhaseState(ph, state, now)
			if qkind == factory.QKindApprove {
				// The question belongs to the approve step itself, so the report
				// the worker sends names the gate and not the step before it.
				// A worker still cannot pass the gate: only the person's door does.
				it.Stream.Cur = i
			}
			if note != "" {
				ph.Note = note
				loopSay(it, now, "fail", note)
			}
		}
		loopSay(it, now, "ask", question)
		return nil
	})
	if err != nil {
		lp.mu.Lock()
		c.pending = ""
		lp.mu.Unlock()
		return loopAnswer{}, false
	}
	if state == factory.PhaseFailed {
		lp.emit(c.id, name, EventFailed, note)
	}
	lp.emit(c.id, name, EventWaiting, question)
	if said == "" {
		said = askLine("", question)
	}
	// THE QUESTION IS THE STEP'S when it names one: the step waits on it (or
	// failed into it), and the line says which.
	var step *factory.StepProgress
	if name != "" {
		event := factory.ProgressWaiting
		if state == factory.PhaseFailed {
			event = factory.ProgressFailed
		}
		step = &factory.StepProgress{Step: name, Round: round, Event: event}
	}
	lp.tellStep(c.id, said, step)
	var a loopAnswer
	select {
	case a = <-c.answers:
	case <-c.ctx.Done():
		return loopAnswer{}, false
	case <-ctx.Done():
		lp.letGoAsk(c)
		return loopAnswer{}, false
	}
	lp.tell(c.id, fmt.Sprintf(sayAnswered, answeredWord(a)))
	said = "no"
	switch {
	case a.words != "":
		said = "noted: " + a.words
	case a.yes:
		said = "yes · carrying on"
	}
	err = lp.write(c, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Question, it.QKind = "", ""
		if i >= 0 && i < len(it.Stream.Phases) {
			setPhaseState(&it.Stream.Phases[i], factory.PhaseRunning, lp.r.now())
		}
		loopSay(it, lp.r.now(), "said", said)
		return nil
	})
	return a, err == nil
}

// approve holds the run at the approve step i until the person answers
// (approve.go in internal/factory): yes is continue, and words with it are
// handed to the steps after as the person's note; no sends the run back to
// the step before with the words. The question stands on the item (pending,
// needs you) and the manager is told, so it can explain what it waits on.
//
// AN APPROVE STEP WITH NOTHING AFTER IT IS THE LANDING: it passes quietly and
// the item lands, where the proof sheet's approval is the person's answer.
// One with no step before it has nothing to send back to, and no stops.
func (lp *floorLoop) approve(c *loopCtl, i int, name string) bool {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil || i >= len(it.Stream.Phases) {
		return false
	}
	ph := it.Stream.Phases
	prev := factory.PreviousWork(ph, i)
	last := true
	for _, p := range ph[i+1:] {
		if p.State != factory.PhaseDone && p.State != factory.PhaseSkipped && p.State != factory.PhaseFailed {
			last = false
			break
		}
	}
	// Even Auto gates wait for the service's recorded decision. A worker's
	// cached mode cannot bypass a Manual switch committed on the factory.
	//
	// THE LANDING IS NOT A PASS. This step once marked itself done here, and
	// on a worker (which never reports an approve step) it stayed "to come"
	// while Auto published past it. It now lands with the step waiting
	// ([factory.LandingGate]), and only the proof approval passes it. One
	// switched off still passes quietly, as if it were not there.
	if last {
		if st, index := lp.stageAt(it, i); index >= 0 && !st.On {
			return lp.finish(c, i, name, "")
		}
		lp.land(c)
		return false
	}
	prevName := ""
	if prev >= 0 {
		prevName = ph[prev].Name
	}
	// THE PLAN IS CHECKED AGAINST THE RULES THE JOB STARTED UNDER: a planned
	// file that an ask-first path or a never path names makes this approval a
	// person's, even in Auto ([factory.AutonomyBlockReason]). The plan's
	// approval is the one after the plan, whatever the step before it is
	// called ([factory.PlanGate]).
	hold := ""
	if factory.PlanGate(ph) == i {
		hold = pipeline.PlanHold(it.StartRules, factory.PlanFilesOf(it))
	}
	if hold != it.RuleHold {
		if lp.write(c, func(saved *factory.Item) error { saved.RuleHold = hold; return nil }) != nil {
			return false
		}
		it.RuleHold = hold
	}
	st, index := lp.stageAt(it, i)
	// AN APPROVAL TURNED OFF RUNS ONLY FOR THE RULES ([forcedGate]): with
	// nothing they guard in the plan it passes as if it were not there.
	if index >= 0 && !st.On && hold == "" {
		return lp.finish(c, i, name, "")
	}
	said := ""
	if factory.Automatic(it) && hold == "" {
		// WHAT THE MANAGER SAYS IS WHO MAY PASS IT ([factory.PassOf]).
		switch kind, cond := factory.PassOf(st); {
		case st.Fixed:
		case kind == factory.PassPerson:
			said = "Only a person can pass this approval after " + prevName + "; it waits for them."
		case kind == factory.PassManagerWhen:
			said = "The manager passes this approval after " + prevName + " when " + strings.TrimSuffix(cond, ".") + "."
		default:
			said = "The manager may pass this approval to continue after " + prevName + "."
		}
	}
	question := factory.ApproveQuestion(prevName)
	if hold != "" && it.StartRules != nil && it.StartRules.UnreadWhy != "" {
		question += " · " + it.StartRules.UnreadWhy
	}
	a, ok := lp.ask(c, i, "approve", factory.QKindApprove, question, factory.PhaseWaiting, "", said)
	if !ok {
		return false
	}
	switch {
	case a.yes:
		if hold != "" {
			_ = lp.write(c, func(saved *factory.Item) error { saved.RuleHold = ""; return nil })
		}
		lp.addNote(c.id, a.words)
		if !lp.finish(c, i, name, "") {
			return false
		}
		// WHO PASSED IT is kept on the step, from the door that answered.
		_ = lp.write(c, func(saved *factory.Item) error {
			if i < len(saved.Stream.Phases) && saved.Stream.Phases[i].State == factory.PhaseDone {
				// A worker's copy never hears who passed it (the service's
				// does, and says so); only a door that said so is said here.
				said := saved.Passing != nil
				factory.StampPassed(saved, i)
				if said {
					loopSay(saved, lp.r.now(), "ok", passedLine(name, saved.Stream.Phases[i].Passed))
				}
			}
			return nil
		})
		return true
	case prev < 0:
		// NOTHING TO SEND BACK TO: the no is a rejection, and it ends the job
		// ([factory.Item.EndRejected]) rather than leaving it stopped with a
		// carry-on that would only ask the same question again.
		_ = lp.write(c, func(saved *factory.Item) error { saved.Passing = nil; return nil })
		lp.reject(c, a.words)
		return false
	}
	_ = lp.write(c, func(saved *factory.Item) error { saved.Passing = nil; return nil })
	return lp.back(c, i, prev, a.words)
}

// askBeforeStep is Manual's word before step i. A yes leaves the step running,
// so the drive loop's next pass runs it without asking again; a no sends back
// the work before it. Either way the drive loop carries on from the store.
func (lp *floorLoop) askBeforeStep(c *loopCtl, i int, name string) bool {
	a, ok := lp.ask(c, i, "approve", factory.QKindApprove, factory.NextStepQuestion(name), factory.PhaseWaiting, "", "")
	if !ok {
		return false
	}
	if a.yes {
		lp.addNote(c.id, a.words)
		return true
	}
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil {
		return false
	}
	return lp.back(c, i, factory.PreviousWork(it.Stream.Phases, i), a.words)
}

// back sends the run from the approve step at i to the step at prev with the
// person's words: prev runs again in a new round, its loop whole, with the
// words in its brief, and the approve step waits again after it.
func (lp *floorLoop) back(c *loopCtl, i, prev int, words string) bool {
	lp.addNote(c.id, words)
	now := lp.r.now()
	name := ""
	err := lp.write(c, func(it *factory.Item) error {
		name = rewindToStep(it, i, prev, words, now)
		return nil
	})
	if err != nil {
		return false
	}
	c.phase = -1
	lp.emit(c.id, name, EventRound, backLine(name, words))
	lp.tell(c.id, backLine(name, words))
	return true
}

// rewindToStep is the stored half of back, shared with the door that answers a
// no when no runner holds the job (person.go): step prev and the approve step
// at i wait again, prev in a new round, and the run's current step is prev.
// It answers prev's name.
func rewindToStep(it *factory.Item, i, prev int, words string, now time.Time) string {
	it.PublishAnyway = nil
	for _, k := range []int{prev, i} {
		setPhaseState(&it.Stream.Phases[k], factory.PhasePending, now)
		it.Stream.Phases[k].Left = 0
	}
	ph := &it.Stream.Phases[prev]
	ph.Round = max(ph.Round, 1) + 1
	grantFreshLoop(it, ph)
	it.Stream.Cur = prev
	loopSay(it, now, "said", backLine(ph.Name, words))
	return ph.Name
}

// backLine is `sent back to plan: use the other file`, or `sent back to plan`.
func backLine(name, words string) string {
	if words = strings.TrimSpace(words); words != "" {
		return fmt.Sprintf(sayBack, name) + ": " + words
	}
	return fmt.Sprintf(sayBack, name)
}

func (lp *floorLoop) addNote(id int, words string) {
	if words = strings.TrimSpace(words); words == "" {
		return
	}
	lp.mu.Lock()
	lp.notes[id] = append(lp.notes[id], words)
	lp.mu.Unlock()
}

// finish marks phase i done, with an event.
func (lp *floorLoop) finish(c *loopCtl, i int, name, note string) bool {
	spent := 0.0
	round := 0
	now := lp.r.now()
	err := lp.move(c, name, EventDone, note, func(it *factory.Item) error {
		if i < len(it.Stream.Phases) {
			setPhaseState(&it.Stream.Phases[i], factory.PhaseDone, now)
			it.Stream.Phases[i].Left = 0
			if note != "" {
				it.Stream.Phases[i].Note = note
			}
			round = it.Stream.Phases[i].Round
		}
		spent = it.Stream.Spent
		return nil
	})
	if err != nil {
		return false
	}
	// THE MANAGER HEARS EVERY STAGE THAT ENDS: how long it took, what it cost
	// and what it said, `plan done · 2m · $0.04 · …`.
	var took time.Duration
	cost := 0.0
	if c.phase == i && !c.phaseAt.IsZero() {
		took = lp.r.now().Sub(c.phaseAt)
		cost = spent - c.phaseSpent
	}
	lp.mu.Lock()
	res := lp.results[c.id][i]
	lp.mu.Unlock()
	lp.tellStep(c.id, doneLine(name, took, cost, lp.summary(res)), &factory.StepProgress{Step: name, Round: round, Event: factory.ProgressDone})
	return true
}

// skip leaves phase i failed and goes on, as the person said.
func (lp *floorLoop) skip(c *loopCtl, i int, name string) bool {
	now := lp.r.now()
	return lp.write(c, func(it *factory.Item) error {
		if i < len(it.Stream.Phases) {
			setPhaseState(&it.Stream.Phases[i], factory.PhaseFailed, now)
		}
		loopSay(it, lp.r.now(), "said", "skipping "+name)
		return nil
	}) == nil
}

// runPhase runs one phase to its end: an approve step's answer, or rounds until the
// stage's until is met, or the person says how to go on. It answers false
// when the item stopped.
func (lp *floorLoop) runPhase(c *loopCtl, i int) bool {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil || i >= len(it.Stream.Phases) {
		return false
	}
	st, index := lp.stageAt(it, i)
	name := st.Name

	// File conditions are decided only at the step, after earlier rounds have
	// recorded their change. A skipped phase is finished and keeps its reason.
	if factory.FileCondition(st) && !factory.Fits(st, it) {
		reason := "skipping " + name + ": when " + st.When + " does not match the change files"
		if (it.Change == nil || len(it.Change.Files) == 0) && len(it.Files) == 0 {
			reason = "skipping " + name + ": when " + st.When + " does not match the triage area"
		}
		now := lp.r.now()
		return lp.write(c, func(it *factory.Item) error {
			ph := &it.Stream.Phases[i]
			skipPhase(it, ph, reason, now)
			lp.mu.Lock()
			delete(lp.results[c.id], i)
			lp.mu.Unlock()
			it.Stream.Cur = i
			loopSay(it, now, "said", reason)
			return nil
		}) == nil
	}

	// AN APPROVE STEP IS A PERSON, so their answer is the step: nothing runs it.
	if st.Kind == factory.StageGate {
		return lp.approve(c, i, name)
	}
	if !lp.allowRound(c, st) {
		return false
	}
	if factory.AsksBeforeStep(it, i) {
		return lp.askBeforeStep(c, i, name)
	}

	exec := lp.r.opts.Exec[kindOf(st)]
	if exec == nil {
		// A stage this machine cannot run is a failure, not a question.
		lp.failStop(c, name, fmt.Errorf("codeaf cannot run a %s stage here", kindOf(st)))
		return false
	}

	round := max(it.Stream.Phases[i].Round, 1)
	limit := max(st.Max, 1)
	if saved := it.Stream.Phases[i].RoundLimit; saved > 0 {
		limit = max(saved, round)
	} else if round > 1 && it.Stream.Phases[i].State == factory.PhasePending {
		// SENT BACK FROM AN APPROVE STEP ([floorLoop.back]): the step has its
		// whole loop again from the round it starts on.
		limit += round - 1
	}
	for {
		if !lp.held(c) {
			return false
		}
		if !lp.capCheck(c, i, name, -1) {
			return false
		}
		if factory.IsCIRepairStage(st) && !lp.refreshPRRound(c, i) {
			return false
		}
		// WHAT THE PERSON TOLD THE MANAGER SINCE THE LAST ROUND IS THIS
		// ROUND'S STEER, read at the boundary so the brief carries it.
		lp.hear(c)
		if c.ctx.Err() != nil || !lp.allowRound(c, st) {
			return false
		}
		kind, text := EventStarted, fmt.Sprintf("%s · round %d", name, round)
		if round > 1 {
			kind = EventRound
		}
		now := lp.r.now()
		// THE CHAT A PAUSE CUT SHORT IS THIS ROUND'S TO CARRY ON; a round
		// with none starts a chat of its own, and the old name is let go.
		lp.mu.Lock()
		resume := c.carry
		c.carry, c.opened = "", ""
		lp.mu.Unlock()
		var carried []string
		var before factory.Phase
		if err := lp.move(c, name, kind, text, func(it *factory.Item) error {
			it.State = factory.StateRunning
			if it.FollowUp != nil && (it.FollowUp.State == "queued" || it.FollowUp.State == "running" || factory.IsCIRepairStage(st)) {
				f := *it.FollowUp
				f.State = "running"
				if strings.HasPrefix(f.Reason, "Waiting for a runner to retry ") {
					f.Reason = "Retrying Follow PR: inspecting CI, review feedback and conflicts."
				}
				it.FollowUp = &f
			}
			it.Stream.Cur = i
			// A QUESTION THE ROUND BEFORE ASKED AND NOBODY ANSWERED went with
			// that round (a pause cut it): the step asks again if it must.
			dropStaleAsk(it)
			ph := &it.Stream.Phases[i]
			before = *ph
			setPhaseState(ph, factory.PhaseRunning, now)
			ph.Round = round
			ph.RoundLimit = limit
			ph.Verification = nil
			// A ROUND BEGINS WHERE ITS CHAT DOES: a fresh chat is a fresh start,
			// and a round carrying on in the chat a pause cut short keeps the
			// start it had (one that never recorded one takes this one).
			if resume == "" || ph.Started.IsZero() {
				ph.Started = now
			}
			// A step that is running again is no longer the failed step.
			// The record keeps the earlier incident.
			ph.Failure = nil
			if it.Failure != nil && it.Failure.Step == name {
				it.Failure = nil
			}
			if resume == "" {
				// A fresh chat reads the answers in its brief's notes.
				ph.Chat = ""
			} else {
				carried = ph.Carry
			}
			ph.Carry = nil
			if round > 1 {
				loopSay(it, now, "thought", text)
			}
			return nil
		}); err != nil {
			return false
		}
		if c.phase != i {
			c.phase, c.phaseAt = i, now
			if cur, err := lp.r.opts.Store.Get(c.id); err == nil && cur.Stream != nil {
				c.phaseSpent = cur.Stream.Spent
			}
			lp.tellStep(c.id, fmt.Sprintf(sayStarted, name), &factory.StepProgress{Step: name, Round: round, Event: factory.ProgressStarted})
		}
		if begin, ok := lp.r.stepBegins.Load(c.id); ok {
			step := factory.StageReport{Name: name, State: factory.PhaseRunning, Round: round}
			if err := begin.(func(context.Context, factory.StageReport) error)(c.ctx, step); err != nil {
				lp.r.Withdraw(c.id)
				return false
			}
		}
		job := lp.job(c, st, index, i, round)
		job.Resume, job.Opened, job.CarryOn = resume, lp.opened(c, i), carried
		if job.Progress != nil {
			job.Progress(followProgressStart(job))
		}
		var res factory.StageResult
		var again bool
		var err error
		if cached := cachedCheckoutRefusal(job); cached != nil {
			res = *cached
		} else {
			res, again, err = lp.round(c, exec, job, before)
		}
		if c.ctx.Err() != nil {
			return false
		}
		if again {
			continue
		}
		if err != nil {
			// A step that errored is a failure, not a question. The job stops,
			// AND WHAT THE STEP REPORTED BEFORE IT FAILED STAYS ON IT: its
			// output, files, claims and notes are the person's to read beside
			// the failure (a link the step reported among them).
			lp.keepFailedResult(c, i, res)
			lp.failStop(c, name, err)
			lp.summarizeFailure(c.id, res.Summary)
			return false
		}
		if res.Spent > 0 && !res.SpendRecorded {
			spent := 0.0
			if job.Item.Stream != nil {
				for _, ph := range job.Item.Stream.Phases {
					if ph.Name == st.Name {
						spent = ph.RoundCosts[round]
						break
					}
				}
			}
			job.Spend(max(0, res.Spent-spent))
		}
		readiness := factory.IsCIRepairStage(st) && job.Item.FollowUp != nil && job.Item.FollowUp.Kind == "readiness"
		if factory.IsCIRepairStage(st) && !readiness {
			res = lp.completedPublishedRepair(job.Item, res)
		}
		if !readiness && factory.IsCIRepairStage(st) && job.Item.PullRequest != nil && job.Item.PullRequest.FeedbackTracked && res.Done && res.Repair != nil && res.Repair.Decision == "no_change" {
			candidate := res
			repair := *res.Repair
			repair.Decision = "fixed"
			candidate.Repair = &repair
			if checked := lp.completedPublishedRepair(job.Item, candidate); checked.Repair.Decision != "no_change" {
				receipts := followRefusalNotes(job, res)
				res.Done, res.Exit = false, 1
				res.Output = "The no-change review needs a complete assessment of current feedback on the clean published head. " + res.Output
				res.Notes = append(res.Notes, verification.FailurePrefix+" "+noChangeReviewRefusal)
				res.Notes = append(res.Notes, receipts...)
			}
		}
		if readiness && (res.Repair == nil || (res.Repair.Decision != "needs_repair" && res.Repair.Decision != "needs_you")) && verificationReason(res) == "" {
			head := ""
			if a := job.Item.FollowUp.Readiness; a != nil {
				head = a.Head
			}
			if why := factory.ReadinessReportFault(head, res); why != "" {
				res.Done, res.Exit = false, 1
				res.Output = "Final review needs correction: " + why + ". " + res.Output
				res.Notes = append(res.Notes, verification.FailurePrefix+" final review report: "+why)
			}
		}
		res = unchangedWrite(st, job, res)
		lp.fold(c, it, st, i, res)
		// Completed work is recorded before the next step asks for admission.
		// Optional memory charges cannot turn this phase back into waiting.
		memoryBefore := -1.0
		if factory.Met(st, res) {
			memoryBefore = job.Item.MemorySpent
		}
		if !lp.capCheck(c, i, name, memoryBefore) {
			return false
		}
		if res.JobBudgetStopped {
			// capCheck returned only after the person gave this job more room.
			// Resume the interrupted chat, without spending an automatic retry.
			lp.mu.Lock()
			c.carry = res.Chat
			lp.mu.Unlock()
			continue
		}
		if res.ExecutionBlocked != "" {
			// Waiting/reporting exhausted its in-round recovery, not a code
			// repair round. The person may retry the same bounded round; passing
			// check evidence stays available to that next conversation.
			_ = lp.write(c, func(cur *factory.Item) error {
				cur.Stream.Phases[i].Verification = &factory.StepVerification{State: "incomplete", Reason: res.Output}
				return nil
			})
			q := name + " verification is incomplete: " + res.Output + " · try again, or stop the job?"
			a, ok := lp.ask(c, i, "rounds", factory.QKindOutOfAttempts, q, factory.PhaseWaiting, res.Output, askLine("", q))
			if !ok {
				return false
			}
			switch factory.OutOfAttemptsChoice(a.yes, a.words) {
			case factory.ChoiceCancel:
				lp.stop(c)
				return false
			case factory.ChoiceSendBack:
				cur, err := lp.r.opts.Store.Get(c.id)
				if err == nil && cur.Stream != nil {
					if prev := factory.PreviousWork(cur.Stream.Phases, i); prev >= 0 {
						return lp.back(c, i, prev, factory.SendBackReason(a.words))
					}
				}
				lp.stop(c)
				return false
			}
			if words := strings.TrimSpace(a.words); words != "" && words != factory.ChoiceTryOnceMore && !strings.EqualFold(words, "yes") {
				lp.addNote(c.id, words)
			}
			continue
		}
		// A SPENDING LIMIT IS A STOP, NOT A SHORTFALL (stagereport.go's
		// [SpendStop]): another round would be refused before it began, so the
		// job stops on the limit's own words and nothing is asked.
		if factory.IsCIRepairStage(st) && res.Repair != nil && res.Repair.Decision == "needs_you" {
			lp.parkCI(c, i, res.Repair.Reason)
			return false
		}
		if readiness {
			if res.Repair != nil && res.Repair.Decision == "needs_repair" && strings.TrimSpace(res.Repair.Reason) != "" {
				if !lp.capCheck(c, i, name, -1) {
					return false
				}
				if e := lp.write(c, func(cur *factory.Item) error {
					f := *cur.FollowUp
					f.Kind = "pr"
					f.Readiness = nil
					f.State = "running"
					f.Request = "Final readiness review found fixable blockers: " + res.Repair.Reason
					f.Reason = "Repairing blockers found by the final readiness review."
					cur.FollowUp = &f
					return nil
				}); e != nil {
					return false
				}
				lp.tell(c.id, "Final readiness review found blockers. Follow PR will repair and verify them: "+res.Repair.Reason)
				round++
				limit = max(limit, round)
				_ = lp.write(c, func(it *factory.Item) error { it.Stream.Phases[i].RoundLimit = limit; return nil })
				continue
			}
			if verificationReason(res) == "" {
				lp.finishReadiness(c, i, res)
				return false
			}
			// Reporting and execution shortfalls use the same bounded round
			// recovery as other steps. They are not an immediate human gate.
		}
		if !readiness && factory.IsCIRepairStage(st) && res.Done && res.Exit == 0 && res.Findings == 0 && verificationReason(res) == "" && res.Repair != nil && res.Repair.Decision == "no_change" {
			fresh, _ := lp.r.opts.Store.Get(c.id)
			if fresh.PullRequest == nil || factory.PRHasRepairableCheckFailure(fresh.PullRequest) || fresh.PullRequest.Conflict {
				lp.parkCI(c, i, "The PR still has failing checks or conflicts and no repair was verified. "+res.Repair.Reason)
			} else {
				_ = lp.move(c, "", EventShipped, "Following PR", func(it *factory.Item) error {
					if it.Question != "" || it.Asking != nil {
						return errors.New("a pending decision already owns this job")
					}
					factory.AcknowledgeReview(it)
					it.State = factory.StateShipped
					it.Question, it.QKind = "", ""
					it.Asking = nil
					it.FollowUp.State, it.FollowUp.Reason, it.FollowUp.Request = "watching", "No additional code changes needed. "+res.Repair.Reason+" Watching CI, review feedback and conflicts; merge remains yours.", ""
					it.Stream.Phases[i].State = factory.PhaseWaiting
					it.Stream.Phases[i].Note = it.FollowUp.Reason
					it.Stream.Ended = lp.r.now()
					return nil
				})
				lp.tell(c.id, "Follow PR: no code change needed. "+res.Repair.Reason+" Watching for new feedback; merge remains yours.")
			}
			return false
		}
		if spendStopped(res) {
			lp.failStop(c, name, errors.New(res.Output))
			return false
		}
		// WHERE THE PERSON READS A STEP BEFORE THE RUN GOES ON IS AN APPROVE
		// STEP after it, never a question the step asks of itself.
		if factory.Met(st, res) {
			return lp.finish(c, i, name, "")
		}
		if reason := verificationReason(res); reason != "" {
			retry := fmt.Sprintf("Verification blocked: %s. Retrying round %d of %d.", reason, round+1, limit)
			if round >= limit {
				retry = "Verification blocked: " + reason + ". Automatic rounds exhausted; waiting for your decision."
			}
			_ = lp.write(c, func(it *factory.Item) error {
				it.Stream.Phases[i].Note = retry
				if it.FollowUp != nil {
					f := *it.FollowUp
					f.Reason = retry
					if round >= limit {
						f.State = "needs_you"
						f.BlockedBy = "verification"
					}
					it.FollowUp = &f
				}
				return nil
			})
			lp.tellStep(c.id, name+": "+retry, &factory.StepProgress{Step: name, Round: round, Event: factory.ProgressFailed})
		}
		if planReportPending(name, &res) && res.Chat != "" {
			lp.mu.Lock()
			c.carry = res.Chat
			lp.mu.Unlock()
		}
		if round >= limit {
			back := false
			if cur, getErr := lp.r.opts.Store.Get(c.id); getErr == nil && cur.Stream != nil {
				back = factory.PreviousWork(cur.Stream.Phases, i) >= 0
			}
			note, why := stepShort(name, st, round, res)
			q := note
			if why != "" {
				q += ": " + why
			}
			q += " · " + stepShortChoices(back)
			if st.Kind == factory.StagePost {
				if reason := skippedPolicyQuestion(job.Item, res); reason != "" {
					q = reason
				}
			}
			// OUT OF ATTEMPTS IS A QUESTION, not a failed step. The phase waits.
			a, ok := lp.ask(c, i, "rounds", factory.QKindOutOfAttempts, q, factory.PhaseWaiting, note, askLine("", q))
			if !ok {
				return false
			}
			switch factory.OutOfAttemptsChoice(a.yes, a.words) {
			case factory.ChoiceSendBack:
				cur, getErr := lp.r.opts.Store.Get(c.id)
				if getErr != nil || cur.Stream == nil {
					return false
				}
				if prev := factory.PreviousWork(cur.Stream.Phases, i); prev >= 0 {
					return lp.back(c, i, prev, factory.SendBackReason(a.words))
				}
				lp.stop(c)
				return false
			case factory.ChoiceCancel:
				lp.stop(c)
				return false
			}
			words := a.words
			if token := strings.ToLower(strings.TrimSpace(words)); token == factory.ChoiceTryOnceMore || token == "yes" {
				words = ""
			}
			lp.addNote(c.id, words)
			round++
			limit = round
			_ = lp.write(c, func(it *factory.Item) error { it.Stream.Phases[i].RoundLimit = limit; return nil })
			continue
		}
		// A FAILING CHECK SENDS THE WORK BACK TO THE CHAT BEFORE IT: running the
		// same command again would fail the same way, so the conversation that
		// made the change gets the failure and another round, and the check runs
		// after it. The check keeps its own count; once its max is spent the
		// person is asked, above.
		if kindOf(st) == factory.StageCheck {
			if prev := lp.chatBefore(c, i); prev >= 0 {
				return lp.checkBack(c, i, prev, round, checkFailure(st, res))
			}
		}
		lp.say(c, "fail", fmt.Sprintf("%s %d/%d: %s · going again", name, round, limit, shortfall(res)))
		round++
	}
}

// chatBefore is the finished conversation step a failing check at i sends
// its work back to: the last finished step before it, when that is a chat;
// -1 otherwise (a check first in its pipeline, or one after another check).
func (lp *floorLoop) chatBefore(c *loopCtl, i int) int {
	cur, err := lp.r.opts.Store.Get(c.id)
	if err != nil || cur.Stream == nil {
		return -1
	}
	prev := factory.PreviousWork(cur.Stream.Phases, i)
	if prev < 0 {
		return -1
	}
	if k := cur.Stream.Phases[prev].Kind; k != "" && k != factory.StageChat {
		return -1
	}
	return prev
}

// checkBack is [floorLoop.back] for a failing check: the chat at prev runs
// again with the failure as its note, and the check's next run is its next
// round, so its max bounds how often the work goes round.
func (lp *floorLoop) checkBack(c *loopCtl, i, prev, round int, words string) bool {
	if lp.write(c, func(it *factory.Item) error {
		it.Stream.Phases[i].Round = round + 1
		return nil
	}) != nil {
		return false
	}
	return lp.back(c, i, prev, words)
}

// checkFailure is what a failing check tells the chat it sends back to: the
// command, how it ended, and the last of what it printed.
func checkFailure(st factory.Stage, res factory.StageResult) string {
	out := strings.TrimSpace(res.Output)
	if lines := strings.Split(out, "\n"); len(lines) > checkBackLines {
		out = strings.Join(lines[len(lines)-checkBackLines:], "\n")
	}
	head := fmt.Sprintf("the check `%s` failed (%s)", strings.TrimSpace(st.Ask), shortfall(res))
	if out == "" {
		return head + "; make it pass"
	}
	return head + "; make it pass. Its output ended:\n" + out
}

// checkBackLines is how much of a failing check's output goes back with it.
const checkBackLines = 20

// stepShort is what the person reads about a step that ended without its
// until holding: the note the step keeps, and why, "" when the note says it.
//
//   - `plan stopped without reporting` — the conversation never called
//     `stage_result`, however it was asked, and no report could be made for
//     it (stagereport.go);
//   - `plan did not finish: <what it said>` — a one-try step that reported it
//     could not;
//   - `test is not proven: 1 of 2 claims shown` — a one-try step whose until
//     did not hold;
//   - `review is not clean after 2 rounds: 2 findings` — a step that loops.
//
// ROUNDS ARE NAMED ONLY WHERE THERE WERE ROUNDS. A plan that runs once read
// `plan is not done after 1 round` on 2026-10-18, which the person took for a
// step that had failed over and over; it had never reported at all.
func stepShort(name string, st factory.Stage, round int, res factory.StageResult) (note, why string) {
	switch {
	case res.Output == stageWalled:
		return name + " ran out of time without reporting", ""
	case strings.HasPrefix(res.Output, stageNoReport):
		return name + " stopped without reporting", strings.TrimPrefix(strings.TrimPrefix(res.Output, stageNoReport), ": ")
	}
	until := strings.TrimSpace(st.Until)
	if until == "" {
		until = factory.UntilDone
	}
	if st.Max > 1 {
		return fmt.Sprintf("%s is not %s after %s", name, until, roundsWord(round)), personShortfall(res)
	}
	if until == factory.UntilDone && !res.Done {
		return name + " did not finish", firstLine(res.Output)
	}
	return name + " is not " + until, personShortfall(res)
}

// personShortfall is [shortfall] as the person's question says it. A CHECK
// THAT WAS NOT CONFIRMED IS COUNTED IN A SENTENCE, NEVER QUOTED: its command
// is a shell line with the worktree's path in it ("cd /Users/…/factory-demo-
// 211 && …"), which the Needs you row printed whole. The commands stay on
// the step's own record ([shortfall]), which its chat and its next round read.
func personShortfall(res factory.StageResult) string {
	n := 0
	for _, note := range res.Notes {
		if strings.HasPrefix(note, verification.FailurePrefix) {
			n++
		}
	}
	switch {
	case n == 1:
		return "1 check it ran was not confirmed to pass"
	case n > 1:
		return strconv.Itoa(n) + " checks it ran were not confirmed to pass"
	}
	return shortfall(res)
}

// stepShortChoices is the end of that question: what the person may do, with
// send it back only when there is a finished step before this one to send the
// work back to. The answers are the same tokens either way
// ([factory.OutOfAttemptOptions]); stopping is the job's own Stop.
func stepShortChoices(back bool) string {
	if back {
		return "try again, send it back, or stop the job?"
	}
	return "try again, or stop the job?"
}

// roundsWord is a count of rounds as a person says it, `1 round` and `2
// rounds`. A NOTE A PERSON READS NEVER SPELLS A PLURAL IN PARENTHESES: `round(s)`
// is the machine admitting it did not count, on a line that has the count.
func roundsWord(n int) string {
	if n == 1 {
		return "1 round"
	}
	return strconv.Itoa(n) + " rounds"
}

// shortfall says in a few words why a round did not meet its until.
func shortfall(res factory.StageResult) string {
	if reason := verificationReason(res); reason != "" {
		return "verification blocked: " + reason
	}
	if !res.Done && strings.HasPrefix(res.Output, sayWriteUnchanged+"\n") {
		return sayWriteUnchanged
	}
	return factory.Shortfall(res)
}

// held ends a paused drive so its bench and worker lease can serve another
// job. The saved stream keeps the interrupted step for the ordinary resume path.
func (lp *floorLoop) held(c *loopCtl) bool {
	c.pauseMu.Lock()
	defer c.pauseMu.Unlock()
	lp.mu.Lock()
	paused := c.paused
	lp.mu.Unlock()
	if paused {
		// Resume must see this drive draining before it can leave the pause lock.
		c.cancel()
	}
	return !paused && c.ctx.Err() == nil
}

// round runs one round under its own ctx, which a pause cancels. again is a
// round a pause cut short: the same round goes on when the item is run
// again, in the same chat when it had one (c.carry).
func (lp *floorLoop) round(c *loopCtl, exec Executor, job Job, before factory.Phase) (res factory.StageResult, again bool, err error) {
	it, err := lp.r.opts.Store.Get(c.id)
	if err == nil {
		err = lp.r.allowWrite(c.ctx, it, job.Stage)
	}
	if err != nil {
		var refused WriteAccessRefusal
		if errors.As(err, &refused) {
			refused.before = &before
			err = refused
		}
		return res, false, err
	}
	rctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	c.pauseMu.Lock()
	lp.mu.Lock()
	if c.paused {
		// Paused before it began: the chat it was to carry on waits for run.
		c.carry = job.Resume
		lp.mu.Unlock()
		c.pauseMu.Unlock()
		return res, true, nil
	}
	c.roundCancel = cancel
	lp.mu.Unlock()
	c.pauseMu.Unlock()
	// The manager can spend while a shell/check stage is running too. Every
	// check executor owns this context. Chat agents use their live family meter
	// so cancellation still returns the round's precise cost and chat identity.
	watchDone, watchExit := make(chan struct{}), make(chan struct{})
	budgetStop := make(chan string, 1)
	var accountingErr error // read only after watchExit closes
	go func() {
		defer close(watchExit)
		if kindOf(job.Stage) != factory.StageCheck {
			return
		}
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-rctx.Done():
				return
			case <-tick.C:
				it, readErr := lp.r.opts.Store.Get(c.id)
				if readErr != nil {
					accountingErr = fmt.Errorf("job budget could not be read: %w", readErr)
					cancel()
					return
				}
				if it.Cap > 0 && factory.JobLifetimeSpent(it) >= it.Cap {
					budgetStop <- fmt.Sprintf(ChatBudgetReached, loopUSD(factory.JobLifetimeSpent(it)), loopUSD(it.Cap))
					cancel()
					return
				}
			}
		}
	}()
	res, err = runIn(rctx, exec, job)
	close(watchDone)
	<-watchExit
	capped := accountingErr != nil
	if accountingErr != nil {
		err = accountingErr
	}
	select {
	case why := <-budgetStop:
		capped = true
		lp.mu.Lock()
		chat := c.opened
		lp.mu.Unlock()
		res.JobBudgetStopped, res.Done, res.Exit = true, false, 1
		res.Output = (SpendStop{Job: true, Said: why}).Error()
		if chat != "" {
			res.Chat = chat
		}
		err = nil
	default:
	}
	if rctx.Err() == nil && job.dirErr == nil {
		lp.recordChange(c, job, res)
	}
	lp.mu.Lock()
	c.roundCancel = nil
	cut := rctx.Err() != nil && !capped
	if cut {
		// THE CUT ROUND'S CHAT IS KEPT, or the one it was carrying on.
		c.carry = c.opened
		if c.carry == "" {
			c.carry = job.Resume
		}
	}
	lp.mu.Unlock()
	if cut && c.ctx.Err() == nil {
		return factory.StageResult{}, true, nil
	}
	return res, false, err
}

// opened is the Job.Opened hook: the round's chat is kept on the item the
// moment it exists, and in the control for a pause to carry on.
func (lp *floorLoop) opened(c *loopCtl, i int) func(string) {
	return func(chat string) {
		if chat = strings.TrimSpace(chat); chat == "" {
			return
		}
		lp.mu.Lock()
		c.opened = chat
		lp.mu.Unlock()
		_ = lp.write(c, func(it *factory.Item) error {
			if i < len(it.Stream.Phases) {
				it.Stream.Phases[i].Chat = chat
			}
			return nil
		})
	}
}

// job is what one round may read.
func (lp *floorLoop) job(c *loopCtl, st factory.Stage, index, i, round int) Job {
	it, _ := lp.r.opts.Store.Get(c.id)
	lp.mu.Lock()
	// THE PERSON'S NOTES COME FIRST: what was settled in the item's own
	// conversation opens every stage's brief, and what earlier stages left
	// follows it in order.
	notes := append(append([]string(nil), it.Notes...), lp.notes[c.id]...)
	// EACH EARLIER RESULT IS NAMED BY ITS OWN STEP, read off the run's phase
	// it came from, so the brief says whose report it is even when an approve
	// step between them reported nothing.
	var prior []factory.StageResult
	var priorNames []string
	for k := 0; k < i; k++ {
		if res, ok := lp.results[c.id][k]; ok {
			prior = append(prior, res)
			name := ""
			if it.Stream != nil && k < len(it.Stream.Phases) {
				name = it.Stream.Phases[k].Name
			}
			priorNames = append(priorNames, name)
		}
	}
	// A ROUND AFTER THE FIRST READS WHAT THE ROUND BEFORE IT FOUND. The
	// results map holds this phase's last result until the round now starting
	// replaces it, so round 2 is handed round 1's findings to fix.
	var last *factory.StageResult
	if res, ok := lp.results[c.id][i]; ok && round > 1 {
		last = &res
	}
	lp.mu.Unlock()
	// THE ROUND RUNS IN THE ITEM'S OWN WORKTREE when the floor makes one
	// (workdir.go), else in the repository's checkout; a worktree that could
	// not be made fails the round rather than running in the person's tree.
	// AN AGENTFIELD STEP WORKS ON ITS NODE, NOT HERE: it gets no folder on
	// this machine, so a run on AgentField never clones the repository here.
	var dir string
	var dirErr error
	if kindOf(st) != factory.StageAgentField {
		dir, dirErr = lp.jobDir(it)
	}
	if dirErr == nil {
		it = lp.settleRules(c, it)
	}
	if dirErr == nil {
		fresh, err := lp.r.opts.Store.Get(c.id)
		dirErr = err
		if dirErr == nil {
			dirErr = lp.r.allowWrite(c.ctx, fresh, st)
		}
	}
	if dirErr == nil && factory.IsCIRepairStage(st) {
		dirErr = lp.reconcilePR(it, dir)
	}
	if dirErr == nil && it.FollowUp != nil && it.FollowUp.Kind == "readiness" && it.FollowUp.Readiness != nil {
		g := lp.r.opts.Git
		if g == nil {
			g = ExecGit{}
		}
		head, e := g.Run(dir, "rev-parse", "HEAD")
		dirErr = e
		if dirErr == nil && strings.TrimSpace(head) != it.FollowUp.Readiness.Head {
			dirErr = errors.New("the readiness checkout diverged from the captured remote head")
		}
		if dirErr == nil {
			status, e := g.Run(dir, "status", "--porcelain")
			dirErr = e
			if dirErr == nil && strings.TrimSpace(status) != "" {
				dirErr = errors.New("the readiness checkout has unfinished local changes")
			}
		}
	}

	// Creating the worktree records its branch. Read that record before the
	// first brief, which otherwise still describes the old default name.
	if dirErr == nil && dir != "" {
		if fresh, err := lp.r.opts.Store.Get(c.id); err == nil && fresh.Stream != nil && it.Stream != nil {
			stream := *it.Stream
			stream.Log = fresh.Stream.Log
			it.Stream = &stream
		}
	}
	if dirErr == nil && dir != "" && it.ChangeBase == "" {
		base, err := changeBase(dir, it)
		if err != nil {
			dirErr = err
		} else if base != "" {
			it.ChangeBase = base
			_ = lp.write(c, func(saved *factory.Item) error { saved.ChangeBase = base; return nil })
		}
	}
	return Job{
		BeforeWrite:     func(ctx context.Context) error { return lp.r.publishBoundary(ctx, c.id) },
		Lifetime:        c.ctx,
		PendingSpend:    c.pendingSpend,
		Item:            it,
		Stage:           st,
		Index:           index,
		Round:           round,
		Notes:           notes,
		Prior:           prior,
		PriorNames:      priorNames,
		Last:            last,
		Dir:             dir,
		Rules:           JobRules(lp.r.opts.Recipe, it),
		dirErr:          dirErr,
		Steer:           c.steer,
		ModelSelections: c.modelSelections,
		Log: func(line string) {
			tone := "thought"
			if strings.HasPrefix(line, factory.ToolCaptionPrefix) {
				tone, line = "tool", strings.TrimPrefix(line, factory.ToolCaptionPrefix)
			}
			lp.say(c, tone, line)
		},
		Progress:     lp.followProgress(c, st, i, round),
		Verification: lp.verificationProgress(c, i, round),
		Spend:        lp.spender(c, st.Name, round),
		Ask: func(ctx context.Context, q factory.Asked) (string, error) {
			return lp.inbox(ctx, c, i, stageLabel(st), q)
		},
	}
}

// spender is the Job.Spend hook: the money's when there is one, else the
// loop's own plain reckoning onto Stream.Spent.
func (lp *floorLoop) spender(c *loopCtl, step string, round int) func(usd float64) {
	if p := lp.r.opts.Pool; p != nil {
		return p.SpendRound(c.id, step, round)
	}
	return func(usd float64) {
		if usd <= 0 {
			return
		}
		_ = lp.r.opts.Store.Annotate(c.id, func(it *factory.Item) error {
			if it.Stream == nil {
				it.Stream = &factory.Stream{}
			}
			spendRound(it, step, round, usd)
			return nil
		})
	}
}

// spendRound writes the job charge and its round receipt atomically, so a
// worker report cannot mistake the interval between them for manager spending.
func spendRound(it *factory.Item, step string, round int, usd float64) {
	addCost(it, usd)
	for i := range it.Stream.Phases {
		ph := &it.Stream.Phases[i]
		if step != "" && ph.Name == step {
			ph.RecordRoundSpend(round, ph.RoundCosts[round]+usd)
			break
		}
	}
}

// capCheck parks the item when its budget is reached after a round: yes raises
// the budget to the next allowed amount above spend, no stops it.
// It answers false when it stopped.
func (lp *floorLoop) capCheck(c *loopCtl, i int, name string, memoryBefore float64) bool {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil {
		return false
	}
	// The person's skipped-check decision permits this forge write without
	// granting any model spending.
	if factory.PublishedAnyway(it) && it.Stream.Phases[i].Kind == factory.StagePost {
		return true
	}
	if memoryBefore >= 0 && it.Stream != nil {
		stream := *it.Stream
		stream.Spent -= max(0, it.MemorySpent-memoryBefore)
		it.Stream = &stream
	}
	reached := it.Cap > 0 && factory.JobLifetimeSpent(it) >= it.Cap
	q, kind := (*Money)(nil).CapQuestion(it)
	if p := lp.r.opts.Pool; p != nil {
		reached = p.CapReached(it)
		if reached {
			q, kind = p.CapQuestion(it)
		}
	}
	if !reached {
		return true
	}
	a, ok := lp.ask(c, i, "cap", kind, q, factory.PhaseWaiting, "", fmt.Sprintf(sayCapAsk, loopUSD(it.Cap)))
	if !ok {
		return false
	}
	if !factory.IsContinue(a.yes, a.words) {
		lp.stop(c)
		return false
	}
	now := lp.r.now()
	line := ""
	if err := lp.write(c, func(it *factory.Item) error {
		NoteContinue(it, now)
		line = "budget raised to " + loopUSD(it.Cap) + " · carrying on"
		return nil
	}); err != nil {
		return false
	}
	lp.tell(c.id, line)
	return true
}

// NoteContinue raises this job's cap when the spend has reached it, and says
// so once. A second caller sees the raised cap and leaves it, so the HTTP
// door and this loop cannot double the same continue.
func NoteContinue(it *factory.Item, now time.Time) bool {
	if !factory.ContinueJob(it) {
		return false
	}
	NoteBudgetRaised(it, now)
	return true
}

// NoteBudgetRaised records the selected cap at the door that changes it.
// The waiting runner separately says this confirmation into the conversation.
func NoteBudgetRaised(it *factory.Item, now time.Time) {
	loopSay(it, now, "said", "budget raised to "+loopUSD(it.Cap)+" · carrying on")
}

// failStop records a step that errored and stops the job. The failure is not
// a question: nobody is asked to skip it.
func (lp *floorLoop) failStop(c *loopCtl, step string, err error) {
	now := lp.r.now()
	if lp.rejectedHere(c, step, now) {
		lp.stop(c)
		return
	}
	code, detail := factory.ClassifyFailure(err.Error())
	var attempts int
	var machine string
	_ = lp.write(c, func(it *factory.Item) error {
		attempts = 1
		machine = it.WorktreeMachine
		if it.Stream != nil {
			for i := range it.Stream.Phases {
				if it.Stream.Phases[i].Name == step && it.Stream.Phases[i].Round > attempts {
					attempts = it.Stream.Phases[i].Round
				}
			}
		}
		failure := factory.NewFailure(step, code, detail, machine, factory.FailureStopped, attempts, now)
		var refused WriteAccessRefusal
		if errors.As(err, &refused) {
			failure = refused.Failure(*it, now)
		}
		factory.StampFailure(it, failure)
		return nil
	})
	if lp.r.opts.Record != nil {
		_, _ = lp.r.opts.Record.Append(context.Background(), record.Event{
			Kind: record.StepFailed, Job: c.id, Detail: code + " · " + step + " · " + factory.FailureStopped,
		})
	}
	lp.stop(c)
}

// rejectedHere ends step as a person's Reject ends the step it answered, when
// the run behind it ended on that no ([factory.AgentFieldRejectedEnd]): the
// step reads failed, with NO FAILURE ON IT OR ON THE JOB and no step_failed
// event, exactly as [factory.Item.EndRejected] leaves a native approve step.
// The control plane cancels a rejected run at once, so its follower returns
// that cancel as the step's error ("the AgentField run cancelled:
// approval_rejected: …"); recorded as a tool_error, it read on the desk's
// rail as a step that failed, not one the person rejected. The factory ends
// the job Rejected with the person's words when it reads the run's end. IT IS
// KEYED ON THE RUN'S DECISION, NEVER ON A PIPELINE'S OR A STEP'S NAME.
func (lp *floorLoop) rejectedHere(c *loopCtl, step string, now time.Time) bool {
	rejected := false
	_ = lp.write(c, func(it *factory.Item) error {
		if _, ok := factory.AgentFieldRejectedEnd(*it); !ok {
			return nil
		}
		rejected = true
		if it.Stream == nil {
			return nil
		}
		for i := range it.Stream.Phases {
			ph := &it.Stream.Phases[i]
			if ph.Name == step || ph.State == factory.PhaseRunning || ph.State == factory.PhaseWaiting {
				ph.State, ph.At, ph.Verification, ph.Failure = factory.PhaseFailed, now, nil, nil
			}
		}
		if f := it.Failure; f != nil && f.Code != factory.CodeStopped {
			it.Failure = nil
		}
		return nil
	})
	return rejected
}

// summarizeFailure gives the job's failure its one-line headline
// ([factory.Failure.Summary]): the step's own summary when it gave one, else
// one line the job's manager writes from the failure's words
// ([Options.FailureSummary]), asked once, off the stop's path, so a slow or
// failed turn never holds the stop. A line that comes back after the job was
// run again is dropped ([factory.SetFailureSummary]).
func (lp *floorLoop) summarizeFailure(id int, given string) {
	it, err := lp.r.opts.Store.Get(id)
	if err != nil || it.Failure == nil || it.Failure.Summary != "" {
		return
	}
	at := it.Failure.At
	if given = factory.FailureSummary(given); given != "" {
		_ = lp.r.opts.Store.Update(id, func(cur *factory.Item) error { factory.SetFailureSummary(cur, at, given); return nil })
		return
	}
	write := lp.r.opts.FailureSummary
	if write == nil {
		return
	}
	failure := *it.Failure
	guard.Go("factory/failure-summary", func() {
		ctx, cancel := context.WithTimeout(context.Background(), lp.shapeWait())
		defer cancel()
		line, err := write(ctx, it, failure)
		if err != nil {
			return
		}
		_ = lp.r.opts.Store.Update(id, func(cur *factory.Item) error { factory.SetFailureSummary(cur, at, line); return nil })
	})
}

// keepFailedResult writes a failed round's result onto its step when the
// step reported anything before it failed, so the failure never takes the
// step's outputs with it. A round that reported nothing leaves the step's
// earlier result as it was.
func (lp *floorLoop) keepFailedResult(c *loopCtl, i int, res factory.StageResult) {
	if strings.TrimSpace(res.Output) == "" && len(res.Files) == 0 && len(res.Claims) == 0 && len(res.Notes) == 0 && len(res.PublishSummary) == 0 {
		return
	}
	kept := res
	kept.Done = false
	_ = lp.write(c, func(it *factory.Item) error {
		if it.Stream != nil && i >= 0 && i < len(it.Stream.Phases) {
			it.Stream.Phases[i].Result = &kept
		}
		return nil
	})
}

// fold writes what a round said onto the item: its result for the stages
// after it, its notes, its claims, its conversation, the first line of what
// it put out, and a plan's edit through Adapt. An edit applied under the
// recipe's ask word stands an approve step after plan ([factory.Edit]), so
// the person ratifies it before anything else runs.
func (lp *floorLoop) fold(c *loopCtl, before factory.Item, st factory.Stage, i int, res factory.StageResult) {
	lp.mu.Lock()
	if lp.results[c.id] == nil {
		lp.results[c.id] = map[int]factory.StageResult{}
	}
	lp.results[c.id][i] = res
	for _, n := range res.Notes {
		if n = strings.TrimSpace(n); n != "" {
			lp.notes[c.id] = append(lp.notes[c.id], n)
		}
	}
	lp.mu.Unlock()

	var recipe factory.Recipe
	if res.Edit != nil {
		var ok bool
		if recipe, ok = jobRecipe(lp.r.opts.Recipe, before); !ok {
			recipe = factory.DefaultRecipe()
		}
	}
	now := lp.r.now()
	_ = lp.write(c, func(it *factory.Item) error {
		if !res.Checkpoint {
			it.Stream.Phases[i].RecordRoundSpend(it.Stream.Phases[i].Round, res.Spent)
		}
		foldResult(it, st, i, res, now)
		if res.Edit == nil {
			return nil
		}
		next, lines, err := factory.Adapt(*it, *res.Edit, recipe)
		if err != nil {
			loopSay(it, now, "fail", "plan's change to the stages was not applied: "+loopFirstLine(err.Error()))
			return nil
		}
		if len(lines) == 0 {
			return nil
		}
		*it = next
		it.Stream.Phases = retail(*it, it.Stream.Phases, i)
		loopSay(it, now, "said", "plan changed the stages: "+strings.Join(lines, " · "))
		return nil
	})
}

// retail rebuilds the phases after phase i from the item's stages as they
// now stand: what has run stays as it ran, and the tail is compiled again.
func retail(it factory.Item, phases []factory.Phase, i int) []factory.Phase {
	out := append([]factory.Phase(nil), phases[:i+1]...)
	from := factory.StageIndex(it.Stages, phases[i].Name)
	if from < 0 {
		return append(out, phases[i+1:]...)
	}
	for _, s := range it.Stages[from+1:] {
		if !runsIn(it, s) {
			continue
		}
		out = append(out, factory.Phase{Name: s.Name, Kind: kindOf(s), State: factory.PhasePending, Note: s.Ask})
	}
	return out
}

// makesSheet says whether a stage's claims go on the proof sheet: the proof
// stage (named so, or the one that runs until proven), a send-back's prove,
// and every check and post, whose claims are an exit code or a receipt.
//
// A PLAN, WRITE OR TEST CONVERSATION'S CLAIMS ARE WHAT IT SAYS IT DID, NOT
// WHAT WAS SHOWN. On the 2026-10-08 hand run every chat stage folded its
// claims onto the sheet, and the person signed off a sheet of 27 rows with
// repeats; those claims go to the log as `claimed: …` lines instead.
func makesSheet(st factory.Stage) bool {
	switch kindOf(st) {
	case factory.StageCheck, factory.StagePost:
		return true
	}
	return factory.IsProofStage(st)
}

// noEvidence is the evidence a claim that gave none is shown with.
const noEvidence = "no evidence given"

// mergeClaim puts a claim on a sheet. Its row is found by its words, read
// without case or punctuation, so a round run again, or a re-check, updates
// the row rather than adding one, and `Refunds count once.` is the row
// `refunds count once`.
//
// A CLAIM WITH NO EVIDENCE IS NOT SHOWN, whatever it says of itself: it is
// marked not OK with [noEvidence]. And a later claim without evidence never
// replaces an earlier one that had some, while one with evidence always
// replaces the row before it.
func mergeClaim(sheet []factory.Claim, cl factory.Claim) []factory.Claim {
	cl.Evidence = strings.TrimSpace(cl.Evidence)
	bare := cl.Evidence == ""
	if bare {
		cl.OK, cl.Evidence = false, noEvidence
	}
	key := claimKey(cl.Text)
	for k := range sheet {
		if claimKey(sheet[k].Text) != key {
			continue
		}
		if bare && sheet[k].Evidence != noEvidence && strings.TrimSpace(sheet[k].Evidence) != "" {
			return sheet
		}
		sheet[k] = cl
		return sheet
	}
	return append(sheet, cl)
}

// claimKey is a claim's words for matching: lower case, letters and digits
// only, single spaces.
func claimKey(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func loopFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// land puts the proof sheet up and waits for the approval. An item with no
// approve step ([factory.HasApprove]) and a proof sheet with every claim
// shown ships itself: that is a banked habit. No sheet is no green proof.
func (lp *floorLoop) land(c *loopCtl) {
	mode := ""
	if it, err := lp.r.opts.Store.Get(c.id); err == nil {
		if err := lp.checkCIScope(it); err != nil {
			lp.parkCI(c, it.Stream.Cur, err.Error())
			return
		}
		// Commit before opening proof so approval names the work being shown.
		if lp.r.opts.Workdir != nil {
			if err := lp.r.allowWrite(c.ctx, it, factory.Stage{Name: "land", Kind: factory.StageCheck}); err != nil {
				lp.failStop(c, "land", err)
				return
			}
			if dir, err := lp.r.opts.Workdir(it); err == nil {
				fresh, readErr := lp.r.opts.Store.Get(c.id)
				if readErr == nil {
					readErr = lp.r.allowWrite(c.ctx, fresh, factory.Stage{Name: "land", Kind: factory.StageCheck})
				}
				if readErr != nil {
					lp.failStop(c, "land", readErr)
					return
				}
				git := lp.r.opts.Git
				if git == nil {
					git = ExecGit{}
				}
				if _, err := git.Run(dir, "rev-parse", "HEAD"); err == nil {
					if err := commitApproved(git, dir, it, ""); err != nil {
						_ = lp.write(c, func(saved *factory.Item) error {
							saved.State = factory.StateNeedsYou
							saved.Question, saved.QKind = "Could not prepare the commit: "+err.Error(), "step"
							return nil
						})
						return
					}
				}
				if head, err := git.Run(dir, "rev-parse", "HEAD"); err == nil {
					_ = lp.write(c, func(saved *factory.Item) error {
						saved.HeadCommit = strings.TrimSpace(head)
						if change, e := readChange(dir, saved.ChangeBase); e == nil {
							saved.Change = change
						}
						return nil
					})
				}
			}
		}
		mode = lp.r.publishPreview(it)
	}
	now := lp.r.now()
	shipped := false
	lp.mu.Lock()
	c.finishing = true
	lp.mu.Unlock()
	err := lp.move(c, "", EventLanded, "landed", func(it *factory.Item) error {
		it.Publish = mode
		shipped = landItem(it, now)

		return nil
	})
	if err == nil && shipped {
		lp.emit(c.id, "", EventShipped, "shipped")
		lp.tell(c.id, sayShipped)
	} else if err == nil {
		if it, e := lp.r.opts.Store.Get(c.id); e == nil && it.FollowUp != nil && it.FollowUp.State == "waiting_approval" {
			if factory.Automatic(it) {
				lp.tell(c.id, "Follow PR repair and proof are complete. Auto is preparing the recorded update to the existing PR; merge remains your decision.")
				return
			}
			lp.tell(c.id, fmt.Sprintf("CI repair and proof are complete. Approval needed: push the verified commit to existing PR #%d. Press Approve to update it.", it.PullRequest.Number))
		} else {
			if it, e := lp.r.opts.Store.Get(c.id); e == nil && factory.Automatic(it) {
				lp.tell(c.id, sayAutoLanded)
			} else {
				lp.tell(c.id, sayLanded)
			}
		}
	}
}

// clean is a sheet with nothing not shown.
func clean(it factory.Item) bool {
	for _, cl := range it.Proof {
		if !cl.OK {
			return false
		}
	}
	for _, cl := range it.Policy {
		if !cl.OK {
			return false
		}
	}
	return true
}

// stop ends the item's stream and keeps its branch; the item is new again
// and its stream stays for the log. The door and the loop both stop through
// here, and only the first stop writes.
func (lp *floorLoop) stop(c *loopCtl) {
	c.wmu.Lock()
	first := c.ctx.Err() == nil
	if c.cause != nil {
		c.cause(factory.ErrPersonStopped)
	}
	c.cancel()
	if first {
		now := lp.r.now()
		var cut *factory.StepProgress
		err := lp.r.opts.Store.Update(c.id, func(it *factory.Item) error {
			// A FAILURE ALREADY ON A RUNNING JOB IS THE STEP'S OWN: failStop
			// wrote it the moment before it stopped the run, it is why the run
			// stopped, and it stays. Every other stop the loop makes is a
			// person's, the Stop door or a no to an approve step, to a step out
			// of rounds or to the budget, and endRun records it as theirs.
			cut = endRun(it, now, it.Failure)
			return nil
		})
		if err == nil {
			lp.emit(c.id, "", EventStopped, "stopped")
		}
		defer lp.tellStep(c.id, sayStopped, cut)
	}
	c.wmu.Unlock()
	// A running drive owns this control until its executor's deferred cleanup
	// has finished. Releasing here let immediate Resume overlap old processes.
	lp.mu.Lock()
	queued := c.benchNo == 0 && !c.reverify
	lp.mu.Unlock()
	if queued {
		lp.release(c)
	}
}

// Withdraw cancels execution whose lease was replaced, without recording a
// person's Stop or changing the replacement's state. Cleanup keeps its bench
// until the executor has finished, just as Stop does.
func (r *Runner) Withdraw(id int) {
	lp := r.loop()
	c := lp.ctl(id)
	if c == nil {
		return
	}
	c.wmu.Lock()
	c.cancel()
	c.wmu.Unlock()
	lp.mu.Lock()
	queued := c.benchNo == 0 && !c.reverify
	lp.mu.Unlock()
	if queued {
		lp.release(c)
	}
}

// Stop ends a stream and keeps its branch; the item returns to new.
func (r *Runner) Stop(id int) error {
	lp := r.loop()
	if c := lp.ctl(id); c != nil {
		if err := r.opts.Store.Update(id, func(it *factory.Item) error {
			factory.MarkPersonStopped(it)
			it.Asking = nil
			return nil
		}); err != nil {
			return err
		}
		lp.stop(c)
		return nil
	}
	now := r.now()
	var cut *factory.StepProgress
	if err := r.opts.Store.Update(id, func(it *factory.Item) error {
		if !factory.UnderWay(*it) {
			return fmt.Errorf("%s is not running", it.Ref())
		}
		cut = endRun(it, now, nil)
		factory.MarkPersonStopped(it)
		it.Asking = nil
		return nil
	}); err != nil {
		return err
	}
	lp.emit(id, "", EventStopped, "stopped")
	lp.tellStep(id, sayStopped, cut)
	return nil
}

// stopStored is a person's stop with no run to cancel. A failure the job
// already carried, a runner that went offline mid-step among them, is replaced
// by the stop, the newer fact.
func stopStored(it *factory.Item, now time.Time) { endRun(it, now, nil) }

// StoppedHere says a step's ctx ended because the job was stopped here: a
// person's Stop, a rejection, a failed step stopping its run. Any other end
// (the engine closing, a lease withdrawn) is not one.
//
// WORK THAT RUNS ELSEWHERE IS CANCELLED ONLY BY A STOP. An executor whose step
// runs on another machine reads this when its ctx ends: a stop cancels the
// remote work, and anything else lets go of it, so quitting this engine never
// cancels a run that does not run here. The next engine start follows it
// again.
func StoppedHere(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), factory.ErrPersonStopped)
}

// reject is a person's no that ends the job: the run stops as [floorLoop.stop]
// stops it, and the job is then closed as rejected with their words, so it
// leaves Needs you and no rule starts it again.
func (lp *floorLoop) reject(c *loopCtl, words string) {
	lp.stop(c)
	now := lp.r.now()
	_ = lp.r.opts.Store.Update(c.id, func(it *factory.Item) error {
		if it.DoneHow == "" {
			it.EndRejected("", words, now)
		}
		return nil
	})
}

// endRun is the one write every stop makes, with a run to cancel or without:
// the job is new again and the stream ends with its branch kept. why is the
// failure that ended the run; nil is a person's stop. A STOP IS ONE FACT ON
// BOTH PATHS: the loop's own stop and a stop of a job no runner here holds
// write exactly this.
//
// A stopped-step failure is recorded only where Stop cut a step. The durable
// person-stop record also covers queued jobs and completed proof. A step under way
// (running or waiting) is failed with [stoppedByYou], and the job carries the
// same stop, so it never reads as a fresh job with a step still running, and
// running that step again carries on from it. A job stopped before any step
// began (queued) or after the last one ended (landed) lost nothing: it is
// stopped with no failed step; a person's Start grants execution again.
//
// It answers the step the stop cut, for the manager's `stopped` line: failed
// when why is the step's own error, stopped when it is a person's; nil when it
// cut none, and the line is then about the job.
func endRun(it *factory.Item, now time.Time, why *factory.Failure) *factory.StepProgress {
	opened := why == nil && it.State == factory.StateShipped && it.PublishStatus != nil && it.PublishStatus.State == "opened"
	event := factory.ProgressStopped
	if why != nil {
		event = factory.ProgressFailed
	}
	var cut *factory.StepProgress
	it.Failure = why
	if why == nil {
		it.StopAfterStep = false
	}
	it.Recovery = nil
	it.Approval = nil
	it.PublishError = ""
	it.Publishing = false
	it.PublishStatus = nil
	it.State = factory.StateNew
	it.Question, it.QKind = "", ""
	s := it.Stream
	if s == nil {
		// A stop at the start gate still needs an end record, even though
		// no step began and there is no step failure to keep.
		s = &factory.Stream{}
		it.Stream = s
	}
	if s.Cur >= 0 && s.Cur < len(s.Phases) {
		ph := &s.Phases[s.Cur]
		if ph.State == factory.PhaseRunning || ph.State == factory.PhaseWaiting {
			if why == nil {
				why = stoppedByYou(ph.Name, ph.Round, now)
				it.Failure = why
			}
			setPhaseState(ph, factory.PhaseFailed, now)
			if why.Step == ph.Name {
				step := *why
				ph.Failure = &step
			}
			cut = &factory.StepProgress{Step: ph.Name, Round: ph.Round, Event: event}
		}
	}
	s.Ended = now
	s.Paused = false
	if opened && it.PullRequest != nil {
		it.State = factory.StateShipped
		it.MarkPublishing("opened", "", "", now)
		loopSay(it, now, "said", stoppedPRWords(it.PullRequest.URL))
		return cut
	}
	loopSay(it, now, "fail", "stopped · branch kept")
	return cut
}

// stoppedByYou is the failure a person's stop leaves on the step it cut and
// on the job. A step's own Stop and the job's Stop write the same one, so
// whoever reads the job reads one fact for both.
func stoppedByYou(step string, round int, now time.Time) *factory.Failure {
	return &factory.Failure{Step: step, Code: factory.CodeStopped, Detail: "Stopped by you", Attempts: round, Next: factory.FailureStopped, At: now}
}

// Pause stops the item's step now, keeping its chat; calling it again (or
// launching the item) resumes the same step in the same chat, with a short
// carry-on note. A paused item stays paused across a restart of codeaf, and
// running it then does the same.
func (r *Runner) Pause(id int) error {
	lp := r.loop()
	c := lp.ctl(id)
	if c == nil {
		it, err := r.opts.Store.Get(id)
		if err != nil {
			return err
		}
		if done, err := r.resumePaused(it); done || err != nil {
			return err
		}
	}
	if c == nil || c.reverify {
		return r.notRunning(id)
	}
	return r.setPaused(c, func(was bool) bool { return !was })
}

// Hold is the pause with its intent said: on holds the item's step now,
// keeping its chat, and off goes on with the same step in the same chat (a
// paused item codeaf was restarted since is put back on a bench). AN ASK
// THAT IS ALREADY TRUE CHANGES NOTHING AND IS NO ERROR: the top bar's button
// sends what it shows, and a doubled press, or a doubled event, must never
// undo itself (the owner saw paused, resumed, paused in one second on
// 2026-10-09, from a toggle pressed twice).
func (r *Runner) Hold(id int, on bool) error {
	lp := r.loop()
	c := lp.ctl(id)
	it, err := r.opts.Store.Get(id)
	if err != nil {
		return err
	}
	if err := factory.CheckPersonStop(it); err != nil {
		return err
	}
	if err := factory.RequireIssueJob(it); err != nil {
		return err
	}
	if c == nil || c.reverify {
		if on {
			if storedPaused(it) {
				return nil
			}
			return r.notRunning(id)
		}
		if storedPaused(it) && c == nil {
			return r.revive(it)
		}
		if c != nil {
			// A re-run of the checks has no step to hold: it is moving.
			return nil
		}
		return r.notRunning(id)
	}
	return r.setPaused(c, func(bool) bool { return on })
}

// setPaused saves a pause before exposing it to the drive. Round admission
// waits for the write, so a failed save leaves execution unchanged.
func (r *Runner) setPaused(c *loopCtl, want func(was bool) bool) error {
	lp := r.loop()
	c.pauseMu.Lock()
	lp.mu.Lock()
	paused := want(c.paused)
	unchanged := paused == c.paused
	// A WITHDRAWN CONTROL IS ENDING, NOT RUNNING. A step edit withdraws the
	// worker running a job (Runner.Withdraw cancels the control without a
	// pause), and the next lease lays the job out paused at its step and asks
	// to go on. The old control is still here while its round lets go of a
	// model call, and it was never paused, so "go on" read as unchanged and was
	// dropped: the old drive then ended and nothing ran the job, which the next
	// worker gave back to Backlog (the live managers lane, job 61). A
	// cancelled control asked to go on on a job the store holds paused is the
	// same handover as a cut pause: wait for it, then revive the job.
	handover := false
	if !paused && !c.paused && c.ctx.Err() != nil && !c.reverify {
		lp.mu.Unlock()
		it, err := r.opts.Store.Get(c.id)
		handover = err == nil && storedPaused(it) && factory.CheckPersonStop(it) == nil
		lp.mu.Lock()
	}
	if !paused && (c.paused || handover) && c.ctx.Err() != nil {
		if c.releasedDone == nil {
			c.releasedDone = make(chan struct{})
		}
		done, released := c.releasedDone, c.released
		lp.mu.Unlock()
		c.pauseMu.Unlock()
		if !released {
			<-done
		}
		it, err := r.opts.Store.Get(c.id)
		if err != nil {
			return err
		}
		return r.revive(it)
	}
	lp.mu.Unlock()
	if unchanged {
		c.pauseMu.Unlock()
		return nil
	}
	kind, word := EventResumed, sayResumed
	if paused {
		kind, word = EventPaused, sayPaused
	}
	now := r.now()
	if err := lp.write(c, func(it *factory.Item) error {
		it.Stream.Paused = paused
		loopSay(it, now, "said", word)
		return nil
	}); err != nil {
		c.pauseMu.Unlock()
		return err
	}
	lp.mu.Lock()
	c.paused = paused
	if paused && c.roundCancel != nil {
		c.roundCancel()
	}
	lp.mu.Unlock()
	c.pauseMu.Unlock()
	lp.emit(c.id, "", kind, word)
	lp.tell(c.id, word)
	return nil
}

// storedPaused says the item's document holds it paused: a run that was cut
// by a pause, whether or not this process still has its control.
func storedPaused(it factory.Item) bool {
	if it.Stream == nil || !it.Stream.Paused {
		return false
	}
	return it.State == factory.StateRunning || it.State == factory.StateQueued
}

// resumePaused runs a paused item again. done says the item was paused and
// this call dealt with it (an error is then the refusal); false leaves the
// door to its ordinary meaning.
func (r *Runner) resumePaused(it factory.Item) (done bool, err error) {
	if !storedPaused(it) {
		return false, nil
	}
	lp := r.loop()
	if c := lp.ctl(it.ID); c != nil {
		lp.mu.Lock()
		held := c.paused && !c.reverify
		lp.mu.Unlock()
		if !held {
			return false, nil
		}
		return true, r.Pause(it.ID)
	}
	return true, r.revive(it)
}

// revive gives a paused item that has no control (codeaf was restarted since
// it paused) a control again and puts it back on a bench. It goes on at the
// phase and round it stopped in, and the phase's chat is carried on.
func (r *Runner) revive(it factory.Item) error {
	if err := factory.CheckPersonStop(it); err != nil {
		return err
	}
	lp := r.loop()
	id := it.ID
	if r.opts.Rail != nil && r.opts.SpentToday != nil {
		if rail := r.opts.Rail(); rail > 0 && r.opts.SpentToday() >= rail {
			return fmt.Errorf("the day rail is %s and today's spend has reached it", loopUSD(rail))
		}
	}
	lp.mu.Lock()
	if lp.ctls[id] != nil {
		lp.mu.Unlock()
		return AlreadyRunning(it.Ref())
	}
	ctx, cause := context.WithCancelCause(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: func() { cause(nil) }, cause: cause, steer: make(chan string, 16), modelSelections: make(chan ModelSelection, 1), answers: make(chan loopAnswer, 1), phase: -1}
	if i := nextPhase(it.Stream.Phases); i >= 0 && it.Stream.Phases[i].State == factory.PhaseRunning {
		c.carry = strings.TrimSpace(it.Stream.Phases[i].Chat)
	}
	lp.ctls[id] = c
	if lp.results[id] == nil {
		// WHAT THE STEPS BEFORE LEFT comes back from the item, where each
		// round's result was kept, with the notes they left.
		lp.results[id] = map[int]factory.StageResult{}
		lp.notes[id] = nil
		for i, ph := range it.Stream.Phases {
			if ph.State == factory.PhaseSkipped || ph.Result == nil {
				continue
			}
			lp.results[id][i] = *ph.Result
			for _, n := range ph.Result.Notes {
				if n = strings.TrimSpace(n); n != "" {
					lp.notes[id] = append(lp.notes[id], n)
				}
			}
		}
	}
	lp.mu.Unlock()
	// The lease door already said an explicit Resume before queueing this run.
	log := it.Stream.Log
	resumeSaid := len(log) > 0 && log[len(log)-1].Text == sayResumed
	line := sayResumed
	if i := nextPhase(it.Stream.Phases); i >= 0 {
		ph := it.Stream.Phases[i]
		// The saved action also identifies retries that retain a running
		// Plan chat. Ordinary continuation keeps its resumed wording.
		log := it.Stream.Log
		if len(log) > 0 && strings.HasPrefix(log[len(log)-1].Text, stepRerunLog) {
			line = fmt.Sprintf(sayTrying, ph.Name)
		}
	}
	now := r.now()
	err := lp.move(c, "", EventResumed, line, func(it *factory.Item) error {
		if !storedPaused(*it) {
			return fmt.Errorf("%s is not paused", it.Ref())
		}
		it.Stream.Paused = false
		if !resumeSaid {
			loopSay(it, now, "said", line)
		}
		return nil
	})
	if err != nil {
		lp.forget(c)
		return err
	}
	if !resumeSaid {
		lp.tell(id, line)
	}
	lp.enqueue(c)
	return nil
}

func (r *Runner) notRunning(id int) error {
	it, err := r.opts.Store.Get(id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%s is not running", it.Ref())
}

// Answer resolves the question the item waits on: yes, no, or words.
func (r *Runner) Answer(id int, yes bool, words string) error {
	it, err := r.opts.Store.Get(id)
	if err != nil {
		return err
	}
	if err := followAnswerRefusal(it, yes, words); err != nil {
		return err
	}
	if it.State == factory.StateNeedsYou && it.QKind == factory.QKindAskFirst {
		if yes {
			return r.Launch(id)
		}
		return r.opts.Store.Update(id, func(cur *factory.Item) error {
			return declineAskFirst(cur, words)
		})
	}
	lp := r.loop()
	lp.mu.Lock()
	c := lp.ctls[id]
	if c == nil || c.pending == "" {
		lp.mu.Unlock()
		it, err := r.opts.Store.Get(id)
		if err != nil {
			return err
		}
		if c == nil && it.QKind == factory.QKindSpendStopped {
			return r.answerStoppedSpending(id, yes, words)
		}
		// A STEP'S QUESTION THE PERSON HOLDS outlives the round that asked
		// it (a pause, a restart): the answer is kept for the step's next
		// round (inbox.go).
		if it.Asking != nil && it.Asking.With == factory.AskedYou {
			return lp.keepAnswer(c, id, loopAnswer{yes: yes, words: strings.TrimSpace(words)})
		}
		return fmt.Errorf("%s is not waiting on you", it.Ref())
	}
	pending := c.pending
	c.pending = ""
	lp.mu.Unlock()
	if err := r.opts.Store.Update(id, func(it *factory.Item) error {
		if err := followAnswerRefusal(*it, yes, words); err != nil {
			return err
		}
		grantAdditionalRound(it, yes, words)
		return nil
	}); err != nil {
		lp.mu.Lock()
		c.pending = pending
		lp.mu.Unlock()
		return err
	}
	c.answers <- loopAnswer{yes: yes, words: strings.TrimSpace(words)}
	return nil
}

// declineAskFirst is a no to a job that asked before it starts: it goes back
// to new, with the person's words on its notes.
func declineAskFirst(it *factory.Item, words string) error {
	if it.QKind != factory.QKindAskFirst {
		return errors.New("job is no longer asking first")
	}
	it.State = factory.StateNew
	it.Question, it.QKind = "", ""
	if words = strings.TrimSpace(words); words != "" {
		it.Notes = append(it.Notes, words)
	}
	return nil
}

// SetModelSelection persists the choice and hands the running round its latest
// complete selection. A slow worker may skip superseded choices, never the last
// one; policy is applied before changing the model at the request boundary.
func (r *Runner) SetModelSelection(id int, model string, automatic *bool) error {
	model = strings.TrimSpace(model)
	c := r.loop().ctl(id)
	if c != nil {
		c.modelMu.Lock()
		defer c.modelMu.Unlock()
	}
	if err := r.opts.Store.Update(id, func(it *factory.Item) error {
		it.Model = model
		if automatic != nil {
			v := *automatic
			it.AutomaticModels = &v
		}
		return nil
	}); err != nil {
		return err
	}
	if c != nil && c.modelSelections != nil {
		// Read back the whole saved preference so a legacy model-only pick cannot
		// erase a preceding policy choice that the worker has not consumed yet.
		it, err := r.opts.Store.Get(id)
		if err != nil {
			return err
		}
		selection := ModelSelection{Model: model, Automatic: it.AutomaticModels}
		for {
			select {
			case c.modelSelections <- selection:
				return nil
			default:
			}
			select {
			case <-c.modelSelections:
			default:
			}
		}
	}
	return nil
}

// SetModel retains the existing policy when only a model id is supplied.
func (r *Runner) SetModel(id int, model string) error {
	return r.SetModelSelection(id, model, nil)
}

// Steer hands words to the running round and the notes future rounds read.
func (r *Runner) Steer(id int, words string) error {
	words = strings.TrimSpace(words)
	if words == "" {
		return nil
	}
	lp := r.loop()
	c := lp.ctl(id)
	if c == nil || c.reverify {
		return r.notRunning(id)
	}
	lp.steer(c, words, true)
	return nil
}

// SignOff (the screen's `approve`) ships a landed item. A claim not shown refuses it unless the
// person changed something first (edited). habitDue is every third clean
// approval on the item's repo, counted off the items' own activity, where a
// clean approval leaves the stored word `signed off`.
func (r *Runner) SignOff(id int, edited bool) (bool, error) {
	lp := r.loop()
	now := r.now()
	var repo string
	err := r.opts.Store.Update(id, func(it *factory.Item) error {
		if err := approveStored(it, edited, now); err != nil {
			return err
		}
		repo = it.Repo
		it.State = factory.StateShipped
		loopSay(it, now, "ok", "shipped · your approval")
		return nil
	})
	if err != nil {
		return false, err
	}
	lp.emit(id, "", EventShipped, "shipped")
	lp.tell(id, sayShipped)
	if edited {
		return false, nil
	}
	return HabitDue(r.opts.Store, repo), nil
}

// ApprovalRefused is the proof gate's check without marking the item. The
// HTTP door runs it before writing an approval, so a claim that is not shown
// never leaves a receipt the publish door would then refuse.
func ApprovalRefused(it *factory.Item, edited bool) error {
	if err := factory.CheckPersonStop(*it); err != nil {
		return err
	}
	if it.State != factory.StateLanded {
		return fmt.Errorf("%s has not landed", it.Ref())
	}
	// NOTHING PUBLISHES PAST AN APPROVE STEP NOBODY PASSED, whatever the mode:
	// Auto passes routine steps through their own door, never by landing past
	// them.
	if gate := factory.UnpassedGate(*it); gate != "" {
		return fmt.Errorf("%s has an approve step nobody has passed yet: %s", it.Ref(), gate)
	}
	if it.FollowUp != nil && it.FollowUp.Kind == "readiness" && !factory.ReadinessPending(*it) {
		return errors.New("final readiness review is incomplete")
	}
	if factory.ReadinessPending(*it) {
		if edited {
			return errors.New("changed work needs another readiness review")
		}
		if err := factory.ReadinessGate(*it); err != nil {
			return err
		}
		if it.FollowUp.Readiness.Head != it.HeadCommit {
			return errors.New("readiness head changed")
		}
	}
	if edited {
		return nil
	}
	bad := 0
	for _, cl := range append(append([]factory.Claim(nil), it.Proof...), it.Policy...) {
		if !cl.OK {
			bad++
		}
	}
	if bad == 1 {
		return fmt.Errorf("%s has a claim not shown", it.Ref())
	}
	if bad > 1 {
		return fmt.Errorf("%s has %d claims not shown", it.Ref(), bad)
	}
	return nil
}

// approveStored is the proof gate's own check and mark, on the stored item: a
// landed item, every claim shown unless the person changed something first
// (edited), its question down and the approval's activity word noted.
func approveStored(it *factory.Item, edited bool, now time.Time) error {
	if err := ApprovalRefused(it, edited); err != nil {
		return err
	}
	it.Question, it.QKind = "", ""
	// The proof approval is the answer the landing's approve step waited for.
	if g := factory.LandingGate(*it); g >= 0 {
		setPhaseState(&it.Stream.Phases[g], factory.PhaseDone, now)
		factory.StampPassed(it, g)
		loopSay(it, now, "ok", passedLine(it.Stream.Phases[g].Name, it.Stream.Phases[g].Passed))
	}
	it.Passing = nil
	if factory.ReadinessPending(*it) {
		it.Note(now, "Ready-for-review transition approved · head "+it.HeadCommit)
	} else if edited {
		it.Note(now, activitySignedEdit)
	} else {
		it.Note(now, activitySigned)
	}
	return nil
}

// HabitDue says a habit is offered: every third clean approval on the repo,
// counted off the items' own activity, where a clean approval leaves the
// stored word `signed off`.
func HabitDue(st *store.Store, repo string) bool {
	items, err := st.List()
	if err != nil {
		return false
	}
	n := 0
	for _, it := range items {
		if it.Repo != repo {
			continue
		}
		for _, ev := range it.Activity {
			if ev.What == activitySigned {
				n++
				break
			}
		}
	}
	return n > 0 && n%habitEvery == 0
}

// SendBack loops a landed item back onto a bench with one more phase, prove,
// whose ask is the person's words. No new brief: the stages before it are
// done and their results stand.
func (r *Runner) SendBack(id int, words string) error {
	words = strings.TrimSpace(words)
	if words == "" {
		return errors.New("say what to prove")
	}
	lp := r.loop()
	for {
		lp.mu.Lock()
		previous := lp.ctls[id]
		if previous == nil {
			break
		}
		finishing := previous.finishing
		lp.mu.Unlock()
		if !finishing {
			return r.notLanded(id)
		}
		// Notifications can expose landed before drive unwinds. Wait for that
		// final store write, then retire only the finished control. Keeping it
		// active until the write ends lets the worker still report the proof.
		previous.wmu.Lock()
		it, err := r.opts.Store.Get(id)
		if err != nil {
			previous.wmu.Unlock()
			return err
		}
		if it.State != factory.StateLanded {
			previous.wmu.Unlock()
			return r.notLanded(id)
		}
		lp.release(previous)
		previous.wmu.Unlock()
	}
	ctx, cause := context.WithCancelCause(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: func() { cause(nil) }, cause: cause, steer: make(chan string, 16), modelSelections: make(chan ModelSelection, 1), answers: make(chan loopAnswer, 1), phase: -1}
	lp.ctls[id] = c
	lp.mu.Unlock()
	now := r.now()
	err := lp.move(c, "", EventQueued, "changes requested: "+words, func(it *factory.Item) error {
		name, err := sendBackStored(it, words, now)
		if err != nil {
			return err
		}
		lp.mu.Lock()
		if lp.extra[id] == nil {
			lp.extra[id] = map[string]factory.Stage{}
		}
		lp.extra[id][name] = factory.Stage{Name: name, Kind: factory.StageChat, Ask: words, Until: factory.UntilDone, On: true}
		lp.mu.Unlock()
		return nil
	})
	if err != nil {
		lp.forget(c)
		return err
	}
	lp.tell(id, fmt.Sprintf(sayChanges, words))
	lp.enqueue(c)
	return nil
}

// sendBackStored puts one more phase, prove, after the last done one, whose
// ask is the person's words, and queues the item again. It answers the
// phase's name.
func sendBackStored(it *factory.Item, words string, now time.Time) (string, error) {
	if it.State != factory.StateLanded || it.Stream == nil || len(it.Stream.Phases) == 0 {
		return "", fmt.Errorf("%s has not landed", it.Ref())
	}
	s := it.Stream
	name := "prove"
	for k := 2; phaseNamed(s.Phases, name) || factory.StageIndex(it.Stages, name) >= 0; k++ {
		name = "prove" + strconv.Itoa(k) // one word, as every stage name is
	}
	at := 0
	for k, p := range s.Phases {
		if p.State == factory.PhaseDone || p.State == factory.PhaseSkipped {
			at = k + 1
		}
	}
	ph := factory.Phase{Name: name, Kind: factory.StageChat, State: factory.PhasePending, Note: words}
	s.Phases = append(s.Phases[:at:at], append([]factory.Phase{ph}, s.Phases[at:]...)...)
	s.Cur = at
	s.Ended = time.Time{}
	it.State = factory.StateQueued
	it.PublishStatus, it.Publishing = nil, false
	it.PublishError = ""
	loopSay(it, now, "said", "changes requested: "+words)
	return name, nil
}

func phaseNamed(ph []factory.Phase, name string) bool {
	for _, p := range ph {
		if p.Name == name {
			return true
		}
	}
	return false
}

func (r *Runner) notLanded(id int) error {
	it, err := r.opts.Store.Get(id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%s has not landed", it.Ref())
}

// Reverify runs every check on a landed item once more and puts what they
// show on its sheet. It takes no bench: a check is a command, not a crew.
func (r *Runner) Reverify(id int) error {
	lp := r.loop()
	for {
		lp.mu.Lock()
		previous := lp.ctls[id]
		if previous == nil {
			break
		}
		finishing := previous.finishing
		lp.mu.Unlock()
		if !finishing {
			return r.notLanded(id)
		}
		previous.wmu.Lock()
		it, err := r.opts.Store.Get(id)
		if err != nil {
			previous.wmu.Unlock()
			return err
		}
		if it.State != factory.StateLanded {
			previous.wmu.Unlock()
			return r.notLanded(id)
		}
		lp.release(previous)
		previous.wmu.Unlock()
	}
	ctx, cause := context.WithCancelCause(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: func() { cause(nil) }, cause: cause, steer: make(chan string, 16), modelSelections: make(chan ModelSelection, 1), answers: make(chan loopAnswer, 1), reverify: true, phase: -1}
	lp.ctls[id] = c
	lp.mu.Unlock()
	now := r.now()
	err := lp.write(c, func(it *factory.Item) error {
		if it.State != factory.StateLanded || len(it.Stream.Phases) == 0 {
			return fmt.Errorf("%s has not landed", it.Ref())
		}
		it.State = factory.StateRunning
		loopSay(it, now, "test", "checking again")
		return nil
	})
	if err != nil {
		lp.forget(c)
		return err
	}
	go lp.drive(c)
	return nil
}

// reverifyRun is the re-check: each check phase once, its claims merged onto
// the sheet, and the item landed again.
func (lp *floorLoop) reverifyRun(c *loopCtl) {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil {
		return
	}
	for i, ph := range it.Stream.Phases {
		if ph.Kind != factory.StageCheck {
			continue
		}
		st, index := lp.stageAt(it, i)
		st.Kind = factory.StageCheck
		if !lp.allowRound(c, st) {
			return
		}
		exec := lp.r.opts.Exec[factory.StageCheck]
		if exec == nil {
			lp.say(c, "fail", fmt.Sprintf("codeaf cannot run a %s stage here", factory.StageCheck))
			break
		}
		if lp.move(c, ph.Name, EventStarted, ph.Name+" · again", func(it *factory.Item) error {
			it.Stream.Cur = i
			return nil
		}) != nil {
			return
		}
		job := lp.job(c, st, index, i, ph.Round+1)
		if !lp.allowRound(c, st) {
			return
		}
		res, err := runIn(c.ctx, exec, job)
		if c.ctx.Err() == nil && job.dirErr == nil {
			lp.recordChange(c, job, res)
		}
		if c.ctx.Err() != nil {
			return
		}
		if err != nil {
			lp.say(c, "fail", ph.Name+" did not finish: "+loopFirstLine(err.Error()))
			continue
		}
		lp.fold(c, it, st, i, res)
		if factory.Met(st, res) {
			lp.finish(c, i, ph.Name, "")
		} else {
			lp.emit(c.id, ph.Name, EventFailed, shortfall(res))
		}
	}
	lp.land(c)
}

// loopUSD spells dollars the way the floor does: whole dollars when whole.
func loopUSD(f float64) string {
	if f == float64(int64(f)) {
		return "$" + strconv.FormatInt(int64(f), 10)
	}
	return "$" + strconv.FormatFloat(f, 'f', 2, 64)
}

// tailPhases is the phases with every one not yet started compiled again from
// the item's stages as they stand now, and whether that changed anything. A
// tail that holds a phase the stages do not (a request for changes' prove) is
// left as it is.
func tailPhases(it factory.Item, phases []factory.Phase) ([]factory.Phase, bool) {
	last := -1
	for i, p := range phases {
		// A PHASE THAT HAS RUN A ROUND IS STARTED, pending or not: one an
		// approve step sent back keeps its round ([floorLoop.back]).
		if (p.State != "" && p.State != factory.PhasePending) || p.Round > 0 {
			last = i
		}
	}
	for _, p := range phases[last+1:] {
		if factory.StageIndex(it.Stages, p.Name) < 0 {
			return phases, false
		}
	}
	var out []factory.Phase
	if last < 0 {
		out = loopPhases(it)
	} else {
		if factory.StageIndex(it.Stages, phases[last].Name) < 0 {
			return phases, false
		}
		out = retail(it, phases, last)
	}
	if samePhases(out, phases) {
		return phases, false
	}
	return out, true
}

// samePhases compares two phase lists by what a tail is compiled from.
func samePhases(a, b []factory.Phase) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Kind != b[i].Kind || a[i].State != b[i].State || a[i].Note != b[i].Note {
			return false
		}
	}
	return true
}

// retailTail rebuilds the phases not yet started from the item's stages at a
// round boundary, so an edit made since (the manager's, through its own
// conversation's `factory_run`) is what runs next.
func (lp *floorLoop) retailTail(c *loopCtl) {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil {
		return
	}
	if _, changed := tailPhases(it, it.Stream.Phases); !changed {
		return
	}
	_ = lp.write(c, func(it *factory.Item) error {
		if phases, changed := tailPhases(*it, it.Stream.Phases); changed {
			it.Stream.Phases = phases
		}
		return nil
	})
}

func (c *loopCtl) pendingSpend() func() {
	c.spending.Add(1)
	return c.spending.Done
}

// Work is already visibly finished. Cancel compilation first, then leave its
// existing periodic worker reports alive until the resulting receipts land.
func (c *loopCtl) settleSpend() {
	c.cancel()
	settled := make(chan struct{})
	go func() { c.spending.Wait(); close(settled) }()
	// ReceiptWait starts when a cancelled call queues its receipt. One second
	// lets that call unwind before the provider's own bounded schedule starts.
	timer := time.NewTimer(provider.ReceiptWait + time.Second)
	defer timer.Stop()
	select {
	case <-settled:
	case <-timer.C:
	}
}

// AnswerAgentField is the worker's door for a person's answer to an
// AgentField run's pause, which reaches it as a lease signal: the decision is
// sent to the control plane through [Options.AgentFieldAnswer] whether or not
// the run is being followed here now, because the control plane holds the
// pause, not this runner.
func (r *Runner) AnswerAgentField(id int, d factory.AgentFieldDecision) error {
	if r.opts.AgentFieldAnswer == nil {
		return errors.New(NoRemoteWords)
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentFieldAnswerWait)
	defer cancel()
	return r.opts.AgentFieldAnswer(ctx, id, d)
}

// agentFieldAnswerWait bounds one try at carrying an answer to the control
// plane; a try that runs out is tried again on the next beat.
const agentFieldAnswerWait = 30 * time.Second

// NoRemoteWords refuses a RunOn job on a factory with no AgentField behind it.
const NoRemoteWords = "this job runs on AgentField and this factory is not connected to AgentField"

// driveRemote runs a RunOn job through Options.Remote and lands its ending
// the way a job's own steps would: done, needs you with the run's words, or
// left to the person's Stop.
// shipAgentField ends a job whose pipeline only runs on AgentField
// ([factory.AgentFieldPipeline]): done, as a run_on job is when its run is.
func (lp *floorLoop) shipAgentField(c *loopCtl) {
	now := lp.r.now()
	_ = lp.write(c, func(it *factory.Item) error {
		it.State = factory.StateShipped
		it.Question, it.QKind = "", ""
		it.Asking = nil
		if it.Stream != nil {
			it.Stream.Ended = now
		}
		it.DoneAt = factory.AgentFieldEnded(*it, now)
		words := "the AgentField run finished"
		if it.PullRequest != nil && it.PullRequest.URL != "" {
			words += " and opened " + it.PullRequest.URL
		}
		loopSay(it, now, "said", words)
		return nil
	})
	lp.emit(c.id, "", EventLanded, "done")
}

func (lp *floorLoop) driveRemote(c *loopCtl) {
	if lp.r.opts.Remote == nil {
		lp.failStop(c, "agentfield", errors.New(NoRemoteWords))
		return
	}
	err := lp.r.opts.Remote(c.ctx, c.id)
	if c.ctx.Err() != nil {
		return
	}
	if err != nil {
		lp.failStop(c, "agentfield", err)
		return
	}
	now := lp.r.now()
	_ = lp.write(c, func(it *factory.Item) error {
		it.State = factory.StateShipped
		if it.Stream != nil {
			it.Stream.Ended = now
		}
		it.DoneAt = factory.RunOnEnded(*it, now)
		loopSay(it, now, "said", "the AgentField run finished")
		return nil
	})
	lp.emit(c.id, "", EventLanded, "done")
}
