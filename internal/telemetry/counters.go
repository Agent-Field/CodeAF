package telemetry

import (
	"math"
	"sync/atomic"
)

// The session counters are the three chokepoints a run passes through — a
// turn sealed, a model call answered, a tool call answered — and the dollar
// figure those model calls spent. They are the numbers [SessionEnded] needs,
// arrived at WITHOUT the binary having to carry its own tally: the wiring
// calls CountTurn, CountModelCall and CountToolCall on the paths the events
// describe, and Snapshot hands the constructor exactly the [SessionStats]
// fields it takes, so nothing between the two adapts anything.
//
// THE COUNTERS COUNT WHETHER OR NOT TELEMETRY IS ENABLED, and they do it by
// never asking. There is no ladder read here, no enabledFor(), no branch on
// the opt-out at all: counting three integers costs a few nanoseconds and
// nothing else, so the decision about whether to SEND them is taken once,
// later, by the event path — where it belongs. A counter that went quiet
// when telemetry was off would make Snapshot lie about a run that DID
// happen, and a counter that asked the ladder on every model call would put
// the opt-out check on the hot path the latency law guards.

// counters is the process's one tally, package-level because the chokepoints
// calling it sit in internal/session and internal/provider, which reach the
// package as a whole rather than through an object a run would have to carry.
// Every field is an atomic int64, which is the whole allocation story: each
// CountX is one Add and one conditional Add, on memory that exists before the
// first session and never grows.
var counters struct {
	turns      atomic.Int64
	modelCalls atomic.Int64
	modelFail  atomic.Int64
	toolCalls  atomic.Int64
	toolFail   atomic.Int64
	// costMicro is the session's spend in integer MICRO-DOLLARS, not float
	// dollars. A float tally added to N times accumulates N rounding errors
	// and the drift is invisible until two paths disagree about a cost that
	// never happened; an integer tally of 1e-6 dollar units is exact for
	// every cost a provider reports at six places, and the one rounding —
	// in costToMicro, at the boundary — is the only one there is.
	costMicro atomic.Int64
}

// costToMicro is the single float-to-integer crossing. math.Round rather than
// truncation so a cost of 0.0000005 cents up rather than down: truncating at
// the boundary takes money off a person's own total, and rounding to nearest
// is the honest reading of "the record's cost".
func costToMicro(costUSD float64) int64 {
	return int64(math.Round(costUSD * 1e6))
}

// microToCost is the crossing back, in Snapshot only, so the constructor
// receives the float dollars its SessionStats field names.
func microToCost(micro int64) float64 {
	return float64(micro) / 1e6
}

// CountTurn records one sealed turn. It is one atomic add and nothing else:
// no allocation, no ladder read, no lock.
func CountTurn() { counters.turns.Add(1) }

// CountModelCall records one model call answered: ok false also counts the
// failure, because model_calls_failed is a field the constructor needs and a
// second call at the chokepoint would double-count the call itself. costUSD
// is folded into the integer tally; a call with no cost figure hands over 0
// and adds nothing, which is the record's own answer.
func CountModelCall(ok bool, costUSD float64) {
	counters.modelCalls.Add(1)
	if !ok {
		counters.modelFail.Add(1)
	}
	counters.costMicro.Add(costToMicro(costUSD))
}

// CountToolCall records one tool call answered, with its own failure count
// for the same reason [CountModelCall] keeps one.
func CountToolCall(ok bool) {
	counters.toolCalls.Add(1)
	if !ok {
		counters.toolFail.Add(1)
	}
}

// Snapshot is the tally as the SessionEnded constructor takes it: the
// SessionStats fields it needs — turns, model calls, model calls failed,
// tool calls, tool calls failed, cost — named as SessionStats names them, so
// the wiring hands the struct straight to the constructor without adapting.
// It builds ONE struct and reads each atomic once; the six numbers are
// therefore not from a single instant, which is fine — a run's last model
// call can land after its last turn sealed, and a snapshot that could not be
// taken mid-run would be a snapshot that could not be taken at all.
//
// The fields SessionEnded fills and Snapshot does not return — Duration,
// StopReason, ExitCode — belong to the run's END, which is the caller's fact:
// elapsed time and exit status live where the process does, and Snapshot
// carries only what the chokepoints counted.
func Snapshot() SessionStats {
	return SessionStats{
		Turns:            int(counters.turns.Load()),
		ModelCalls:       int(counters.modelCalls.Load()),
		ModelCallsFailed: int(counters.modelFail.Load()),
		ToolCalls:        int(counters.toolCalls.Load()),
		ToolCallsFailed:  int(counters.toolFail.Load()),
		CostUSD:          microToCost(counters.costMicro.Load()),
	}
}

// resetCountersForTest zeroes the tally. The counters are deliberately not
// reset between tests — they are process-wide state the way any global
// counter is — but a test that asserts exact totals cannot share a tally
// with the test before it.
func resetCountersForTest() {
	counters.turns.Store(0)
	counters.modelCalls.Store(0)
	counters.modelFail.Store(0)
	counters.toolCalls.Store(0)
	counters.toolFail.Store(0)
	counters.costMicro.Store(0)
}
