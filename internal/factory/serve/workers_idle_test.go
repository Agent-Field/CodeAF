package serve_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// AN IDLE RUNNER DECODES NOTHING TO LEARN NOTHING CHANGED. Every runner slot
// holds a pull open and makes a fresh one each wait, and each look listed and
// decoded every job on the floor: fifteen idle slots on the fourth live run's
// floor of eighty jobs spent a third of the idle engine's CPU finding no work.
// With no lease held, a pull on a floor no file of which changed (the pull's
// own bookkeeping aside) does not look again; a job that arrives is found at
// once.
func TestAnIdlePullDoesNotLookAgainAtAnUnchangedFloor(t *testing.T) {
	f := workerSetup(t)
	worker := f.attach(t, "sam")
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
	before := serve.PullLooks()
	for range 10 {
		// The bookkeeping a pull and a poll write is rewritten between pulls.
		if err := os.WriteFile(filepath.Join(f.st.Root(), "sources.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
	}
	if got := serve.PullLooks() - before; got != 0 {
		t.Fatalf("10 idle pulls of an unchanged floor looked %d times", got)
	}
	job := f.add(t, "sam")
	if pulled := f.pull(t, worker); pulled.Job.ID != job.ID {
		t.Fatalf("the job that arrived was not found: %+v", pulled.Job)
	}
	// With a lease held, every pull looks: a hold can expire by the clock.
	other := f.attach(t, "sam")
	before = serve.PullLooks()
	f.request(t, "GET", "/v1/workers/pull", other.Credential, nil, 204, nil)
	f.request(t, "GET", "/v1/workers/pull", other.Credential, nil, 204, nil)
	if got := serve.PullLooks() - before; got < 2 {
		t.Fatalf("pulls while a lease is held looked %d times, want every time", got)
	}
}

// A RUNNER'S REQUESTS SETTLE THE FLOOR AT MOST ONCE A SECOND. A settle lists
// and decodes every job and ran before every request, so fifteen idle slots
// re-pulling together decoded the whole floor fifteen times each wait. A
// person's request still settles first, every time.
func TestARunnersRequestsSettleTheFloorAtMostOnceASecond(t *testing.T) {
	f := workerSetup(t)
	worker := f.attach(t, "sam")
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
	before := serve.Settles()
	for range 10 {
		f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
	}
	if got := serve.Settles() - before; got != 0 {
		t.Fatalf("10 pulls in one instant settled %d times", got)
	}
	f.clock.Add(int64(time.Second))
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
	if got := serve.Settles() - before; got != 1 {
		t.Fatalf("a pull a second later settled %d times, want 1", got)
	}
}
