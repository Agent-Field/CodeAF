package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

// The desk's real wire, factory and runner share only the public job doors.
// A held fake model round makes the person's toggle precede the step boundary.
func TestFactoryStopAfterStep(t *testing.T) {
	for _, leased := range []bool{false, true} {
		for _, choice := range []string{"yes", "off", "no", "last"} {
			t.Run(fmt.Sprintf("leased=%v/%s", leased, choice), func(t *testing.T) {
				st, err := store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				mirror := st
				if leased {
					mirror, err = store.Open(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
				}
				entered, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int64
				holdAt := int64(1)
				if choice == "last" {
					holdAt = 3
				}
				runner := factoryrun.New(factoryrun.Options{Store: mirror, Exec: map[factory.StageKind]factoryrun.Executor{
					factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, j factoryrun.Job) (factory.StageResult, error) {
						if calls.Add(1) == holdAt {
							close(entered)
							select {
							case <-release:
							case <-ctx.Done():
								return factory.StageResult{}, ctx.Err()
							}
						}
						return factory.StageResult{Done: true, Output: "done", Spent: .25}, nil
					}),
				}})
				var doors factory.RunnerDoors = runner
				if leased {
					desk, e := lease.Open(st, lease.Options{})
					if e != nil {
						t.Fatal(e)
					}
					doors = lease.NewDoors(desk)
				}
				seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(doors))
				seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
				history, err := record.Open(st, record.Options{})
				if err != nil {
					t.Fatal(err)
				}
				handler := serve.New(seam, serve.Config{Store: st, Record: history, PullWait: 10 * time.Millisecond})
				srv := httptest.NewServer(handler)
				t.Cleanup(func() { srv.Close(); handler.Close() })
				hub := testFactoryHub(t)
				id := appFactoryID(srv.URL)
				attached(t, hub, appFactoryRow{ID: id, URL: srv.URL, Mode: api.FactorySolo})
				wire := newFactoryWire(t, hub)
				if leased {
					ctx, cancel := context.WithCancel(context.Background())
					done := make(chan struct{})
					go func() {
						defer close(done)
						worker.Run(ctx, worker.Options{
							Dial:   func(int) *client.WorkerClient { return client.Open(srv.URL, "").Worker("") },
							Attach: api.WorkerAttach{Machine: "fixture", Name: "fixture", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
							Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: 5 * time.Millisecond, BeatEvery: time.Hour,
						})
					}()
					t.Cleanup(func() { cancel(); <-done })
				}
				job, err := st.Create(factory.Item{Repo: "acme/api", Title: "Three steps", Cap: 5, Stages: []factory.Stage{
					{Name: "plan", Kind: factory.StageChat, On: true, Until: factory.UntilDone},
					{Name: "write", Kind: factory.StageChat, On: true, Until: factory.UntilDone},
					{Name: "check", Kind: factory.StageChat, On: true, Until: factory.UntilDone},
				}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runner.Stop(job.ID) })
				read := func() map[string]any {
					var out map[string]any
					wire.must("factory.job", map[string]any{"factory": id, "job": job.ID}, &out)
					return out
				}
				act := func(method string, more map[string]any) appwire.FactoryJobActed {
					more["factory"], more["job"] = id, job.ID
					var out appwire.FactoryJobActed
					wire.must(method, more, &out)
					return out
				}
				act("factory.job_start", map[string]any{"expect": map[string]any{"version": read()["version"]}})
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("model did not start")
				}
				toggle := func(on bool) { act("factory.job_stop_after_step", map[string]any{"on": on}) }
				toggle(true)
				got := read()
				if got["stop_after_step"] != true {
					t.Fatalf("toggle not readable: %v", got)
				}
				if choice == "off" {
					toggle(false)
				}
				close(release)
				if choice != "off" {
					eventually(t, "spend stop question", func() bool { return read()["state"] == "needs_you" })
					raw, _ := json.Marshal(read())
					step, spent := "plan", "0.25"
					if choice == "last" {
						step, spent = "check", "0.75"
					}
					want := "Stopped spending after " + step + " as you asked · $" + spent + " so far. Carry on?"
					if !strings.Contains(string(raw), want) || !strings.Contains(string(raw), "spend_stopped") {
						t.Fatalf("question: %s", raw)
					}
					time.Sleep(50 * time.Millisecond)
					if calls.Load() != holdAt {
						t.Fatalf("model crossed boundary: %d calls", calls.Load())
					}
					if err := client.Open(srv.URL, "").Seam().Answer(job.ID, choice != "no", ""); err != nil {
						t.Fatal(err)
					}
				}
				eventually(t, "run ends", func() bool { return !runner.Running(job.ID) })
				wantCalls := int64(3)
				if choice == "no" {
					wantCalls = 1
				}
				if calls.Load() != wantCalls {
					t.Fatalf("calls=%d want=%d", calls.Load(), wantCalls)
				}
				eventually(t, "final result", func() bool { j := read(); return j["state"] != "running" && j["state"] != "needs_you" })
				if read()["stop_after_step"] == true {
					t.Fatal("flag survived the answer or off")
				}
			})
		}
	}
}
