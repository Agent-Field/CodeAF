package serve_test

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// pullFixture is a factory whose long poll waits wait, over a floor that
// counts every item document it reads back.
func pullFixture(t *testing.T, wait time.Duration) (*workerFixture, *atomic.Int64) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reads := &atomic.Int64{}
	// The output policy is asked once for every item read that carries a
	// stream, so it counts the floor's document reads.
	st.SetOutputPolicy(func() (int, int) { reads.Add(1); return 0, 0 })
	f := &workerFixture{st: st}
	cfg := config
	cfg.Store, cfg.PullWait = st, wait
	desk, err := lease.Open(st, cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	f.handler = serve.New(factory.Seam{Answer: lease.NewDoors(desk).Answer}, cfg)
	f.server = httptest.NewServer(f.handler)
	t.Cleanup(f.server.Close)
	t.Cleanup(f.handler.Close)
	return f, reads
}

// An idle long poll reads the floor when it opens and when the floor changes,
// not on a timer: every runner slot holds one open, so a timed re-read costs
// slots x jobs document reads every tick on a machine nobody is using.
func TestAnIdlePullDoesNotRereadAnUnchangedFloor(t *testing.T) {
	f, reads := pullFixture(t, time.Second)
	if _, err := f.st.Create(factory.Item{Title: "in backlog", Repo: "owner/repo", Kind: factory.KindIssue, State: factory.StateNew, Stream: &factory.Stream{}}); err != nil {
		t.Fatal(err)
	}
	worker := f.attach(t, "sam")
	reads.Store(0)
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
	// One read settles the request, one is the first look, and one more is
	// allowed for the watch reporting the job's own creation late. A timed
	// look made ten more for every second of the wait.
	if n := reads.Load(); n > 3 {
		t.Fatalf("an idle one-second pull read the unchanged floor %d times", n)
	}
}

// A job queued after an open pull made its first look is handed out by that
// pull, long before its wait ends.
func TestAnOpenPullTakesAJobQueuedDuringItsWait(t *testing.T) {
	f, reads := pullFixture(t, 20*time.Second)
	if _, err := f.st.Create(factory.Item{Title: "in backlog", Repo: "owner/repo", Kind: factory.KindIssue, State: factory.StateNew, Stream: &factory.Stream{}}); err != nil {
		t.Fatal(err)
	}
	worker := f.attach(t, "sam")
	reads.Store(0)
	done := make(chan api.WorkerPull, 1)
	go func() { done <- f.pull(t, worker) }()
	// Two reads of the backlog job are the request's settle and the pull's
	// first look: the queued job below comes after that look.
	for reads.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	queued := f.add(t, "sam")
	select {
	case pulled := <-done:
		if pulled.Lease.Job != queued.ID {
			t.Fatalf("the pull took job %d, want %d", pulled.Lease.Job, queued.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the open pull did not take a job queued during its wait")
	}
}
