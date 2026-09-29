package executor

import (
	"context"
	"errors"
	"os/exec"
	"sync"
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
	Around(ctx context.Context, call Call, run func() (output []byte, failed bool)) error
}

// Call is one tool call as the seat is told of it.
type Call struct {
	Tool string
	Args []byte
	// Changed is the workspace-relative paths the call is known to change; nil
	// means unknown, and the whole tree is then looked at.
	Changed []string
	// Spawns, when set, is told of every process group the tool starts for the
	// call, so a record of the call can name what to end if the harness dies
	// while it runs. Nil is a call nobody follows.
	Spawns *Spawns
}

// Stance is a seat that runs every call under one declared class and records
// none of them. Setup makes every call one of a setup turn (network open,
// external); Observer, when set, sees each call that ran to a result.
type Stance struct {
	Class    Class
	Setup    bool
	Observer Observer
	Secrets  SecretSource // added to every process's environment; nil adds none
}

// In implements Seat.
func (s Stance) In(dir string) Runner {
	return Local{Root: dir, Class: s.Class, Jail: DefaultJail(), Setup: s.Setup, Observer: s.Observer, Secrets: s.Secrets}
}

// ForSetup is the same stance for a setup turn's calls.
func (s Stance) ForSetup() Seat {
	s.Setup = true
	return s
}

// SetupSeat is a seat that can run a setup turn's calls.
type SetupSeat interface {
	Seat
	ForSetup() Seat
}

// ForSetup is the seat a setup turn's calls run on. A seat that cannot tell a
// setup call apart is returned as it is.
func ForSetup(s Seat) Seat {
	if setup, ok := s.(SetupSeat); ok {
		return setup.ForSetup()
	}
	return s
}

// Around implements Seat: a stance keeps no record, so the call just runs.
func (Stance) Around(_ context.Context, _ Call, run func() ([]byte, bool)) error {
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

// ErrNoSeat is what a tool call answers when its context carries no session.
// Work that belongs to no session says so with [Host]; a tool call that lost
// its session is a bug, and running it unjailed on the host would hide it.
var ErrNoSeat = errors.New("executor: this call carries no session seat")

// For is the seat of the session whose call ctx belongs to. A context that
// carries none gets a seat that refuses every call with [ErrNoSeat].
func For(ctx context.Context) Seat {
	if seat, ok := ctx.Value(seatKey{}).(Seat); ok {
		return seat
	}
	if testSeat != nil {
		return testSeat
	}
	return unseated{}
}

// testSeat is what a test binary that runs tools without a session says its
// calls run on. It is set by internal/executor/executortest and nowhere else
// (law_test.go), so no production binary has it.
var testSeat Seat

// UseInTests names the seat a test binary's seatless calls run on. Only
// executortest may call it.
func UseInTests(seat Seat) { testSeat = seat }

type unseated struct{ Stance }

// ForSetup implements SetupSeat: a session that was lost stays lost in a setup
// turn, so a promoted Stance form never lets its calls run.
func (u unseated) ForSetup() Seat { return u }

// In implements Seat.
func (unseated) In(string) Runner { return refused{} }

type refused struct{ Local }

// Exec implements Executor.
func (refused) Exec(context.Context, ExecRequest, func(Chunk)) (ExecResult, error) {
	return ExecResult{}, ErrNoSeat
}

// Command implements Runner.
func (refused) Command(context.Context, ExecRequest) (*exec.Cmd, error) { return nil, ErrNoSeat }

// Spawns is where one tool call reports the process groups it starts. The call
// owns the process (a shell tool starts it through Command), so only the tool
// knows the moment it exists; the record around the call listens here.
type Spawns struct {
	mu sync.Mutex
	on func(pgid int)
}

type spawnsKey struct{}

// WithSpawns returns a context whose tool calls report into the Spawns handed
// back, which the caller puts on the [Call] it gives the seat.
func WithSpawns(ctx context.Context) (context.Context, *Spawns) {
	s := &Spawns{}
	return context.WithValue(ctx, spawnsKey{}, s), s
}

// Watch sets who is told of each group. A nil Spawns hears nobody.
func (s *Spawns) Watch(on func(pgid int)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = on
}

// Spawned is a tool's word that it started the process group pgid for the call
// ctx belongs to. A context with no call to tell makes it a no-op.
func Spawned(ctx context.Context, pgid int) {
	s, _ := ctx.Value(spawnsKey{}).(*Spawns)
	if s == nil {
		return
	}
	s.mu.Lock()
	on := s.on
	s.mu.Unlock()
	if on != nil {
		on(pgid)
	}
}

// Interrupted is what a crash left unfinished on a seat: calls that began and
// never ended. Lines are for the person, Note is for the model's next turn, and
// Close is said once both were told, so the calls stop counting as open.
type Interrupted interface {
	Lines() []string
	Note() string
	Close() error
}

// Interruptible is a seat that can name what a crash left behind.
type Interruptible interface {
	Interrupted() Interrupted
}

// InterruptedOn is what the seat's last run left unfinished, and nil for a seat
// that keeps no record.
func InterruptedOn(s Seat) Interrupted {
	if i, ok := s.(Interruptible); ok {
		return i.Interrupted()
	}
	return nil
}
