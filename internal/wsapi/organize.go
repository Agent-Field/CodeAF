package wsapi

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// OrganizeExistingKey is the coalesce key for the visible Organize existing
// chats survey. Automatic after-message jobs keep chat:rev keys.
const OrganizeExistingKey = workspace.OrganizeExistingKey

const (
	organizeQueued  = "queued"
	organizeRunning = "running"
	organizeDelayed = "delayed"
	organizeDone    = "done"
	organizeCancel  = "cancel"
)

// errOrganizeUnwired is a labelled refusal when this service has no job table.
// Tests inject a fake store that is membership-only; production Open binds
// *workspace.Store. Never a fake queued or done OrganizeView.
var errOrganizeUnwired = errors.New("folders are not wired here")

// OrganizeView is the explicit survey as the Folders place draws it.
// State is person-facing: queued | running | delayed | done | cancel.
type OrganizeView struct {
	JobID, State, Detail string
}

type jobQueue interface {
	EnqueueJob(ctx context.Context, job workspace.Job) (workspace.Job, error)
	LookupJob(ctx context.Context, jobType, coalesceKey string) (workspace.Job, error)
	CancelJob(ctx context.Context, jobType, coalesceKey string) (workspace.Job, error)
}

var _ jobQueue = (*workspace.Store)(nil)

// OrganizeExistingChats enqueues observe_and_organize with OrganizeExistingKey.
// A second call while pending or leased returns the same row.
func (s *Service) OrganizeExistingChats(ctx context.Context) (OrganizeView, error) {
	jobs, err := s.jobQueue(ctx)
	if err != nil {
		return OrganizeView{}, err
	}
	job, err := jobs.EnqueueJob(ctx, workspace.Job{
		Type:        workspace.JobOrganize,
		CoalesceKey: OrganizeExistingKey,
	})
	if err != nil {
		return OrganizeView{}, wrapStoreError(err)
	}
	return organizeViewOf(job), nil
}

// OrganizeStatus is the live or last explicit survey. No row is empty, not done.
func (s *Service) OrganizeStatus(ctx context.Context) (OrganizeView, error) {
	jobs, err := s.jobQueue(ctx)
	if err != nil {
		return OrganizeView{}, err
	}
	job, err := jobs.LookupJob(ctx, workspace.JobOrganize, OrganizeExistingKey)
	if errors.Is(err, workspace.ErrNotFound) {
		return OrganizeView{}, nil
	}
	if err != nil {
		return OrganizeView{}, wrapStoreError(err)
	}
	return organizeViewOf(job), nil
}

// CancelOrganize is the visible cancel. Nothing live is success, not a fake
// cancelled job.
func (s *Service) CancelOrganize(ctx context.Context) error {
	jobs, err := s.jobQueue(ctx)
	if err != nil {
		return err
	}
	_, err = jobs.CancelJob(ctx, workspace.JobOrganize, OrganizeExistingKey)
	if errors.Is(err, workspace.ErrNotFound) {
		return nil
	}
	return wrapStoreError(err)
}

func (s *Service) jobQueue(ctx context.Context) (jobQueue, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	jobs, ok := s.store.(jobQueue)
	if !ok || jobs == nil {
		return nil, errOrganizeUnwired
	}
	return jobs, nil
}

func organizeViewOf(job workspace.Job) OrganizeView {
	return OrganizeView{
		JobID:  job.ID,
		State:  personOrganizeState(job.State),
		Detail: personOrganizeDetail(job.State, job.Error),
	}
}

func personOrganizeState(store string) string {
	switch store {
	case workspace.JobPending:
		return organizeQueued
	case workspace.JobLeased:
		return organizeRunning
	case workspace.JobDeferred, workspace.JobFailed:
		return organizeDelayed
	case workspace.JobCompleted:
		return organizeDone
	case workspace.JobCancelled:
		return organizeCancel
	default:
		return ""
	}
}

func personOrganizeDetail(store, detail string) string {
	detail = strings.TrimSpace(detail)
	if strings.EqualFold(detail, "checked") {
		return delayedDetail
	}
	if store == workspace.JobDeferred && detail == "" {
		return delayedDetail
	}
	return detail
}
