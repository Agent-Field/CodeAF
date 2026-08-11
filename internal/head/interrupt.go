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
// # THE SEAM, HALF OPEN (12.3.3)
//
// The store half has landed: `store.CommandHeadInterrupt` is a legal kind and a
// global command, so RequestInterrupt's journal road is real and a stop is now a
// durable row like every other authority in the product.
//
// The reconciler half has NOT. `applyCommand` and its call sites live in
// internal/resident/resident.go with no registration seam, and internal/resident
// is co-working territory for this campaign. The remaining edit, for whoever
// takes it, is one case:
//
//	case store.CommandHeadInterrupt:
//	    return h.head.ApplyInterrupt(command), nil
//
// where the resident already holds the head it serves. ApplyInterrupt is
// idempotent and reports whether there was a turn to stop, which is exactly the
// resolution the reconciler wants to journal.
//
// Until that case exists the row is written and nothing drains it, which is why
// this door ALSO stops the turn in process on its way out. That is not
// belt-and-braces and it is not a race: the two roads reach the same idempotent
// handle, and a door that journals a stop the surface can see and then lets the
// turn keep talking is a lie told in the one place the person is watching.
// When the reconciler's case lands, ApplyInterrupt finds the turn already gone
// and says so, which is a true resolution rather than a second cancellation.

// HeadInterruptKind is the command kind a journaled turn-cancel carries. It is
// an alias rather than a second spelling: the store owns the kind list, and two
// constants holding one string is how a kind quietly becomes two kinds.
const HeadInterruptKind = store.CommandHeadInterrupt

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
// It journals first and on purpose: the funnel is the product's one authority
// path, and a stop that rode past it would be the second engine Part 3 forbids.
// Then it stops the turn in process, because until the reconciler's one case
// lands nothing drains that row, and a door that reports a stop while the turn
// carries on talking is worse than one that never claimed it.
//
// The two roads are not a race. Both end at the same handle and the handle is
// idempotent: whichever arrives second finds no turn and says so.
//
// The route reported is the durable one when the row landed, because that is the
// fact a surface needs — a journaled stop is replayable, reaches a headless
// caller and survives this process. NothingRunning is reserved for the case
// where neither road found anything at all to do.
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
	_, journalErr := h.store.RequestCommand(store.Command{
		SessionID: sessionID, Kind: HeadInterruptKind, Instruction: instruction,
	})
	stopped := h.Interrupt(partial)
	switch {
	case journalErr == nil:
		return InterruptJournaled, nil
	case stopped:
		return InterruptInProcess, nil
	default:
		return InterruptNothingRunning, nil
	}
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
