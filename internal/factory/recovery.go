package factory

import (
	"fmt"
	"strings"
	"time"
)

// RecoveryLimit bounds automatic restarts of one interrupted step. The window
// also bounds waiting for a runner; neither limit applies to an explicit retry.
const RecoveryLimit = 3
const RecoveryWindow = 2 * time.Minute

// StepRecovery belongs to the service, not to a worker report. Its attempt count
// survives a service restart and repeated runner losses, until this step succeeds
// or the person deliberately tries again. No publication or merge is replayed.
type StepRecovery struct {
	BlockedBy string    `json:"blocked_by,omitempty"`
	BlockedAt time.Time `json:"blocked_at,omitzero"`
	Step      string    `json:"step"`
	State     string    `json:"state"`
	Attempts  int       `json:"attempts"`
	Limit     int       `json:"limit"`
	NextAt    time.Time `json:"next_at,omitzero"`
	Deadline  time.Time `json:"deadline"`
	LastAt    time.Time `json:"last_at"`
	Reason    string    `json:"reason"`
}

// TransientStepFailure admits only known connection failures. Authentication,
// permissions, unknown tool errors, human stops and publication need their own
// recovery doors, rather than an optimistic replay of an uncertain side effect.
func TransientStepFailure(f *Failure) bool {
	return f != nil && (f.Code == CodeMachineLost || f.Code == CodeTransportError || (f.Code == CodeProviderError && ConnectionFailure(f.Detail))) && f.Step != "" && f.Step != "publishing"
}

// RecoveryBlockReason is checked again under the desk and item locks before
// starting another attempt, and by lease admission before a worker can take it.
func RecoveryBlockReason(it Item) string {
	switch {
	case !Automatic(it):
		return "Automatic retry paused because the job is in Manual. Try again when ready."
	case it.Publishing:
		return "Publication may have started. Check its outcome before retrying publication."
	case JobState(it) == "done" || it.Stream == nil:
		return "This run has ended."
	case it.Cap <= 0 || JobSpent(it) >= it.Cap:
		return "Job budget reached. Raise Budget before trying again."
	}
	return ""
}

// PlanStepRecovery records a wait once per failure. It never starts execution;
// the lease door does that after checking ownership, the lease and current mode.
func PlanStepRecovery(it *Item, now time.Time) bool {
	f, rec := it.Failure, it.Recovery
	if !IsIssueJob(*it) || JobState(*it) == "done" || it.State == StateDismissed {
		if rec != nil {
			it.Recovery = nil
			return true
		}
		return false
	}
	if rec != nil && rec.State == "running" && it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			if ph.Name == rec.Step && ph.State == PhaseDone {
				it.Recovery = nil
				return true
			}
		}
		if it.FollowUp != nil && (it.FollowUp.State == "watching" || it.FollowUp.State == "healthy") {
			it.Recovery = nil
			return true
		}
	}
	if rec != nil && rec.State == "exhausted" && rec.BlockedBy == "budget" && RecoveryBlockReason(*it) == "" && rec.Attempts < rec.Limit && TransientStepFailure(f) && f.Step == rec.Step && it.Asking == nil && (it.Question == "" || it.QKind == NeedsRunnerOffline || it.QKind == "cap") && !it.Stream.Paused {
		cp := *rec
		// Time spent waiting for more budget is not time spent attempting a
		// recovery. Preserve the remaining window and cumulative attempts.
		if !cp.BlockedAt.IsZero() {
			cp.Deadline = cp.Deadline.Add(now.Sub(cp.BlockedAt))
		}
		if now.Before(cp.Deadline) {
			cp.State, cp.Reason, cp.BlockedBy = "waiting", "Budget restored; retrying the interrupted step.", ""
			cp.NextAt = now
			cp.BlockedAt = time.Time{}
			it.Recovery = &cp
			return true
		}
	}
	if rec != nil && rec.State == "queued" {
		if why := RecoveryBlockReason(*it); why != "" || !now.Before(rec.Deadline) {
			cp := *rec
			if why == "" {
				why = "The runner did not return within the automatic retry window. Try again when ready."
			}
			cp.State, cp.Reason = "exhausted", why
			if it.Cap <= 0 || JobSpent(*it) >= it.Cap {
				cp.BlockedBy, cp.BlockedAt = "budget", now
			}
			it.Recovery, it.State = &cp, StateNeedsYou
			StampFailure(it, NewFailure(cp.Step, CodeMachineLost, why, it.WorktreeMachine, FailureStopped, cp.Attempts, now))
			it.Question, it.QKind = why, NeedsRunnerOffline
			return true
		}
		return false
	}
	if !TransientStepFailure(f) || f.Next != FailureStopped {
		return false
	}
	if rec != nil && rec.State == "exhausted" && RecoveryBlockReason(*it) == "" &&
		(strings.HasPrefix(rec.Reason, "Job budget reached") || strings.HasPrefix(rec.Reason, "Automatic retry paused because") || strings.HasPrefix(rec.Reason, "Repository is turned off")) {
		cp := *rec
		cp.Reason = "Automatic recovery is paused. Try again to resume this step."
		it.Recovery = &cp
		return true
	}
	if rec != nil && rec.LastAt.Equal(f.At) {
		return false
	}
	if rec == nil || rec.Step != f.Step {
		rec = &StepRecovery{Step: f.Step, Limit: RecoveryLimit, Deadline: now.Add(RecoveryWindow)}
	}
	cp := *rec
	cp.LastAt, cp.Reason = f.At, f.Detail
	if why := RecoveryBlockReason(*it); why != "" {
		cp.State, cp.Reason = "exhausted", why
		if it.Cap <= 0 || JobSpent(*it) >= it.Cap {
			cp.BlockedBy, cp.BlockedAt = "budget", now
		}
	} else if cp.Attempts >= cp.Limit || !now.Before(cp.Deadline) {
		cp.State, cp.Reason = "exhausted", fmt.Sprintf("Automatic recovery stopped after %d of %d retries. Try again to make another attempt.", cp.Attempts, cp.Limit)
	} else {
		delay := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}[cp.Attempts]
		cp.State, cp.NextAt = "waiting", now.Add(delay)
		if cp.NextAt.After(cp.Deadline) {
			cp.NextAt = cp.Deadline
		}
	}
	it.Recovery = &cp
	return true
}

// ConnectionFailure recognises transport failures after the model's own retry
// allowance. Explicit denials and local cancellation never become network work.
func ConnectionFailure(msg string) bool {
	low := strings.ToLower(msg)
	for _, deny := range []string{"unauthorized", "forbidden", "permission denied", "invalid api key", "context canceled", "cancelled by"} {
		if strings.Contains(low, deny) {
			return false
		}
	}
	for _, transient := range []string{"connection reset", "connection refused", "connection to the model dropped", "network is unreachable", "i/o timeout", "context deadline exceeded", "model did not answer in time", "temporarily unavailable", "unexpected eof", "http 502", "http 503", "http 504", "status 502", "status 503", "status 504", "rate limit", "asked to slow down"} {
		if strings.Contains(low, transient) {
			return true
		}
	}
	return false
}
