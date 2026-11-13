package run

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// lingerExec's first round outlives its cancelled context until linger is
// closed, as a model's turn does when it is cut mid-call; every later round
// finishes at once.
type lingerExec struct {
	calls   atomic.Int32
	started chan struct{}
	linger  chan struct{}
}

func (e *lingerExec) Run(ctx context.Context, job Job) (factory.StageResult, error) {
	if e.calls.Add(1) == 1 {
		close(e.started)
		<-e.linger
		return factory.StageResult{}, ctx.Err()
	}
	return factory.StageResult{Done: true}, nil
}

// A WITHDRAWN DRIVE THAT IS STILL ENDING DOES NOT SWALLOW THE NEXT LEASE'S
// RESUME. A step edit withdraws the worker running a job and the factory
// offers the job again; the next lease lays it out paused at the step it was
// on and asks the runner to go on (worker.stand). In the live managers lane the
// withdrawn Write was still in its model call when that ask came: the runner
// found the old, cancelled control, read "not paused, asked not paused" as
// nothing to do, and the job was given back to Backlog the moment the old
// drive let go (job 61 on 2026-11-07, "new · write running"). The ask must
// wait for the old drive and then run the job on from its step.
func TestGoOnAfterAWithdrawWaitsForTheOldDriveAndRunsOn(t *testing.T) {
	e := &lingerExec{started: make(chan struct{}), linger: make(chan struct{})}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: e}, nil)
	id := g.add("one", chat("write"), chat("test"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	<-e.started
	g.r.Withdraw(id)
	// The next lease's stand: the job as the factory handed it over again,
	// paused at Write, while the withdrawn drive is still ending.
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Stream.Paused = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	resumed := make(chan error, 1)
	go func() { resumed <- g.r.Hold(id, false) }()
	// The old drive ends only once the ask has either answered or is waiting
	// on it, so the two are never left to a race.
	var answered error
	for done := false; !done; runtime.Gosched() {
		select {
		case answered = <-resumed:
			done = true
			resumed <- answered
		default:
			if c := g.r.loop().ctl(id); c != nil {
				g.r.loop().mu.Lock()
				done = c.releasedDone != nil
				g.r.loop().mu.Unlock()
			}
		}
	}
	close(e.linger)
	if err := <-resumed; err != nil {
		t.Fatalf("go on after the withdraw = %v", err)
	}
	it := g.waitState(id, factory.StateLanded)
	if n := e.calls.Load(); n != 3 {
		t.Fatalf("the executor ran %d rounds, want the cut Write, Write again and Test", n)
	}
	if it.Stream.Paused {
		t.Fatal("the job landed still marked paused")
	}
}
