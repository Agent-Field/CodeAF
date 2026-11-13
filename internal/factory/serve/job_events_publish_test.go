package serve_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// E1: a publish a runner reports refused is one needs-you outcome, at the time
// factory.job answers for the wait, and a retried publish refused again is
// one more. The report arrives while the service's copy still runs the
// publish and the done lands the job after it; one refusal was recorded twice
// (hand test, 2026-11-12: publish_failed at 17:38:54.849 and 17:38:55.360).
func TestJobEventsRecordEachRefusedPublishOnce(t *testing.T) {
	f, _, it, _, _ := publishingSetup(t)
	clock := func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }
	landed := clock()
	// The run that landed: its one check passed half a second before it ended.
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.Stream = &factory.Stream{Cur: 0, Ended: landed, Phases: []factory.Phase{
			{Name: "verify", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1, At: landed.Add(-511 * time.Millisecond)},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	worker := f.attach(t, "sam")
	path := fmt.Sprintf("/v1/jobs/%d", it.ID)
	refused := func(attempt int) api.Job {
		t.Helper()
		pulled := f.pull(t, worker)
		lease := "/v1/workers/leases/" + pulled.Lease.ID
		f.clock.Add(int64(250 * time.Millisecond))
		p := &factory.Publishing{State: "failed", Code: "no_push_permission", Detail: "permission denied", Attempt: attempt, At: clock()}
		f.request(t, "POST", lease+"/report", worker.Credential, api.WorkerReport{Publishing: p}, 200, nil)
		f.clock.Add(int64(250 * time.Millisecond))
		f.request(t, "POST", lease+"/done", worker.Credential, api.WorkerDone{State: api.StateNeedsYou, Ready: true}, 200, nil)
		var job api.Job
		f.request(t, "GET", path, "", nil, 200, &job)
		if job.State != api.StateNeedsYou || job.NeedsYou == nil || job.NeedsYou.Kind != factory.NeedsPublishFailed {
			t.Fatalf("refused publish %d: %+v", attempt, job.NeedsYou)
		}
		return job
	}
	recorded := func(after int64, job api.Job) int64 {
		t.Helper()
		got, err := live.ReadOutcomes(f.st.Root(), &after, 100)
		if err != nil || got.Reset || len(got.Events) != 1 {
			t.Fatalf("one refusal recorded %d outcomes: %+v %v", len(got.Events), got.Events, err)
		}
		e := got.Events[0]
		if e.Kind != "needs_you" || e.Need != factory.NeedsPublishFailed || !e.At.Equal(job.NeedsYou.Since) {
			t.Fatalf("outcome %+v, factory.job waits since %s", e, job.NeedsYou.Since)
		}
		return got.Cursor
	}
	// The landed run waits for its proof's approval: that wait is recorded
	// before the publish starts.
	start, err := live.ReadOutcomes(f.st.Root(), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if approved := keyedRequest(f.handler, path+"/approve", `{"commit":"`+approvedHead+`"}`, "approval"); approved.Code != 200 {
		t.Fatalf("approve=%d %s", approved.Code, approved.Body)
	}
	settleApproval(t, f)
	first := refused(1)
	cursor := recorded(start.Cursor, first)
	if retried := keyedRequest(f.handler, path+"/retry_publish", "", "retry"); retried.Code != 200 {
		t.Fatalf("retry=%d %s", retried.Code, retried.Body)
	}
	second := refused(2)
	recorded(cursor, second)
	if second.NeedsYou.Since.Equal(first.NeedsYou.Since) {
		t.Fatalf("the second refusal waits since the first: %s", second.NeedsYou.Since)
	}
}
