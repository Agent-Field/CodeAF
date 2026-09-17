package telemetry

import (
	"sync"
	"testing"
	"time"
)

// TestSnapshotReturnsTheFieldsSessionEndedNeeds is the contract the wiring
// counts on: Snapshot hands the SessionEnded constructor exactly the six
// fields it takes, named as SessionStats names them. This compiles against
// those names, so the day either side drifts this test stops building and
// the wiring agent learns before it adapts anything.
func TestSnapshotReturnsTheFieldsSessionEndedNeeds(t *testing.T) {
	resetCountersForTest()
	defer resetCountersForTest()

	CountTurn()
	CountTurn()
	CountModelCall(true, 0.25)
	CountModelCall(false, 0)
	CountToolCall(true)
	CountToolCall(true)
	CountToolCall(false)

	// The struct SessionEnded's own constructor is about to take: field
	// names are SessionStats's, and a rename there fails this line.
	got := Snapshot()
	want := SessionStats{
		Turns:            2,
		ModelCalls:       2,
		ModelCallsFailed: 1,
		ToolCalls:        3,
		ToolCallsFailed:  1,
		CostUSD:          0.25,
	}
	if got != want {
		t.Fatalf("Snapshot() = %+v, want %+v", got, want)
	}

	// And the constructor takes it unchanged. This is the line that keeps
	// the two sides from drifting: if the constructor's signature ever
	// grows or renames, this call stops compiling.
	ended := SessionEnded(ModeTask, got, "run-for-counters", time.Unix(0, 0))
	if ended.Props["stop_reason"] != StopUnknown || ended.Props["exit_code"] != 0 {
		t.Fatalf("SessionEnded built %v", ended.Props)
	}
}

// TestCountersCountWithoutAllocating is the allocation law. Each CountX is a
// couple of atomic adds on package-level memory; a test that allocates would
// put the cost on the hot path the latency law guards.
func TestCountersCountWithoutAllocating(t *testing.T) {
	resetCountersForTest()
	defer resetCountersForTest()

	before := testing.AllocsPerRun(1000, func() {
		CountTurn()
		CountModelCall(true, 0.001)
		CountToolCall(false)
	})
	if before != 0 {
		t.Fatalf("CountTurn+CountModelCall+CountToolCall allocates %.0f bytes per run; the chokepoints call these", before)
	}
}

// TestCountersCountWhetherOrNotTelemetryIsEnabled is the off-by-design rule
// applied to the counters: the tally must not ask the ladder, so the same
// increments land whether a test runs under `go test` or the binary. Every
// state the package can be put in is exercised, and the counters come out
// identical.
func TestCountersCountWhetherOrNotTelemetryIsEnabled(t *testing.T) {
	states := []struct {
		name string
		set  func()
	}{
		{"off by default", func() {}},
	}
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			resetCountersForTest()
			defer resetCountersForTest()
			state.set()
			CountTurn()
			CountModelCall(true, 0.5)
			CountToolCall(true)
			got := Snapshot()
			if got.Turns != 1 || got.ModelCalls != 1 || got.ToolCalls != 1 {
				t.Fatalf("counters went quiet under %s: %+v", state.name, got)
			}
		})
	}
}

// TestCountersSurviveManyGoroutines is the -race test: N goroutines each do a
// fixed number of increments under a WaitGroup, and the totals must be
// exactly N-times-fixed.
func TestCountersSurviveManyGoroutines(t *testing.T) {
	resetCountersForTest()
	defer resetCountersForTest()

	const (
		goroutines = 16
		turns      = 1000
		models     = 500
		tools      = 750
		// Two costs whose float sums would drift if the tally were float:
		// 1e-6 dollars is exactly representable in micro-dollars, and
		// 16*500 of them is exact. A float tally of the same additions
		// would already be wrong in the sixth place.
		modelCost = 1e-6
	)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < turns; i++ {
				CountTurn()
			}
			for i := 0; i < models; i++ {
				// One failure in ten, so the failed counts get a
				// share of the contention too.
				CountModelCall(i%10 != 0, modelCost)
			}
			for i := 0; i < tools; i++ {
				// Every seventh tool fails, so the failed counts get a
				// share of the contention too. tools-1 is 749, which is
				// itself a multiple of 7, so the loop holds 108
				// failures — not the 750/7 a floor would guess.
				CountToolCall(i%7 != 0)
			}
		}()
	}
	wg.Wait()

	got := Snapshot()
	want := SessionStats{
		Turns:            goroutines * turns,
		ModelCalls:       goroutines * models,
		ModelCallsFailed: goroutines * (models / 10),
		ToolCalls:        goroutines * tools,
		ToolCallsFailed:  goroutines * ((tools - 1) / 7 + 1),
		CostUSD:          float64(goroutines*models) * modelCost,
	}
	if got.Turns != want.Turns ||
		got.ModelCalls != want.ModelCalls ||
		got.ModelCallsFailed != want.ModelCallsFailed ||
		got.ToolCalls != want.ToolCalls ||
		got.ToolCallsFailed != want.ToolCallsFailed {
		t.Fatalf("exact totals lost under contention:\n got %+v\nwant %+v", got, want)
	}
	// The cost is checked to 1e-6 rather than exactly, so the assertion
	// says "no drift" without claiming the division back from micros is
	// bit-identical to a float multiplication — the tally itself is exact
	// integers, and this tolerance covers only the one division Snapshot
	// does to hand back dollars.
	if diff := got.CostUSD - want.CostUSD; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("cost drifted: got %v, want %v within 1e-6", got.CostUSD, want.CostUSD)
	}
}

// TestCountersTakeAnyCostWithoutDrift is the float-cost rule. A cost that
// rounds at the sixth place must go in and come back out at the sixth
// place, and the running total must never lose a cent to accumulation.
func TestCountersTakeAnyCostWithoutDrift(t *testing.T) {
	resetCountersForTest()
	defer resetCountersForTest()

	const count = 100000 // 0.1 billionths each? No: 0.1 dollars each, 100k times.
	// Ten cents a call, ten thousand calls, is exactly $1000.00 — a number
	// that a float tally of 0.1s would miss in the third place.
	for i := 0; i < 10000; i++ {
		CountModelCall(true, 0.1)
	}
	got := Snapshot()
	if diff := got.CostUSD - 1000.0; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("accumulated cost drifted: got %v, want 1000 within 1e-6", got.CostUSD)
	}

	resetCountersForTest()
	// A cost with more places than a micro-dollar holds is rounded once,
	// at the boundary, and the same rounding is what comes back.
	CountModelCall(true, 0.0000014)
	if got := Snapshot().CostUSD; got < 0.000001 || got > 0.000002 {
		t.Fatalf("a sub-micro cost rounded to %v, want the nearest micro-dollar", got)
	}
}
