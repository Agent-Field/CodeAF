package session

// The session's spend rail.
//
// One number, checked in one place: before a turn starts. Everything about it
// follows from where the check sits.
//
//   - IT NEVER CUTS A TURN IN HALF. A turn that has crossed the line mid-work
//     finishes: the model has files open and a tool batch in flight, and a
//     session killed between an assistant's tool_calls and their results is a
//     transcript no provider will accept back. The rail stops the NEXT turn.
//
//   - IT READS THE JOURNALED FIGURE. The check is against the session's own
//     accumulated Usage — the provider's own cost numbers, folded in per
//     response — so it is exact rather than an estimate of what a turn might
//     cost. A rail that guessed at the next turn's price would refuse turns that
//     would have been free.
//
//   - IT REFUSES BEFORE IT RECORDS. A refused turn does no work at all: the
//     message is not journaled, no request is sent, no tool runs. The person's
//     text is theirs to send again once they raise the rail, which is the only
//     honest thing to do with a message the session never answered.
//
// The daily budget in internal/config is a different rail for a different
// scope — every job on the machine, over a day. This one is one conversation's
// own ceiling, and it is off by default (SpendRailUSD 0).

import (
	"errors"
	"fmt"
	"math"
)

// SetSpendRail binds a setting written in an open chat before the next turn
// or delegated run reads the ceiling. In-flight work keeps its admitted limit.
func (a *Agent) SetSpendRail(usd float64) error {
	if usd < 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return fmt.Errorf("conversation limit must be a finite non-negative amount")
	}
	a.liveSpendRail.Store(math.Float64bits(usd))
	a.liveSpendRailSet.Store(true)
	return nil
}

func (a *Agent) spendRailUSD() float64 {
	if a.liveSpendRailSet.Load() {
		return math.Float64frombits(a.liveSpendRail.Load())
	}
	return a.config.SpendRailUSD
}

// ErrSpendRail is what a refused turn carries in its EventError. It is a named
// sentinel so a surface can match it with errors.Is and say the one thing worth
// saying — the rail, not a provider fault — instead of matching on words.
var ErrSpendRail = errors.New("session: the spend rail was reached")

// railBlockLocked reports why a turn may not start, or nil. It is called with
// a.mu held, from the one place turns begin.
func (a *Agent) railBlockLocked() error {
	if err := a.launchBudgetBlockLocked(); err != nil {
		return err
	}
	rail := a.spendRailUSD()
	if rail <= 0 {
		return nil
	}
	spent := a.usage.CostUSD
	if spent < rail {
		return nil
	}
	// THE TRIP LINE NAMES THE LIMIT, THE FIGURE AND THE DOOR, in one line, and
	// says nothing about it twice (docs/design/spending/DESIGN.md).
	//
	// It says `limit` and not `rail`: the machinery's word is this file's and the
	// person's word is theirs. And it names `/budget` rather than a bare letter,
	// because the person reading this is standing in front of a message box —
	// their refused message is still in it, theirs to send again — and every
	// printable key there belongs to that box. A door a refusal names has to be
	// one that works from where the refusal is read.
	return spendRailReached{said: fmt.Sprintf(
		"conversation limit reached · %s spent of %s · /budget changes it",
		railMoney(spent), railMoney(rail))}
}

// spendRailReached is the refused turn's error, and it exists for ONE reason:
// the sentinel's own words must not reach a person.
//
// `fmt.Errorf("%w: …", ErrSpendRail, …)` prints the sentinel in front of the
// sentence, so what a person read on the refused turn was `session: the spend
// rail was reached: conversation limit reached · …` — the machinery's name for
// the mechanism, twice, over the sentence written for them. A sentinel is
// matched with [errors.Is] and never read, so it says nothing here: this type
// carries the sentence, unwraps to the sentinel, and every existing
// `errors.Is(err, ErrSpendRail)` is unchanged.
type spendRailReached struct{ said string }

func (e spendRailReached) Error() string { return e.said }
func (e spendRailReached) Unwrap() error { return ErrSpendRail }

// railMoney writes a figure the way money is written on this line: whole dollars
// when the figure is whole, cents when it is not, and four decimals under a cent
// — because `$0.00 spent of $0.00` is a refusal that names no figure at all,
// which is the one thing a refusal has to do.
func railMoney(usd float64) string {
	switch {
	case usd == float64(int64(usd)):
		return fmt.Sprintf("$%d", int64(usd))
	case usd < 0.01:
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// railCap holds a run's fuel tank to the session's own rail.
//
// THE RAIL IS A CEILING ON EVERYTHING THIS SESSION SPENDS, and an adaptive run
// is the one thing it starts that spends money somewhere the rail cannot see: a
// run's nodes are child agents with tanks of their own, and [Agent.railBlockLocked]
// only ever stops the NEXT turn of this conversation. So a session that was given
// a cap hands out no tank larger than that cap, and the run's own gate — which
// finishes what is in flight, starts nothing new and asks the person for more
// (internal/orchestrate's Charge) — is where the figure is actually honoured.
//
// A session with no rail changes nothing: the caller's tank is the caller's, and
// zero there still means the run nobody bounded.
func (a *Agent) railCap(asked float64) float64 {
	rail := a.spendRailUSD()
	if launch := a.interactiveBudget().USD; launch > 0 && (rail <= 0 || launch < rail) {
		rail = launch
	}
	if rail <= 0 {
		return asked
	}
	if asked <= 0 || asked > rail {
		return rail
	}
	return asked
}
