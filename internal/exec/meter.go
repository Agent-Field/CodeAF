package exec

import (
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The per-turn meter.
//
// One summed row per node was the whole of the accounting, and it cannot answer
// the question every governor, judge and benchmark actually has. A node row says
// a leaf spent 265,000 prompt tokens; it does not say whether that was two turns
// carrying 130k each or twenty-four carrying eleven, and those are opposite
// findings — the first is a node given too much material, the second is a node
// going round in circles re-sending the same material at full price. Measured on
// a real run the journal held 25 rows for 111 model calls, and half of the
// surprise metric the planner recalibrates from is turns.
//
// So the shape is journaled beside the total: what each turn sent, what it got
// back, what the provider served warm. Nothing is derived from it here — it is
// evidence, and its whole job is to be summable back to exactly the row that was
// already being written.

// TurnUsage is one turn of one leaf: the model call it made, plus whatever its
// tools spent on its behalf while it ran.
//
// Tool spend is folded into the turn that caused it rather than kept aside,
// because the invariant that makes this ledger trustworthy is that the rows sum
// to the node row — a reader who has to remember which costs were left out has
// been handed a second, quieter accounting rather than a finer one.
type TurnUsage struct {
	// Turn is the leaf's own turn number, counting from one.
	Turn int `json:"turn"`
	// Usage is this turn's whole bill, model call and tools together.
	Usage
	// Sent is in_k: what this turn's own model call put on the wire, the whole
	// prompt including the part the provider served from cache. Summed across
	// turns it is the leaf's cumulative context pressure — the quantity a
	// per-turn window bound cannot see because no single turn is large, and a
	// cache-discounted spend meter cannot see because most of it was cheap.
	Sent int `json:"sent"`
}

// meterTurn records one model call as a turn of its own and adds it to the
// running node total. The node total is written exactly as it always was; the
// turn row is the same numbers, kept separately.
func (o *Outcome) meterTurn(response *ai.Response) {
	before := o.Usage
	addUsage(&o.Usage, response)
	row := TurnUsage{Turn: o.Turns}
	row.Usage = Usage{
		Calls:            o.Usage.Calls - before.Calls,
		PromptTokens:     o.Usage.PromptTokens - before.PromptTokens,
		CompletionTokens: o.Usage.CompletionTokens - before.CompletionTokens,
		CachedTokens:     o.Usage.CachedTokens - before.CachedTokens,
		Cost:             o.Usage.Cost - before.Cost,
	}
	row.Sent = row.PromptTokens
	o.PerTurn = append(o.PerTurn, row)
}

// meterTool folds one tool's own model spend into the node total and into the
// turn that asked for it.
//
// A tool that never spends leaves both untouched, which is almost every tool. A
// result arriving with no turn to attribute it to — nothing has metered a turn
// yet — still reaches the node total, because a bill nobody can place is still a
// bill; the ledger says so by summing to less than the node row, which is the
// honest reading of "this was spent outside any turn".
func (o *Outcome) meterTool(spent Usage) {
	o.Usage.merge(spent)
	if spent == (Usage{}) || len(o.PerTurn) == 0 {
		return
	}
	o.PerTurn[len(o.PerTurn)-1].Usage.merge(spent)
}

// contextPressure is Σ over turns of in_k: every prompt token this leaf has put
// on the wire, warm or cold, counted once per send.
//
// It is read off the turn rows rather than off Usage.PromptTokens, and the
// difference is exactly the tool spend meterTool folds in: a tool that runs its
// own model inflates the node's prompt total without the leaf's own transcript
// having grown by a byte, and a bound on transcript growth that moved when a
// tool called a model would be measuring the wrong thing.
func (o *Outcome) contextPressure() int {
	total := 0
	for _, turn := range o.PerTurn {
		total += turn.Sent
	}
	return total
}

// reuseCeiling is the leaf's third bound, and the first one written against the
// model rather than against the money.
//
// The other two are both grants. spent() bounds what a leaf COSTS and weights
// cache reads at a tenth, which is honest about the bill and blind to runaway;
// rawSpent() bounds the same grant undiscounted, which is a multiple of a number
// set per task and therefore says nothing about the model doing the work. What
// neither can see is the shape underneath: Σ over turns of in_k, the quantity
// that climbs when nothing ever leaves a transcript. Measured, that sum carried
// a duplication factor of 7.45x — 90% of every input token a re-send — while a
// 265k-token node metered at 54% of its grant and 58% of its raw ceiling and
// stopped itself, unassisted, at turn 17.
//
// The bound is stated in ctxbudget, against the working set the transcript is
// allowed to fill, so it scales with the model instead of with the task's
// wallet. Zero means the window was never known — see ctxbudget.Known — and an
// unknown window is not governed: a bound derived from a guess would land honest
// leaves on evidence nobody has.
func reuseCeiling(contextTokens int) int {
	return ctxbudget.For(contextTokens).ReuseCeiling()
}

// pressureReached reports whether cumulative context pressure has crossed the
// reuse bound. An absent ceiling never fires, however much has been sent.
func pressureReached(pressure, ceiling int) bool {
	return ceiling > 0 && pressure >= ceiling
}
