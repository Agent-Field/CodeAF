package serve_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

func TestWorkerReportsStepBeginBeforePaidExecutor(t *testing.T) {
	f := workerSetupAs(t, "owner")
	attached := f.attach(t, "owner")
	it := f.add(t, "owner")
	if err := f.st.Update(it.ID, func(it *factory.Item) error { it.Cap, it.SpentBefore = 1, .875; return nil }); err != nil {
		t.Fatal(err)
	}
	boundary := make(chan struct{}, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
			var report api.WorkerReport
			if json.Unmarshal(data, &report) == nil {
				for _, step := range report.Steps {
					if step.Name == "proof" && step.State == api.StepRunning {
						if err := f.st.Update(it.ID, func(it *factory.Item) error { it.SpentBefore = 1; return nil }); err != nil {
							t.Error(err)
						}
						select {
						case boundary <- struct{}{}:
						default:
						}
					}
				}
			}
		}
		f.handler.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paid := make(chan struct{}, 1)
	runner := run.New(run.Options{Store: mirror, Exec: map[factory.StageKind]run.Executor{
		factory.StageChat: run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
			paid <- struct{}{}
			return factory.StageResult{Done: true}, nil
		}),
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx, worker.Options{
			Dial: func(int) *client.WorkerClient {
				return client.Open(proxy.URL, config.AccessToken).Worker(attached.Credential)
			},
			Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: time.Millisecond, BeatEvery: time.Millisecond,
		})
	}()
	defer func() { cancel(); <-done }()
	select {
	case <-paid:
		t.Fatal("paid executor began before the server authorized its step begin")
	case <-boundary:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not report step begin")
	}
	select {
	case <-paid:
		t.Fatal("paid executor began after the server reached its lifetime cap")
	case <-time.After(100 * time.Millisecond):
	}
}
