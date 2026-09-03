package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestALeafsRoomGrowsToFitTheWallItRunsUnder proves A1 and A4: the wall's
// unused room reaches the leaf, and the watchdog derived from that room lands
// on the wall rather than below it.
func TestALeafsRoomGrowsToFitTheWallItRunsUnder(t *testing.T) {
	leaf := SubharnessFor(LinearSubharness)
	wall := 45 * time.Minute
	room := leaf.DeadlineWithin(150_000, wall)
	if want := wall - watchdogPad; room != want {
		t.Fatalf("a 45m wall granted %s, want %s", room, want)
	}
	if got := WatchdogAbove(room); got != wall {
		t.Fatalf("the watchdog lands at %s, want the wall %s", got, wall)
	}
}

// TestALeafsRoomIsNeverSmallerThanItsTokenGrantBought proves A2: the wall can
// widen the token-sized floor, but never take any of that floor away.
func TestALeafsRoomIsNeverSmallerThanItsTokenGrantBought(t *testing.T) {
	leaf := SubharnessFor(LinearSubharness)
	if got, want := leaf.DeadlineWithin(150_000, 5*time.Minute), leaf.Deadline(150_000); got != want {
		t.Fatalf("a short wall narrowed the leaf to %s, want its %s token floor", got, want)
	}
}

// TestNoWallLeavesALeafsRoomExactlyAsItWas proves A3 to the nanosecond over
// the floor, the first scaled grant and a larger scaled grant.
func TestNoWallLeavesALeafsRoomExactlyAsItWas(t *testing.T) {
	leaf := SubharnessFor(LinearSubharness)
	for _, tokens := range []int{0, 150_000, 1_000_000} {
		if got, want := leaf.DeadlineWithin(tokens, 0), leaf.Deadline(tokens); got != want {
			t.Errorf("%d tokens with no wall granted %s, want %s", tokens, got, want)
		}
	}
}

// TestTheDeadlineReserveIsNotSpendableByTheWork proves A5 at the late-order
// seam: the first call consumes the leaf's whole lease, and four landing turns
// still run on the reserve ordered after that lease has expired.
func TestTheDeadlineReserveIsNotSpendableByTheWork(t *testing.T) {
	space := workspace(t)
	turns := make([][]ai.ToolCall, landingTurns+1)
	for index := 1; index <= landingTurns; index++ {
		turns[index] = []ai.ToolCall{call(
			fmt.Sprintf("landing-%d", index), "write",
			fmt.Sprintf(`{"path":"landing-%d.txt","text":"landed"}`, index),
		)}
	}
	client := &scriptedCompleter{
		turns:  turns,
		delays: []time.Duration{2 * time.Second},
	}
	linear := NewLinear(client, space, nil, 10, 1_000_000, time.Second)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 21, Brief: "land the work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDeadline || outcome.Exhausted != StopDeadline {
		t.Fatalf("landing ended with stop %q and exhaustion %q, want deadline", outcome.Stop, outcome.Exhausted)
	}
	if len(client.seen) != landingTurns+1 {
		t.Fatalf("model calls = %d, want the spent work call and %d landing calls", len(client.seen), landingTurns)
	}
	for index := 1; index <= landingTurns; index++ {
		if _, err := os.Stat(filepath.Join(space.Root(), fmt.Sprintf("landing-%d.txt", index))); err != nil {
			t.Fatalf("landing turn %d did not execute: %v", index, err)
		}
	}
}

// TestALeafEndedByItsClockCarriesItsNumbers proves A6 on the arm where the
// lease expires inside one call and the landing's own clock expires inside the
// next. That arm used to name no meter at all.
func TestALeafEndedByItsClockCarriesItsNumbers(t *testing.T) {
	client := &scriptedCompleter{delays: []time.Duration{2 * time.Second, 2 * time.Second}}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Second)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 22, Brief: "work until the clock"})
	if err == nil {
		t.Fatal("a landing cut by its own clock returned no error")
	}
	if outcome.Stop != StopDeadline || outcome.Exhausted != StopDeadline {
		t.Fatalf("clock ending = stop %q, exhausted %q", outcome.Stop, outcome.Exhausted)
	}
	if outcome.Meter.Name != MeterDeadline || outcome.Meter.Unit != "seconds" {
		t.Fatalf("clock meter = %+v", outcome.Meter)
	}
	if outcome.Meter.Allowed != 1 || outcome.Meter.Reached < 1 {
		t.Fatalf("clock figures = allowed %d, reached %d", outcome.Meter.Allowed, outcome.Meter.Reached)
	}
}

// TestALeafNeverOutlivesTheWatchdogAboveItsLease proves the amended A7. A
// landing may cross the leaf's lease now, but it remains under both the node
// watchdog above that lease and the errand wall carried by the granted context.
func TestALeafNeverOutlivesTheWatchdogAboveItsLease(t *testing.T) {
	const lease = time.Second
	const wall = 1500 * time.Millisecond
	granted, cancel := context.WithTimeout(context.Background(), wall)
	defer cancel()
	client := &scriptedCompleter{delays: []time.Duration{10 * time.Second, 10 * time.Second}}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, lease)
	started := time.Now()
	outcome, err := linear.Run(granted, Task{NodeID: 23, Brief: "respect every wall"})
	elapsed := time.Since(started)
	if err == nil || outcome.Stop != StopDeadline {
		t.Fatalf("wall ending = outcome %+v, err %v", outcome, err)
	}
	if elapsed > wall+2*time.Second {
		t.Fatalf("the leaf returned after %s, past its %s errand wall", elapsed, wall)
	}
	if elapsed > WatchdogAbove(lease) {
		t.Fatalf("the leaf returned after %s, past the %s watchdog above its lease", elapsed, WatchdogAbove(lease))
	}
}
