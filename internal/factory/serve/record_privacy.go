package serve

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// machineEventObject is what a runner event's object reads as once its machine
// is withheld. It is a kind word, never an id, so nothing about a peer's machine
// survives in it.
const machineEventObject = "runner"

// recordForViewer is the one door a record event passes through on its way to
// a person: the audit and activity lists, their CSV export, and the live
// activity feed. ALL PEER MACHINE INFORMATION IS FORBIDDEN, the operator
// included, so an event another person's machine wrote is shown without it.
//
// The kind, the actor, the job and the time are kept, so the history still
// reads as "raj took job 7" and "raj added a runner"; only the machine goes.
// The persisted bytes are never touched: this returns a copy, and an old row
// that named a machine is redacted here at read time, which is why a Took
// written before the machine left its detail is covered too.
//
// The owner is proven by the event's handle matching the viewer, normalized.
// A viewer who is nobody has no machines, so that case fails closed, and the
// operator has no bypass (machineViewer maps them to their own login only).
// A solo factory has one person, so all of it is theirs.
func (h *Handler) recordForViewer(r *http.Request, e record.Event) record.Event {
	if h.mode() == api.FactorySolo {
		return e
	}
	viewer := h.machineViewer(r)
	if viewer != "" && factory.NormalizeHandle(e.Handle) == viewer {
		return e
	}
	switch e.Kind {
	case record.RunnerAdded:
		// Object is runner:<machine id> and the detail is the runner's name.
		e.Object, e.Detail = machineEventObject, ""
	case record.RunnerChanged:
		// Capacity is private machine metadata too, even without its name.
		e.Object, e.Detail = machineEventObject, ""
	case record.Took:
		// Took.Detail was only ever the taker's machine id.
		e.Detail = ""
	case record.Assigned:
		// New assignments explicitly record an @handle. Legacy unstructured
		// detail can be a departed machine's name; a missing registration is
		// not evidence that it was public human text.
		if !strings.HasPrefix(e.Detail, "@") {
			e.Detail = ""
		}
	}
	return e
}

// recordsForViewer redacts a list into a new slice, leaving the read one alone.
func (h *Handler) recordsForViewer(r *http.Request, events []record.Event) []record.Event {
	out := make([]record.Event, len(events))
	for i, e := range events {
		out[i] = h.recordForViewer(r, e)
	}
	return out
}
