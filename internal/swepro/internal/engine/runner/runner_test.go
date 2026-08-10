package runner

// Deterministic concurrency tests. No test here sleeps to "let things settle":
// work, result, latch, and state-transition rendezvous are all channels.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// blockingWork returns a work body that announces itself on `started` and then
// waits for `release` or for interruption.
func blockingWork(value string, started chan<- struct{}, release <-chan struct{}) (Work[string], *atomic.Bool) {
	var interrupted atomic.Bool
	return func(ctx context.Context) (string, error) {
		close(started)
		select {
		case <-release:
			return value, nil
		case <-ctx.Done():
			interrupted.Store(true)
			return "", context.Cause(ctx)
		}
	}, &interrupted
}

func waitForState(t *testing.T, r *Runner[string], want string) {
	t.Helper()
	states := map[string]state{
		"Idle":         stIdle,
		"Running":      stRunning,
		"Shell":        stShell,
		"ShellThenRun": stShellThenRun,
	}
	st, ok := states[want]
	if !ok {
		t.Fatalf("unknown state %q", want)
	}
	if err := r.waitForState(context.Background(), st); err != nil {
		t.Fatalf("wait for %q: %v", want, err)
	}
}

func TestRunLifecycle(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	work, _ := blockingWork("A", started, release)

	idle := make(chan struct{})
	r := New[string](context.Background(), Options[string]{OnIdle: func() { close(idle) }})

	if r.State() != "Idle" || r.Busy() {
		t.Fatalf("fresh runner is %q busy=%v", r.State(), r.Busy())
	}

	out := make(chan string, 1)
	go func() {
		v, err := r.EnsureRunning(context.Background(), work)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		out <- v
	}()

	<-started
	if s := r.State(); s != "Running" {
		t.Fatalf("state during a run is %q", s)
	}
	if !r.Busy() {
		t.Fatal("Busy() must be true while Running")
	}
	select {
	case <-idle:
		t.Fatal("onIdle fired while the run was in flight")
	default:
	}

	close(release)
	if v := <-out; v != "A" {
		t.Fatalf("got %q, want A", v)
	}
	// finishRun runs onIdle BEFORE settling the deferred, so by the time the
	// caller has its value the runner is already idle.
	select {
	case <-idle:
	default:
		t.Fatal("onIdle must have fired before the caller resumed")
	}
	if r.State() != "Idle" || r.Busy() {
		t.Fatalf("post-run state is %q busy=%v", r.State(), r.Busy())
	}
}

func TestEnsureRunningNeverCallsOnBusy(t *testing.T) {
	t.Parallel()
	// runner.ts only yields `busy` from startShell (:153). A plain turn
	// therefore never marks the session busy. [BUG-CANDIDATE], preserved.
	var busyCalls atomic.Int32
	r := New[string](context.Background(), Options[string]{OnBusy: func() { busyCalls.Add(1) }})
	if _, err := r.EnsureRunning(context.Background(), func(context.Context) (string, error) {
		return "A", nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := busyCalls.Load(); n != 0 {
		t.Errorf("onBusy called %d times by ensureRunning", n)
	}
}

func TestDesignL1008To1012EnsureRunningJoinOrStart(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	work, _ := blockingWork("first", started, release)
	r := New[string](context.Background(), Options[string]{})

	first := make(chan string, 1)
	go func() {
		v, err := r.EnsureRunning(context.Background(), work)
		if err != nil {
			t.Errorf("first call: %v", err)
		}
		first <- v
	}()
	<-started

	// A cancelled caller context makes the join return immediately after it
	// has selected the existing deferred. That is a channel-driven proof that
	// the second work was discarded before the first run is released.
	joinCtx, cancelJoin := context.WithCancel(context.Background())
	cancelJoin()
	secondStarted := make(chan struct{})
	_, err := r.EnsureRunning(joinCtx, func(context.Context) (string, error) {
		close(secondStarted)
		return "second", nil
	})
	if err == nil || !IsInterruptOnly(err) {
		t.Fatalf("joined caller = %#v, want interrupt-only", err)
	}
	select {
	case <-secondStarted:
		t.Fatal("Running must discard the joining caller's work")
	default:
	}
	if r.State() != "Running" {
		t.Fatalf("join changed state to %q", r.State())
	}

	close(release)
	if got := <-first; got != "first" {
		t.Errorf("started work returned %q", got)
	}
}

func TestCancelRunningWithOnInterrupt(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	work, interrupted := blockingWork("A", started, release)

	var idleCalls atomic.Int32
	r := New[string](context.Background(), Options[string]{
		OnIdle:      func() { idleCalls.Add(1) },
		OnInterrupt: func() (string, error) { return "@last-assistant", nil },
	})

	out := make(chan string, 1)
	go func() {
		v, err := r.EnsureRunning(context.Background(), work)
		if err != nil {
			t.Errorf("a cancelled turn with onInterrupt must not error: %v", err)
		}
		out <- v
	}()

	<-started
	r.Cancel()

	if v := <-out; v != "@last-assistant" {
		t.Errorf("got %q, want the onInterrupt value", v)
	}
	if !interrupted.Load() {
		t.Error("the work never observed its context being cancelled")
	}
	if n := idleCalls.Load(); n != 1 {
		t.Errorf("onIdle fired %d times, want exactly 1", n)
	}
	if r.State() != "Idle" {
		t.Errorf("post-cancel state is %q", r.State())
	}
}

func TestCancelRunningWithoutOnInterruptDies(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	work, _ := blockingWork("A", started, release)

	r := New[string](context.Background(), Options[string]{})
	errCh := make(chan error, 1)
	go func() {
		_, err := r.EnsureRunning(context.Background(), work)
		errCh <- err
	}()

	<-started
	r.Cancel()

	err := <-errCh
	// awaitDone's `Effect.die(new Cancelled())`.
	if err == nil || !HasDefects(err) {
		t.Fatalf("want a defect, got %#v", err)
	}
	if !errors.Is(Squash(err), ErrCancelled) {
		t.Errorf("want the squashed defect to be Cancelled, got %v", Squash(err))
	}
	if IsInterruptOnly(err) {
		t.Error("a die is not an interrupt")
	}
}

func TestCancelIdleIsSilent(t *testing.T) {
	t.Parallel()
	var idleCalls atomic.Int32
	r := New[string](context.Background(), Options[string]{OnIdle: func() { idleCalls.Add(1) }})
	r.Cancel()
	r.Cancel()
	if n := idleCalls.Load(); n != 0 {
		t.Errorf("cancelling an Idle runner ran onIdle %d times", n)
	}
	if r.State() != "Idle" {
		t.Errorf("state is %q", r.State())
	}
}

func TestStartShellRunsOnBusyThenOnIdle(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	work, _ := blockingWork("SH", started, release)

	var order []string
	var mu sync.Mutex
	record := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }

	r := New[string](context.Background(), Options[string]{
		OnIdle: func() { record("idle") },
		OnBusy: func() { record("busy") },
	})

	out := make(chan string, 1)
	go func() {
		v, err := r.StartShell(context.Background(), work, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		out <- v
	}()

	<-started
	if s := r.State(); s != "Shell" {
		t.Fatalf("state during a shell is %q", s)
	}
	close(release)
	if v := <-out; v != "SH" {
		t.Fatalf("got %q, want SH", v)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "busy" || order[1] != "idle" {
		t.Errorf("hook order = %v, want [busy idle]", order)
	}
}

func TestDesignL1014To1018StartShellPanicsWhenBusy(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	work, _ := blockingWork("A", started, release)

	r := New[string](context.Background(), Options[string]{
		Busy: func() { panic(&BusyError{SessionID: "ses_1"}) },
	})
	go func() {
		_, _ = r.EnsureRunning(context.Background(), work)
	}()
	<-started

	rec := func() (v any) {
		defer func() { v = recover() }()
		_, _ = r.StartShell(context.Background(), func(context.Context) (string, error) {
			t.Error("the second shell body must never run")
			return "", nil
		}, nil)
		return nil
	}()
	be, ok := rec.(*BusyError)
	if !ok {
		t.Fatalf("want a *BusyError panic, got %#v", rec)
	}
	if be.SessionID != "ses_1" || be.Error() != "Session ses_1 is busy" {
		t.Errorf("BusyError = %q", be.Error())
	}
}

func TestStartShellPanicsWithTheUnreachableFallback(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	work, _ := blockingWork("A", started, release)

	// No Busy hook, so runner.ts:148's `throw new Error("Runner is busy")` —
	// dead code in codeaf — becomes reachable.
	r := New[string](context.Background(), Options[string]{})
	go func() { _, _ = r.EnsureRunning(context.Background(), work) }()
	<-started

	rec := func() (v any) {
		defer func() { v = recover() }()
		_, _ = r.StartShell(context.Background(), func(context.Context) (string, error) { return "", nil }, nil)
		return nil
	}()
	err, ok := rec.(error)
	if !ok || err.Error() != "Runner is busy" {
		t.Fatalf("want the Runner is busy fallback, got %#v", rec)
	}
}

func TestDesignL1021To1023ShellThenRunHandoff(t *testing.T) {
	t.Parallel()
	shStarted, shRelease := make(chan struct{}), make(chan struct{})
	shellWork, _ := blockingWork("SH", shStarted, shRelease)
	runStarted, runRelease := make(chan struct{}), make(chan struct{})
	runWorkFn, _ := blockingWork("RUN", runStarted, runRelease)

	var idleCalls atomic.Int32
	r := New[string](context.Background(), Options[string]{OnIdle: func() { idleCalls.Add(1) }})

	shOut, runOut := make(chan string, 1), make(chan string, 1)
	go func() {
		v, err := r.StartShell(context.Background(), shellWork, nil)
		if err != nil {
			t.Errorf("shell: %v", err)
		}
		shOut <- v
	}()
	<-shStarted

	go func() {
		v, err := r.EnsureRunning(context.Background(), runWorkFn)
		if err != nil {
			t.Errorf("run: %v", err)
		}
		runOut <- v
	}()
	waitForState(t, r, "ShellThenRun")

	if n := idleCalls.Load(); n != 0 {
		t.Fatalf("onIdle fired %d times before the shell finished", n)
	}
	close(shRelease)
	<-runStarted // the promotion started the pending run
	waitForState(t, r, "Running")
	if n := idleCalls.Load(); n != 0 {
		t.Errorf("the promotion path must NOT run onIdle, saw %d", n)
	}
	if v := <-shOut; v != "SH" {
		t.Errorf("shell got %q", v)
	}
	close(runRelease)
	if v := <-runOut; v != "RUN" {
		t.Errorf("run got %q", v)
	}
	if n := idleCalls.Load(); n != 1 {
		t.Errorf("onIdle fired %d times overall, want 1", n)
	}
}

func TestCancelShellThenRunFailsThePendingRun(t *testing.T) {
	t.Parallel()
	shStarted, shRelease := make(chan struct{}), make(chan struct{})
	defer close(shRelease)
	shellWork, _ := blockingWork("SH", shStarted, shRelease)

	r := New[string](context.Background(), Options[string]{
		OnInterrupt: func() (string, error) { return "@interrupted", nil },
	})

	shOut := make(chan string, 1)
	go func() {
		v, _ := r.StartShell(context.Background(), shellWork, nil)
		shOut <- v
	}()
	<-shStarted

	runOut := make(chan string, 1)
	go func() {
		v, err := r.EnsureRunning(context.Background(), func(context.Context) (string, error) {
			t.Error("the pending run must never start after a cancel")
			return "", nil
		})
		if err != nil {
			t.Errorf("pending run: %v", err)
		}
		runOut <- v
	}()
	waitForState(t, r, "ShellThenRun")

	r.Cancel()
	if v := <-runOut; v != "@interrupted" {
		t.Errorf("pending run got %q, want the onInterrupt value", v)
	}
	if v := <-shOut; v != "@interrupted" {
		t.Errorf("shell got %q, want the onInterrupt value", v)
	}
}

// stopShell awaits the ready latch before it touches the shell — a rendezvous,
// so the shell can reach a safe point before being killed (runner.ts:110).
func TestDesignL1028To1034InterruptDuringShellWaitsForReady(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	sawCancel := make(chan struct{})
	work := func(ctx context.Context) (string, error) {
		close(started)
		<-ctx.Done()
		close(sawCancel)
		return "", context.Cause(ctx)
	}

	ready := NewLatch()
	r := New[string](context.Background(), Options[string]{
		OnInterrupt: func() (string, error) { return "@interrupted", nil },
	})

	shOut := make(chan string, 1)
	go func() {
		v, _ := r.StartShell(context.Background(), work, ready)
		shOut <- v
	}()
	<-started

	cancelDone := make(chan struct{})
	go func() { r.Cancel(); close(cancelDone) }()

	// Cancel updates the ref synchronously and only THEN blocks in stopShell,
	// so Idle is the proof that it is parked on the latch.
	waitForState(t, r, "Idle")
	select {
	case <-cancelDone:
		t.Fatal("Cancel returned without waiting for the ready latch")
	case <-sawCancel:
		t.Fatal("the shell was interrupted before the ready latch opened")
	default:
	}

	ready.Open()
	<-cancelDone
	<-sawCancel
	if v := <-shOut; v != "@interrupted" {
		t.Errorf("shell got %q", v)
	}
}

func TestShellFailurePropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	r := New[string](context.Background(), Options[string]{
		OnInterrupt: func() (string, error) { return "@interrupted", nil },
	})
	_, err := r.StartShell(context.Background(), func(context.Context) (string, error) {
		return "", boom
	}, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("want the failure to propagate, got %#v", err)
	}
	if r.State() != "Idle" {
		t.Errorf("state is %q", r.State())
	}
}

// A shell whose cause is interrupt-only resolves through onInterrupt even
// though nobody called Cancel (runner.ts:163, disjunct 1).
func TestShellInterruptOnlyCauseWithoutCancel(t *testing.T) {
	t.Parallel()
	r := New[string](context.Background(), Options[string]{
		OnInterrupt: func() (string, error) { return "@interrupted", nil },
	})
	v, err := r.StartShell(context.Background(), func(context.Context) (string, error) {
		return "", Interrupt()
	}, nil)
	if err != nil || v != "@interrupted" {
		t.Fatalf("got (%q, %v)", v, err)
	}
}

// A defect in the cause vetoes the "we asked it to stop" reading even when
// cancel WAS called (runner.ts:164's !hasDies).
func TestShellCancelledCauseWithADefectPropagates(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	ready := NewLatch()
	boom := errors.New("defect")
	work := func(ctx context.Context) (string, error) {
		close(started)
		<-ctx.Done()
		return "", NewCause(
			Reason{Kind: KindDefect, Defect: boom, Err: boom},
			Reason{Kind: KindInterrupt},
		)
	}
	r := New[string](context.Background(), Options[string]{
		OnInterrupt: func() (string, error) { return "@interrupted", nil },
	})
	errCh := make(chan error, 1)
	go func() {
		_, err := r.StartShell(context.Background(), work, ready)
		errCh <- err
	}()
	<-started
	ready.Open()
	r.Cancel()
	err := <-errCh
	if err == nil || !HasDefects(err) {
		t.Fatalf("want the cause to propagate, got %#v", err)
	}
}

// The caller's context governs the WAIT, not the run: a caller that walks away
// gets an interrupt and the run carries on to completion.
func TestCallerContextInterruptsOnlyTheWait(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	finished := make(chan struct{})
	work := func(ctx context.Context) (string, error) {
		close(started)
		<-release
		close(finished)
		return "A", nil
	}
	idle := make(chan struct{})
	r := New[string](context.Background(), Options[string]{OnIdle: func() { close(idle) }})

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := r.EnsureRunning(callerCtx, work)
		errCh <- err
	}()
	<-started
	cancelCaller()

	err := <-errCh
	if err == nil || !IsInterruptOnly(err) {
		t.Fatalf("want an interrupt-only cause, got %#v", err)
	}
	select {
	case <-finished:
		t.Fatal("the run died with its caller")
	default:
	}
	close(release)
	<-finished
	<-idle
}

func TestWorkPanicBecomesADefect(t *testing.T) {
	t.Parallel()
	r := New[string](context.Background(), Options[string]{})
	_, err := r.EnsureRunning(context.Background(), func(context.Context) (string, error) {
		panic("kaboom")
	})
	if err == nil || !HasDefects(err) {
		t.Fatalf("want a defect, got %#v", err)
	}
	if got := Squash(err).Error(); got != "kaboom" {
		t.Errorf("squashed defect = %q", got)
	}
	if r.State() != "Idle" {
		t.Errorf("a panicking run must still reach Idle, state is %q", r.State())
	}
}

func TestDesignL1050OnIdlePanicBecomesDefect(t *testing.T) {
	t.Parallel()
	r := New[string](context.Background(), Options[string]{
		OnIdle: func() { panic("idle hook") },
	})
	_, err := r.EnsureRunning(context.Background(), func(context.Context) (string, error) {
		return "A", nil
	})
	if err == nil || !HasDefects(err) {
		t.Fatalf("want onIdle panic as a defect, got %#v", err)
	}
	if got := Squash(err).Error(); got != "idle hook" {
		t.Errorf("squashed defect = %q", got)
	}
	if r.State() != "Idle" {
		t.Errorf("state after hook defect is %q", r.State())
	}
}

// Under -race this is the interesting one: every transition is serialised by
// the mutex, so however the goroutines interleave the runner ends Idle and
// onIdle fires exactly once per run that actually started.
func TestConcurrentEnsureRunningInvariants(t *testing.T) {
	t.Parallel()
	const n = 64
	var startCount, idleCount atomic.Int32
	r := New[string](context.Background(), Options[string]{OnIdle: func() { idleCount.Add(1) }})

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := r.EnsureRunning(context.Background(), func(context.Context) (string, error) {
				startCount.Add(1)
				return "A", nil
			})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if v != "A" {
				t.Errorf("got %q, want A", v)
			}
		}()
	}
	wg.Wait()

	if r.State() != "Idle" || r.Busy() {
		t.Errorf("final state is %q busy=%v", r.State(), r.Busy())
	}
	starts, idles := startCount.Load(), idleCount.Load()
	if starts < 1 || starts > n {
		t.Errorf("%d works started, want 1..%d", starts, n)
	}
	if idles != starts {
		t.Errorf("onIdle fired %d times for %d runs", idles, starts)
	}
}

func TestLatchIsIdempotent(t *testing.T) {
	t.Parallel()
	l := NewLatch()
	select {
	case <-l.Await():
		t.Fatal("a fresh latch must be closed")
	default:
	}
	l.Open()
	l.Open()
	<-l.Await()
	<-l.Await()
}
