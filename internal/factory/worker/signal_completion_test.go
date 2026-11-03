package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFinishedRunnerRetriesFailedSignalBeforeReleasingLease(t *testing.T) {
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := mirror.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew, Stages: []factory.Stage{{Name: "check", Kind: factory.StageCheck, On: true, Ask: "true"}}})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	runner := run.New(run.Options{Store: mirror, RepoDir: func(string) string { return t.TempDir() }, Exec: map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(ctx context.Context, _ run.Job) (factory.StageResult, error) {
		close(entered)
		<-ctx.Done()
		return factory.StageResult{}, ctx.Err()
	})}})
	lockPath := filepath.Join(mirror.Root(), fmt.Sprintf("%d.lock", item.ID))
	released := make(chan string, 1)
	beats := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/heartbeat") {
			beats++
			if beats == 1 {
				<-entered
				if err := runner.Stop(item.ID); err != nil {
					t.Error(err)
				}
				waitSignalRunner(t, runner, item.ID)
				// Break writes while leaving the completed job readable by final reporting.
				if err := os.Rename(lockPath, lockPath+".saved"); err != nil {
					t.Error(err)
				}
				if err := os.Mkdir(lockPath, 0700); err != nil {
					t.Error(err)
				}
			} else if beats == 2 {
				if err := os.Remove(lockPath); err != nil {
					t.Error(err)
				}
				if err := os.Rename(lockPath+".saved", lockPath); err != nil {
					t.Error(err)
				}
			}
			_ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{Signals: []api.WorkerSignal{{Verb: "model", Words: "chosen", Delivery: "lease/model"}}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/done") {
			it, err := mirror.Get(item.ID)
			if err != nil {
				t.Error(err)
			}
			released <- it.Model
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		Options{cap: &capacity{jobs: map[int]chan struct{}{}}, Runner: runner, Mirror: mirror, BeatEvery: 10 * time.Millisecond, ReportEvery: time.Hour, DeliveryRoot: t.TempDir()}.work(ctx, client.Open(server.URL, "").Worker("worker"), api.WorkerPull{Lease: api.WorkerLease{ID: "lease_test", Job: item.ID}, Job: api.FromItem(item)})
	}()
	select {
	case model := <-released:
		if model != "chosen" {
			t.Errorf("released lease before failed model retried: model=%q", model)
		}
	case <-ctx.Done():
		t.Error("worker did not finish after retry")
	}
	cancel()
	<-done
}

func TestManagerRetriesFailedSignalBeforeReleasingLease(t *testing.T) {
	var beats int
	var j *job
	released := make(chan string, 1)
	j, _ = deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lockPath := filepath.Join(j.o.Mirror.Root(), fmt.Sprintf("%d.lock", j.id))
		if strings.HasSuffix(r.URL.Path, "/heartbeat") {
			beats++
			if beats == 1 {
				if err := os.Rename(lockPath, lockPath+".saved"); err != nil {
					t.Error(err)
				}
				if err := os.Mkdir(lockPath, 0700); err != nil {
					t.Error(err)
				}
			} else if beats == 2 {
				if err := os.Remove(lockPath); err != nil {
					t.Error(err)
				}
				if err := os.Rename(lockPath+".saved", lockPath); err != nil {
					t.Error(err)
				}
			}
			_ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{Signals: []api.WorkerSignal{{Verb: "model", Words: "chosen", Delivery: "lease/model"}}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/done") {
			it, err := j.o.Mirror.Get(j.id)
			if err != nil {
				t.Error(err)
			}
			released <- it.Model
		}
		w.WriteHeader(http.StatusOK)
	}))
	// A completed manager turn makes the thread-only worker eligible to finish.
	j.accepted["message"] = true
	j.o.BeatEvery = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	j.converse(ctx)
	select {
	case model := <-released:
		if model != "chosen" {
			t.Fatalf("manager released lease before failed model retried: model=%q", model)
		}
	default:
		t.Fatal("manager did not release after retry")
	}
}
