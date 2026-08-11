package head

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Turn-cancel, promoted from a keypress.
//
// Part 2.4: `Head.Interrupt` is an in-process handle bound to a TUI keypress. It
// is not a command kind, not journaled, and not reachable from another surface,
// from a headless caller, or from the head itself. Decision 7's lens law says
// chat and headless differ only in where the task comes from, and an escape key
// that only exists in one of them is a feature that only exists in one of them.
//
// This file is the whole of the head's half. RequestInterrupt is the door: it
// journals the stop as an ordinary command through the ONE funnel, exactly like
// every other authority in the product, and falls back to the in-process handle
// when the journal cannot carry it yet. ApplyInterrupt is the arm the reconciler
// would call — complete, tested, and callable from one line.
//
// # THE ONE-CASE SEAM (12.3.3), NOT TAKEN IN THIS LANE
//
// The reconciler's command dispatch has no extension point: `applyCommand` and
// its call sites live in internal/resident/resident.go with no registration
// seam, and Wave 0 batch 2 had to grant itself a two-line exception to add
// CommandSetModel. internal/resident is co-working territory for this campaign
// and this lane does not edit it. internal/store's kind list is closed the same
// way — `validCommandKind` refuses anything not in it — so journaling this kind
// needs BOTH halves, and half of it is somebody else's file.
//
// The exact edit, for whoever takes it:
//
//  1. internal/store/thread.go, in the CommandKind const block:
//     CommandHeadInterrupt CommandKind = "head_interrupt"
//     and add it to isGlobalCommand's set — it targets no node. It must NOT be
//     added to validateNodeCommand's status table for the same reason.
//  2. internal/resident/resident.go, in applyCommand's switch:
//     case store.CommandHeadInterrupt:
//     return h.head.ApplyInterrupt(command), nil
//     where the resident already holds the head it serves. ApplyInterrupt is
//     idempotent and reports whether there was a turn to stop, which is exactly
//     the resolution the reconciler wants to journal.
//
// Until then RequestInterrupt takes the in-process route and SAYS SO in its
// return, because a door that silently degrades is worse than one that reports
// which way it went.

// HeadInterruptKind is the command kind a journaled turn-cancel will carry. It
// is declared here rather than in the store because the store's kind list is
// closed and this lane may not open it; the constant exists now so the door, the
// arm and their tests are all written against one spelling rather than three.
const HeadInterruptKind store.CommandKind = "head_interrupt"

// InterruptRoute says which way a stop actually travelled.
type InterruptRoute string

const (
	// InterruptJournaled is the destination: the stop is a durable row, replayed
	// like everything else, reachable from any surface and from a headless caller.
	InterruptJournaled InterruptRoute = "journaled"
	// InterruptInProcess is today: the handle the TUI's escape key already holds.
	InterruptInProcess InterruptRoute = "in-process"
	// InterruptNothingRunning is not a failure. A surface that asked at the wrong
	// moment must be able to tell that nothing happened.
	InterruptNothingRunning InterruptRoute = "nothing-running"
)

// ErrInterruptUnreachable is returned when neither route exists.
var ErrInterruptUnreachable = errors.New("head interrupt: no route to the turn in flight")

// RequestInterrupt stops the head's turn in flight and reports how.
//
// It tries the journal first and on purpose: the funnel is the product's one
// authority path, and a stop that rode past it would be the second engine Part 3
// forbids. The store refuses the kind today, and that refusal is read as "the
// seam above is still closed" rather than as an error — the in-process handle is
// the same stop by a narrower road, and the caller is told which road it was.
//
// partial is whatever of the turn the reader had already seen, so the transcript
// keeps the words that did arrive, marked where they stopped.
func (h *Head) RequestInterrupt(sessionID, reason, partial string) (InterruptRoute, error) {
	if h == nil || h.store == nil {
		return "", ErrInterruptUnreachable
	}
	instruction := strings.TrimSpace(reason)
	if instruction == "" {
		instruction = "stop the turn in flight"
	}
	if _, err := h.store.RequestCommand(store.Command{
		SessionID: sessionID, Kind: HeadInterruptKind, Instruction: instruction,
	}); err == nil {
		// The seam is open. The reconciler's arm calls ApplyInterrupt; nothing is
		// stopped here, because a command that acted at its request site would
		// have acted twice by the time the reconciler replayed it.
		return InterruptJournaled, nil
	}
	if h.Interrupt(partial) {
		return InterruptInProcess, nil
	}
	return InterruptNothingRunning, nil
}

// ApplyInterrupt is the reconciler's arm, written here so the resident's edit is
// one line rather than a design. It reports whether there was a turn to stop,
// which is what the reconciler journals as the command's resolution.
//
// The partial is deliberately not carried on the command: what the reader had
// seen is a property of the surface that was watching, not of the journal, and a
// stop arriving from a headless caller has no partial to carry. The turn's own
// machinery already keeps whatever the in-process path handed it.
func (h *Head) ApplyInterrupt(command store.Command) bool {
	if h == nil || command.Kind != HeadInterruptKind {
		return false
	}
	return h.Interrupt("")
}

// interrupt is the belt tool. It is for the sentence that means "stop what you
// are doing" rather than "cancel that job" — cancelling work is control, and
// conflating the two would let an impatient sentence throw away a running
// subtree. Whatever the turn had already said stays, marked where it stopped.
func (run *beltRun) interrupt(args map[string]any) (string, bool) {
	reason := strings.TrimSpace(beltString(args, "reason"))
	route, err := run.head.RequestInterrupt(run.user.SessionID, reason, "")
	if err != nil {
		return "that could not be done: " + err.Error(), true
	}
	switch route {
	case InterruptNothingRunning:
		return "there is no turn in flight to stop — if they meant work on the board, that is control", false
	case InterruptJournaled:
		run.acted, run.spoke = true, true
		return "the stop is journaled; the turn ends and its own line says where it stopped", false
	default:
		run.acted, run.spoke = true, true
		return fmt.Sprintf("stopped (%s); the turn ends and its own line says where it stopped", route), false
	}
}
