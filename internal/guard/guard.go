// Package guard is the one place a panic stops being fatal. aforge is
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
		scope = "aforge"
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
	// One structured line, then the stack. The TUI redirects the standard
	// logger to ~/.aforge/chat.log for its lifetime, so this never tears
	// through the alt screen.
	log.Printf("fault scope=%q panic=%v\n%s", fault.Scope, recovered, stack)
	return fault
}

// Recover absorbs a panic in the deferring goroutine and lets it end quietly:
// `defer guard.Recover("narrator")`. Use it where there is no result to carry
// the fault — a fire-and-forget spawn whose only obligation is not to crash.
func Recover(scope string) {
	if recovered := recover(); recovered != nil {
		_ = note(scope, recovered, debug.Stack())
	}
}

// Go spawns fn under Recover. Every fire-and-forget goroutine in aforge starts
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
