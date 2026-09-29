package executor

import (
	"context"
	"os/exec"
)

// Runner is what a tool site needs from its seat: buffered or streamed calls
// through Exec, and an unstarted command through Command for a process whose
// lifetime the site owns.
type Runner interface {
	Executor
	Command(ctx context.Context, req ExecRequest) (*exec.Cmd, error)
}

var _ Runner = Local{}

// Seat is the executor a session owns. Tools ask it for a Runner rooted at the
// directory they work in; the class, and whatever records the work, belong to
// the seat and never to the site.
type Seat interface {
	In(dir string) Runner
	// Around runs one tool call (docs/ARCHITECTURE.md 4.3: the boundary the
	// tree is sealed at) and hands run's output and verdict to whatever keeps
	// the record. The tool's own result is run's business; the error is only
	// ever the seat's, and means the call was not run.
	Around(ctx context.Context, tool string, args []byte, run func() (output []byte, failed bool)) error
}

// Stance is a seat that runs every call under one declared class and records
// none of them.
type Stance struct{ Class Class }

// In implements Seat.
func (s Stance) In(dir string) Runner {
	return Local{Root: dir, Class: s.Class, Jail: DefaultJail()}
}

// Around implements Seat: a stance keeps no record, so the call just runs.
func (Stance) Around(_ context.Context, _ string, _ []byte, run func() ([]byte, bool)) error {
	run()
	return nil
}

// Host is the seat of work that belongs to no session (plumbing outside a chat,
// and every session while cells are off): unjailed, network open, unrecorded.
// A site that means this says so by name.
var Host Seat = Stance{Class: HostBound}

type seatKey struct{}

// With returns a context whose tool calls run on seat.
func With(ctx context.Context, seat Seat) context.Context {
	if seat == nil {
		return ctx
	}
	return context.WithValue(ctx, seatKey{}, seat)
}

// For is the seat of the session whose call ctx belongs to. A context that
// carries no session is plumbing, and plumbing runs on [Host].
func For(ctx context.Context) Seat {
	if seat, ok := ctx.Value(seatKey{}).(Seat); ok {
		return seat
	}
	return Host
}
