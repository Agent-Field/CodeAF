package worker_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
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

// rig is a factory over loopback whose runner doors are the lease's, and one
// worker with a real Runner whose only executor is the check: no model.
type rig struct {
	st    *store.Store
	floor factory.Seam // the floor as any window reaches it: over HTTP
	doors factory.Seam // the server's own seam, past the record's approval binding
	run   *run.Runner
}

func newRig(t *testing.T, localLease ...func(string, api.WorkerLease)) rig {
	t.Helper()
	var keepLocal func(string, api.WorkerLease)
	if len(localLease) > 0 {
		keepLocal = localLease[0]
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(lease.NewDoors(desk)))
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	handler := serve.New(seam, serve.Config{Address: "http://test", Store: st, Record: history, PullWait: 50 * time.Millisecond})
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close(); handler.Close() })

	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r := run.New(run.Options{
		Store:   mirror,
		Exec:    map[factory.StageKind]run.Executor{factory.StageCheck: run.NewCheckExecutor(run.CheckOptions{})},
		RepoDir: func(string) string { return dir },
	})
	var mu sync.Mutex
	creds := map[int]string{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx, worker.Options{
			Dial: func(slot int) *client.WorkerClient {
				mu.Lock()
				defer mu.Unlock()
				return client.Open(server.URL, "").Worker(creds[slot])
			},
			Keep: func(slot int, w *client.WorkerClient) {
				mu.Lock()
				defer mu.Unlock()
				creds[slot] = w.Credential()
			},
			Attach:      api.WorkerAttach{Name: "laptop", Handle: "me", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
			Runner:      r,
			Mirror:      mirror,
			LocalLease:  keepLocal,
			Slots:       2,
			ReportEvery: 20 * time.Millisecond,
			BeatEvery:   20 * time.Millisecond,
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	return rig{st: st, floor: client.Open(server.URL, "").Seam(), doors: seam, run: r}
}

func TestWorkerLocalReceiptFollowsPullHeartbeatAndRelease(t *testing.T) {
	type receipt struct {
		base string
		held api.WorkerLease
	}
	receipts := make(chan receipt, 100)
	g := newRig(t, func(base string, held api.WorkerLease) { receipts <- receipt{base, held} })
	id := g.add(t, factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, On: true}, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	next := func() receipt {
		t.Helper()
		select {
		case r := <-receipts:
			return r
		case <-time.After(5 * time.Second):
			t.Fatal("the worker did not update its local lease receipt")
			return receipt{}
		}
	}
	pulled := next()
	if pulled.base == "" || pulled.held.ID == "" || pulled.held.Job != id || pulled.held.Worker == "" {
		t.Fatalf("pull receipt = %+v", pulled)
	}
	renewed := next()
	if renewed.base != pulled.base || renewed.held.ID != pulled.held.ID || !renewed.held.ExpiresAt.After(pulled.held.ExpiresAt) {
		t.Fatalf("renewed receipt = %+v, pull = %+v", renewed, pulled)
	}
	if err := g.floor.Stop(id); err != nil {
		t.Fatal(err)
	}
	for r := next(); ; r = next() {
		if r.held.ID == "" {
			if r.held.Job != id || r.base != pulled.base {
				t.Fatalf("release receipt = %+v", r)
			}
			break
		}
	}
}

func (g rig) add(t *testing.T, stages ...factory.Stage) int {
	t.Helper()
	id, err := g.st.Add(context.Background(), factory.Item{Title: "prove it builds", Repo: "api", Tier: factory.TierOwner, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (g rig) waitFor(t *testing.T, id int, want factory.State) factory.Item {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		it, err := g.st.Get(id)
		if err == nil && it.State == want {
			return it
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d never became %s; it is %s (%q) %+v", id, want, it.State, it.Question, it.Stream)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func check(cmd string) factory.Stage {
	return factory.Stage{Name: "test", Kind: factory.StageCheck, Ask: cmd, On: true}
}

func logHas(it factory.Item, text string) int {
	n := 0
	if it.Stream != nil {
		for _, l := range it.Stream.Log {
			if strings.Contains(l.Text, text) {
				n++
			}
		}
	}
	return n
}

// TestAJobRunsOnTheWorkerThatPullsIt is the whole road on loopback: a run
// pressed on the factory queues the job, the worker pulls it, its runner runs
// the check here, and the step's result, the claim and the landing reach the
// factory's store as reports; the lease is given back. With no approve step
// and every claim shown, the job ships itself.
func TestAJobRunsOnTheWorkerThatPullsIt(t *testing.T) {
	g := newRig(t)
	id := g.add(t, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateShipped)
	if len(it.Proof) != 1 || !it.Proof[0].OK || it.Proof[0].Text != "true passes" {
		t.Fatalf("proof on the factory = %+v", it.Proof)
	}
	if ph := it.Stream.Phases[0]; ph.State != factory.PhaseDone || ph.Result == nil || ph.Result.Exit != 0 {
		t.Fatalf("the step's result did not land: %+v", ph)
	}
	if n := logHas(it, "shipped · habit"); n != 1 {
		t.Fatalf("the landing is said %d times on the factory's log: %+v", n, it.Stream.Log)
	}
	desk, _ := lease.Open(g.st, lease.Options{})
	if _, err := desk.Drain("any", "any"); err == nil {
		t.Fatal("a lease is still held")
	}
	if err := g.floor.Launch(id); err == nil || !strings.Contains(err.Error(), "has shipped") {
		t.Fatalf("a second run = %v, not the runner's own refusal", err)
	}
}

// TestAnApproveStepWaitsForThePersonsAnswer: the question reaches the factory,
// the person's yes reaches the worker through the heartbeat, and the run goes
// on past the step only then.
func TestAnApproveStepWaitsForThePersonsAnswer(t *testing.T) {
	g := newRig(t)
	id := g.add(t, factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, On: true}, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	held := g.waitFor(t, id, factory.StateNeedsYou)
	if held.Question == "" || held.Stream.Phases[0].State != factory.PhaseWaiting {
		t.Fatalf("the factory does not show the question: %q %+v", held.Question, held.Stream.Phases)
	}
	if err := g.doors.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateLanded)
	if it.Stream.Phases[0].State != factory.PhaseDone || it.Stream.Phases[1].State != factory.PhaseDone {
		t.Fatalf("steps after the answer: %+v", it.Stream.Phases)
	}
}

// TestAStopTakesTheLeaseAndTheWorkerLetsGo: stop on the factory puts the job
// back to backlog at once, and the worker's runner stops when it learns the
// lease is gone.
func TestAStopTakesTheLeaseAndTheWorkerLetsGo(t *testing.T) {
	g := newRig(t)
	id := g.add(t, check("sleep 30"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateRunning)
	for deadline := time.Now().Add(5 * time.Second); !g.run.Running(id); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the worker never ran the job")
		}
	}
	if err := g.floor.Stop(id); err != nil {
		t.Fatal(err)
	}
	if it, _ := g.st.Get(id); it.State != factory.StateNew {
		t.Fatalf("a stopped job is %s", it.State)
	}
	for deadline := time.Now().Add(5 * time.Second); g.run.Running(id); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the worker kept running a stopped job")
		}
	}
}

// TestAPauseAndResumeReachTheRunner: hold on the factory pauses the worker's
// runner through the lease, and hold off lets it go on to the end.
func TestAPauseAndResumeReachTheRunner(t *testing.T) {
	g := newRig(t)
	id := g.add(t, check("sleep 0.3"), factory.Stage{Name: "again", Kind: factory.StageCheck, Ask: "true", On: true})
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateRunning)
	if err := g.floor.Hold(id, true); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if it, _ := g.st.Get(id); logHas(it, "paused") > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the worker's runner never said paused")
		}
	}
	if err := g.floor.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateShipped)
}
