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
)

// ErrSpendRail is what a refused turn carries in its EventError. It is a named
// sentinel so a surface can match it with errors.Is and say the one thing worth
// saying — the rail, not a provider fault — instead of matching on words.
var ErrSpendRail = errors.New("session: the spend rail was reached")

// railBlockLocked reports why a turn may not start, or nil. It is called with
// a.mu held, from the one place turns begin.
func (a *Agent) railBlockLocked() error {
	rail := a.config.SpendRailUSD
	if rail <= 0 {
		return nil
	}
	spent := a.usage.CostUSD
	if spent < rail {
		return nil
	}
	return fmt.Errorf("%w: this session has spent $%.2f of its $%.2f rail — raise it to keep going",
		ErrSpendRail, spent, rail)
}
