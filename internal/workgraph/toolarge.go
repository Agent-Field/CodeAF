package workgraph

import (
	"errors"
	"fmt"
)

// NarrowScope is the advice a [TooLarge] carries: ask for less.
const NarrowScope = "needs_narrow_scope"

// TooLarge is the typed refusal for a scope whose working set would not fit the
// hard memory budget. IT IS RAISED BEFORE THE ALLOCATION IT PREVENTS — from file
// sizes before anything is read, from the facts counted so far while they are
// read, from the weighed input before it is built — and never after the memory
// has been spent. Nothing is cut to fit and nothing is served partly: the caller
// is told what was needed against what is allowed and to ask for a smaller scope
// (a task, an agent, a smaller crew).
type TooLarge struct {
	// What is the quantity that did not fit: "files", "facts" or "build".
	What string `json:"what"`
	// Need and Limit are bytes: the estimate of what the scope needs at peak,
	// and the most this engine admits for it.
	Need  int64 `json:"need"`
	Limit int64 `json:"limit"`
	// Reason is always [NarrowScope], so a surface branches on a word.
	Reason string `json:"reason"`
	// Scope is the largest scope kind worth asking for instead: "agent" or
	// "task". Empty when the asker is already at the smallest.
	Scope string `json:"scope,omitempty"`
}

func (e *TooLarge) Error() string {
	return fmt.Sprintf("workgraph: too large: %s need ~%d MiB, the engine admits %d MiB; ask for a smaller scope", e.What, e.Need>>20, e.Limit>>20)
}

// Is makes every [TooLarge] match [ErrTooLarge].
func (e *TooLarge) Is(target error) bool { return target == ErrTooLarge }

// ErrTooLarge matches any [TooLarge] under errors.Is.
var ErrTooLarge = errors.New("workgraph: too large")

// NewTooLarge is a refusal for what needing need bytes against a limit.
func NewTooLarge(what string, need, limit int64) *TooLarge {
	return &TooLarge{What: what, Need: need, Limit: limit, Reason: NarrowScope}
}
