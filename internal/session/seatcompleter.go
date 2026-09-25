package session

import (
	"context"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// A CALL KNOWS WHICH SEAT MADE IT.
//
// The spend guard (spendguard.go) holds the checker to a ceiling of its own on
// each task. It used to keep that ceiling by MODEL, which works only while no
// other seat shares the checker's model — and a fresh profile's narrow fix puts
// one model in all three seats, so the ceiling had to be dropped exactly where
// the crew is most uniform. The seat is known in one place, where the run
// engine seats a task by its role (internal/run's CrewFactory), so that is where
// every call of the task is marked, and the guard reads the mark rather than
// guessing the seat from the model. A fallback to another model keeps the mark,
// because the seat did not change.

// crewSeatContextKey carries the seat a run task sits through every call it
// makes, fallbacks included.
type crewSeatContextKey struct{}

// crewSeatOf is the seat a call was made for, empty for a call no crew seat
// made (an auxiliary call, a probe), which no seat's ceiling holds.
func crewSeatOf(ctx context.Context) crewroute.Seat {
	seat, _ := ctx.Value(crewSeatContextKey{}).(crewroute.Seat)
	return seat
}

// SeatCompleter is next with every call marked as the seat's, so a guard
// attributes the call's spend to that seat even when another seat runs the
// same model or a fallback moves the seat to another. It keeps next's model
// chain, because a completer that dropped it would silently end model
// fallback for every task it wraps.
func SeatCompleter(seat crewroute.Seat, next Completer) Completer {
	marked := seatCompleter{seat: seat, next: next}
	if chain, ok := next.(modelChain); ok {
		return seatChain{seatCompleter: marked, chain: chain}
	}
	return marked
}

// seatCompleter is one seat's completer with its calls marked.
type seatCompleter struct {
	seat crewroute.Seat
	next Completer
}

func (c seatCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	return c.next.CompleteWithMessages(context.WithValue(ctx, crewSeatContextKey{}, c.seat), messages, options...)
}

// seatChain keeps the model chain of a completer that has one.
type seatChain struct {
	seatCompleter
	chain modelChain
}

func (c seatChain) FallbackModels(model string) []string { return c.chain.FallbackModels(model) }
