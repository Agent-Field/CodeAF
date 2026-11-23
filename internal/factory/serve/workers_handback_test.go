package serve_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A QUITTING RUNNER'S HOLD IS ON THE QUEUE AT ONCE. Before, a clean quit let
// the hold run out: the job waited the lease's whole silence and was then
// marked offline mid-step. A handed-back hold queues the running job with no
// failure, and the next pull, at the same instant, takes it.
func TestAHandedBackHoldIsTakenAtOnceWithNoFailure(t *testing.T) {
	f := workerSetup(t)
	first := f.attach(t, "sam")
	job := f.add(t, "sam")
	pulled := f.pull(t, first)
	if err := f.st.Update(job.ID, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", State: factory.PhaseRunning, Round: 1}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := "/v1/workers/leases/" + pulled.Lease.ID + "/handback"

	// Only the hold's own runner may give it back.
	other := f.attach(t, "sam")
	f.request(t, "POST", path, other.Credential, struct{}{}, 403, nil)

	f.request(t, "POST", path, first.Credential, struct{}{}, 200, nil)
	it, err := f.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if it.State != factory.StateQueued || it.Failure != nil || it.QKind != "" {
		t.Fatalf("a handed-back job is %s, failure %+v, asking %q; want queued with none", it.State, it.Failure, it.QKind)
	}
	// The same instant: no hold's silence is waited out.
	again := f.pull(t, other)
	if again.Job.ID != job.ID {
		t.Fatalf("the next pull took job %d, want %d", again.Job.ID, job.ID)
	}
	// A hold given back is gone: a second hand-back of it is refused.
	f.request(t, "POST", path, first.Credential, struct{}{}, 409, nil)
}
