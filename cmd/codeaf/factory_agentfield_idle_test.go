package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// A FOLLOWED RUN THAT IS NOT MOVING DOES NOT REWRITE ITS JOB (live e2e round
// 9, idle CPU): a run waiting on a person was read every half second, and
// every reading wrote the job again with a new revision and Changed stamped
// now, waking every watcher of the factory's folder twice a second for a job
// that said the same thing. Readings that change nothing write nothing.
func TestAFollowedRunThatIsNotMovingDoesNotRewriteItsJob(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	var writes atomic.Int64
	st.ObserveWrites(func(_, _ factory.Item) { writes.Add(1) })
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "running", "2026-11-08T12:00:00Z", "")}
	r := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.Step(ctx, factoryrun.Job{Item: it, Stage: stage})
	}()
	// Let the run be dispatched and its first readings land, then count the
	// writes of readings that see the same tree.
	time.Sleep(300 * time.Millisecond)
	settled := writes.Load()
	time.Sleep(500 * time.Millisecond)
	idle := writes.Load() - settled
	cancel()
	<-done
	if idle > 0 {
		t.Fatalf("about 50 readings of a run that did not move wrote its job %d times, want none", idle)
	}
}
