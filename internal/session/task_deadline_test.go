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

// AND A PARKED COMMAND IS NOT WHAT THE DEADLINE IS FOR ANY MORE. The deadline is
// a bound on how long a node may WORK, and the runner watches it for the case
// where nothing else will notice ([childRun.drain]) — but a worker parked on a
// command it started is not working, it is WAITING, and its wait has a bound of
// its own ([jobParkBoundShare], a third of the allowance, well before the wall).
// So the deadline checkpoint never fires for a parked command: the park hands the
// turn back with its own record first, and the command is left running. The law
// this replaces — a parked worker reaching the deadline and renewing it — was
// exactly the silent hour the park's own bound exists to cut short.
func TestAParkedCommandIsHandedBackAtItsOwnBoundBeforeItsDeadline(t *testing.T) {
	clock := newFakeClock()
	record := &askLog{}
	suite := holdACommand(t, "checking", "finished")
	defer suite.release(t)
	completer := &scriptedCompleter{steps: []step{
		bashStep(record, "held-check", suite.text),
		sayStep(record, "The check is still running; here is where I got to."),
	}}
	here := jobNest(t, completer)
	here.node.taskNow = clock.now
	here.node.taskTimer = clock.timer
	checks := 0
	here.session.config.TaskProgressCheck = func(string, []string) (bool, string) {
		checks++
		return false, "the check has exhausted its allowance"
	}

	const allowance = time.Minute
	done, stopped := runParent(t, here, taskLimits{maxSteps: 200, noProgress: 6, deadline: allowance})

	waitPromoted(t, here.node)
	waitParkedOnItsCommand(t, here.node, completer, theCallAlone)
	// Advancing only the park's share of the wall hands the turn back; the whole
	// allowance, where the deadline sits, is never reached.
	clock.advance(allowance / jobParkBoundShare)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parked worker never came back at the park's own bound")
	}
	if checks != 0 {
		t.Fatalf("the deadline checkpoint ran %d times while the worker was parked, want the park's own bound to end the wait first", checks)
	}
	if *stopped != "" {
		t.Fatalf("the worker was stopped with %q, want the park's bound to hand the turn back rather than the deadline", *stopped)
	}
	if got := completer.requests(); got != 2 {
		t.Fatalf("the worker was asked %d times, want the call and the turn that reads the park's record", got)
	}
	if read := userTextIn(completer.request(1)); !strings.Contains(read, jobParkBoundReason) {
		t.Fatalf("the turn after the park's bound reads %q, want the bound's own reason", read)
	}
	// AND THE COMMAND IS STILL RUNNING: the bound ended the wait, not the work.
	if one := here.node.jobs.find(1); one == nil || !one.running() {
		t.Fatal("the park's bound cut a command that was still running")
	}
}
