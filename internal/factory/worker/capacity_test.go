package worker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

// capRig is a factory with one worker whose capacity follows the owner's
// saved number. Time is injected: the capacity read happens only when the
// test sends a tick, and a job holds its slot until the test opens its fifo,
// so nothing here waits on a clock.
type capRig struct {
	url    string
	st     *store.Store
	floor  factory.Seam
	ticks  chan time.Time
	events chan leaseEvent
	dir    string
	jobs   map[int]string
	// reads counts the capacity reads that reached the factory.
	mu    sync.Mutex
	reads int
	dials map[int]int
	// attaches records every POST /v1/workers/attach the old-server gate saw.
	attaches []attachSeen
}

type attachSeen struct {
	header string
	err    error
}

// legacyAttach is the attach body a factory from before live_capacity decodes.
// It has no such field, and like serve.decode it refuses unknown ones.
type legacyAttach struct {
	Runner          string          `json:"runner,omitempty"`
	Owner           string          `json:"owner,omitempty"`
	Kind            string          `json:"kind,omitempty"`
	Slots           int             `json:"slots,omitempty"`
	Checkouts       []api.Checkout  `json:"checkouts,omitempty"`
	Machine         string          `json:"machine,omitempty"`
	Name            string          `json:"name"`
	Handle          string          `json:"handle"`
	OS              string          `json:"os,omitempty"`
	CodeafVersion   string          `json:"codeaf_version,omitempty"`
	TakesUnassigned map[string]bool `json:"takes_unassigned,omitempty"`
}

// oldStrictGate runs a legacy strict decode over every attach body before the
// real handler sees it, and records the outcome. A refusal is answered 422 the
// way the old factory did, and is never retried by the worker.
func (g *capRig) oldStrictGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workers/attach" {
			next.ServeHTTP(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err := dec.Decode(&legacyAttach{})
		g.mu.Lock()
		g.attaches = append(g.attaches, attachSeen{header: r.Header.Get(api.LiveCapacityHeader), err: err})
		g.mu.Unlock()
		if err != nil {
			http.Error(w, `{"error":"unknown field"}`, http.StatusUnprocessableEntity)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		next.ServeHTTP(w, r)
	})
}

type leaseEvent struct {
	job      int
	released bool
}

// newCapRig starts the worker. desired false leaves Options.Desired nil, the
// pre-change fixed-slot worker.
func newCapRig(t *testing.T, slots int, desired bool) *capRig {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(lease.NewDoors(desk)))
	// This machine's person: the worker attaches as them and so does the PUT.
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "me"}, nil }
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	handler := serve.New(seam, serve.Config{Address: "http://test", Store: st, Record: history, PullWait: 20 * time.Millisecond})
	g := &capRig{ticks: make(chan time.Time), events: make(chan leaseEvent, 256), dir: t.TempDir(), jobs: map[int]string{}, dials: map[int]int{}}
	server := httptest.NewServer(g.oldStrictGate(handler))
	t.Cleanup(func() { server.Close(); handler.Close() })
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g.url, g.st, g.floor = server.URL, st, client.Open(server.URL, "").Seam()
	repo := t.TempDir()
	r := run.New(run.Options{
		Store:   mirror,
		Exec:    map[factory.StageKind]run.Executor{factory.StageCheck: run.NewCheckExecutor(run.CheckOptions{})},
		RepoDir: func(string) string { return repo },
	})
	var mu sync.Mutex
	creds := map[int]string{}
	held := map[int]bool{}
	opts := worker.Options{
		Dial: func(slot int) *client.WorkerClient {
			mu.Lock()
			defer mu.Unlock()
			g.mu.Lock()
			g.dials[slot]++
			g.mu.Unlock()
			return client.Open(server.URL, "").Worker(creds[slot])
		},
		Keep: func(slot int, w *client.WorkerClient) {
			mu.Lock()
			defer mu.Unlock()
			creds[slot] = w.Credential()
		},
		// No handle: a loopback factory gives the worker its own person, the
		// same one a PUT from this machine is made as.
		Attach: api.WorkerAttach{Machine: "laptop", Name: "laptop (pid 1)", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
		Runner: r, Mirror: mirror, Slots: slots,
		LocalLease: func(_ string, l api.WorkerLease) {
			// One event per job when it is first held and once when it is let go.
			mu.Lock()
			defer mu.Unlock()
			switch {
			case l.ID != "" && !held[l.Job]:
				held[l.Job] = true
				g.events <- leaseEvent{job: l.Job}
			case l.ID == "" && held[l.Job]:
				delete(held, l.Job)
				g.events <- leaseEvent{job: l.Job, released: true}
			}
		},
		ReportEvery: 20 * time.Millisecond, BeatEvery: 20 * time.Millisecond,
	}
	if desired {
		opts.CapacityTick = g.ticks
		opts.Desired = func(ctx context.Context) (int, bool) {
			mu.Lock()
			credential := creds[0]
			mu.Unlock()
			if credential == "" {
				return 0, false
			}
			self, err := client.Open(server.URL, "").Worker(credential).Self(ctx)
			if err != nil {
				return 0, false
			}
			g.mu.Lock()
			g.reads++
			g.mu.Unlock()
			return self.Slots, true
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx, opts) }()
	t.Cleanup(func() {
		cancel()
		// Open every fifo so no check is left blocking the shutdown.
		for id := range g.jobs {
			g.release(t, id)
		}
		<-done
	})
	return g
}

// tick drives one capacity read to completion. The second send can only be
// received once the supervisor has finished handling the first.
func (g *capRig) tick(t *testing.T) {
	t.Helper()
	for range 2 {
		select {
		case g.ticks <- time.Time{}:
		case <-time.After(10 * time.Second):
			t.Fatal("the worker never took a capacity tick")
		}
	}
}

// job queues and launches a check that holds its slot until release.
func (g *capRig) job(t *testing.T) int {
	t.Helper()
	fifo := filepath.Join(g.dir, "fifo-"+time.Now().Format("150405.000000000"))
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := g.st.Add(context.Background(), factory.Item{Title: "hold a slot", Repo: "api", Tier: factory.TierOwner, Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "cat " + fifo + " >/dev/null", On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	g.jobs[id] = fifo
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	return id
}

// release lets the job's check finish.
func (g *capRig) release(t *testing.T, id int) {
	t.Helper()
	// Non-blocking: with no reader there is nothing to release.
	f, err := os.OpenFile(g.jobs[id], os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	f.Close()
}

// next is the next lease event.
func (g *capRig) next(t *testing.T) leaseEvent {
	t.Helper()
	select {
	case e := <-g.events:
		return e
	case <-time.After(20 * time.Second):
		t.Fatal("the worker did not pull or let go of a job")
		return leaseEvent{}
	}
}

// pulled collects n distinct jobs the worker took, in any order.
func (g *capRig) pulled(t *testing.T, n int) map[int]bool {
	t.Helper()
	got := map[int]bool{}
	for len(got) < n {
		e := g.next(t)
		if e.released {
			t.Fatalf("job %d was let go while %d jobs were expected to run", e.job, n)
		}
		got[e.job] = true
	}
	return got
}

// setSlots is the owner's PUT, made as this machine's person.
func (g *capRig) setSlots(t *testing.T, n int) {
	t.Helper()
	body, _ := json.Marshal(api.RunnerSettings{Slots: n})
	req, _ := http.NewRequest(http.MethodPut, g.url+"/v1/runners/laptop", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT slots=%d answered %d", n, resp.StatusCode)
	}
}

// TestARaisedCapacityStartsAnotherConcurrentJob: a worker begun at two slots
// holds two jobs, the owner raises to three, and the third job runs beside
// them. Before the change the worker spawned its slot goroutines once and a
// raise only moved the server's cap, so the third job sat queued.
func TestARaisedCapacityStartsAnotherConcurrentJob(t *testing.T) {
	g := newCapRig(t, 2, true)
	first, second := g.job(t), g.job(t)
	g.pulled(t, 2)
	third := g.job(t)
	g.setSlots(t, 3)
	g.tick(t)
	if e := g.next(t); e.released || e.job != third {
		t.Fatalf("after the raise the next event was %+v, want job %d to start beside %d and %d", e, third, first, second)
	}
}

// TestALoweredCapacityLetsRunningJobsFinishAndHoldsTheCap: lowering from three
// to one cancels nothing; new work waits until the running ones are done and
// then runs one at a time; a later raise brings the second slot back.
func TestALoweredCapacityLetsRunningJobsFinishAndHoldsTheCap(t *testing.T) {
	g := newCapRig(t, 3, true)
	running := []int{g.job(t), g.job(t), g.job(t)}
	g.pulled(t, 3)
	g.setSlots(t, 1)
	g.tick(t)
	waiting := g.job(t)
	// Two finish; the lowered cap still counts the third, so the waiting job
	// is not pulled. Only releases may arrive.
	for _, id := range running[:2] {
		g.release(t, id)
		if e := g.next(t); !e.released {
			t.Fatalf("job %d was pulled while the lowered cap was full: %+v", e.job, e)
		}
	}
	g.release(t, running[2])
	if e := g.next(t); !e.released || e.job != running[2] {
		t.Fatalf("the last running job did not finish on its own: %+v", e)
	}
	if e := g.next(t); e.released || e.job != waiting {
		t.Fatalf("the waiting job should run once the cap is free: %+v", e)
	}
	// Raise again: the retired slots start again and both new jobs run at once.
	g.setSlots(t, 3)
	g.tick(t)
	a, b := g.job(t), g.job(t)
	got := g.pulled(t, 2)
	if !got[a] || !got[b] {
		t.Fatalf("after the raise jobs %d and %d should run together, got %v", a, b, got)
	}
}

// TestAWorkerWithoutADesiredReadStaysAtItsOwnSlots: the pre-change worker,
// built with no Desired, keeps its slot count however the owner moves the
// server's cap.
func TestAWorkerWithoutADesiredReadStaysAtItsOwnSlots(t *testing.T) {
	g := newCapRig(t, 1, false)
	first := g.job(t)
	if e := g.next(t); e.job != first {
		t.Fatalf("%+v", e)
	}
	g.setSlots(t, 2)
	second := g.job(t)
	// The server would now allow a second lease, but this worker was built
	// with one slot and no way to learn otherwise. Releasing the first lets
	// the single slot go on to the second.
	g.release(t, first)
	if e := g.next(t); !e.released || e.job != first {
		t.Fatalf("%+v", e)
	}
	if e := g.next(t); e.released || e.job != second {
		t.Fatalf("%+v", e)
	}
}

// liveFlag reads the roster as a loopback caller and returns the one runner's
// live_capacity, so the test asks the real door a future screen would.
func (g *capRig) liveFlag(t *testing.T) bool {
	t.Helper()
	res, err := http.Get(g.url + "/v1/runners")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var list api.Runners
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil || len(list.Runners) != 1 {
		t.Fatalf("roster: %v %+v", err, list)
	}
	return list.Runners[0].LiveCapacity
}

// TestOnlyAnElasticWorkerAttestsLiveCapacity: a worker with a Desired read
// says it follows capacity changes; one without never does, so a screen can
// refuse to promise a raise to it.
func TestOnlyAnElasticWorkerAttestsLiveCapacity(t *testing.T) {
	for _, elastic := range []bool{true, false} {
		g := newCapRig(t, 1, elastic)
		g.job(t)
		g.next(t)
		if got := g.liveFlag(t); got != elastic {
			t.Fatalf("elastic=%v listed live_capacity=%v", elastic, got)
		}
	}
}

// TestAnElasticWorkerAttachesToAnOldStrictFactoryWithOnePost: the attestation
// rides in a header, so a factory that refuses unknown JSON fields accepts the
// attach on the first and only POST, and a current factory still rosters true.
// A plain worker sends no header at all.
func TestAnElasticWorkerAttachesToAnOldStrictFactoryWithOnePost(t *testing.T) {
	for _, elastic := range []bool{true, false} {
		g := newCapRig(t, 1, elastic)
		g.job(t)
		g.next(t)
		g.mu.Lock()
		seen := append([]attachSeen(nil), g.attaches...)
		g.mu.Unlock()
		if len(seen) != 1 {
			t.Fatalf("elastic=%v: %d attach POSTs, want exactly 1", elastic, len(seen))
		}
		if seen[0].err != nil {
			t.Fatalf("elastic=%v: old strict decoder refused the body: %v", elastic, seen[0].err)
		}
		want := ""
		if elastic {
			want = api.LiveCapacityHeaderValue
		}
		if seen[0].header != want {
			t.Fatalf("elastic=%v: header %q, want %q", elastic, seen[0].header, want)
		}
		if got := g.liveFlag(t); got != elastic {
			t.Fatalf("elastic=%v listed live_capacity=%v", elastic, got)
		}
	}
}
