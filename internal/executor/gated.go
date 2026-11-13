package executor

import "context"

// WorkRefusal retains the drive's sentence and the device that continued the
// chat. Surfaces may dress the facts without classifying the sentence.
type WorkRefusal struct {
	Line       string `json:"line"`
	HolderID   string `json:"holder_id,omitempty"`
	HolderName string `json:"holder_name,omitempty"`
}

func (r *WorkRefusal) Error() string { return r.Line }

// CheckWork asks the seat's existing ownership gate before a session records
// user work. A seat with no gate keeps its ordinary admission behaviour.
func CheckWork(seat Seat) error {
	if gate, ok := seat.(interface{ CheckWork() error }); ok {
		return gate.CheckWork()
	}
	return nil
}

// Gated is seat with a gate in front of every tool call: while gate answers an
// error the call is refused with that error and nothing runs, and while it
// answers nil the seat is untouched. It is how a chat that another machine
// took over stops acting (a viewer only shows). A nil gate leaves the seat as
// it was. An optional admission callback reads that same ownership fact
// without the tool gate's capture or relay wait.
func Gated(seat Seat, gate func() error, admission ...func() error) Seat {
	if gate == nil {
		return seat
	}
	work := gate
	if len(admission) > 0 {
		work = admission[0]
	}
	return gated{Seat: seat, gate: gate, work: work}
}

type gated struct {
	Seat
	gate func() error
	work func() error
}

// CheckWork reads the ownership fact before a turn or task can start.
func (g gated) CheckWork() error {
	if g.work != nil {
		return g.work()
	}
	return nil
}

// ForSetup implements SetupSeat: a setup turn's calls are gated too.
func (g gated) ForSetup() Seat { return Gated(ForSetup(g.Seat), g.gate, g.work) }

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

// Drain forwards the pending capture boundary to the seat beneath.
func (g gated) Drain(ctx context.Context) error { return Drain(ctx, g.Seat) }
