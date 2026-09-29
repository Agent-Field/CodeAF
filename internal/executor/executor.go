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

// ExecRequest is one process to run.
type ExecRequest struct {
	Argv    []string
	Env     []string      // nil inherits the host environment
	Dir     string        // relative to the workspace root, never under .cell/
	Timeout time.Duration // zero means no limit beyond the context
	Class   Class
	Net     NetPolicy
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
	PGID int
}

// ExecResult is what one call did.
type ExecResult struct {
	Exit       int // -1 when the process was killed or never exited
	TimedOut   bool
	Stdout     []byte
	Stderr     []byte
	Wall       time.Duration
	SideEffect SideEffect
	Services   []Service
}

// Executor runs a request and streams output to onOutput, which may be nil.
// A process that exits non-zero is a result, not an error; an error means the
// call could not run or was cancelled by its context.
type Executor interface {
	Exec(ctx context.Context, req ExecRequest, onOutput func(Chunk)) (ExecResult, error)
}
