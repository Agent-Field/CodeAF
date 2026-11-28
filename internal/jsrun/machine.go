package jsrun

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dop251/goja"
)

// THE MACHINE: one goja interpreter, its three ceilings and its recorder.
//
// It is what a host runs ON, and nothing about what a host EXPOSES. The saved
// program's host (host.go) binds the six doors of [exec.Env] onto it; anything
// else this package runs binds its own surface onto the same machine and gets
// the same ceilings, the same uncatchable stop and the same journal ordering
// without a second copy of any of them. Keeping the machine free of bundles,
// manifests and prompts is the point of the split: those are facts about a
// saved program, and a machine that knew them could only ever run one.
type machine struct {
	ctx     context.Context
	vm      *goja.Runtime
	rec     *recorder
	limits  limits
	unwatch func()
	// halted carries the watchdog's reason to a host waiting outside the
	// interpreter (fuel.go's watch). It holds one reason and is never closed.
	halted chan string
}

// limits are the three ceilings with every default already applied. The host
// that builds a machine resolves them ([Fuel.steps], [Fuel.wall]); the machine
// only enforces, so no default is spelled twice.
type limits struct {
	// steps is how many host calls the program may make.
	steps int
	// tokens is the ceiling on the recorder's ledger. Zero is no ceiling.
	tokens int
	// wall is the deadline the watchdog enforces.
	wall time.Duration
	// stepUnit names what steps counts in the sentence a spent ceiling gives;
	// empty is "steps". A script's ceiling is on tool calls, and calling them
	// steps would read as the task's own step budget ending.
	stepUnit string
}

// newMachine builds a fresh interpreter. The watchdog is NOT started here: a
// host binds its surface first, and a host that fails to bind must not leave a
// goroutine waiting to interrupt an interpreter that will never run.
func newMachine(ctx context.Context, rec *recorder, ceilings limits) *machine {
	return &machine{ctx: ctx, vm: goja.New(), rec: rec, limits: ceilings, halted: make(chan string, 1)}
}

// watch starts the deadline watchdog for this machine.
func (m *machine) watch() {
	m.unwatch = watch(m.ctx, m.vm, m.limits.wall, m.halted)
}

// close stops the watchdog. A run that finished on its own must not leave a
// goroutine waiting to interrupt an interpreter nobody is using.
func (m *machine) close() {
	if m.unwatch != nil {
		m.unwatch()
	}
}

// throw raises a JavaScript error inside the program.
//
// A FAILED DOOR IS CATCHABLE AND A SPENT BUDGET IS NOT, and the difference is
// deliberate: a tool that was not there, a prompt that does not exist, a model
// call that failed are all things a program may legitimately have a plan B for,
// so they arrive as ordinary exceptions. Running out of fuel arrives through the
// interrupt (fuel.go), which no `catch` can reach. An error that nobody catches
// ends the run, and the host that owns the run decides what that means.
func (m *machine) throw(err error) {
	panic(m.vm.NewGoError(err))
}

// parse hands structured JSON back to the program as a JavaScript value,
// falling back to the text when the bytes will not parse. A door that promised
// a shape and sent something else is the Env's problem to report; this
// machine's job is not to lose what did come back.
func (m *machine) parse(raw json.RawMessage, fallback string) goja.Value {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return m.vm.ToValue(fallback)
	}
	return m.vm.ToValue(decoded)
}

// interruption reads why the machine was stopped, when it was. A stop that
// carries no [halt] — an interrupt this package did not raise — still ends the
// run, with the plainest true sentence.
func interruption(err error) (string, bool) {
	var interrupted *goja.InterruptedError
	if !errors.As(err, &interrupted) {
		return "", false
	}
	if stop, ok := interrupted.Value().(halt); ok {
		return stop.why, true
	}
	return "it stopped before it finished.", true
}
