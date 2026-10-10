package automation

// clock.go is the one loop that runs automations: hold the lock, look every few
// seconds, take what is due, run it, and stop when the last window closes.
//
// IT IS A PROCESS OF ITS OWN (`codeaf clock`, started by any window that finds
// no clock running) and not a goroutine in a window or a session host. A run
// must not die with whichever window happened to hold the clock while another
// window is still open, a remote machine has no window process at all, and a
// session host lingers half an hour after its last window and is tied to one
// project. The clock is tied to none of those lifetimes: it lives exactly as
// long as some window is open, plus a short grace.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// ErrHeld means another process holds the clock. It is the ordinary answer for
// every clock but one, and not a failure.
var ErrHeld = errors.New("another codeaf already holds the automations clock")

// The clock's own pace. They are variables so a test can run a day in a
// second; nothing in the product writes them.
var (
	// pollEvery is how often the clock looks: for due slots, for runs somebody
	// asked for, for stop requests and for open windows.
	pollEvery = 2 * time.Second
	// closingGrace is how long the clock waits, with no window open, before it
	// stops runs and leaves. It covers a window restarting itself onto a new
	// build, which closes and reopens its presence in the same second.
	closingGrace = 30 * time.Second
	// stopWait bounds how long the clock waits for a stopped run to record
	// itself before it records the run for it.
	stopWait = 15 * time.Second
)

// MaxWork is how many pieces of work run at once. A Monday morning after a
// weekend away can make every routine due in the same second; they queue and
// two run at a time rather than all at once.
const MaxWork = 2

// maxLooks bounds watches looking at once. A look is short, so more run side
// by side than pieces of work do.
const maxLooks = 4

// The lines a run carries when the clock, and not the run, decided its end.
const (
	lineClosed  = "codeaf closed before it finished"
	lineStopped = "stopped by you"
	lineDeleted = "the automation was deleted"
)

// Runner does what a run does. internal/session implements it over the
// person's own configuration: their models, keys, connected accounts and
// approval rules.
type Runner interface {
	// Look takes one look for a watch and says what it saw.
	Look(ctx context.Context, a Automation) (Sight, error)
	// Judge asks the model whether what a look saw meets the watch's condition.
	Judge(ctx context.Context, a Automation, sight Sight) (Judgment, error)
	// Work carries out the automation's brief as unattended work. evidence is
	// what a watch saw when it spoke, and empty otherwise.
	Work(ctx context.Context, a Automation, run Run, evidence string) (Report, error)
}

// Sight is what one look saw.
type Sight struct {
	// Text is the output, clipped to what a judgment can read.
	Text string
	// USD is what the look itself cost — a tool on a paid service may.
	USD float64
}

// Judgment is the model's answer to "does this meet the condition?".
type Judgment struct {
	// Met is the answer, read only when Sure.
	Met bool
	// Sure is false when the model could not decide; an unsure judgment
	// changes nothing about what the watch has seen.
	Sure bool
	// Line is the model's one sentence of reasoning.
	Line string
	USD  float64
}

// Report is how a piece of work says it went.
type Report struct {
	// Outcome is done, incomplete or your call. A run that ends because its
	// context ended is recorded by the clock, whatever it reports.
	Outcome    Outcome
	Line       string
	Detail     string
	USD        float64
	Transcript string
}

// Clock runs automations while windows are open.
type Clock struct {
	Store    *Store
	Presence *Presence
	Runner   Runner
	// Now is the clock's time. Nil is the wall clock.
	Now func() time.Time
	// Log receives one line for anything that went wrong outside a run — a
	// store error, a run that could not be recorded. Nil discards.
	Log func(string)

	mu       sync.Mutex
	inflight map[int64]*flight
	work     int
	looks    int
	wg       sync.WaitGroup
}

// flight is one run in hand.
type flight struct {
	cancel context.CancelCauseFunc
	work   bool
}

// errStopRequested and errClosing are the causes the clock cancels a run with,
// so the run's record says which.
var (
	errStopRequested = errors.New(lineStopped)
	errClosing       = errors.New(lineClosed)
)

// LockName is the clock's lock file inside the store's root.
const LockName = "clock.lock"

// Held reports whether some process holds the clock right now. A window asks
// before it starts one.
func Held(root string) bool {
	file, err := os.OpenFile(filepath.Join(root, LockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		return filelock.IsBusy(err)
	}
	_ = filelock.Unlock(file)
	return false
}

// Run holds the clock until ctx ends or no window has been open for the grace,
// then stops what is running and returns. It returns [ErrHeld] at once when
// another process holds the clock.
func (c *Clock) Run(ctx context.Context) error {
	if c.Store == nil || c.Runner == nil || c.Presence == nil {
		return errors.New("automations: a clock needs a store, a runner and a presence")
	}
	release, err := c.lock()
	if err != nil {
		return err
	}
	defer release()
	c.inflight = map[int64]*flight{}
	// A CLOCK IS ONLY EVER TAKEN FROM A PROCESS THAT IS GONE, so a run still
	// recorded as running is one nobody is running any more.
	if n, err := c.Store.Abandoned(lineClosed); err != nil {
		c.log("could not close runs a previous clock left: " + err.Error())
	} else if n > 0 {
		c.log(fmt.Sprintf("closed %d run(s) a previous clock left running", n))
	}
	var empty time.Time
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	for {
		now := c.now()
		windows, err := c.Presence.Count()
		if err != nil {
			c.log("could not count open windows: " + err.Error())
			windows = 1 // A FOLDER WE CANNOT READ IS NOT A CLOSED CODEAF.
		}
		switch {
		case windows > 0:
			empty = time.Time{}
			c.pass(ctx, now)
		case empty.IsZero():
			empty = now
		case now.Sub(empty) >= closingGrace:
			c.stopAll(errClosing)
			return nil
		}
		select {
		case <-ctx.Done():
			c.stopAll(errClosing)
			return nil
		case <-ticker.C:
		}
	}
}

// lock takes the clock's file lock, or reports [ErrHeld].
func (c *Clock) lock() (func(), error) {
	path := filepath.Join(c.Store.Root(), LockName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("automations: %w", err)
	}
	if err := filelock.Lock(file, true, true); err != nil {
		_ = file.Close()
		if filelock.IsBusy(err) {
			return nil, ErrHeld
		}
		return nil, fmt.Errorf("automations: take the clock: %w", err)
	}
	_ = file.Truncate(0)
	_, _ = file.WriteString(fmt.Sprintf("%d\n", os.Getpid()))
	return func() {
		_ = filelock.Unlock(file)
		_ = file.Close()
	}, nil
}

// pass is one look: stop what somebody asked to stop, take what is due, and
// start what is queued as far as the limits allow.
func (c *Clock) pass(ctx context.Context, now time.Time) {
	if stops, err := c.Store.StopRequested(); err == nil {
		for id := range stops {
			c.cancel(id, errStopRequested)
		}
	}
	due, err := c.Store.Due(now)
	if err != nil {
		c.log("could not read what is due: " + err.Error())
		return
	}
	busy := c.busyAutomations()
	for _, a := range due {
		if busy[a.ID] {
			// ONE RUN AT A TIME PER AUTOMATION. A slot that comes while the
			// last run is still going is passed over, not queued behind it: a
			// fifteen-minute routine whose run takes twenty would otherwise
			// queue forever.
			if err := c.skip(a, now); err != nil && !errors.Is(err, errChanged) {
				c.log("could not move " + a.Title + " on: " + err.Error())
			}
			continue
		}
		if _, err := c.Store.Take(a, now); err != nil && !errors.Is(err, errChanged) {
			c.log("could not take " + a.Title + ": " + err.Error())
		}
	}
	queued, err := c.Store.Queued()
	if err != nil {
		c.log("could not read queued runs: " + err.Error())
		return
	}
	for _, run := range queued {
		c.start(ctx, run)
	}
}

// busyAutomations is the set of automations with a run queued or in hand.
func (c *Clock) busyAutomations() map[string]bool {
	busy := map[string]bool{}
	active, err := c.Store.Active()
	if err != nil {
		return busy
	}
	for _, run := range active {
		busy[run.AutomationID] = true
	}
	return busy
}

// skip moves a busy automation past a slot without queuing a run for it.
func (c *Clock) skip(a Automation, now time.Time) error {
	if !a.Schedule.Repeats() {
		return nil
	}
	next, err := a.Schedule.Next(now)
	if err != nil {
		return err
	}
	return c.Store.advance(a, next)
}

// start begins a queued run if the limits allow, and leaves it queued if not.
func (c *Clock) start(ctx context.Context, run Run) {
	a, err := c.Store.Get(run.AutomationID)
	c.mu.Lock()
	if _, already := c.inflight[run.ID]; already {
		c.mu.Unlock()
		return
	}
	if err != nil {
		c.mu.Unlock()
		run.Outcome, run.Line = OutcomeStopped, lineDeleted
		if errors.Is(err, ErrNotFound) {
			_ = c.Store.Start(run.ID)
			c.finish(run)
		}
		return
	}
	isWork := a.Action.Do != ""
	switch {
	case isWork && c.work >= MaxWork:
		c.mu.Unlock()
		return
	case !isWork && a.Look != nil && c.looks >= maxLooks:
		c.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	f := &flight{cancel: cancel, work: isWork}
	c.inflight[run.ID] = f
	if isWork {
		c.work++
	} else if a.Look != nil {
		c.looks++
	}
	c.mu.Unlock()
	if err := c.Store.Start(run.ID); err != nil {
		c.log("could not start a run of " + a.Title + ": " + err.Error())
	}
	run.Started = c.now()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer c.landed(run.ID)
		c.execute(runCtx, a, run)
	}()
}

// landed takes a run out of hand.
func (c *Clock) landed(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, ok := c.inflight[id]
	if !ok {
		return
	}
	f.cancel(nil)
	if f.work {
		c.work--
	} else if c.looks > 0 {
		c.looks--
	}
	delete(c.inflight, id)
}

// cancel stops one run in hand with a cause its record will name.
func (c *Clock) cancel(id int64, cause error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f, ok := c.inflight[id]; ok {
		f.cancel(cause)
	}
}

// stopAll stops every run in hand, waits a bounded time for each to record
// itself, and records any that did not.
func (c *Clock) stopAll(cause error) {
	c.mu.Lock()
	for _, f := range c.inflight {
		f.cancel(cause)
	}
	c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(stopWait):
		c.log("runs did not stop in time; recording them as stopped")
		if _, err := c.Store.Abandoned(lineClosed); err != nil {
			c.log("could not record stopped runs: " + err.Error())
		}
	}
}

// execute carries out one run and records what it came to.
func (c *Clock) execute(ctx context.Context, a Automation, run Run) {
	limits := a.Limits.Effective()
	ctx, cancel := context.WithTimeout(ctx, limits.Time)
	defer cancel()
	switch a.Kind() {
	case KindReminder:
		run.Outcome, run.Line = OutcomeDone, strings.TrimSpace(a.Action.Say)
	case KindWatch:
		run = c.watch(ctx, a, run)
	default:
		report, err := c.Runner.Work(ctx, a, run, "")
		run = c.reported(ctx, run, report, err, limits)
	}
	c.finish(run)
}

// watch is one look and its judgment, and — on the change to "yes" — the
// action. A watch speaks on the CHANGE: while its condition stays true it is
// quiet, and a "no" re-arms it so the next "yes" speaks again.
func (c *Clock) watch(ctx context.Context, a Automation, run Run) Run {
	sight, err := c.Runner.Look(ctx, a)
	run.USD += sight.USD
	if err != nil {
		return c.ended(ctx, run, OutcomeUnchecked, "couldn't look: "+oneLine(err.Error()), OutcomeUnchecked)
	}
	judgment, err := c.Runner.Judge(ctx, a, sight)
	run.USD += judgment.USD
	switch {
	case err != nil:
		return c.ended(ctx, run, OutcomeUnchecked, "couldn't decide: "+oneLine(err.Error()), OutcomeUnchecked)
	case !judgment.Sure:
		return c.ended(ctx, run, OutcomeUnchecked, firstNonEmpty(judgment.Line, "the model could not tell"), OutcomeUnchecked)
	case !judgment.Met:
		c.setSeen(a, "no")
		run.Outcome, run.Line = OutcomeQuiet, firstNonEmpty(judgment.Line, "not yet")
		return run
	case a.Seen == "yes":
		run.Outcome, run.Line = OutcomeQuiet, firstNonEmpty(judgment.Line, "still true")
		return run
	}
	c.setSeen(a, "yes")
	if a.Look.Once {
		if err := c.Store.FinishWatch(a.ID, a.Revision); err != nil && !errors.Is(err, errChanged) {
			c.log("could not finish " + a.Title + ": " + err.Error())
		}
	}
	run.Detail = sight.Text
	if say := strings.TrimSpace(a.Action.Say); say != "" {
		run.Outcome, run.Line = OutcomeDone, say
		if line := strings.TrimSpace(judgment.Line); line != "" {
			run.Detail = line + "\n\n" + sight.Text
		}
		return run
	}
	report, err := c.Runner.Work(ctx, a, run, sight.Text)
	return c.reported(ctx, run, report, err, a.Limits.Effective())
}

// reported turns a piece of work's report, or the reason it has none, into the
// run's record. The context's end is read FIRST: a run that was stopped or ran
// out of time is that, whatever the work managed to say on its way out.
func (c *Clock) reported(ctx context.Context, run Run, report Report, err error, limits Limits) Run {
	run.USD += report.USD
	run.Transcript = firstNonEmpty(report.Transcript, run.Transcript)
	run.Detail = firstNonEmpty(report.Detail, run.Detail)
	if ctx.Err() != nil {
		ended := c.ended(ctx, run, "", "", OutcomeIncomplete)
		if ended.Outcome == OutcomeIncomplete {
			ended.Line = "ran out of its " + durationWords(limits.Time)
		}
		return ended
	}
	switch {
	case err != nil:
		run.Outcome, run.Line = OutcomeIncomplete, "a fault: "+oneLine(err.Error())
	case report.Outcome == "":
		run.Outcome, run.Line = OutcomeIncomplete, firstNonEmpty(report.Line, "it ended without saying how it went")
	default:
		run.Outcome, run.Line = report.Outcome, strings.TrimSpace(report.Line)
	}
	return run
}

// ended records a run whose context ended — stopped by the person, by codeaf
// closing, or out of time, which reads as late (out of time) — and otherwise
// the outcome it was given. late is what running out of time means for this
// kind of run: an unfinished piece of work, or a watch that couldn't check.
func (c *Clock) ended(ctx context.Context, run Run, outcome Outcome, line string, late Outcome) Run {
	if ctx.Err() != nil {
		switch cause := context.Cause(ctx); {
		case errors.Is(cause, errStopRequested):
			run.Outcome, run.Line = OutcomeStopped, lineStopped
		case errors.Is(cause, errClosing):
			run.Outcome, run.Line = OutcomeStopped, lineClosed
		default:
			run.Outcome, run.Line = late, "ran out of time"
		}
		return run
	}
	run.Outcome, run.Line = outcome, line
	return run
}

// setSeen records a watch's judgment, unless the person changed the watch
// while it was looking — their change is newer, and it reset what was seen.
func (c *Clock) setSeen(a Automation, seen string) {
	if err := c.Store.SetSeen(a.ID, a.Revision, seen); err != nil && !errors.Is(err, errChanged) {
		c.log("could not record what " + a.Title + " saw: " + err.Error())
	}
}

// finish records a run's end.
func (c *Clock) finish(run Run) {
	if run.Outcome == "" {
		run.Outcome, run.Line = OutcomeIncomplete, firstNonEmpty(run.Line, "it ended without an outcome")
	}
	run.Line = clipRunes(oneLine(run.Line), 400)
	run.Detail = clipRunes(run.Detail, 16*1024)
	if err := c.Store.Finish(run); err != nil {
		c.log(fmt.Sprintf("could not record run %d: %v", run.ID, err))
	}
}

func (c *Clock) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Clock) log(line string) {
	if c.Log != nil {
		c.Log(line)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func clipRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

// durationWords is a limit as a person says it: "30m", "1h", "1h30m".
func durationWords(d time.Duration) string {
	d = d.Round(time.Minute)
	hours, minutes := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", minutes)
}
