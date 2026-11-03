package lease_test

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestReviewStopRevokedLeaseCannotAdmitNextStep(t *testing.T) {
	testRevokedLeaseBoundary(t, false)
}

func TestAuthority503AfterPersonStopCannotAdmitNextStep(t *testing.T) {
	testRevokedLeaseBoundary(t, true)
}

func testRevokedLeaseBoundary(t *testing.T, inject503 bool) {
	f := setup(t)
	stages := []factory.Stage{
		{Name: "first", Kind: factory.StageChat, On: true, Ask: "first", Until: "done"},
		{Name: "second", Kind: factory.StageChat, On: true, Ask: "second", Until: "done"},
	}
	it := f.add(t, factory.Item{State: factory.StateQueued, Assignee: "sam", Stages: stages})
	held, err := f.desk.Pull(worker("w1", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.store.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.State = factory.StateNew
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := mirror.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	entered, release, second := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	var lost atomic.Int32
	var faultArmed, faultApplied atomic.Bool
	r := run.New(run.Options{Store: mirror, Benches: 1, Exec: map[factory.StageKind]run.Executor{
		factory.StageChat: run.ExecutorFunc(func(ctx context.Context, job run.Job) (factory.StageResult, error) {
			if job.Stage.Name == "first" {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return factory.StageResult{}, ctx.Err()
				}
			} else {
				second <- struct{}{}
			}
			return factory.StageResult{Done: true, Output: "done", Spent: .01}, nil
		}),
	}})
	t.Cleanup(func() { _ = r.Stop(it.ID) })
	// Use the worker's HTTP adapter: the server's ErrNoLease becomes the
	// client's typed ErrLeaseGone before the runner sees the boundary read.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if faultArmed.CompareAndSwap(true, false) {
			faultApplied.Store(true)
			http.Error(w, "authority unavailable", http.StatusServiceUnavailable)
			return
		}
		_, err := f.desk.Renew(held.ID, held.Worker)
		if errors.Is(err, lease.ErrNoLease) {
			lost.Add(1)
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"on":false}`))
	}))
	defer server.Close()
	workerClient := client.Open(server.URL, "").Worker("credential")
	r.SetStopAfterStepReader(it.ID, func(ctx context.Context) (bool, error) {
		return workerClient.StopAfterStep(ctx, held.ID)
	})
	if err := r.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first step never started")
	}
	if err := lease.NewDoors(f.desk).Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	stopped, err := f.store.Get(it.ID)
	if err != nil || !factory.StoppedByPerson(stopped) {
		t.Fatalf("authoritative Stop missing: %+v %v", stopped, err)
	}
	faultArmed.Store(inject503)
	close(release)
	select {
	case <-second:
		t.Fatalf("second paid step ran after authoritative Stop; boundary had observed ErrNoLease %d times", lost.Load())
	case <-time.After(2 * time.Second):
		if lost.Load() == 0 {
			t.Fatal("runner did not check the revoked lease")
		}
	}
	for deadline := time.Now().Add(2 * time.Second); r.Running(it.ID); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("revoked runner did not settle")
		}
	}
	select {
	case <-second:
		t.Fatal("second paid step ran during revoked runner settlement")
	default:
	}

	if inject503 && !faultApplied.Load() {
		t.Fatal("authority did not return the injected 503 after Stop")
	}

}
