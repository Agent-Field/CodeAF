package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

const (
	// OrganizeOwner is the lease owner both the in-window standing pass and
	// `codeaf tick` write. One name, so a fence from either door is the same
	// worker as far as the store is concerned.
	OrganizeOwner = "tick"
	// organizePassLimit bounds how many jobs one elected pass will lease, so a
	// backlog cannot eat the standing walk's remaining window.
	organizePassLimit = 4
	// organizeAttemptCap is how many leases a job may take before it is failed
	// rather than retried forever.
	organizeAttemptCap = 8
)

// OrganizeJobs is the collections.db job table the tick leases against.
type OrganizeJobs interface {
	LeaseJob(ctx context.Context, types []string, owner, until string) (workspace.Job, error)
	FinishJob(ctx context.Context, id, fence, state, detail string) error
}

// Organizer is the model-using half of a job, owned by the session lane. It
// runs outside the collections writer transaction. Nil means that half is not
// bound: the tick leaves pending rows pending rather than inventing membership.
type Organizer interface {
	Organize(ctx context.Context, job workspace.Job) (state, detail string, err error)
}

// OrganizeCoalesceKey is chat id + source revision. A second enqueue for the
// same key while the row is pending or leased is a no-op success in the store.
func OrganizeCoalesceKey(chatID, sourceRev string) string {
	return chatID + ":" + sourceRev
}

// OrganizeRevision is the source revision of one journalled user message: the
// transcript ordinal plus a short hash of the words, so two enqueues of the
// same line share a key and a later line does not.
func OrganizeRevision(ordinal int, text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%d:%x", ordinal, sum[:8])
}

// ProcessOrganizeJobs leases observe_and_organize work on the elected standing
// pass. Organize itself happens outside the lease transaction; FinishJob is a
// later write that revalidates the fence. A missing organizer leaves pending
// rows untouched — absent, not a stub that claims the workspace was checked.
func ProcessOrganizeJobs(ctx context.Context, jobs OrganizeJobs, work Organizer, enabled bool, now time.Time) error {
	if jobs == nil || work == nil {
		return nil
	}
	until := now.UTC().Add(standing.TickWindow).Format(time.RFC3339)
	for n := 0; n < organizePassLimit; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		job, err := jobs.LeaseJob(ctx, []string{workspace.JobOrganize}, OrganizeOwner, until)
		if errors.Is(err, workspace.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		state, detail := settleOrganizeJob(ctx, work, enabled, job)
		if err := jobs.FinishJob(ctx, job.ID, job.Fence, state, detail); err != nil {
			return err
		}
	}
	return nil
}

func settleOrganizeJob(ctx context.Context, work Organizer, enabled bool, job workspace.Job) (string, string) {
	if job.Attempt >= organizeAttemptCap {
		return workspace.JobFailed, "too many attempts"
	}
	if !enabled {
		return workspace.JobCancelled, "workspace.organize is off"
	}
	state, detail, err := work.Organize(ctx, job)
	if err != nil {
		return workspace.JobFailed, err.Error()
	}
	if !validOrganizeFinish(state) {
		return workspace.JobDeferred, "discovery delayed"
	}
	return state, detail
}

func validOrganizeFinish(state string) bool {
	switch state {
	case workspace.JobCompleted, workspace.JobDeferred, workspace.JobFailed, workspace.JobCancelled:
		return true
	}
	return false
}

func organizeChatID(place Place, fallback string) string {
	if id := strings.TrimSpace(place.ID()); id != "" {
		return id
	}
	return strings.TrimSpace(fallback)
}
