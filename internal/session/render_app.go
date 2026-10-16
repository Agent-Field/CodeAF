package session

import (
	"fmt"
	"syscall"
)

// RenderOutcome is how a cancel request ended.
type RenderOutcome string

const (
	// RenderOutcomeCancelled: the Stop was accepted before the bytes were
	// committed; nothing will be written.
	RenderOutcomeCancelled RenderOutcome = "cancelled"
	// RenderOutcomeTooLate: the render had already committed to saving; it was
	// NOT stopped and will finish.
	RenderOutcomeTooLate RenderOutcome = "too_late"
	// RenderOutcomeEnded: the render had already ended; the record is unchanged.
	RenderOutcomeEnded RenderOutcome = "already_ended"
)

// RenderCancel is the answer to [Agent.CancelRender].
type RenderCancel struct {
	Outcome RenderOutcome `json:"outcome"`
	Render  RenderRecord  `json:"render"`
}

func (a *Agent) renderJobs() *jobRegistry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.jobs
}

// Renders lists this session's renders — image attempts, video and music —
// newest first.
func (a *Agent) Renders(limit int, cursor string) (RenderPage, error) {
	jobs := a.renderJobs()
	if jobs == nil {
		return RenderPage{Renders: []RenderRecord{}}, nil
	}
	return jobs.renderLedger().list(limit, cursor)
}

// Render answers one render by its stable id: [ErrRenderNotFound] only when the
// whole history was searched, [ErrRenderBusy] when the search ran out of time.
func (a *Agent) Render(id string) (RenderRecord, error) {
	jobs := a.renderJobs()
	if jobs == nil {
		return RenderRecord{}, ErrRenderNotFound
	}
	return jobs.renderLedger().get(id)
}

// CancelRender asks a running render to stop, and NEVER waits for the provider:
// the answer is the ledger's own, written at the instant the Stop is accepted.
// It does not tell the provider to cancel — there is no such call — so a
// cancelled record carries upstream "unknown".
func (a *Agent) CancelRender(id string) (RenderCancel, error) {
	jobs := a.renderJobs()
	if jobs == nil {
		return RenderCancel{}, ErrRenderNotFound
	}
	ledger := jobs.renderLedger()
	record, err := ledger.get(id)
	if err != nil {
		return RenderCancel{}, err
	}
	if record.State != RenderRunning {
		return RenderCancel{Outcome: RenderOutcomeEnded, Render: record}, nil
	}
	// A synchronous attempt (an image) has no job: its Stop is the ledger's
	// own, answered in the same three words (render_attempt.go).
	if record.Job == 0 {
		if outcome, held := ledger.stopAttempt(id); held {
			record, _ = ledger.get(id)
			return RenderCancel{Outcome: outcome, Render: record}, nil
		}
	}
	target := jobs.find(record.Job)
	if target == nil {
		return RenderCancel{}, fmt.Errorf("render %s is recorded as running but its job is not in this session", id)
	}
	claimed, committed := target.requestKillRender()
	outcome := RenderOutcomeEnded
	switch {
	case claimed:
		target.signal(syscall.SIGTERM)
		outcome = RenderOutcomeCancelled
	case committed:
		outcome = RenderOutcomeTooLate
	}
	record, _ = ledger.get(id)
	return RenderCancel{Outcome: outcome, Render: record}, nil
}
