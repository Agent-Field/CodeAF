package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

type refusingSignals struct{ *fakeRuntime }

func (r refusingSignals) Signal(processgroup.Identity, bool) error {
	return errors.New("signal refused")
}

func awaitGrace(t *testing.T, c *fakeClock) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		c.mu.Lock()
		ready := false
		for _, timer := range c.timers {
			if !timer.done && timer.at.Equal(c.now.Add(StopGrace)) {
				ready = true
			}
		}
		c.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("stop never reached its grace barrier")
}

func TestRestartCannotLaunchASecondProcessWhenTheOldOneCannotStop(t *testing.T) {
	h := newHarness(t)
	h.s.rt = refusingSignals{h.rt}
	row := h.keep("owned", PolicyOnFailure)
	if _, err := h.s.Restart("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	awaitGrace(t, h.clock)
	h.clock.fireDue(StopGrace)
	settle(t, h.s)
	if h.rt.starts != 1 {
		t.Fatalf("restart launched %d processes while the original group was still alive", h.rt.starts)
	}
	got := h.get(row.ID)
	if !got.Running || got.PID != row.PID {
		t.Fatalf("a failed stop lost ownership of the original process: %+v", got)
	}
}

// refuse swaps in signals that are never delivered, under the lock the
// supervisor reads its runtime with.
func refuse(h *harness) {
	h.s.mu.Lock()
	h.s.rt = refusingSignals{h.rt}
	h.s.mu.Unlock()
}

// TestAnAutomaticRestartThatCannotStopRestsAndKeepsTheProcess: three failed
// checks, a backoff, a stop that cannot end it — the policy gives up rather
// than signal forever, nothing second is launched, and the old process's own
// listener is never called "another program".
func TestAnAutomaticRestartThatCannotStopRestsAndKeepsTheProcess(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	refuse(h)
	h.rt.portsUsed[5173] = true
	h.rt.fail("web", true)
	h.advance(30 * time.Second)
	if got := h.get(row.ID); got.State != StateRestarting {
		t.Fatalf("%+v", got)
	}
	h.clock.fireDue(FirstBackoff)
	awaitGrace(t, h.clock)
	h.clock.fireDue(StopGrace)
	settle(t, h.s)
	got := h.get(row.ID)
	if h.rt.launches != 1 || !got.Running || got.PID != row.PID || got.State != StateGaveUp || got.PortConflict ||
		strings.Contains(got.Reason, "another program") || !strings.Contains(got.Reason, "could not stop it to restart") {
		t.Fatalf("launches %d, row %+v", h.rt.launches, got)
	}
	starts := h.rt.launches
	h.advance(time.Hour)
	if h.rt.launches != starts {
		t.Fatal("a restart that could not stop its process kept trying")
	}
}

// TestTheOriginalsExitStillCountsAfterAFailedReplacement: the process a
// failed restart kept is still the row's, so its later exit is seen.
func TestTheOriginalsExitStillCountsAfterAFailedReplacement(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyNever)
	refuse(h)
	if _, err := h.s.Restart("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	awaitGrace(t, h.clock)
	h.clock.fireDue(StopGrace)
	settle(t, h.s)
	if got := h.get(row.ID); !got.Running || got.State != StateStarting {
		t.Fatalf("the failed restart put back what it found: %+v", got)
	}
	h.rt.exit(t, h.s, row.PID, 2)
	if got := h.get(row.ID); got.Running || got.State != StateFailed || got.ExitCode == nil || *got.ExitCode != 2 {
		t.Fatalf("the original's exit was ignored: %+v", got)
	}
}

// TestAStopThatCannotEndItKeepsItOwned: the row stays running and says so,
// and a stop can be asked for again.
func TestAStopThatCannotEndItKeepsItOwned(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyAlways)
	refuse(h)
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	awaitGrace(t, h.clock)
	h.clock.fireDue(StopGrace)
	settle(t, h.s)
	got := h.get(row.ID)
	if !got.Running || got.PID != row.PID || got.State != StateStopped || !strings.Contains(got.Reason, "did not stop") {
		t.Fatalf("%+v", got)
	}
	if err := h.s.Remove("agent-a", row.ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("a process that did not stop was removable: %v", err)
	}
	h.s.mu.Lock()
	h.s.rt = h.rt
	h.s.mu.Unlock()
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	if got := h.get(row.ID); got.Running {
		t.Fatalf("the second stop did not end it: %+v", got)
	}
}

// TestAReopenKeepsAProcessThatIsStillOursInAnyState: a gave-up row whose
// process could not be stopped is taken back running, not forgotten.
func TestAReopenKeepsAProcessThatIsStillOursInAnyState(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	refuse(h)
	h.rt.fail("web", true)
	h.advance(30 * time.Second)
	h.clock.fireDue(FirstBackoff)
	awaitGrace(t, h.clock)
	h.clock.fireDue(StopGrace)
	settle(t, h.s)
	if got := h.get(row.ID); got.State != StateGaveUp || !got.Running {
		t.Fatalf("%+v", got)
	}
	h.reopen()
	got := h.get(row.ID)
	if !got.Running || got.PID != row.PID || !got.ReAdopted || h.rt.launches != 1 {
		t.Fatalf("a live owned process was forgotten on reopen: %+v", got)
	}
}

// TestCloseRacingEveryVerbLeavesNothingBehind: whatever interleaving wins,
// no work starts after Close, no write lands after it, and every verb after
// it says closed.
func TestCloseRacingEveryVerbLeavesNothingBehind(t *testing.T) {
	for i := 0; i < 20; i++ {
		h := newHarness(t)
		row := h.keep("web", PolicyOnFailure)
		var wg sync.WaitGroup
		wg.Add(5)
		go func() { defer wg.Done(); _, _ = h.s.Stop("agent-a", row.ID) }()
		go func() { defer wg.Done(); _, _ = h.s.Restart("agent-a", row.ID) }()
		go func() { defer wg.Done(); _, _ = h.s.StopOwner("agent-a", "agent closed") }()
		go func() { defer wg.Done(); _, _ = h.s.SetPolicy("agent-a", row.ID, PolicyNever) }()
		go func() { defer wg.Done(); h.s.Close() }()
		wg.Wait()
		h.s.Close()
		if _, err := h.s.Stop("agent-a", row.ID); !errors.Is(err, ErrClosed) {
			t.Fatalf("stop after close: %v", err)
		}
		if _, err := h.s.Restart("agent-a", row.ID); !errors.Is(err, ErrClosed) {
			t.Fatalf("restart after close: %v", err)
		}
		if err := h.s.save(); !errors.Is(err, ErrClosed) {
			t.Fatalf("a write after close: %v", err)
		}
	}
}

// TestStopAllSaysWhatItCouldNotStop: a quit asked to stop everything is told,
// by name, which process is still running, rather than told all is well.
func TestStopAllSaysWhatItCouldNotStop(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyAlways)
	refuse(h)
	result := make(chan error, 1)
	go func() { result <- h.s.StopAll(context.Background(), "quit") }()
	awaitGrace(t, h.clock)
	h.clock.fireDue(StopGrace)
	err := <-result
	if !errors.Is(err, ErrNotStopped) || !strings.Contains(err.Error(), row.ID) {
		t.Fatalf("stop all said %v", err)
	}
	if got := h.get(row.ID); !got.Running {
		t.Fatalf("the process it could not stop is no longer listed as running: %+v", got)
	}
}

// TestStopOwnerWaitProvesItsOwnersGoneAndLeavesOthers: closing one agent
// waits for its processes to be gone, says which one would not go, and never
// stops another agent's.
func TestStopOwnerWaitProvesItsOwnersGoneAndLeavesOthers(t *testing.T) {
	h := newHarness(t)
	mine := h.keep("web", PolicyAlways)
	theirs, err := h.s.Keep("agent-b", Spec{Name: "api", Command: "node api.js", Dir: h.dir, Health: Health{Port: 4011}}, PolicyNever, Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	refuse(h)
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := h.s.StopOwnerWait(context.Background(), "agent-a", "agent closed")
		done <- result{n, err}
	}()
	awaitGrace(t, h.clock)
	h.clock.fireDue(StopGrace)
	got := <-done
	if got.n != 1 || !errors.Is(got.err, ErrNotStopped) || !strings.Contains(got.err.Error(), mine.ID) {
		t.Fatalf("a refused close said %d, %v", got.n, got.err)
	}
	if row := h.get(mine.ID); !row.Running {
		t.Fatalf("the process that would not stop is hidden: %+v", row)
	}
	if row, _ := h.s.Get("agent-b", theirs.ID); !row.Running || row.State == StateStopped {
		t.Fatalf("another agent's service was stopped: %+v", row)
	}

	// With signals that work, the same close waits for the exit and is clean.
	h.s.mu.Lock()
	h.s.rt = h.rt
	h.s.mu.Unlock()
	n, err := h.s.StopOwnerWait(context.Background(), "agent-a", "agent closed")
	if err != nil || n != 1 || h.get(mine.ID).Running {
		t.Fatalf("a working close said %d, %v: %+v", n, err, h.get(mine.ID))
	}
}

// TestStopOwnerWaitEndsWithItsContext: a close that runs out of time says so
// rather than claiming the processes are gone.
func TestStopOwnerWaitEndsWithItsContext(t *testing.T) {
	h := newHarness(t)
	mine := h.keep("web", PolicyAlways)
	refuse(h)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.s.StopOwnerWait(ctx, "agent-a", "agent closed"); !errors.Is(err, ErrNotStopped) {
		t.Fatalf("an expired wait said %v", err)
	}
	// The process then goes by itself, which ends the grace still running.
	h.rt.exit(t, h.s, mine.PID, 0)
	if got := h.get(mine.ID); got.Running {
		t.Fatalf("%+v", got)
	}
}

// TestAFencedOwnerStartsNothingNew: while an agent's close waits, neither the
// agent nor the person starts anything for it; other agents are not fenced.
func TestAFencedOwnerStartsNothingNew(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyNever)
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	release := h.s.Fence("agent-a")
	if _, err := h.s.Start("agent-a", row.ID); !errors.Is(err, ErrClosing) {
		t.Fatalf("start under a fence: %v", err)
	}
	if _, err := h.s.Restart("agent-a", row.ID); !errors.Is(err, ErrClosing) {
		t.Fatalf("restart under a fence: %v", err)
	}
	if _, err := h.s.Keep("agent-a", Spec{Name: "api", Command: "node api.js", Dir: h.dir, Health: Health{Port: 4012}}, PolicyNever, Provenance{}); !errors.Is(err, ErrClosing) {
		t.Fatalf("keep under a fence: %v", err)
	}
	if _, err := h.s.Keep("agent-b", Spec{Name: "api", Command: "node api.js", Dir: h.dir, Health: Health{Port: 4012}}, PolicyNever, Provenance{}); err != nil {
		t.Fatalf("another agent was fenced: %v", err)
	}
	release()
	release()
	if _, err := h.s.Start("agent-a", row.ID); err != nil {
		t.Fatalf("after the fence: %v", err)
	}
}

// TestTheLastRowAfterAStopIsNotStopping: a process that exits on SIGTERM is
// followed by a row that says neither running nor stopping.
func TestTheLastRowAfterAStopIsNotStopping(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyNever)
	var mu sync.Mutex
	var last Event
	h.s.Subscribe(func(e Event) { mu.Lock(); last = e; mu.Unlock() })
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := last
		mu.Unlock()
		if !got.Row.Running && !got.Row.Stopping && got.Row.ID == row.ID {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the last row said %+v", got.Row)
		}
		time.Sleep(time.Millisecond)
	}
}
