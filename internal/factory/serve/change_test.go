package serve_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// The real worker report route must make issue changes readable at both
// public doors and preserve them after the factory store is reopened.
func TestWorkerChangeFactsReachJobAndQueueAndSurviveRestart(t *testing.T) {
	f := workerSetup(t)
	it := f.add(t, "sam")
	worker := f.attach(t, "sam")
	pulled := f.pull(t, worker)
	change := &factory.Change{Branch: "factory/issue-7", Files: []factory.ChangedFile{{Path: "fix.go", Added: 4, Removed: 1, Status: "modified"}}, Stat: &factory.ChangeStat{Files: 1, Added: 4, Removed: 1}, Tests: []factory.StepTests{{Step: "test", TestCounts: factory.TestCounts{Passed: 3, Skipped: 1}}}}
	path := "/v1/workers/leases/" + pulled.Lease.ID + "/report"
	f.request(t, "POST", path, worker.Credential, api.WorkerReport{Change: change, ChangeBase: "base-sha", PR: &api.PR{Number: 8, URL: "https://github.com/owner/repo/pull/8", Draft: true}}, 200, nil)
	var job api.Job
	f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "", nil, 200, &job)
	if !reflect.DeepEqual(job.Change, change) || job.PR == nil || !job.PR.Draft {
		t.Fatalf("job lost facts: %+v", job)
	}
	var queue api.Queue
	f.request(t, "GET", "/v1/queue", "", nil, 200, &queue)
	if len(queue.Jobs) != 1 || !reflect.DeepEqual(queue.Jobs[0].Change, change) {
		t.Fatalf("queue lost facts: %+v", queue.Jobs)
	}
	st, err := store.Open(f.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := st.Get(it.ID)
	if err != nil || !reflect.DeepEqual(saved.Change, change) || saved.ChangeBase != "base-sha" {
		t.Fatalf("saved facts = %+v, %v", saved, err)
	}
	before, _ := f.st.Revision(it.ID)
	f.request(t, "POST", path, worker.Credential, api.WorkerReport{Change: &factory.Change{Files: []factory.ChangedFile{{Path: "fix.go", Added: -1, Status: "modified"}}}}, 400, nil)
	after, _ := f.st.Revision(it.ID)
	if before != after {
		t.Fatal("invalid change report wrote the job")
	}
}
