package subharness

import (
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHAT A RUN COST, which is a figure only this package is in a position to add
// up.
//
// A run is one request per verify, one per free-text condition, and a whole
// bounded tool loop per agent.loop — dozens of calls a surface never sees,
// because what comes back to it is a trail of outputs and a saved trace. Until
// this ledger existed the money was not merely unwired anywhere: nothing
// captured it, so a harness RUN was free to every cost surface in the program
// while DESIGNING one was billed to the session that asked for it.
//
// It is the SUM and not a list of calls, because the door it goes through on
// the other side takes one set of figures and a call count (internal/session's
// addAuxiliaryUsage). The per-call detail that matters to a person reading a
// run afterwards is the trail, which this does not replace.
//
// NOTHING HERE LOCKS, and that is the walk's law rather than an omission: run.go
// hands the executor one node at a time, and a called harness runs inside the
// node that called it, so every fold below happens on the one goroutine the run
// has. An executor that ran lanes concurrently would own this ledger's safety
// along with everything else it changed about the walk.
type Usage struct {
	// Model is what the run ran on as the PROVIDER reported it, and it is empty
	// when the calls did not agree — a run whose nodes pinned models of their
	// own is not a run one name describes, and picking one of them would be
	// this package stating a fact nobody gave it.
	Model string

	// Calls is every request the run made, the intermediate rounds of a tool
	// loop included. A call the provider reported no usage for is still counted:
	// a missing count is not a call that did not happen.
	Calls int

	// Input and Output are the provider's token counts, summed in the same
	// spelling the session's own accounting keeps them: Input is prompt tokens
	// as reported, with cache reads beside it rather than inside it.
	Input  int
	Output int

	// CacheRead and CacheWrite are the prompt-cache accounting, read through
	// ai.Usage's accessors so that both dialects — Anthropic-native and the
	// OpenAI-style nesting — land in the same field.
	CacheRead  int
	CacheWrite int

	// CostUSD is the provider's OWN figure, summed. Zero is "the provider did
	// not say", which is not the same fact as "free" — a caller that wants a
	// price for a quiet provider has to derive one from the tokens, and a
	// caller that cannot show nothing (the emptiness law).
	CostUSD float64

	// mixed remembers that two calls named different models, so that a third
	// call agreeing with the first cannot quietly restore a name the run has
	// already outgrown.
	mixed bool
}

// Reported says whether the provider gave this run any accounting at all.
//
// It is the question a caller asks before billing somebody: a run of ten calls
// on an endpoint that publishes no usage leaves every field zero, and folding
// that into a session total would be this build claiming a run was free.
func (u Usage) Reported() bool {
	return u.Input != 0 || u.Output != 0 || u.CacheRead != 0 || u.CacheWrite != 0 || u.CostUSD != 0
}

// add folds one call in.
//
// A NIL RECEIVER IS A CALLER THAT IS NOT COUNTING, which is every existing
// caller of this package and both of its own tests' executors — the ledger is
// opt-in through [ModelExecOpts.Usage] and a bridge without one must not have
// to be written differently.
func (u *Usage) add(model string, usage *ai.Usage) {
	if u == nil {
		return
	}
	u.Calls++
	u.note(model)
	if usage == nil {
		return
	}
	u.Input += usage.PromptTokens
	u.Output += usage.CompletionTokens
	u.CacheRead += usage.CacheReadTokens()
	u.CacheWrite += usage.CacheCreationTokens()
	if usage.Cost != nil {
		u.CostUSD += *usage.Cost
	}
}

// addLoop folds a whole bounded tool loop in.
//
// IT READS THE TRACE AND NOT THE RESPONSE, and the two are not interchangeable:
// the trace already carries the loop's LAST call — both implementations record
// every round including the final one (internal/provider's recordTurnUsage and
// the SDK's own ToolCallTrace) — so a caller that added the returned response
// as well would bill that call twice.
//
// The response is read only when there is no trace with anything in it: a loop
// that failed before it recorded a round, or an implementation that reported
// only its answer, is still a run with something to pay for.
func (u *Usage) addLoop(trace *ai.ToolCallTrace, response *ai.Response) {
	if u == nil {
		return
	}
	if trace != nil && len(trace.Usage) > 0 {
		for _, turn := range trace.Usage {
			u.add(turn.Model, turn.Usage)
		}
		return
	}
	if response != nil {
		u.add(response.Model, response.Usage)
	}
}

// note records which model answered, and forgets the name the moment two calls
// disagree. A call that named no model says nothing either way — an endpoint
// that echoes no model back is not evidence that the run changed models.
func (u *Usage) note(model string) {
	if u.mixed {
		return
	}
	model = strings.TrimSpace(model)
	switch {
	case model == "":
		return
	case u.Model == "":
		u.Model = model
	case u.Model != model:
		u.Model, u.mixed = "", true
	}
}
