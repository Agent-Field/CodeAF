package session

import (
	"sync/atomic"
	"testing"
	"time"
)

// countingBudget counts every look at the job's budget, which in the engine is
// a stat of the job's document (and, for a person's Stop, a look at every job
// document on the floor).
type countingBudget struct {
	liveBudgetStub
	looks atomic.Int64
}

func (b *countingBudget) Refusal() string { b.looks.Add(1); return b.liveBudgetStub.Refusal() }

// AN IDLE MANAGER LOOKS ONCE A SECOND, NOT TEN TIMES. The fifth live A+B run's engine idled at
// 21% of a core with 82 jobs on the floor, and one of the busiest stacks was
// this watch: ten looks a second for every open manager, each a stat of its
// job's document, and a person's-Stop look that read the whole floor, though
// the manager owned nothing a Stop or a spent budget could end. The watch now
// looks ten times a second only while the manager owns work in flight.
func TestIdleManagerBudgetWatchLooksOnceASecond(t *testing.T) {
	b := &countingBudget{liveBudgetStub: liveBudgetStub{cap: 1}}
	var stopLooks atomic.Int64
	a, _ := jobsAgent(t)
	a.config.JobBudget = b
	a.config.FactoryWorkRefusal = func() string { stopLooks.Add(1); return "" }
	a.watchJobBudget()
	time.Sleep(950 * time.Millisecond)
	if n, m := b.looks.Load(), stopLooks.Load(); n > 1 || m > 1 {
		t.Fatalf("an idle manager looked at its budget %d times and at the person's Stop %d times in under a second, want at most once", n, m)
	}
	// It still looks: a budget spent while idle is noticed within a second.
	waitFor(t, "the idle watch looks again", func() bool { return b.looks.Load() >= 2 })
	// Work in flight is watched as before: a spent budget still ends it.
	id := startJob(t, a, "sleep 30")
	before := b.looks.Load()
	time.Sleep(500 * time.Millisecond)
	if n := b.looks.Load() - before; n < 3 {
		t.Fatalf("a manager with a command running looked %d times in 500ms, want about five", n)
	}
	b.Spend(1)
	waitFor(t, "budget stops owned background command", func() bool { return !a.jobs.find(id).running() })
}
