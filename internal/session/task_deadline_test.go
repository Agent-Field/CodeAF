package session

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// No tool completion arrives to trigger step(). The deadline must still be
// observed, and cancellation must release the producer before drain returns.
func TestASilentWorkerStillReachesItsDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := newFakeClock()
	armed := make(chan struct{}, 1)
	child := &Agent{taskNow: clock.now, taskTimer: func(after time.Duration) (<-chan time.Time, func()) {
		ch, stop := clock.timer(after)
		armed <- struct{}{}
		return ch, stop
	}}
	events := make(chan Event)
	go func() { <-ctx.Done(); close(events) }()
	run := &childRun{ctx: ctx, runCtx: ctx, stop: cancel, child: child,
		deadline: clock.now().Add(time.Minute), log: io.Discard}
	done := make(chan struct{})
	go func() { run.drain(events); close(done) }()
	select {
	case <-armed:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("a silent worker never armed its deadline; only finished tools can stop it")
	}
	clock.advance(time.Minute)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("the deadline did not cancel the silent worker")
	}
	if !strings.Contains(run.stopped, "deadline checkpoint") {
		t.Fatalf("stop reason = %q", run.stopped)
	}
}

func TestFinishingAWorkerDisarmsItsDeadline(t *testing.T) {
	clock := newFakeClock()
	child := &Agent{taskNow: clock.now, taskTimer: clock.timer}
	run := &childRun{child: child, deadline: clock.now().Add(time.Minute), log: io.Discard,
		stop: func() { t.Error("finished work was canceled") }}
	events := make(chan Event)
	close(events)
	run.drain(events)
	clock.mu.Lock()
	remaining := len(clock.timers)
	clock.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("finished worker left %d deadline timers", remaining)
	}
}

func TestAParkedCommandReachesItsDeadlineAndCanRenewOnce(t *testing.T) {
	clock := newFakeClock()
	record := &askLog{}
	suite := holdACommand(t, "checking", "finished")
	defer suite.release(t)
	completer := &scriptedCompleter{steps: []step{
		bashStep(record, "held-check", suite.text),
		sayStep(record, "The check did not finish; work remains unverified."),
	}}
	here := jobNest(t, completer)
	here.node.taskNow = clock.now
	armed := make(chan time.Duration, 8)
	here.node.taskTimer = func(after time.Duration) (<-chan time.Time, func()) {
		ch, stop := clock.timer(after)
		armed <- after
		return ch, stop
	}
	checks := 0
	here.session.config.TaskProgressCheck = func(string, []string) (bool, string) {
		checks++
		return checks == 1, "the check has exhausted its allowance"
	}
	done, stopped := runParent(t, here, taskLimits{
		maxSteps: 200, noProgress: 6, deadline: time.Minute,
	})
	waitPromoted(t, here.node)
	waitParkedOnItsCommand(t, here.node, completer, 1)
	waitTimer := func() {
		t.Helper()
		select {
		case after := <-armed:
			if after != time.Minute {
				t.Fatalf("deadline allowance = %v, want one minute", after)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the worker did not arm its deadline")
		}
	}
	waitTimer()
	clock.advance(time.Minute)
	waitTimer() // The extension must arm another bounded allowance.
	if record.asks() != 1 {
		t.Fatal("renewing the wait asked the worker to poll its unfinished command")
	}
	clock.advance(time.Minute)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the parked worker did not stop when renewal was refused")
	}
	if checks != 2 || !strings.Contains(*stopped, "deadline checkpoint") {
		t.Fatalf("checks = %d; stopped = %q", checks, *stopped)
	}
}
