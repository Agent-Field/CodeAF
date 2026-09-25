package run_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// A SLOT COUNT OF ZERO IS NO BOUND. It is the figure the chat door hands the
// engine out of the box — `task.parallel` is 0 and 0 means no limit — and the
// supervisor used to read it as one, so every ready leaf of a conversation's
// task ran after the one before it while the setting promised otherwise. The
// test hands the run more ready leaves than any small bound would admit and
// asks the seat how many it held at once.
func TestSupervisorLaunchesEveryReadyLeafAtOnceWithNoSlotBound(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	var leaves []plandb.TaskSpec
	for _, id := range []string{"l1", "l2", "l3", "l4", "l5", "l6"} {
		leaves = append(leaves, plandb.TaskSpec{ID: id, Title: id})
		// Each leaf holds its seat long enough that six launched together are
		// six on the seat together; a serial run would show a peak of one.
		seat.actions[id] = holdSeat(150 * time.Millisecond)
	}
	seat.actions["root"] = splitRoot(t, store, leaves...)
	supervisor := run.NewSupervisor(store, t.TempDir(), 0, run.Limits{}, seat.workerFor)

	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if peak := seat.peakConcurrency(); peak != len(leaves) {
		t.Fatalf("peak concurrency = %d under no slot bound, want all %d leaves on the seat together", peak, len(leaves))
	}
}

// MORE WORKERS OUT THAN THE RETURN CHANNEL HOLDS MUST STILL DRAIN. The channel
// workers hand their returns through has a fixed depth on an unbounded run, so
// a drain that waited for every goroutine before reading a return would wait
// on a worker that is itself waiting to be read. Two cheap leaves come home
// fast and spend the run past its dollar limit while the other thirty-eight
// are still on their seats, which is the road that reaches drain with workers
// in flight; the test is that Run comes back. It carries a wall of its own
// because a deadlock inside drain never looks at the run's context.
func TestAnUnboundedRunEndedEarlyStillDrainsEveryWorker(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	var leaves []plandb.TaskSpec
	for i := 0; i < 40; i++ {
		id := "l" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		leaves = append(leaves, plandb.TaskSpec{ID: id, Title: id})
		hold := 3 * time.Second
		if i < 2 {
			hold = 300 * time.Millisecond
		}
		seat.actions[id] = holdSeat(hold)
	}
	seat.actions["root"] = splitRoot(t, store, leaves...)
	// holdSeat banks $0.30 a leaf, so the two fast ones alone cross the limit.
	supervisor := run.NewSupervisor(store, t.TempDir(), 0, run.Limits{CostUSD: 0.50}, seat.workerFor)

	outcomes := make(chan run.Outcome, 1)
	go func() { outcomes <- supervisor.Run(ctx) }()
	var outcome run.Outcome
	select {
	case outcome = <-outcomes:
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not come back: drain is waiting on workers that are waiting to hand in their returns")
	}

	if outcome != run.OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeLimit)
	}
	// The property under test is that more workers were out than the channel
	// could hold, not the exact count, which is why the bar is the depth and
	// not the number of leaves.
	if peak := seat.peakConcurrency(); peak <= 16 {
		t.Fatalf("peak concurrency = %d, want more workers out than the return channel holds", peak)
	}
}
