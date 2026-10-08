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

	"github.com/Agent-Field/codeaf/internal/factory"
)

// loopLogMost is how many lines a stream keeps, the newest last: the same
// window the mock floor keeps, so the stream view scrolls the same way.
const loopLogMost = 400

// habitEvery is how many clean sign-offs on one repo offer a habit, and the
// activity word a clean sign-off leaves on the item, which is what is counted.
const (
	habitEvery         = 3
	activitySigned     = "signed off"
	activitySignedEdit = "signed off with changes"
)

// errLoopStopped is a write refused because the item was stopped under it.
var errLoopStopped = errors.New("factory run: the item was stopped")

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
}

// loopCtl is one item's controls, reached by the doors.
type loopCtl struct {
	id      int
	ctx     context.Context
	cancel  context.CancelFunc
	steer   chan string
	answers chan loopAnswer
	// wmu holds a write and the check that the item was not stopped as one
	// step; askMu keeps one question at a time.
	wmu   sync.Mutex
	askMu sync.Mutex
	// reverify says the control runs the checks again, holding no bench.
	reverify bool

	// Guarded by floorLoop.mu.
	pending     string // the question the item waits on: "" is none
	paused      bool
	resume      chan struct{}
	roundCancel context.CancelFunc
	benchNo     int
	released    bool
}

type loopAnswer struct {
	yes   bool
	words string
}

// loop is the runner's state machine, made on first use.
func (r *Runner) loop() *floorLoop {
	r.loopOnce.Do(func() {
		r.floor = &floorLoop{
			r:       r,
			ctls:    map[int]*loopCtl{},
			bench:   map[int]int{},
			results: map[int]map[int]factory.StageResult{},
			notes:   map[int][]string{},
			extra:   map[int]map[string]factory.Stage{},
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
	if err := launchable(it); err != nil {
		return err
	}
	if r.opts.Rail != nil && r.opts.SpentToday != nil {
		if rail := r.opts.Rail(); rail > 0 && r.opts.SpentToday() >= rail {
			return fmt.Errorf("the day rail is %s and today's spend has reached it", loopUSD(rail))
		}
	}
	// A STRANGER'S WORDS ARE DRAFTED ON, NEVER WRITTEN FROM, without a person:
	// the write stage is the one that changes the repository.
	if it.Tier == factory.TierStranger {
		for _, s := range it.Stages {
			if s.On && s.Name == "write" && factory.Fits(s, it) {
				return errors.New("a stranger's work does not run write on this floor")
			}
		}
	}
	lp.mu.Lock()
	if lp.ctls[id] != nil {
		lp.mu.Unlock()
		return fmt.Errorf("%s is already running", it.Ref())
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: cancel, steer: make(chan string, 16), answers: make(chan loopAnswer, 1)}
	lp.ctls[id] = c
	lp.results[id] = map[int]factory.StageResult{}
	lp.notes[id] = nil
	delete(lp.extra, id)
	lp.mu.Unlock()

	now := r.now()
	err = lp.move(c, "", EventQueued, "queued", func(it *factory.Item) error {
		if err := launchable(*it); err != nil {
			return err
		}
		it.State = factory.StateQueued
		it.Question, it.QKind = "", ""
		it.Proof, it.Policy = nil, nil
		it.Stream = &factory.Stream{Started: now, Phases: loopPhases(*it)}
		loopSay(it, now, "said", "queued")
		return nil
	})
	if err != nil {
		lp.forget(c)
		return err
	}
	lp.enqueue(c)
	return nil
}

// launchable says whether an item may be launched from where it stands: new,
// dismissed, or never put anywhere yet.
func launchable(it factory.Item) error {
	switch it.State {
	case factory.StateNew, factory.StateDismissed, "":
		return nil
	case factory.StateLanded:
		return fmt.Errorf("%s has landed · sign it off, or send it back", it.Ref())
	case factory.StateShipped:
		return fmt.Errorf("%s has shipped", it.Ref())
	}
	return fmt.Errorf("%s is already running", it.Ref())
}

// loopPhases compiles an item's stages into its phases, in order: a stage
// that is on and fits the item is a pending phase, and one that does not is
// left out. A RUN IS ITS STAGES AT LAUNCH; only plan changes the tail.
func loopPhases(it factory.Item) []factory.Phase {
	var out []factory.Phase
	for _, s := range it.Stages {
		if !s.On || !factory.Fits(s, it) {
			continue
		}
		out = append(out, factory.Phase{Name: s.Name, Kind: kindOf(s), State: factory.PhasePending, Note: s.Ask})
	}
	return out
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
	for {
		it, err := lp.r.opts.Store.Get(c.id)
		if err != nil || it.Stream == nil {
			return
		}
		i := nextPhase(it.Stream.Phases)
		if i < 0 {
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
		if p.State != factory.PhaseDone && p.State != factory.PhaseFailed {
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
func (lp *floorLoop) ask(c *loopCtl, i int, pending, qkind, question string, state factory.PhaseState, note string) (loopAnswer, bool) {
	c.askMu.Lock()
	defer c.askMu.Unlock()
	lp.mu.Lock()
	c.pending = pending
	lp.mu.Unlock()
	now := lp.r.now()
	name := ""
	err := lp.write(c, func(it *factory.Item) error {
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = question, qkind
		if i >= 0 && i < len(it.Stream.Phases) {
			ph := &it.Stream.Phases[i]
			name = ph.Name
			ph.State = state
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
	var a loopAnswer
	select {
	case a = <-c.answers:
	case <-c.ctx.Done():
		return loopAnswer{}, false
	}
	said := "no"
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
			it.Stream.Phases[i].State = factory.PhaseRunning
		}
		loopSay(it, lp.r.now(), "said", said)
		return nil
	})
	return a, err == nil
}

// gate is a person's yes before a stage: yes goes on, words go on with the
// words in the notes, no stops the item.
func (lp *floorLoop) gate(c *loopCtl, i int, name string) bool {
	a, ok := lp.ask(c, i, "gate", "gate", name+" is ready · go, or change it?", factory.PhaseWaiting, "")
	if !ok {
		return false
	}
	if !a.yes && a.words == "" {
		lp.stop(c)
		return false
	}
	lp.addNote(c.id, a.words)
	return true
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
	return lp.move(c, name, EventDone, note, func(it *factory.Item) error {
		if i < len(it.Stream.Phases) {
			it.Stream.Phases[i].State = factory.PhaseDone
			it.Stream.Phases[i].Left = 0
			if note != "" {
				it.Stream.Phases[i].Note = note
			}
		}
		return nil
	}) == nil
}

// skip leaves phase i failed and goes on, as the person said.
func (lp *floorLoop) skip(c *loopCtl, i int, name string) bool {
	return lp.write(c, func(it *factory.Item) error {
		if i < len(it.Stream.Phases) {
			it.Stream.Phases[i].State = factory.PhaseFailed
		}
		loopSay(it, lp.r.now(), "said", "skipping "+name)
		return nil
	}) == nil
}

// runPhase runs one phase to its end: its gate, then rounds until the
// stage's until is met, or the person says how to go on. It answers false
// when the item stopped.
func (lp *floorLoop) runPhase(c *loopCtl, i int) bool {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil || i >= len(it.Stream.Phases) {
		return false
	}
	st, index := lp.stageAt(it, i)
	name := st.Name

	// A GATE STAGE IS A PERSON, so its answer is the stage: nothing runs it.
	if st.Kind == factory.StageGate {
		if !lp.gate(c, i, name) {
			return false
		}
		return lp.finish(c, i, name, "")
	}
	// A SHIP GATE IS THE SIGN-OFF ON THE LANDED ITEM, never a stop before
	// its stage, so a proof stage that carries one runs straight through.
	if st.Gate != factory.GateShip && factory.GateApplies(st, it) {
		if !lp.gate(c, i, name) {
			return false
		}
	}

	exec := lp.r.opts.Exec[kindOf(st)]
	if exec == nil {
		note := fmt.Sprintf("codeaf cannot run a %s stage here", kindOf(st))
		a, ok := lp.ask(c, i, "cannot", "scope", name+" cannot run here · skip it, or stop?", factory.PhaseFailed, note)
		if !ok {
			return false
		}
		if !a.yes && a.words == "" {
			lp.stop(c)
			return false
		}
		return lp.skip(c, i, name)
	}

	round := max(it.Stream.Phases[i].Round, 1)
	limit := max(st.Max, 1)
	planAsked := false
	for {
		if !lp.held(c) {
			return false
		}
		kind, text := EventStarted, fmt.Sprintf("%s · round %d", name, round)
		if round > 1 {
			kind = EventRound
		}
		now := lp.r.now()
		if err := lp.move(c, name, kind, text, func(it *factory.Item) error {
			it.State = factory.StateRunning
			it.Stream.Cur = i
			ph := &it.Stream.Phases[i]
			ph.State = factory.PhaseRunning
			ph.Round = round
			if round > 1 {
				loopSay(it, now, "thought", text)
			}
			return nil
		}); err != nil {
			return false
		}
		job := lp.job(c, st, index, i, round)
		res, again, err := lp.round(c, exec, job)
		if c.ctx.Err() != nil {
			return false
		}
		if again {
			continue
		}
		if err != nil {
			a, ok := lp.ask(c, i, "failed", "scope", fmt.Sprintf("%s did not finish: %s · skip it, or stop?", name, loopFirstLine(err.Error())), factory.PhaseFailed, name+" did not finish")
			if !ok {
				return false
			}
			switch {
			case a.words != "":
				lp.addNote(c.id, a.words)
				continue
			case a.yes:
				return lp.skip(c, i, name)
			}
			lp.stop(c)
			return false
		}
		if res.Spent > 0 {
			job.Spend(res.Spent)
		}
		adaptedAsk := lp.fold(c, it, st, i, res)
		if !lp.capCheck(c, i, name) {
			return false
		}
		if factory.Met(st, res) {
			cur, err := lp.r.opts.Store.Get(c.id)
			if err != nil {
				return false
			}
			q := ""
			switch {
			case adaptedAsk:
				q = "plan changed the stages · go, or change it?"
			case name == "plan" && cur.Gate == factory.GatePlan && !planAsked:
				q = "plan is ready · go, or change it?"
			}
			if q != "" {
				planAsked = true
				a, ok := lp.ask(c, i, "plan", "plan", q, factory.PhaseWaiting, "")
				if !ok {
					return false
				}
				if !a.yes && a.words == "" {
					lp.stop(c)
					return false
				}
				if a.words != "" {
					lp.addNote(c.id, a.words)
					round++
					limit = max(limit, round)
					continue
				}
			}
			return lp.finish(c, i, name, "")
		}
		if round >= limit {
			until := strings.TrimSpace(st.Until)
			if until == "" {
				until = factory.UntilDone
			}
			note := fmt.Sprintf("%s is not %s after %d round(s)", name, until, round)
			q := note + ": " + shortfall(res) + " · one more round, or go on as is?"
			a, ok := lp.ask(c, i, "rounds", "scope", q, factory.PhaseFailed, note)
			if !ok {
				return false
			}
			if !a.yes && a.words == "" {
				lp.say(c, "said", "going on as is · "+note)
				return lp.finish(c, i, name, note)
			}
			lp.addNote(c.id, a.words)
			round++
			limit = round
			continue
		}
		lp.say(c, "fail", fmt.Sprintf("%s %d/%d: %s · going again", name, round, limit, shortfall(res)))
		round++
	}
}

// shortfall says in a few words why a round did not meet its until.
func shortfall(res factory.StageResult) string {
	switch {
	case !res.Done:
		return "no result"
	case res.Findings == 1:
		return "1 finding"
	case res.Findings > 1:
		return strconv.Itoa(res.Findings) + " findings"
	case res.Exit != 0:
		return "exit " + strconv.Itoa(res.Exit)
	}
	shown := 0
	for _, cl := range res.Claims {
		if cl.OK && strings.TrimSpace(cl.Evidence) != "" {
			shown++
		}
	}
	if len(res.Claims) == 0 {
		return "no claims"
	}
	return fmt.Sprintf("%d of %d claims shown", shown, len(res.Claims))
}

// held waits while the item is paused, and answers false when it stopped.
func (lp *floorLoop) held(c *loopCtl) bool {
	lp.mu.Lock()
	paused, resume := c.paused, c.resume
	lp.mu.Unlock()
	if !paused {
		return c.ctx.Err() == nil
	}
	select {
	case <-resume:
		return c.ctx.Err() == nil
	case <-c.ctx.Done():
		return false
	}
}

// round runs one round under its own ctx, which a pause cancels. again is a
// round a pause cut short, which starts over from the same round.
func (lp *floorLoop) round(c *loopCtl, exec Executor, job Job) (res factory.StageResult, again bool, err error) {
	rctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	lp.mu.Lock()
	if c.paused {
		lp.mu.Unlock()
		return res, true, nil
	}
	c.roundCancel = cancel
	lp.mu.Unlock()
	res, err = exec.Run(rctx, job)
	lp.mu.Lock()
	c.roundCancel = nil
	cut := rctx.Err() != nil
	lp.mu.Unlock()
	if cut && c.ctx.Err() == nil {
		return factory.StageResult{}, true, nil
	}
	return res, false, err
}

// job is what one round may read.
func (lp *floorLoop) job(c *loopCtl, st factory.Stage, index, i, round int) Job {
	it, _ := lp.r.opts.Store.Get(c.id)
	lp.mu.Lock()
	// THE PERSON'S NOTES COME FIRST: what was settled in the item's own
	// conversation opens every stage's brief, and what earlier stages left
	// follows it in order.
	notes := append(append([]string(nil), it.Notes...), lp.notes[c.id]...)
	var prior []factory.StageResult
	for k := 0; k < i; k++ {
		if res, ok := lp.results[c.id][k]; ok {
			prior = append(prior, res)
		}
	}
	lp.mu.Unlock()
	dir := ""
	if lp.r.opts.RepoDir != nil {
		dir = lp.r.opts.RepoDir(it.Repo)
	}
	return Job{
		Item:  it,
		Stage: st,
		Index: index,
		Round: round,
		Notes: notes,
		Prior: prior,
		Dir:   dir,
		Steer: c.steer,
		Log:   func(line string) { lp.say(c, "thought", line) },
		Room:  func(chat string) { lp.room(c, i, chat) },
		Spend: lp.spender(c),
	}
}

// room writes a round's conversation onto its phase as soon as it is made:
// the item page's `enter` on a running stage walks into it. A stopped item
// takes no write, which is [floorLoop.write]'s own refusal.
func (lp *floorLoop) room(c *loopCtl, i int, chat string) {
	_ = lp.write(c, func(it *factory.Item) error {
		if i >= 0 && i < len(it.Stream.Phases) {
			it.Stream.Phases[i].Chat = chat
		}
		return nil
	})
}

// spender is the Job.Spend hook: the money's when there is one, else the
// loop's own plain reckoning onto Stream.Spent.
func (lp *floorLoop) spender(c *loopCtl) func(usd float64) {
	if p := lp.r.opts.Pool; p != nil {
		return p.Spend(c.id)
	}
	return func(usd float64) {
		if usd <= 0 {
			return
		}
		_ = lp.r.opts.Store.Annotate(c.id, func(it *factory.Item) error {
			if it.Stream == nil {
				it.Stream = &factory.Stream{}
			}
			it.Stream.Spent += usd
			return nil
		})
	}
}

// capCheck parks the item when its cap is reached after a round: yes raises
// the cap by as much again, no stops it. It answers false when it stopped.
func (lp *floorLoop) capCheck(c *loopCtl, i int, name string) bool {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil {
		return false
	}
	reached := it.Cap > 0 && it.Stream != nil && it.Stream.Spent >= it.Cap
	q, kind := "cap of "+loopUSD(it.Cap)+" reached · "+loopUSD(it.Cap)+" more, or stop?", "cap"
	if p := lp.r.opts.Pool; p != nil {
		reached = p.CapReached(it)
		if reached {
			q, kind = p.CapQuestion(it)
		}
	}
	if !reached {
		return true
	}
	a, ok := lp.ask(c, i, "cap", kind, q, factory.PhaseWaiting, "")
	if !ok {
		return false
	}
	if !a.yes {
		lp.stop(c)
		return false
	}
	now := lp.r.now()
	return lp.write(c, func(it *factory.Item) error {
		it.Cap *= 2
		loopSay(it, now, "said", "cap raised to "+loopUSD(it.Cap)+" · carrying on")
		return nil
	}) == nil
}

// fold writes what a round said onto the item: its result for the stages
// after it, its notes, its claims, its conversation, the first line of what
// it put out, and a plan's edit through Adapt. It answers whether the edit
// was applied under ask, so the person ratifies it before anything else runs.
func (lp *floorLoop) fold(c *loopCtl, before factory.Item, st factory.Stage, i int, res factory.StageResult) (adaptedAsk bool) {
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
		if lp.r.opts.Recipe != nil {
			recipe = lp.r.opts.Recipe(before.Repo)
		} else {
			recipe = factory.DefaultRecipe()
		}
	}
	now := lp.r.now()
	_ = lp.write(c, func(it *factory.Item) error {
		ph := &it.Stream.Phases[i]
		if res.Chat != "" {
			ph.Chat = res.Chat
		}
		it.Stream.Findings = res.Findings
		for _, cl := range res.Claims {
			if cl.Medium == "policy" {
				it.Policy = mergeClaim(it.Policy, cl)
			} else {
				it.Proof = mergeClaim(it.Proof, cl)
			}
		}
		if line := loopFirstLine(res.Output); line != "" {
			tone := "said"
			if kindOf(st) == factory.StageCheck {
				tone = "test"
			}
			loopSay(it, now, tone, st.Name+": "+line)
		}
		if !factory.Met(st, res) && res.Done {
			ph.Note = shortfall(res)
		}
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
		adaptedAsk = recipe.AdaptFor(it.Kind) == factory.AdaptAsk
		return nil
	})
	return adaptedAsk
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
		if !s.On || !factory.Fits(s, it) {
			continue
		}
		out = append(out, factory.Phase{Name: s.Name, Kind: kindOf(s), State: factory.PhasePending, Note: s.Ask})
	}
	return out
}

// mergeClaim puts a claim on a sheet: one with the same words is replaced,
// so a round run again, or a re-check, updates its row rather than adding one.
func mergeClaim(sheet []factory.Claim, cl factory.Claim) []factory.Claim {
	for k := range sheet {
		if sheet[k].Text == cl.Text {
			sheet[k] = cl
			return sheet
		}
	}
	return append(sheet, cl)
}

func loopFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// land puts the proof sheet up and waits for the sign-off. An item whose gate
// is none, with every claim shown, ships itself: that is a banked habit.
func (lp *floorLoop) land(c *loopCtl) {
	now := lp.r.now()
	shipped := false
	err := lp.move(c, "", EventLanded, "landed", func(it *factory.Item) error {
		it.State = factory.StateLanded
		it.Question, it.QKind = "", ""
		it.Stream.Ended = now
		it.Stream.Paused = false
		if n := len(it.Stream.Phases); n > 0 {
			it.Stream.Cur = n - 1
		}
		if it.Gate == factory.GateNone && clean(*it) {
			it.State = factory.StateShipped
			shipped = true
			loopSay(it, now, "ok", "shipped · habit · proof all green")
			return nil
		}
		loopSay(it, now, "said", "landed · proof sheet ready · your sign-off")
		return nil
	})
	if err == nil && shipped {
		lp.emit(c.id, "", EventShipped, "shipped")
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
	c.cancel()
	if first {
		now := lp.r.now()
		err := lp.r.opts.Store.Update(c.id, func(it *factory.Item) error {
			it.State = factory.StateNew
			it.Question, it.QKind = "", ""
			if s := it.Stream; s != nil {
				if s.Cur < len(s.Phases) {
					switch s.Phases[s.Cur].State {
					case factory.PhaseRunning, factory.PhaseWaiting:
						s.Phases[s.Cur].State = factory.PhaseFailed
					}
				}
				s.Ended = now
				s.Paused = false
				loopSay(it, now, "fail", "stopped · branch kept")
			}
			return nil
		})
		if err == nil {
			lp.emit(c.id, "", EventStopped, "stopped")
		}
	}
	c.wmu.Unlock()
	lp.release(c)
}

// Stop ends a stream and keeps its branch; the item returns to new.
func (r *Runner) Stop(id int) error {
	lp := r.loop()
	if c := lp.ctl(id); c != nil {
		lp.stop(c)
		return nil
	}
	it, err := r.opts.Store.Get(id)
	if err != nil {
		return err
	}
	switch it.State {
	case factory.StateQueued, factory.StateRunning, factory.StateNeedsYou:
		// AN ITEM NOBODY IS RUNNING IS NOT RUNNING, whatever its document
		// says: a process that died mid-round left it so, and stop puts it
		// back where a person can launch it again.
		now := r.now()
		if err := r.opts.Store.Update(id, func(it *factory.Item) error {
			it.State = factory.StateNew
			it.Question, it.QKind = "", ""
			if it.Stream != nil {
				it.Stream.Ended = now
				it.Stream.Paused = false
				loopSay(it, now, "fail", "stopped · branch kept")
			}
			return nil
		}); err != nil {
			return err
		}
		lp.emit(id, "", EventStopped, "stopped")
		return nil
	}
	return fmt.Errorf("%s is not running", it.Ref())
}

// Pause holds the item's bench without ending it; calling it again resumes.
// A round a pause cuts short starts over from the same round.
func (r *Runner) Pause(id int) error {
	lp := r.loop()
	c := lp.ctl(id)
	if c == nil || c.reverify {
		return r.notRunning(id)
	}
	lp.mu.Lock()
	c.paused = !c.paused
	paused := c.paused
	if paused {
		c.resume = make(chan struct{})
		if c.roundCancel != nil {
			c.roundCancel()
		}
	} else if c.resume != nil {
		close(c.resume)
		c.resume = nil
	}
	lp.mu.Unlock()
	kind, word := EventResumed, "resumed"
	if paused {
		kind, word = EventPaused, "paused"
	}
	now := r.now()
	return lp.move(c, "", kind, word, func(it *factory.Item) error {
		it.Stream.Paused = paused
		loopSay(it, now, "said", word)
		return nil
	})
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
	lp := r.loop()
	lp.mu.Lock()
	c := lp.ctls[id]
	if c == nil || c.pending == "" {
		lp.mu.Unlock()
		it, err := r.opts.Store.Get(id)
		if err != nil {
			return err
		}
		return fmt.Errorf("%s is not waiting on you", it.Ref())
	}
	c.pending = ""
	lp.mu.Unlock()
	c.answers <- loopAnswer{yes: yes, words: strings.TrimSpace(words)}
	return nil
}

// Steer hands words to the round running now, and to the notes every round
// after it reads. It never waits: a round that is not listening reads them
// in its next brief.
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
	select {
	case c.steer <- words:
	default:
	}
	lp.addNote(id, words)
	lp.say(c, "said", "steer: "+words)
	return nil
}

// SignOff ships a landed item. A claim not shown refuses it unless the
// person changed something first (edited). habitDue is every third clean
// sign-off on the item's repo, counted off the items' own activity, where a
// clean sign-off leaves `signed off`.
func (r *Runner) SignOff(id int, edited bool) (bool, error) {
	lp := r.loop()
	now := r.now()
	var repo string
	err := r.opts.Store.Update(id, func(it *factory.Item) error {
		if it.State != factory.StateLanded {
			return fmt.Errorf("%s has not landed", it.Ref())
		}
		if !edited {
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
		}
		repo = it.Repo
		it.State = factory.StateShipped
		it.Question, it.QKind = "", ""
		loopSay(it, now, "ok", "shipped · your sign-off")
		if edited {
			it.Note(now, activitySignedEdit)
		} else {
			it.Note(now, activitySigned)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	lp.emit(id, "", EventShipped, "shipped")
	if edited {
		return false, nil
	}
	items, err := r.opts.Store.List()
	if err != nil {
		return false, nil
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
	return n > 0 && n%habitEvery == 0, nil
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
	lp.mu.Lock()
	if lp.ctls[id] != nil {
		lp.mu.Unlock()
		return r.notLanded(id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: cancel, steer: make(chan string, 16), answers: make(chan loopAnswer, 1)}
	lp.ctls[id] = c
	lp.mu.Unlock()
	now := r.now()
	err := lp.move(c, "", EventQueued, "sent back: "+words, func(it *factory.Item) error {
		if it.State != factory.StateLanded || len(it.Stream.Phases) == 0 {
			return fmt.Errorf("%s has not landed", it.Ref())
		}
		s := it.Stream
		name := "prove"
		for k := 2; phaseNamed(s.Phases, name) || factory.StageIndex(it.Stages, name) >= 0; k++ {
			name = "prove " + strconv.Itoa(k)
		}
		at := 0
		for k, p := range s.Phases {
			if p.State == factory.PhaseDone {
				at = k + 1
			}
		}
		ph := factory.Phase{Name: name, Kind: factory.StageChat, State: factory.PhasePending, Note: words}
		s.Phases = append(s.Phases[:at:at], append([]factory.Phase{ph}, s.Phases[at:]...)...)
		s.Cur = at
		s.Ended = time.Time{}
		it.State = factory.StateQueued
		lp.mu.Lock()
		if lp.extra[id] == nil {
			lp.extra[id] = map[string]factory.Stage{}
		}
		lp.extra[id][name] = factory.Stage{Name: name, Kind: factory.StageChat, Ask: words, Until: factory.UntilDone, On: true}
		lp.mu.Unlock()
		loopSay(it, now, "said", "sent back: "+words)
		return nil
	})
	if err != nil {
		lp.forget(c)
		return err
	}
	lp.enqueue(c)
	return nil
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
	lp.mu.Lock()
	if lp.ctls[id] != nil {
		lp.mu.Unlock()
		return r.notLanded(id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: cancel, steer: make(chan string, 16), answers: make(chan loopAnswer, 1), reverify: true}
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
		res, err := exec.Run(c.ctx, lp.job(c, st, index, i, ph.Round+1))
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
