// Package guard is the one place a panic stops being fatal. codeaf is
// event-sourced: the graph loses nothing when a goroutine dies, so a fault
// should be absorbed, recorded, and degraded around — never allowed to take
// the terminal surface down while the user is working.
//
// It is deliberately three functions and one error type. Anything larger would
// be a framework, and a framework is not what a deferred recover needs.
package guard

import (
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync/atomic"
)

// Fault is what a recovered panic becomes: an ordinary error the caller can
// record, fail a node with, or hand back to a model as text.
type Fault struct {
	Scope     string
	Recovered any
	Stack     []byte
}

// Error reads as one sentence because it ends up in node failure text and in
// tool results the model has to act on.
func (f *Fault) Error() string {
	scope := strings.TrimSpace(f.Scope)
	if scope == "" {
		scope = "codeaf"
	}
	return fmt.Sprintf("internal fault in %s: %v", scope, f.Recovered)
}

// Note records a recovered panic and returns the error that stands in for it.
// Call it from a deferred recover when the fault must become a result rather
// than end the goroutine. The stack is written once, here.
func Note(scope string, recovered any) error {
	return note(scope, recovered, debug.Stack())
}

func note(scope string, recovered any, stack []byte) error {
	fault := &Fault{Scope: scope, Recovered: recovered, Stack: stack}
	// One structured line, then the stack. Every door that opens the v3 surface
	// runs it through cmd/codeaf's runSurface, which parks the standard logger
	// in the profile's chat.log — `~/.codeaf/chat.log` unless CODEAF_PROFILE_DIR
	// or CODEAF_HOME moves it — for as long as the surface owns the terminal, so
	// this lands in a file a person can read afterwards rather than tearing
	// through the alt screen. It was true of the in-process door alone until
	// #404; the ssh, relay and unix-socket doors left it on stderr.
	log.Printf("fault scope=%q panic=%v\n%s", fault.Scope, recovered, stack)
	return fault
}

// The fault hook is called from Recover once the fault has been recorded, with the
// scope Recover was given and the stack it captured for the log. It is nil by
// default, and it is a variable rather than a call because of where this
// package sits: internal/telemetry spools its own writes through guard.Go, so
// an import from here back to anything that would report a fault is a cycle.
// The reporting arrives as a func value instead, set once by the top of the
// binary and left nil by every package that has nothing to report to.
//
// It runs on the recovered goroutine, inside the deferred Recover, so it must
// neither block nor panic: the goroutine ends the moment it returns, and a
// fault in the reporter would be the one fault guard could not absorb.
type faultHook func(scope string, stack []byte)

// onFault holds the hook behind an atomic pointer: the binary sets it once at
// start-up and every recovered goroutine reads it, and a plain variable read
// on one goroutine while another writes it is a race the detector rightly
// reports.
var onFault atomic.Pointer[faultHook]

// SetOnFault installs the hook, or removes it with nil.
func SetOnFault(fn func(scope string, stack []byte)) {
	if fn == nil {
		onFault.Store(nil)
		return
	}
	hook := faultHook(fn)
	onFault.Store(&hook)
}

// Recover absorbs a panic in the deferring goroutine and lets it end quietly:
// `defer guard.Recover("narrator")`. Use it where there is no result to carry
// the fault — a fire-and-forget spawn whose only obligation is not to crash.
func Recover(scope string) {
	if recovered := recover(); recovered != nil {
		stack := debug.Stack()
		_ = note(scope, recovered, stack)
		if hook := onFault.Load(); hook != nil {
			(*hook)(scope, stack)
		}
	}
}

// Go spawns fn under Recover. Every fire-and-forget goroutine in codeaf starts
// here, so "no goroutine can kill the surface" is one grep, not a habit.
func Go(scope string, fn func()) {
	go func() {
		defer Recover(scope)
		fn()
	}()
}

// IsFault reports whether err came from a recovered panic, which is how a
// caller tells "the work failed" from "the work faulted".
func IsFault(err error) bool {
	var fault *Fault
	return errors.As(err, &fault)
}
