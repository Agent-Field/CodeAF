package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

type fakeOrganizeJobs struct {
	mu       sync.Mutex
	pending  []workspace.Job
	finished []finishedJob
	leaseErr error
}

type finishedJob struct {
	ID, Fence, State, Detail string
}

func (f *fakeOrganizeJobs) LeaseJob(_ context.Context, _ []string, owner, until string) (workspace.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leaseErr != nil {
		return workspace.Job{}, f.leaseErr
	}
	if len(f.pending) == 0 {
		return workspace.Job{}, workspace.ErrNotFound
	}
	job := f.pending[0]
	f.pending = f.pending[1:]
	job.State = workspace.JobLeased
	job.Owner = owner
	job.Fence = "fence-" + job.ID
	job.LeaseUntil = until
	job.Attempt++
	return job, nil
}

func (f *fakeOrganizeJobs) FinishJob(_ context.Context, id, fence, state, detail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, finishedJob{ID: id, Fence: fence, State: state, Detail: detail})
	return nil
}

type fakeOrganizer struct {
	state, detail string
	err           error
	saw           []workspace.Job
}

func (o *fakeOrganizer) Organize(_ context.Context, job workspace.Job) (string, string, error) {
	o.saw = append(o.saw, job)
	return o.state, o.detail, o.err
}

func TestProcessOrganizeJobsLeavesPendingWhenTheOrganizerIsAbsent(t *testing.T) {
	jobs := &fakeOrganizeJobs{pending: []workspace.Job{{ID: "j1", Type: workspace.JobOrganize}}}
	if err := ProcessOrganizeJobs(context.Background(), jobs, nil, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(jobs.pending) != 1 || len(jobs.finished) != 0 {
		t.Fatalf("absent organizer leased or finished: pending=%d finished=%d", len(jobs.pending), len(jobs.finished))
	}
}

func TestProcessOrganizeJobsLeasesOutsideTheWriterAndFinishes(t *testing.T) {
	jobs := &fakeOrganizeJobs{pending: []workspace.Job{{
		ID: "j1", Type: workspace.JobOrganize, ChatID: "chat-a", SourceRev: "1:deadbeef",
	}}}
	work := &fakeOrganizer{state: workspace.JobCompleted, detail: "no-action"}
	if err := ProcessOrganizeJobs(context.Background(), jobs, work, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(work.saw) != 1 || work.saw[0].ID != "j1" || work.saw[0].Fence == "" {
		t.Fatalf("organizer did not see the leased job: %+v", work.saw)
	}
	if len(jobs.finished) != 1 || jobs.finished[0].State != workspace.JobCompleted || jobs.finished[0].Fence != work.saw[0].Fence {
		t.Fatalf("finish %+v", jobs.finished)
	}
}

func TestProcessOrganizeJobsCancelsWhenOrganizeIsOff(t *testing.T) {
	jobs := &fakeOrganizeJobs{pending: []workspace.Job{{ID: "j1", Type: workspace.JobOrganize}}}
	work := &fakeOrganizer{state: workspace.JobCompleted}
	if err := ProcessOrganizeJobs(context.Background(), jobs, work, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(work.saw) != 0 {
		t.Fatal("off still called the organizer")
	}
	if len(jobs.finished) != 1 || jobs.finished[0].State != workspace.JobCancelled {
		t.Fatalf("off finish %+v", jobs.finished)
	}
}

func TestProcessOrganizeJobsFailsAfterTooManyAttempts(t *testing.T) {
	jobs := &fakeOrganizeJobs{pending: []workspace.Job{{ID: "j1", Attempt: organizeAttemptCap}}}
	work := &fakeOrganizer{state: workspace.JobCompleted}
	if err := ProcessOrganizeJobs(context.Background(), jobs, work, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(work.saw) != 0 {
		t.Fatal("capped job still ran")
	}
	if len(jobs.finished) != 1 || jobs.finished[0].State != workspace.JobFailed {
		t.Fatalf("cap finish %+v", jobs.finished)
	}
}

func TestProcessOrganizeJobsStopsOnAnEmptyQueue(t *testing.T) {
	jobs := &fakeOrganizeJobs{leaseErr: workspace.ErrNotFound}
	work := &fakeOrganizer{state: workspace.JobCompleted}
	if err := ProcessOrganizeJobs(context.Background(), jobs, work, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(work.saw) != 0 {
		t.Fatal("empty queue called the organizer")
	}
}

func TestProcessOrganizeJobsRecordsAnOrganizerErrorAsFailed(t *testing.T) {
	jobs := &fakeOrganizeJobs{pending: []workspace.Job{{ID: "j1"}}}
	work := &fakeOrganizer{err: errors.New("embedder down")}
	if err := ProcessOrganizeJobs(context.Background(), jobs, work, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(jobs.finished) != 1 || jobs.finished[0].State != workspace.JobFailed || jobs.finished[0].Detail != "embedder down" {
		t.Fatalf("error finish %+v", jobs.finished)
	}
}

func TestOrganizeCoalesceKeyAndRevisionAreStableForOneLine(t *testing.T) {
	rev := OrganizeRevision(3, "customers must authenticate")
	if !strings.HasPrefix(rev, "3:") || len(rev) < 6 {
		t.Fatalf("revision %q", rev)
	}
	if OrganizeRevision(3, "customers must authenticate") != rev {
		t.Fatal("same line produced two revisions")
	}
	if OrganizeRevision(4, "customers must authenticate") == rev {
		t.Fatal("a later ordinal reused the revision")
	}
	key := OrganizeCoalesceKey("aaaaaaaaaaaaaaaa", rev)
	if key != "aaaaaaaaaaaaaaaa:"+rev {
		t.Fatalf("coalesce key %q", key)
	}
}
