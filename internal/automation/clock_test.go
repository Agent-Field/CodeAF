package automation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fast runs the clock at a test's pace for the length of one test.
func fast(t *testing.T) {
	t.Helper()
	poll, grace, wait := pollEvery, closingGrace, stopWait
	pollEvery, closingGrace, stopWait = 5*time.Millisecond, 60*time.Millisecond, 2*time.Second
	t.Cleanup(func() { pollEvery, closingGrace, stopWait = poll, grace, wait })
}

// fakeRunner scripts what looks, judgments and work come to.
type fakeRunner struct {
	mu        sync.Mutex
	judgments []Judgment
	judgeErr  error
	report    Report
	workErr   error
	block     bool // work waits for its context to end
	working   atomic.Int32
	peak      atomic.Int32
	worked    atomic.Int32
	evidence  []string
}

func (f *fakeRunner) Look(ctx context.Context, a Automation) (Sight, error) {
	return Sight{Text: "conclusion: failure", USD: 0.001}, nil
}

func (f *fakeRunner) Judge(ctx context.Context, a Automation, s Sight) (Judgment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.judgeErr != nil {
		return Judgment{}, f.judgeErr
	}
	if len(f.judgments) == 0 {
		return Judgment{Sure: true, Met: false, Line: "fine", USD: 0.002}, nil
	}
	j := f.judgments[0]
	f.judgments = f.judgments[1:]
	return j, nil
}

func (f *fakeRunner) Work(ctx context.Context, a Automation, run Run, evidence string) (Report, error) {
	now := f.working.Add(1)
	defer f.working.Add(-1)
	for {
		peak := f.peak.Load()
		if now <= peak || f.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	f.worked.Add(1)
	f.mu.Lock()
	f.evidence = append(f.evidence, evidence)
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return Report{Line: "partial", USD: 0.3}, nil
	}
	time.Sleep(20 * time.Millisecond)
	return f.report, f.workErr
}

// rig is a store, a held window and a running clock.
type rig struct {
	store  *Store
	runner *fakeRunner
	window func()
	done   chan error
	cancel context.CancelFunc
}

func startClock(t *testing.T, runner *fakeRunner) *rig {
	t.Helper()
	fast(t)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	presence := NewPresence(s.Root())
	window, err := presence.Hold("test window")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &rig{store: s, runner: runner, window: window, done: make(chan error, 1), cancel: cancel}
	clock := &Clock{Store: s, Presence: presence, Runner: runner, Log: func(line string) { t.Log("clock: " + line) }}
	go func() { r.done <- clock.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		window()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
			t.Error("the clock did not stop")
		}
	})
	return r
}

// settle waits until a run of the automation is over and returns the newest.
func (r *rig) settle(t *testing.T, id string, count int) []Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := r.store.Runs(id, 50)
		if err != nil {
			t.Fatal(err)
		}
		over := 0
		for _, run := range runs {
			if run.Phase == PhaseOver {
				over++
			}
		}
		if over >= count {
			return runs
		}
		time.Sleep(5 * time.Millisecond)
	}
	runs, _ := r.store.Runs(id, 50)
	t.Fatalf("waited for %d run(s) of %s; have %+v", count, id, runs)
	return nil
}

func (r *rig) now(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := r.store.QueueNow(id)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("could not queue a run: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestClockSaysADueReminder(t *testing.T) {
	r := startClock(t, &fakeRunner{})
	a, err := r.store.Create(reminder(time.Now().Add(-time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	runs := r.settle(t, a.ID, 1)
	if runs[0].Outcome != OutcomeDone || runs[0].Line != "time to leave" || runs[0].Why != WhyOnTime {
		t.Fatalf("run %+v", runs[0])
	}
	if got, _ := r.store.Get(a.ID); got.Status != StatusFinished {
		t.Fatalf("a said reminder is %s", got.Status)
	}
}

// A REMINDER MISSED WHILE CODEAF WAS CLOSED IS SAID LATE, NOT DROPPED.
func TestClockSaysAMissedReminderLate(t *testing.T) {
	r := startClock(t, &fakeRunner{})
	a, err := r.store.Create(reminder(time.Now().Add(-26 * time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	runs := r.settle(t, a.ID, 1)
	if runs[0].Why != WhyLate || runs[0].Outcome != OutcomeDone || runs[0].Late() < 25*time.Hour {
		t.Fatalf("run %+v (late %v)", runs[0], runs[0].Late())
	}
}

// NOTHING RUNS WHILE CODEAF IS CLOSED. With the last window gone the clock waits
// out its grace, stops the work in hand — recording it as stopped, not lost —
// and leaves.
func TestClockStopsWorkWhenTheLastWindowCloses(t *testing.T) {
	runner := &fakeRunner{block: true}
	r := startClock(t, runner)
	a, _ := r.store.Create(routine("1h"))
	r.now(t, a.ID)
	deadline := time.Now().Add(5 * time.Second)
	for runner.working.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	r.window()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("the clock ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the clock kept running with no window open")
	}
	runs, _ := r.store.Runs(a.ID, 5)
	if runs[0].Outcome != OutcomeStopped || runs[0].Line != lineClosed || runs[0].USD != 0.3 {
		t.Fatalf("the interrupted run reads %+v", runs[0])
	}
	r.done <- nil // the cleanup waits on it
}

func TestOnlyOneClockAtATime(t *testing.T) {
	r := startClock(t, &fakeRunner{})
	time.Sleep(20 * time.Millisecond)
	second := &Clock{Store: r.store, Presence: NewPresence(r.store.Root()), Runner: &fakeRunner{}}
	if err := second.Run(context.Background()); !errors.Is(err, ErrHeld) {
		t.Fatalf("a second clock = %v, want ErrHeld", err)
	}
	if !Held(r.store.Root()) {
		t.Fatal("Held says nobody holds the clock")
	}
}

func watchOn(say string, once bool) Automation {
	return Automation{
		Title: "ci", Schedule: Schedule{Every: "1h"}, Workspace: "/tmp/p",
		Look:   &Look{Command: "gh run list -L1", Condition: "the latest run failed", Once: once},
		Action: Action{Say: say},
	}
}

// A WATCH SPEAKS ON THE CHANGE. Red, still red, green, red again: it speaks on
// the first and the last, and is quiet in between.
func TestWatchSpeaksOnTheChangeAndIsQuietWhileItHolds(t *testing.T) {
	runner := &fakeRunner{judgments: []Judgment{
		{Sure: true, Met: true, Line: "red"},
		{Sure: true, Met: true, Line: "still red"},
		{Sure: true, Met: false, Line: "green"},
		{Sure: true, Met: true, Line: "red again"},
	}}
	r := startClock(t, runner)
	a, _ := r.store.Create(watchOn("CI is red", false))
	want := []Outcome{OutcomeDone, OutcomeQuiet, OutcomeQuiet, OutcomeDone}
	for i := range want {
		r.now(t, a.ID)
		r.settle(t, a.ID, i+1)
	}
	runs, _ := r.store.Runs(a.ID, 10)
	for i, w := range want {
		got := runs[len(runs)-1-i]
		if got.Outcome != w {
			t.Fatalf("look %d came to %s (%q), want %s", i+1, got.Outcome, got.Line, w)
		}
	}
	if runs[len(runs)-1].Line != "CI is red" || !strings.Contains(runs[len(runs)-1].Detail, "conclusion: failure") {
		t.Fatalf("the first speaking run reads %+v", runs[len(runs)-1])
	}
}

func TestOnceWatchFinishesAfterItSpeaks(t *testing.T) {
	r := startClock(t, &fakeRunner{judgments: []Judgment{{Sure: true, Met: true, Line: "ready"}}})
	a, _ := r.store.Create(watchOn("it is ready", true))
	r.now(t, a.ID)
	r.settle(t, a.ID, 1)
	if got, _ := r.store.Get(a.ID); got.Status != StatusFinished {
		t.Fatalf("a once-watch that spoke is %s", got.Status)
	}
}

// A MODEL THAT CANNOT DECIDE CHANGES NOTHING, AND SAYS SO. The run reads
// "couldn't check" with the reason; what the watch had seen is untouched.
func TestUnsureOrFailedJudgmentIsCouldntCheck(t *testing.T) {
	runner := &fakeRunner{judgments: []Judgment{{Sure: true, Met: true}, {Sure: false, Line: "the output was empty"}}}
	r := startClock(t, runner)
	a, _ := r.store.Create(watchOn("CI is red", false))
	r.now(t, a.ID)
	r.settle(t, a.ID, 1)
	r.now(t, a.ID)
	runs := r.settle(t, a.ID, 2)
	if runs[0].Outcome != OutcomeUnchecked || runs[0].Line != "the output was empty" {
		t.Fatalf("an unsure look reads %+v", runs[0])
	}
	if got, _ := r.store.Get(a.ID); got.Seen != "yes" {
		t.Fatalf("an unsure look changed what was seen to %q", got.Seen)
	}
	runner.mu.Lock()
	runner.judgeErr = errors.New("no API key")
	runner.mu.Unlock()
	r.now(t, a.ID)
	runs = r.settle(t, a.ID, 3)
	if runs[0].Outcome != OutcomeUnchecked || !strings.Contains(runs[0].Line, "no API key") {
		t.Fatalf("a failed judgment reads %+v", runs[0])
	}
}

func TestWorkOutcomesAreFactsNotGuesses(t *testing.T) {
	cases := []struct {
		name    string
		report  Report
		err     error
		outcome Outcome
		line    string
	}{
		{"reported done", Report{Outcome: OutcomeDone, Line: "drafted"}, nil, OutcomeDone, "drafted"},
		{"needs the person", Report{Outcome: OutcomeYourCall, Line: "needed your ok to run gh"}, nil, OutcomeYourCall, "needed your ok to run gh"},
		{"said something but never reported", Report{Line: "I'll start by…"}, nil, OutcomeIncomplete, "I'll start by…"},
		{"said nothing at all", Report{}, nil, OutcomeIncomplete, "it ended without saying how it went"},
		{"a fault", Report{}, errors.New("provider refused"), OutcomeIncomplete, "a fault: provider refused"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := startClock(t, &fakeRunner{report: c.report, workErr: c.err})
			a, _ := r.store.Create(routine("1h"))
			r.now(t, a.ID)
			runs := r.settle(t, a.ID, 1)
			if runs[0].Outcome != c.outcome || runs[0].Line != c.line {
				t.Fatalf("came to %s %q, want %s %q", runs[0].Outcome, runs[0].Line, c.outcome, c.line)
			}
		})
	}
}

func TestWorkThatRunsOutOfTimeIsIncomplete(t *testing.T) {
	r := startClock(t, &fakeRunner{block: true})
	a := routine("1h")
	a.Limits.Time = 30 * time.Millisecond
	a, _ = r.store.Create(a)
	r.now(t, a.ID)
	runs := r.settle(t, a.ID, 1)
	if runs[0].Outcome != OutcomeIncomplete || !strings.HasPrefix(runs[0].Line, "ran out of its") {
		t.Fatalf("a run past its limit reads %+v", runs[0])
	}
}

func TestStopRequestStopsARun(t *testing.T) {
	runner := &fakeRunner{block: true}
	r := startClock(t, runner)
	a, _ := r.store.Create(routine("1h"))
	r.now(t, a.ID)
	deadline := time.Now().Add(5 * time.Second)
	var run Run
	for time.Now().Before(deadline) {
		active, _ := r.store.Active()
		if len(active) == 1 && active[0].Phase == PhaseRunning {
			run = active[0]
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := r.store.RequestStop(run.ID); err != nil {
		t.Fatal(err)
	}
	runs := r.settle(t, a.ID, 1)
	if runs[0].Outcome != OutcomeStopped || runs[0].Line != lineStopped {
		t.Fatalf("a stopped run reads %+v", runs[0])
	}
}

// TWO PIECES OF WORK AT ONCE, the rest wait their turn.
func TestWorkRunsTwoAtATime(t *testing.T) {
	runner := &fakeRunner{report: Report{Outcome: OutcomeDone, Line: "ok"}}
	r := startClock(t, runner)
	var ids []string
	for i := 0; i < 5; i++ {
		a, _ := r.store.Create(routine("1h"))
		ids = append(ids, a.ID)
		r.now(t, a.ID)
	}
	for _, id := range ids {
		r.settle(t, id, 1)
	}
	if peak := runner.peak.Load(); peak > MaxWork {
		t.Fatalf("%d pieces of work ran at once, the limit is %d", peak, MaxWork)
	}
	if runner.worked.Load() != 5 {
		t.Fatalf("worked %d times, want 5", runner.worked.Load())
	}
}

// A SLOT THAT COMES WHILE THE LAST RUN IS STILL GOING IS PASSED OVER.
func TestASlotIsSkippedWhileItsLastRunIsStillGoing(t *testing.T) {
	runner := &fakeRunner{block: true}
	r := startClock(t, runner)
	a, _ := r.store.Create(routine("1m"))
	r.now(t, a.ID)
	deadline := time.Now().Add(5 * time.Second)
	for runner.working.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	// Force the slot due while the run holds it.
	if _, err := r.store.db.Exec(`UPDATE automations SET next_ms = ? WHERE id = ?`, ms(time.Now().Add(-time.Second)), a.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	active, _ := r.store.Active()
	if len(active) != 1 {
		t.Fatalf("a busy automation queued another run: %+v", active)
	}
	if got, _ := r.store.Get(a.ID); !got.Next.After(time.Now()) {
		t.Fatalf("the skipped slot was not moved on: next %v", got.Next)
	}
}

func TestWatchWorkGetsTheEvidence(t *testing.T) {
	runner := &fakeRunner{judgments: []Judgment{{Sure: true, Met: true, Line: "red", USD: 0.002}}, report: Report{Outcome: OutcomeDone, Line: "fixed"}}
	r := startClock(t, runner)
	a := watchOn("", false)
	a.Action = Action{Do: "find out why CI failed"}
	a, err := r.store.Create(a)
	if err != nil {
		t.Fatal(err)
	}
	r.now(t, a.ID)
	runs := r.settle(t, a.ID, 1)
	if runs[0].Outcome != OutcomeDone || runs[0].Line != "fixed" {
		t.Fatalf("watch work reads %+v", runs[0])
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.evidence) != 1 || runner.evidence[0] != "conclusion: failure" {
		t.Fatalf("the work was handed %q", runner.evidence)
	}
	if want := 0.001 + 0.002; runs[0].USD < want-1e-9 || runs[0].USD > want+1e-9 {
		// the fake work reports no cost; the look and the judgment do
		t.Fatalf("usd = %v, want %v", runs[0].USD, want)
	}
}

// THE MEMORY PASS RIDES THE CLOCK: asked while a window is open, once per
// [tidyEvery] and never twice at once, and its line goes to the clock's log.
func TestTheClockAsksTheMemoryPassWhileAWindowIsOpen(t *testing.T) {
	fast(t)
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	release, err := NewPresence(root).Hold("test window")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	asked := make(chan struct{}, 4)
	var logged []string
	var logMu sync.Mutex
	clock := &Clock{Store: store, Presence: NewPresence(root), Runner: &fakeRunner{},
		Tidy: func(context.Context) (Tidied, error) {
			asked <- struct{}{}
			return Tidied{Merged: 2}, nil
		},
		Log: func(line string) { logMu.Lock(); logged = append(logged, line); logMu.Unlock() },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- clock.Run(ctx) }()
	select {
	case <-asked:
	case <-time.After(10 * time.Second):
		t.Fatal("the clock never asked the memory pass")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
		t.Fatal("the pass was asked twice inside one tidy interval")
	default:
	}
	logMu.Lock()
	defer logMu.Unlock()
	if len(logged) == 0 || logged[len(logged)-1] != "consolidated · 2 merged" {
		t.Fatalf("the pass's line did not reach the log: %q", logged)
	}
}
