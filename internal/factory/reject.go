package factory

import (
	"strings"
	"time"
)

// Rejection is a person's no that ended a job at one of its approvals: who
// said it, the words they gave, and when. It is kept on the job whole, so the
// job's page, its Brief and its manager all read the same decision.
type Rejection struct {
	By    string    `json:",omitempty"`
	Words string    `json:",omitempty"`
	At    time.Time `json:",omitempty"`
}

// RejectedWords is the note a rejected job carries, and the detail of the
// activity row its rejection writes: `Rejected · <words>`, or `Rejected` with
// no words.
func RejectedWords(words string) string {
	if words = strings.TrimSpace(words); words != "" {
		return "Rejected · " + words
	}
	return "Rejected"
}

// EndRejected ends the job as the person's rejection of what it asked them to
// approve. The caller has already ended its run (the loop's stop, or a stop of
// a stored job); this is the decision written on top of that stop.
//
// A REJECTION IS A DECISION, NOT A PAUSE. A rejected run used to leave its
// job stopped: in Needs you, offering to carry on from the step it was
// rejected at, and new, which is the state a rule's start: auto picks up. It
// now ends the job the way Close does: done, closed here with its work and
// spending kept, nothing on GitHub changed, out of Needs you and out of every
// rule's reach, with the person's words on it. Reopen and Run as new are the
// ways back, and neither is offered as a default.
func (it *Item) EndRejected(by, words string, now time.Time) {
	words = strings.TrimSpace(words)
	stopped := true
	it.PersonStopped = &stopped
	it.DoneHow, it.DoneAt = "closed_locally", now
	it.Rejection = &Rejection{By: NormalizeHandle(by), Words: words, At: now}
	it.State, it.Marked, it.Manual = StateNew, false, true
	it.Publishing, it.Approval, it.StopUndo, it.Recovery = false, nil, nil, nil
	// Who was passing it is spent by the answer, as by any answer to an approval.
	it.Passing = nil
	it.Question, it.QKind, it.Asking = "", "", nil
	for i := range it.Thread {
		if it.Thread[i].State == "pending" {
			it.Thread[i].State = "cancelled"
		}
	}
	// THE RUN'S RECORD IS CLOSED WITH THE JOB: a step still reading waiting
	// would tell every reader the run is still asking.
	if s := it.Stream; s != nil {
		for i := range s.Phases {
			if s.Phases[i].State == PhaseWaiting || s.Phases[i].State == PhaseRunning {
				s.Phases[i].State, s.Phases[i].At, s.Phases[i].Verification = PhaseFailed, now, nil
			}
		}
		if s.Ended.IsZero() {
			s.Ended = now
		}
		s.Paused = false
	}
	it.Note(now, RejectedWords(words))
}

// WaitsOnApproval says the job is waiting on a person's approval right now:
// an approve step, a start that asked first, or a step paused on an approval
// its run asked for elsewhere (an [Approval] still waiting). It is what makes
// Reject a door the job offers.
func WaitsOnApproval(it Item) bool {
	if it.State != StateNeedsYou || JobState(it) != "needs_you" {
		return false
	}
	if it.QKind == QKindApprove || it.QKind == QKindAskFirst {
		return true
	}
	return it.Approval != nil && it.Approval.State == "waiting"
}
