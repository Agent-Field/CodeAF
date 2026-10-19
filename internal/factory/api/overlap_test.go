package api

import (
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// TestOnlyRunningJobsOverlapAndInFloorOrder pins what the queue's overlap
// column says now that it compares running jobs only: two running jobs that
// change the same file name each other, in floor order, and a job that is not
// running names nobody and is named by nobody, whatever files it lists.
func TestOnlyRunningJobsOverlapAndInFloorOrder(t *testing.T) {
	file := []factory.FileChange{{Path: "internal/tui3/tree.go"}}
	items := []factory.Item{
		{ID: 1, Repo: "acme/api", Num: 11, State: factory.StateRunning, Files: file},
		{ID: 2, Repo: "acme/api", Num: 12, State: factory.StateQueued, Files: file},
		{ID: 3, Repo: "acme/api", Num: 13, State: factory.StateRunning, Change: &factory.Change{Files: []factory.ChangedFile{{Path: "internal/tui3/tree.go"}}}},
		{ID: 4, Repo: "acme/web", Num: 14, State: factory.StateRunning, Files: file},
		{ID: 5, Repo: "acme/api", Num: 15, State: factory.StateRunning, Files: file},
	}
	q := FromSnapshot(factory.Snapshot{Items: items}, "sam")
	ref := func(i int) string { return factory.JobRef(items[i]) }
	want := [][]string{{ref(2), ref(4)}, nil, {ref(0), ref(4)}, nil, {ref(0), ref(2)}}
	for i, job := range q.Jobs {
		if !slices.Equal(job.Overlap, want[i]) {
			t.Errorf("job %d overlaps %v, want %v", job.ID, job.Overlap, want[i])
		}
	}
}
