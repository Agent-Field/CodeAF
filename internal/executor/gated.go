package executor

import "context"

// Gated is seat with a gate in front of every tool call: while gate answers an
// error the call is refused with that error and nothing runs, and while it
// answers nil the seat is untouched. It is how a chat that another machine
// took over stops acting (a viewer only shows). A nil gate leaves the seat as
// it was.
func Gated(seat Seat, gate func() error) Seat {
	if gate == nil {
		return seat
	}
	return gated{Seat: seat, gate: gate}
}

type gated struct {
	Seat
	gate func() error
}

// ForSetup implements SetupSeat: a setup turn's calls are gated too.
func (g gated) ForSetup() Seat { return Gated(ForSetup(g.Seat), g.gate) }

// NoteModelCall implements ModelNoter: the seat beneath keeps the record.
func (g gated) NoteModelCall(m ModelCall) { NoteModelCall(g.Seat, m) }

// Interrupted implements Interruptible: the answer is the seat beneath's.
func (g gated) Interrupted() Interrupted { return InterruptedOn(g.Seat) }

// Around implements Seat: a refused call is not run.
func (g gated) Around(ctx context.Context, call Call, run func() ([]byte, bool)) error {
	if err := g.gate(); err != nil {
		return err
	}
	return g.Seat.Around(ctx, call, run)
}

// Settle implements Settler: the seat beneath keeps the record.
func (g gated) Settle(ctx context.Context) { Settle(ctx, g.Seat) }
