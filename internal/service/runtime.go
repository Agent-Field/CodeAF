package service

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// Exit is how a service's process ended. Known is false for a process this
// codeaf did not start, whose exit status went to somebody else.
type Exit struct {
	Code  int
	Known bool
}

// Runtime is everything the supervisor does to the machine. The platform's is
// [PlatformRuntime]; a test hands in a scripted one, which is how the policy,
// the budget and the clock are proven without a real process or a real wait.
type Runtime interface {
	// Launch makes the spec's process group and stops it at a gate, BEFORE the
	// approved command runs: the identity is known and nothing of the person's
	// command has happened yet. [Launch.Commit] lets it run; [Launch.Abort],
	// or this codeaf ending before either, makes the gate exit without running
	// it. exited is called exactly once, from a goroutine of its own, with the
	// identity, when the leader ends — whether or not it was ever committed.
	Launch(spec Spec, logPath string, exited func(processgroup.Identity, Exit)) (Launch, error)
	// Alive reports whether the recorded group still runs AND is still the one
	// recorded. An identity that cannot be proven is not alive.
	Alive(id processgroup.Identity) bool
	// Signal sends SIGTERM, or SIGKILL when kill is set, to the recorded group,
	// and sends nothing when its identity no longer matches.
	Signal(id processgroup.Identity, kill bool) error
	// Probe runs the spec's one check, within ctx.
	Probe(ctx context.Context, spec Spec) error
	// PortInUse reports whether something already accepts connections on the
	// local port. It is asked only BEFORE a service starts, which is the one
	// moment a listener there is proven not to be the service's own.
	PortInUse(port int) bool
	// Boot names the running boot, or "" where the platform cannot. A store
	// written in another boot holds no process that can still be running.
	Boot() string
}

// Launch is a process group held at its gate.
//
// THE GATE IS WHAT MAKES A KEPT PROCESS CRASH-SAFE. A detached command that ran
// before its identity was on disk would, if codeaf died in between, be a
// process no later codeaf could ever find or prove — the orphan this package
// exists to prevent. So the group is made first, its identity written down,
// and only then is the command allowed to run; a codeaf that dies before the
// commit leaves a gate that reads end-of-file and exits having run nothing.
type Launch interface {
	Identity() processgroup.Identity
	// Commit lets the approved command run. An error means the gate is gone
	// and the command did not run; its exit is still reported.
	Commit() error
	// Abort makes the gate exit without running the command.
	Abort()
}

// Clock is the supervisor's time: the one timer it keeps and the stop grace.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a pending AfterFunc.
type Timer interface {
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

// RealClock is the wall clock.
func RealClock() Clock { return realClock{} }
