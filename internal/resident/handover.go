package resident

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// HandoverFunc is asked to give up the resident role. It answers whether the
// role will actually be released and says why in the words the requester and
// the journal both read.
//
// It is called from inside a tick, so it must return promptly: releasing a
// lease, draining a runner and stopping a head all belong to the goroutine the
// implementation starts, not to the pass that is still holding the command
// queue open.
type HandoverFunc func(request Handover) (accepted bool, reason string)

// Handover is one request to take over the resident role.
type Handover struct {
	// Seq and At are the journal's own coordinates for the request. At is what
	// makes the request answerable: a resident only owes an answer to a request
	// made while it was already serving.
	Seq int64
	At  time.Time
	// Reason is the requester's own words, verbatim.
	Reason    string
	SessionID string
}

// WithHandover installs the seam that lets another window take the resident
// role while this one is still healthy. Without it — a wake pass, a test, any
// process with no surface to demote to — a handover request is rejected in
// words rather than left pending, so the requester learns immediately that it
// must wait for the heartbeat to go stale instead of waiting forever.
func (r *Reconciler) WithHandover(hand HandoverFunc) *Reconciler {
	r.handover = hand
	return r
}

// WithResidentSince tells the reconciler when this process took the role. A
// handover journaled before that moment was addressed to whoever was serving
// then, and answering it would make a freshly promoted resident stand straight
// back down — the loop that would otherwise pass the role around a ring of
// windows forever.
func (r *Reconciler) WithResidentSince(at time.Time) *Reconciler {
	r.residentSince = at
	return r
}

func (r *Reconciler) applyHandoverCommand(command store.Command) (commandOutcome, error) {
	reason := strings.TrimSpace(command.Instruction)
	if r.handover == nil {
		result := "this process cannot hand the resident role over; it has no surface to demote to"
		return commandOutcome{status: store.CommandRejected, result: result, receipt: result}, nil
	}
	if !r.residentSince.IsZero() && !command.Time.After(r.residentSince) {
		result := "the resident role had already moved by the time this was read"
		return commandOutcome{status: store.CommandRejected, result: result, receipt: result}, nil
	}
	accepted, note := r.handover(Handover{
		Seq: command.Seq, At: command.Time, Reason: reason, SessionID: command.SessionID,
	})
	note = strings.TrimSpace(note)
	if !accepted {
		if note == "" {
			note = "the resident role is staying here for now"
		}
		return commandOutcome{status: store.CommandRejected, result: note, receipt: note}, nil
	}
	if note == "" {
		note = fmt.Sprintf("standing down as resident: %s", reason)
	}
	return commandOutcome{status: store.CommandApplied, result: note, receipt: note}, nil
}
