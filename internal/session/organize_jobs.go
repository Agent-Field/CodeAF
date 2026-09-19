package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
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
	return workspace.OrganizeChatKey(chatID, sourceRev)
}

// OrganizeRevision is the source revision of one journalled user message: the
// transcript ordinal plus a short hash of the words, so two enqueues of the
// same line share a key and a later line does not.
func OrganizeRevision(ordinal int, text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%d:%x", ordinal, sum[:8])
}

// OrganizePass is one elected walk of observe_and_organize. RailBlocked is
// the standing DailyRail exhausted: finish deferred/delayed rather than spend
// after standing is already blocked.
type OrganizePass struct {
	Jobs        OrganizeJobs
	Work        Organizer
	Enabled     bool
	Now         time.Time
	RailBlocked bool
}

// ProcessOrganizeJobs leases observe_and_organize work on the elected standing
// pass. Organize itself happens outside the lease transaction; FinishJob is a
// later write that revalidates the fence. A missing organizer leaves pending
// rows untouched — absent, not a stub that claims the workspace was checked.
func ProcessOrganizeJobs(ctx context.Context, jobs OrganizeJobs, work Organizer, enabled bool, now time.Time) error {
	return ProcessOrganizePass(ctx, OrganizePass{Jobs: jobs, Work: work, Enabled: enabled, Now: now})
}

// ProcessOrganizePass is [ProcessOrganizeJobs] with the DailyRail bit.
func ProcessOrganizePass(ctx context.Context, pass OrganizePass) error {
	if pass.Jobs == nil || pass.Work == nil {
		return nil
	}
	until := pass.Now.UTC().Add(standing.TickWindow).Format(time.RFC3339)
	for n := 0; n < organizePassLimit; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		job, err := pass.Jobs.LeaseJob(ctx, []string{workspace.JobOrganize}, OrganizeOwner, until)
		if errors.Is(err, workspace.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		state, detail := settleOrganizeJob(ctx, pass.Work, pass.Enabled, pass.RailBlocked, job)
		if err := pass.Jobs.FinishJob(ctx, job.ID, job.Fence, state, detail); err != nil {
			return err
		}
	}
	return nil
}

func settleOrganizeJob(ctx context.Context, work Organizer, enabled, railBlocked bool, job workspace.Job) (string, string) {
	if job.Attempt >= organizeAttemptCap {
		return workspace.JobFailed, "too many attempts"
	}
	if !enabled && !explicitOrganize(job) {
		return workspace.JobCancelled, "workspace.organize is off"
	}
	if railBlocked {
		return workspace.JobDeferred, "discovery delayed"
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

func explicitOrganize(job workspace.Job) bool {
	return job.CoalesceKey == workspace.OrganizeExistingKey || job.CauseID == workspace.OrganizeThisCause
}

// OrganizeKick coalesces event-driven standing passes: at most one in-flight
// worker plus one dirty follow-up. A second Request while running does not
// spawn another goroutine.
type OrganizeKick struct {
	mu      sync.Mutex
	running bool
	dirty   bool
}

// Request runs fn. A call while a run is in flight records one follow-up.
func (k *OrganizeKick) Request(fn func()) {
	if k == nil || fn == nil {
		return
	}
	k.mu.Lock()
	if k.running {
		k.dirty = true
		k.mu.Unlock()
		return
	}
	k.running = true
	k.mu.Unlock()
	guard.Go("session/organize-kick", func() {
		for {
			fn()
			k.mu.Lock()
			if !k.dirty {
				k.running = false
				k.mu.Unlock()
				return
			}
			k.dirty = false
			k.mu.Unlock()
		}
	})
}

func organizeChatID(place Place, fallback string) string {
	if id := strings.TrimSpace(place.ID()); id != "" {
		return id
	}
	return strings.TrimSpace(fallback)
}
