package runner

// run-state.ts's map bookkeeping. Channel-driven throughout; the one place
// that waits on a transition uses the registry's own AssertNotBusy as the
// condition.

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type statusLog struct {
	mu   sync.Mutex
	rows []string
}

func (s *statusLog) set(sessionID, status string) {
	s.mu.Lock()
	s.rows = append(s.rows, sessionID+"="+status)
	s.mu.Unlock()
}

func (s *statusLog) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.rows))
	copy(out, s.rows)
	return out
}

func noInterrupt() (string, error) { return "", errors.New("onInterrupt not expected") }

func TestRegistryReusesAndThenDropsTheRunner(t *testing.T) {
	t.Parallel()
	var st statusLog
	rg := NewRegistry[string](context.Background(), st.set)

	started, release := make(chan struct{}), make(chan struct{})
	work, _ := blockingWork("A", started, release)

	out := make(chan string, 1)
	go func() {
		v, err := rg.EnsureRunning(context.Background(), "s1", noInterrupt, work)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		out <- v
	}()
	<-started

	first := rg.Runner("s1", noInterrupt)
	if second := rg.Runner("s1", noInterrupt); second != first {
		t.Error("a second lookup while busy must return the same runner")
	}
	if err := rg.AssertNotBusy("s1"); err == nil {
		t.Error("AssertNotBusy must reject a busy session")
	} else if be, ok := err.(*BusyError); !ok || be.SessionID != "s1" {
		t.Errorf("want *BusyError{s1}, got %#v", err)
	}
	if err := rg.AssertNotBusy("s2"); err != nil {
		t.Errorf("an unknown session is not busy, got %v", err)
	}

	close(release)
	if v := <-out; v != "A" {
		t.Fatalf("got %q", v)
	}

	// onIdle deleted the entry, so the next turn gets a FRESH runner (and its
	// `ids` restart at 0).
	if third := rg.Runner("s1", noInterrupt); third == first {
		t.Error("the runner must be dropped on idle")
	}
	if got := st.snapshot(); len(got) != 1 || got[0] != "s1=idle" {
		// A plain turn never sets "busy" — only startShell does.
		t.Errorf("status log = %v, want [s1=idle]", got)
	}
}

func TestRegistryShellSetsBusyThenIdle(t *testing.T) {
	t.Parallel()
	var st statusLog
	rg := NewRegistry[string](context.Background(), st.set)

	if _, err := rg.StartShell(context.Background(), "s1", noInterrupt,
		func(context.Context) (string, error) { return "SH", nil }, nil); err != nil {
		t.Fatal(err)
	}
	got := st.snapshot()
	if len(got) != 2 || got[0] != "s1=busy" || got[1] != "s1=idle" {
		t.Errorf("status log = %v, want [s1=busy s1=idle]", got)
	}
}

func TestDesignL1094To1108RegistryBusyErrorPath(t *testing.T) {
	t.Parallel()
	rg := NewRegistry[string](context.Background(), nil)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	work, _ := blockingWork("A", started, release)
	go func() { _, _ = rg.EnsureRunning(context.Background(), "s1", noInterrupt, work) }()
	<-started

	rec := func() (v any) {
		defer func() { v = recover() }()
		_, _ = rg.StartShell(context.Background(), "s1", noInterrupt,
			func(context.Context) (string, error) { return "", nil }, nil)
		return nil
	}()
	be, ok := rec.(*BusyError)
	if !ok || be.SessionID != "s1" {
		t.Fatalf("want *BusyError{s1}, got %#v", rec)
	}
}

func TestRegistryCancelWithoutARunnerJustSetsIdle(t *testing.T) {
	t.Parallel()
	var st statusLog
	rg := NewRegistry[string](context.Background(), st.set)
	rg.Cancel("s1")
	if got := st.snapshot(); len(got) != 1 || got[0] != "s1=idle" {
		t.Errorf("status log = %v, want [s1=idle]", got)
	}
}

func TestRegistryCancelStopsTheRun(t *testing.T) {
	t.Parallel()
	rg := NewRegistry[string](context.Background(), nil)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	work, interrupted := blockingWork("A", started, release)

	out := make(chan string, 1)
	go func() {
		v, err := rg.EnsureRunning(context.Background(), "s1",
			func() (string, error) { return "@interrupted", nil }, work)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		out <- v
	}()
	<-started

	rg.Cancel("s1")
	if v := <-out; v != "@interrupted" {
		t.Errorf("got %q, want the onInterrupt value", v)
	}
	if !interrupted.Load() {
		t.Error("the work never saw its cancellation")
	}
	if err := rg.AssertNotBusy("s1"); err != nil {
		t.Errorf("session still busy after cancel: %v", err)
	}
}

// The first caller's onInterrupt is the one that sticks: run-state.ts returns
// the existing runner without rebinding its hooks. Staged through a shell so
// the second caller's arrival is observable as ShellThenRun rather than raced.
func TestRegistryKeepsTheFirstOnInterrupt(t *testing.T) {
	t.Parallel()
	rg := NewRegistry[string](context.Background(), nil)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	shellWork, _ := blockingWork("SH", started, release)

	first := make(chan string, 1)
	go func() {
		v, _ := rg.StartShell(context.Background(), "s1",
			func() (string, error) { return "@first", nil }, shellWork, nil)
		first <- v
	}()
	<-started

	second := make(chan string, 1)
	go func() {
		v, _ := rg.EnsureRunning(context.Background(), "s1",
			func() (string, error) { return "@second", nil },
			func(context.Context) (string, error) {
				t.Error("the pending work must never run")
				return "B", nil
			})
		second <- v
	}()
	waitForState(t, rg.Runner("s1", noInterrupt), "ShellThenRun")

	rg.Cancel("s1")
	if v := <-first; v != "@first" {
		t.Errorf("shell caller got %q", v)
	}
	if v := <-second; v != "@first" {
		t.Errorf("pending caller got %q, want the first onInterrupt", v)
	}
}

func TestRegistryCloseCancelsEverything(t *testing.T) {
	t.Parallel()
	rg := NewRegistry[string](context.Background(), nil)

	const n = 8
	outs := make([]chan string, n)
	for i := 0; i < n; i++ {
		started, release := make(chan struct{}), make(chan struct{})
		defer close(release)
		work, _ := blockingWork("A", started, release)
		outs[i] = make(chan string, 1)
		go func(out chan string) {
			v, err := rg.EnsureRunning(context.Background(), sessionName(i),
				func() (string, error) { return "@interrupted", nil }, work)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			out <- v
		}(outs[i])
		<-started
	}

	if err := rg.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i, out := range outs {
		if v := <-out; v != "@interrupted" {
			t.Errorf("session %d got %q", i, v)
		}
	}
	for i := 0; i < n; i++ {
		if err := rg.AssertNotBusy(sessionName(i)); err != nil {
			t.Errorf("session %d still busy: %v", i, err)
		}
	}
	// Close on an empty registry is a no-op.
	if err := rg.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func sessionName(i int) string { return string(rune('a' + i)) }

func TestRegistryNilStatusIsSafe(t *testing.T) {
	t.Parallel()
	rg := NewRegistry[string](nil, nil)
	rg.Cancel("s1")
	if _, err := rg.EnsureRunning(context.Background(), "s1", noInterrupt,
		func(context.Context) (string, error) { return "A", nil }); err != nil {
		t.Fatal(err)
	}
}
