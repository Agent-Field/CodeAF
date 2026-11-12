package serve_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// A JOB PARKED AT ITS BUDGET IS ONE WAIT, HOWEVER MANY REPORTS ITS RUNNER
// SENDS WHILE IT WAITS. The worker sends the cap question again, with no clock
// of its own, on every later report that carries thread messages, usage or
// acknowledgements (worker.flush), so the factory reads the wait as parked
// and not as work at the exhausted cap. Each one used to be stamped with the
// report's arrival: the wait's clock moved, factory.job's needs_you.since moved
// with it, and every report recorded another budget needs_you the desktop app
// could not tell from a new wait (its key is needs_you:budget:<at>).
func TestJobEventsRecordAParkedBudgetOnce(t *testing.T) {
	f := workerSetupAs(t, "sam")
	worker := f.attach(t, "sam")
	job := f.add(t, "sam")
	if err := f.st.Update(job.ID, func(it *factory.Item) error {
		it.Cap, it.Autonomy = 5, factory.AutonomyAuto
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	endpoint := "/v1/workers/leases/" + f.pull(t, worker).Lease.ID
	path := fmt.Sprintf("/v1/jobs/%d", job.ID)
	start, err := live.ReadOutcomes(f.st.Root(), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	// The first report parks the job: its step spent the budget, and the
	// runner asks the person (the mirror's loop-made ask has no clock).
	asked := &api.Question{Kind: "cap", Text: "budget of $5 reached", FromStep: "proof"}
	f.request(t, "POST", endpoint+"/report", worker.Credential, api.WorkerReport{Cost: 5, Question: asked}, 200, nil)
	var first api.Job
	f.request(t, "GET", path, "", nil, 200, &first)
	if first.State != api.StateNeedsYou || first.NeedsYou == nil || first.NeedsYou.Kind != factory.NeedsSpendingLimit {
		t.Fatalf("the job is not parked at its budget: %s %+v", first.State, first.NeedsYou)
	}
	// Later reports while it waits: the manager's notes, a little usage, an
	// acknowledgement; each carries the same question again with no clock.
	later := []api.WorkerReport{
		{Question: asked, Notes: []string{"the manager looked at the plan"}},
		{Question: asked, Cost: 0.01},
		{Question: asked, Talk: "manager-1"},
	}
	for i, report := range later {
		f.clock.Add(int64(3 * time.Second))
		f.request(t, "POST", endpoint+"/report", worker.Credential, report, 200, nil)
		var job api.Job
		f.request(t, "GET", path, "", nil, 200, &job)
		if job.NeedsYou == nil || job.NeedsYou.Kind != factory.NeedsSpendingLimit || !job.NeedsYou.Since.Equal(first.NeedsYou.Since) {
			t.Errorf("report %d moved the wait: needs_you %+v, first since %s", i+1, job.NeedsYou, first.NeedsYou.Since)
		}
	}
	stored, err := f.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	asks := 0
	for _, line := range stored.Stream.Log {
		if line.Tone == "ask" {
			asks++
		}
	}
	if asks != 1 {
		t.Errorf("the job's log asks %d times for one parked budget: %+v", asks, stored.Stream.Log)
	}
	got, err := live.ReadOutcomes(f.st.Root(), &start.Cursor, 100)
	if err != nil || got.Reset {
		t.Fatalf("outcomes: %+v %v", got, err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("one parked budget recorded %d outcomes: %+v", len(got.Events), got.Events)
	}
	if e := got.Events[0]; e.Kind != "needs_you" || e.Need != factory.NeedsSpendingLimit || e.Reason != "budget" || !e.At.Equal(first.NeedsYou.Since) {
		t.Fatalf("outcome %+v, factory.job waits since %s", e, first.NeedsYou.Since)
	}
}
