package serve_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestWorkerAgentFieldPipelineCompletionHasDoneAt(t *testing.T) {
	for _, remoteEnd := range []bool{true, false} {
		t.Run(fmt.Sprintf("remote_end=%t", remoteEnd), func(t *testing.T) {
			f := workerSetupAs(t, "sam")
			job, err := f.st.Create(factory.Item{Title: "answer a request", Repo: "owner/repo", State: factory.StateQueued, Assignee: "sam",
				Stages: []factory.Stage{{Name: "approve", Kind: factory.StageGate, Pass: "person", On: true}, {Name: "answer", Kind: factory.StageAgentField, Target: "support.answer", On: true}}})
			if err != nil {
				t.Fatal(err)
			}
			worker := f.attach(t, "sam")
			pulled := f.pull(t, worker)
			endpoint := "/v1/workers/leases/" + pulled.Lease.ID
			report := factory.RunReport{Question: &factory.Asked{Stage: "approve", Question: "Continue?", With: factory.AskedYou}, QuestionKind: factory.QKindApprove, Stages: []factory.StageReport{{Name: "approve", State: factory.PhaseWaiting}}}
			f.request(t, "POST", endpoint+"/report", worker.Credential, api.FromRunReport(report), 200, nil)
			f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/answer", job.ID), "", map[string]bool{"yes": true}, 200, nil)
			ended := time.Unix(0, f.clock.Load()).UTC()
			remote := ended.Add(-time.Second)
			if !remoteEnd {
				remote = time.Time{}
			}
			report = factory.RunReport{AgentFieldSteps: map[string]*factory.AgentFieldRun{"answer": {Target: "support.answer", Status: "succeeded", Ended: remote}},
				Stages: []factory.StageReport{{Name: "answer", State: factory.PhaseDone, Result: &factory.StageResult{Done: true, Output: "The answer is ready."}}}}
			f.request(t, "POST", endpoint+"/report", worker.Credential, api.FromRunReport(report), 200, nil)
			f.request(t, "POST", endpoint+"/done", worker.Credential, api.WorkerDone{State: api.StateDone}, 200, nil)
			want := ended
			if remoteEnd {
				want = remote
			}
			check := func(j api.Job) {
				t.Helper()
				if j.State != api.StateDone || j.DoneHow != "answered" || !j.DoneAt.Equal(want) {
					t.Errorf("completed pipeline: state=%s done_how=%s done_at=%s; want done, answered, %s", j.State, j.DoneHow, j.DoneAt, want)
				}
				if j.RunOn != "" || j.PR != nil {
					t.Errorf("answer pipeline acquired a route or PR: %+v", j)
				}
			}
			var got api.Job
			f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", job.ID), "", nil, 200, &got)
			check(got)
			var queue api.Queue
			f.request(t, "GET", "/v1/queue", "", nil, 200, &queue)
			if len(queue.Jobs) != 1 {
				t.Fatalf("queue has %d jobs", len(queue.Jobs))
			}
			check(queue.Jobs[0])
			reopened, err := store.Open(f.st.Root())
			if err != nil {
				t.Fatal(err)
			}
			saved, err := reopened.Get(job.ID)
			if err != nil {
				t.Fatal(err)
			}
			check(api.FromItem(saved))
			if saved.Stream == nil || saved.Stream.Ended.IsZero() {
				t.Fatal("completion did not end the stream")
			}
		})
	}
}
