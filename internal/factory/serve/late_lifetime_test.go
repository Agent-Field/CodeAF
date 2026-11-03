package serve_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	factoryworker "github.com/Agent-Field/codeaf/internal/factory/worker"
)

func TestLateArchivedUsageStopsAdmittedWorker(t *testing.T) {
	f, it, worker, _, body := retainedNoChangeSetup(t)
	path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
	var recovered api.WorkerRecovered
	f.request(t, "POST", path, worker.Credential, body, 200, &recovered)
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State, it.Cap = factory.StateNew, 1
		it.PullRequest, it.FollowUp = nil, nil
		it.Publish, it.HeadCommit = "", ""
		it.Stages = []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true, Until: factory.UntilDone, Max: 1}}
		return run.QueueItem(it, now.Add(time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	replacement := f.pull(t, worker)
	mirrorItem := api.ToItem(replacement.Job)
	if got := factory.JobLifetimeSpent(mirrorItem); got != .875 {
		t.Fatalf("admission lifetime = %g, want .875", got)
	}
	// A receipt from the cleared run arrives after the replacement is admitted.
	body.AdditionalCost = .25
	f.request(t, "POST", path, worker.Credential, body, 200, &recovered)
	saved, err := f.st.Get(it.ID)
	if err != nil || factory.JobLifetimeSpent(saved) != 1 {
		t.Fatalf("late receipt lifetime = %g, err %v", factory.JobLifetimeSpent(saved), err)
	}
	var beat api.WorkerHeartbeat
	f.request(t, "POST", fmt.Sprintf("/v1/workers/leases/%s/heartbeat", replacement.Lease.ID), worker.Credential, struct{}{}, 200, &beat)
	if len(beat.Signals) != 0 {
		t.Fatalf("unexpected heartbeat controls: %+v", beat.Signals)
	}
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// This is the same queued-to-new transition worker.stand performs.
	mirrorItem.State = factory.StateNew
	if err := mirror.Save(mirrorItem); err != nil {
		t.Fatal(err)
	}
	paid := make(chan struct{}, 1)
	runner := run.New(run.Options{Store: mirror, Exec: map[factory.StageKind]run.Executor{
		factory.StageChat: run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
			paid <- struct{}{}
			return factory.StageResult{Done: true, Spent: .125}, nil
		}),
	}})
	w := client.Open(f.server.URL, config.AccessToken).Worker(worker.Credential)
	runner.SetStepBegin(it.ID, func(ctx context.Context, step factory.StageReport) error {
		return w.Report(ctx, replacement.Lease.ID, api.FromRunReport(factory.RunReport{Stages: []factory.StageReport{step}}))
	})
	t.Cleanup(func() { _ = runner.Stop(it.ID) })
	if err := runner.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-paid:
		t.Fatalf("paid executor began after late recovery: authoritative lifetime=%g cap=%g; admitted mirror lifetime=%g; heartbeat signals=%d", factory.JobLifetimeSpent(saved), saved.Cap, factory.JobLifetimeSpent(mirrorItem), len(beat.Signals))
	case <-time.After(time.Second):
	}
}

func TestLateArchivedUsageStopsActualWorker(t *testing.T) {
	f, it, attached, _, body := retainedNoChangeSetup(t)
	path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
	var recovered api.WorkerRecovered
	f.request(t, "POST", path, attached.Credential, body, 200, &recovered)
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State, it.Cap = factory.StateNew, 1
		it.PullRequest, it.FollowUp = nil, nil
		it.Publish, it.HeadCommit = "", ""
		it.Stages = []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true, Until: factory.UntilDone, Max: 1}}
		return run.QueueItem(it, now.Add(time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	admitted, deliver := make(chan struct{}), make(chan struct{})
	var stalled atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, r)
		if r.URL.Path == "/v1/workers/pull" && response.Code == 200 && stalled.CompareAndSwap(false, true) {
			close(admitted)
			select {
			case <-deliver:
			case <-r.Context().Done():
				return
			}
		}
		for key, values := range response.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	}))
	t.Cleanup(proxy.Close)
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paid := make(chan factory.Item, 1)
	runner := run.New(run.Options{Store: mirror, Exec: map[factory.StageKind]run.Executor{
		factory.StageChat: run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
			saved, _ := f.st.Get(it.ID)
			paid <- saved
			return factory.StageResult{Done: true, Spent: .125}, nil
		}),
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		factoryworker.Run(ctx, factoryworker.Options{
			Dial: func(int) *client.WorkerClient {
				return client.Open(proxy.URL, config.AccessToken).Worker(attached.Credential)
			},
			Runner: runner, Mirror: mirror, Slots: 1,
			ReportEvery: time.Millisecond, BeatEvery: time.Millisecond,
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-admitted:
	case <-time.After(3 * time.Second):
		t.Fatal("replacement worker never pulled")
	}
	body.AdditionalCost = .25
	f.request(t, "POST", path, attached.Credential, body, 200, &recovered)
	if recovered.Job.LifetimeCost != 1 {
		t.Fatalf("late receipt lifetime=%g, want 1", recovered.Job.LifetimeCost)
	}
	close(deliver)
	select {
	case saved := <-paid:
		t.Fatalf("actual worker began paid executor with authoritative lifetime=%g cap=%g after recovery and accounting anchor", factory.JobLifetimeSpent(saved), saved.Cap)
	case <-time.After(2 * time.Second):
	}
}
