// Package executor is the one path from a tool call to an operating-system
// process (docs/ARCHITECTURE.md section 8). A request names what to run, where
// (relative to the workspace root), for how long, under which class and network
// policy; the result names what happened, whether the world outside the
// workspace could have been touched, and which children were left running.
//
// Two implementations exist: Local, which spawns here, and Remote, which will
// spawn on another device. The structural law in law_test.go keeps every other
// tool-execution package from reaching os/exec around this one.
package executor

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// Class is declared per workspace, never detected.
type Class int

const (
	// Sandboxed is the default: a jail applies where one is installed.
	Sandboxed Class = iota
	// FilesOnly runs no process at all.
	FilesOnly
	// HostBound runs unjailed on the home device.
	HostBound
)

// NetPolicy is the side-effect oracle. The zero value denies the network.
type NetPolicy struct {
	// Open grants the whole network; Allow grants named hosts only.
	Open  bool
	Allow []string
}

// Denies reports whether the call cannot reach the network at all.
func (p NetPolicy) Denies() bool { return !p.Open && len(p.Allow) == 0 }

// SideEffect says where a call's effects can land.
type SideEffect string

const (
	// EffectLocal: effects live only in the workspace.
	EffectLocal SideEffect = "local"
	// EffectExternal: effects may have reached the world beyond it.
	EffectExternal SideEffect = "external"
)

// classify is the whole rule: no network, no external effect.
func classify(p NetPolicy) SideEffect {
	if p.Denies() {
		return EffectLocal
	}
	return EffectExternal
}

// Group says how the child is placed among the harness's own processes.
type Group int

const (
	// GroupOwn gives the child its own process group, so a kill reaches
	// everything it started. It is the default.
	GroupOwn Group = iota
	// GroupSession gives it a new session: its own group and no controlling
	// terminal, so a program that opens /dev/tty is refused.
	GroupSession
	// GroupInherit leaves it in the harness's group.
	GroupInherit
)

// OpenNet is the policy of a call that may reach the whole network.
var OpenNet = NetPolicy{Open: true}

// ExecRequest is one process to run.
type ExecRequest struct {
	Argv    []string
	Env     []string      // nil inherits the host environment
	Dir     string        // relative to the workspace root, never under .cell/
	Timeout time.Duration // zero means no limit beyond the context
	Class   Class
	Net     NetPolicy

	Stdin     io.Reader     // nil reads nothing
	Group     Group         // where the child sits; the zero value is its own group
	WaitDelay time.Duration // bound on draining output after exit; zero picks a short default
	Combined  bool          // stderr shares stdout's pipe, so arrival order is kept
	Stream    bool          // output goes to onOutput only; the result keeps none of it
}

// Stream names an output stream.
type Stream int

const (
	Stdout Stream = iota
	Stderr
)

// Chunk is a piece of output, delivered as it is produced.
type Chunk struct {
	Stream Stream
	Data   []byte
}

// Service is a process group still alive after its call returned.
type Service struct {
	Name    string   // basename of the first member's argv[0]
	Argv    []string // the first member's command line (the request's off Linux)
	PGID    int
	Ports   []int  // listening TCP ports, when known (Linux only)
	DataDir string // cell-relative data directory, when the service keeps one
}

// SnapshotExact is the seal's quality rule (docs/ARCHITECTURE.md 4.3, derived,
// never stored): a snapshot is exact iff the call left no process alive.
// Anything still running may be writing, so the seal is best effort.
func SnapshotExact(res ExecResult) bool { return len(res.Services) == 0 }

// ExecResult is what one call did.
type ExecResult struct {
	Exit        int // -1 when the process was killed or never exited
	Status      string
	TimedOut    bool
	PipesForced bool // a child kept the output pipes open and they were closed on it
	Stdout      []byte
	Stderr      []byte
	Wall        time.Duration
	SideEffect  SideEffect
	Services    []Service
	// JailDegraded: the jail could not apply every limit on this kernel.
	JailDegraded bool
}

// Failure is nil for a clean exit and otherwise the process's own status
// sentence, such as "exit status 2" or "signal: killed". A clean exit whose
// output pipes had to be closed on a lingering child reports exec.ErrWaitDelay,
// as os/exec does.
func (r ExecResult) Failure() error {
	switch {
	case r.Exit != 0:
		return errors.New(r.Status)
	case r.PipesForced:
		return exec.ErrWaitDelay
	}
	return nil
}

// Executor runs a request and streams output to onOutput, which may be nil.
// A process that exits non-zero is a result, not an error; an error means the
// call could not run or was cancelled by its context.
type Executor interface {
	Exec(ctx context.Context, req ExecRequest, onOutput func(Chunk)) (ExecResult, error)
}

// Observer is told about every call that ran: the request and what came back.
// It must not block, and it never changes the result.
type Observer interface {
	Observe(req ExecRequest, res ExecResult)
}
