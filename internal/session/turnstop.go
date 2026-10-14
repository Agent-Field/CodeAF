package session

import "errors"

// TurnStop names a turn ending that is a decision rather than a failure, for a
// reader that cannot hold the error itself.
//
// AN ERROR'S TYPE DOES NOT SURVIVE JSON. An engine on the far side of a wire
// sends its EventError as a sentence, and a surface that decided by
// `errors.Is(err, ErrSpendRail)` was deciding from a string that could never
// match: a turn stopped at a spending cap was drawn as a request that failed.
// So the kinds a surface is entitled to decide by are named once, here, and the
// wire carries the name beside the sentence (internal/remote's [EventWire]).
type TurnStop string

const (
	// TurnStopSpendRail is a spending cap or conversation limit ([ErrSpendRail]).
	TurnStopSpendRail TurnStop = "spend_rail"
	// TurnStopLaunchBudget is the limit a conversation was launched with
	// ([ErrLaunchBudget]).
	TurnStopLaunchBudget TurnStop = "launch_budget"
)

// turnStops is the whole list. A kind added here is on the wire the same day,
// and internal/appapi's guard fails until its mapping gives the kind a reason.
var turnStops = []struct {
	kind     TurnStop
	sentinel error
}{
	{TurnStopSpendRail, ErrSpendRail},
	{TurnStopLaunchBudget, ErrLaunchBudget},
}

// TurnStops lists every kind [TurnStopOf] can answer with.
func TurnStops() []TurnStop {
	kinds := make([]TurnStop, 0, len(turnStops))
	for _, stop := range turnStops {
		kinds = append(kinds, stop.kind)
	}
	return kinds
}

// TurnStopOf reads the kind off an error's type, never off its words. It is
// empty for an ordinary failure.
func TurnStopOf(err error) TurnStop {
	for _, stop := range turnStops {
		if errors.Is(err, stop.sentinel) {
			return stop.kind
		}
	}
	return ""
}

// TurnStopError rebuilds the typed error on the receiving side of a wire: the
// sentence the person reads, unwrapping to the sentinel a surface matches. An
// unknown kind — a newer peer's — keeps the sentence and no type, which is what
// that peer's error was before the kind existed.
func TurnStopError(kind TurnStop, said string) error {
	for _, stop := range turnStops {
		if stop.kind == kind {
			return turnStopped{said: said, sentinel: stop.sentinel}
		}
	}
	return errors.New(said)
}

type turnStopped struct {
	said     string
	sentinel error
}

func (e turnStopped) Error() string { return e.said }
func (e turnStopped) Unwrap() error { return e.sentinel }
