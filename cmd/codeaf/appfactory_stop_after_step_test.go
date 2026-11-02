package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	t.Run("spending answer guards", testSpendAnswerGuards)
	t.Run("unconfirmed answer is sent once", testSpendAnswerUncertain)
	t.Run("saved questions", testSavedSpendingQuestions)
	t.Run("unfinished step resumes", testUnfinishedSpendingStep)
	t.Run("owner and guard", testSpendingStopOwnerAndGuard)
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
				if !strings.Contains(wire.hello, "factory.job_stop_after_step") || !strings.Contains(wire.hello, "factory.job_spend_answer") {
					t.Fatal("hello omits spending hold")
				}
				if leased {
					ctx, cancel := context.WithCancel(context.Background())
					done := make(chan struct{})
					go func() {
						defer close(done)
						worker.Run(ctx, worker.Options{
							Dial:   func(int) *client.WorkerClient { return client.Open(srv.URL, "").Worker("") },
							Attach: api.WorkerAttach{Machine: "fixture", Name: "fixture", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
							Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: 5 * time.Millisecond, BeatEvery: 5 * time.Millisecond,
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
				if can := read()["can"].(map[string]any); can["stop_after_step"] != true {
					t.Fatalf("running toggle unavailable: %v", can)
				}
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
					if read()["can"].(map[string]any)["spend_answer"] != true {
						t.Fatal("spending answer unavailable")
					}
					out := act("factory.job_spend_answer", map[string]any{"carry_on": choice != "no", "expect": map[string]any{"version": read()["version"]}})
					if out.Action != "spend_answer" {
						t.Fatalf("answer receipt: %+v", out)
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
				eventually(t, "final result", func() bool {
					j := read()
					if choice == "no" {
						return j["failure"] != nil
					}
					ny, _ := j["needs_you"].(map[string]any)
					return ny["kind"] == "approve_proof" || j["state"] == "done"
				})
				if choice == "no" {
					f, _ := read()["failure"].(map[string]any)
					if f["code"] != "stopped" {
						t.Fatalf("no must be a person stop: %v", f)
					}
				}
				if read()["stop_after_step"] == true {
					t.Fatal("flag survived the answer or off")
				}
				if choice != "off" {
					events, err := history.Since(context.Background(), time.Time{})
					if err != nil {
						t.Fatal(err)
					}
					answered := 0
					for _, event := range events {
						if event.Job == job.ID && event.Kind == record.Answered {
							answered++
						}
					}
					if answered != 1 {
						t.Fatalf("answered events=%d want=1", answered)
					}
					questions, _ := read()["questions"].([]any)
					want := "yes"
					if choice == "no" {
						want = "no"
					}
					found := false
					for _, question := range questions {
						q, _ := question.(map[string]any)
						a, _ := q["answer"].(map[string]any)
						if q["kind"] == "spend_stopped" && a["text"] == want {
							found = true
						}
					}
					if !found {
						t.Fatalf("saved %s spending answer missing: %v", want, questions)
					}
				}
			})
		}
	}
}

func savedSpendingRunner(t *testing.T, it factory.Item, exec factoryrun.Executor) (*factoryrun.Runner, *store.Store, int) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	r := factoryrun.New(factoryrun.Options{Store: st, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: exec}})
	t.Cleanup(func() { _ = r.Stop(job.ID) })
	return r, st, job.ID
}

func savedSpendingItem() factory.Item {
	return factory.Item{Repo: "acme/api", Title: "Saved run", Cap: 5, StopAfterStep: true,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Cur: 0, Spent: .25, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true}},
			{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
		}},
	}
}

func testSavedSpendingQuestions(t *testing.T) {
	for _, yes := range []bool{true, false} {
		t.Run(fmt.Sprint(yes), func(t *testing.T) {
			it := savedSpendingItem()
			it.State, it.QKind, it.Question = factory.StateNeedsYou, "spend_stopped", "Stopped spending after plan as you asked · $0.25 so far. Carry on?"
			var calls atomic.Int64
			r, st, id := savedSpendingRunner(t, it, factoryrun.ExecutorFunc(func(context.Context, factoryrun.Job) (factory.StageResult, error) {
				calls.Add(1)
				return factory.StageResult{Done: true}, nil
			}))
			if err := r.Answer(id, yes, ""); err != nil {
				t.Fatalf("saved spending answer: %v", err)
			}
			eventually(t, "saved answer settles", func() bool { return !r.Running(id) })
			got, err := st.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if yes {
				want = 1
			}
			if calls.Load() != want || got.StopAfterStep {
				t.Fatalf("saved answer calls=%d flag=%v", calls.Load(), got.StopAfterStep)
			}
			if !yes && (got.Failure == nil || got.Failure.Code != factory.CodeStopped) {
				t.Fatalf("saved no is not a person stop: %+v", got.Failure)
			}
		})
	}
}

func testUnfinishedSpendingStep(t *testing.T) {
	it := savedSpendingItem()
	it.State = factory.StateRunning
	it.Stream.Cur, it.Stream.Paused = 1, true
	it.Stream.Phases[1].State, it.Stream.Phases[1].Round = factory.PhaseRunning, 1
	entered, release := make(chan struct{}), make(chan struct{})
	r, st, id := savedSpendingRunner(t, it, factoryrun.ExecutorFunc(func(ctx context.Context, j factoryrun.Job) (factory.StageResult, error) {
		if j.Stage.Name != "write" {
			return factory.StageResult{}, fmt.Errorf("resumed wrong step %s", j.Stage.Name)
		}
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return factory.StageResult{}, ctx.Err()
		}
		return factory.StageResult{Done: true, Spent: .25}, nil
	}))
	if err := r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("spending hold displaced the unfinished write step")
	}
	close(release)
	eventually(t, "hold follows resumed write", func() bool {
		got, _ := st.Get(id)
		return got.QKind == "spend_stopped" && strings.Contains(got.Question, "after write")
	})
	if err := r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "resumed run settles", func() bool { return !r.Running(id) })
}

func testSpendingStopOwnerAndGuard(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	it := f.create(t, factory.Item{State: factory.StateQueued, Assignee: "alice"})
	method := "factory.job_stop_after_step"
	if !alice.job(t, it.ID).Can.StopAfterStep || bob.job(t, it.ID).Can.StopAfterStep {
		t.Fatal("toggle does not belong to owner")
	}
	alice.w.mustFail(method, map[string]any{"factory": alice.conn.ID, "job": it.ID}, appwire.CodeBadParams)
	bob.refused(t, method, bob.params(t, it.ID, map[string]any{"on": true}), appwire.CodeForbidden)
	stale := alice.params(t, it.ID, map[string]any{"on": false})
	out := alice.act(t, method, it.ID, map[string]any{"on": true})
	if out.Action != "stop_after_step" || !alice.job(t, it.ID).StopAfterStep {
		t.Fatalf("toggle reply: %+v", out)
	}
	alice.w.mustFail(method, stale, appwire.CodeStale)
	events, err := f.rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "stop_after_step_changed" && e.Handle == "alice" && e.Detail == "stop_after_step=true" {
			found = true
		}
	}
	if !found {
		t.Fatal("record omits the person's spending choice")
	}
}

func testSpendAnswerGuards(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	method := "factory.job_spend_answer"
	for _, kind := range []string{"", "cap", factory.QKindAskFirst, factory.QKindSpendStopped} {
		it := savedSpendingItem()
		it.Assignee, it.QKind, it.Question = "alice", kind, "Carry on?"
		it.State = factory.StateNeedsYou
		if kind == "" {
			it.State, it.Question = factory.StateRunning, ""
		}
		job := f.create(t, it)
		was := f.hold(t, job.ID)
		can := alice.job(t, job.ID).Can
		raw, _ := json.Marshal(can)
		var flags map[string]any
		_ = json.Unmarshal(raw, &flags)
		if flags["spend_answer"] != (kind == factory.QKindSpendStopped) {
			t.Fatalf("can for %q: %s", kind, raw)
		}
		for _, value := range []any{nil, "yes", 1} {
			params := alice.params(t, job.ID, map[string]any{"carry_on": value})
			if value == nil {
				delete(params, "carry_on")
			}
			alice.w.mustFail(method, params, appwire.CodeBadParams)
		}
		alice.w.mustFail(method, map[string]any{"factory": alice.conn.ID, "job": job.ID, "carry_on": true}, appwire.CodeBadParams)
		if kind != factory.QKindSpendStopped {
			alice.refused(t, method, alice.params(t, job.ID, map[string]any{"carry_on": false}), appwire.CodeConflict)
		} else {
			bob.refused(t, method, bob.params(t, job.ID, map[string]any{"carry_on": false}), appwire.CodeForbidden)
			var other map[string]any
			b, _ := json.Marshal(bob.job(t, job.ID).Can)
			_ = json.Unmarshal(b, &other)
			if other["spend_answer"] != false {
				t.Fatal("another owner can answer")
			}
			stale := alice.params(t, job.ID, map[string]any{"carry_on": true})
			stale["expect"] = map[string]any{"version": 99999}
			alice.w.mustFail(method, stale, appwire.CodeStale)
		}
		f.same(t, job.ID, was, "refused spending answer")
	}
}

func testSpendAnswerUncertain(t *testing.T) {
	var posts atomic.Int64
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			if r.URL.Path != "/v1/jobs/4/spend_answer" {
				t.Errorf("unexpected write: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"receipt unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"document_id":4,"version":7,"state":"needs_you","needs_you":{"kind":"spend_stopped"}}`))
	})
	hub := testFactoryHub(t)
	_, conn := attachStub(t, hub, stub, actionTestCredential)
	wire := newFactoryWire(t, hub)
	wire.mustFail("factory.job_spend_answer", map[string]any{"factory": conn.ID, "job": 4, "carry_on": true, "expect": map[string]any{"version": 7}}, appwire.CodeUncertain)
	if posts.Load() != 1 {
		t.Fatalf("unconfirmed writes=%d want=1", posts.Load())
	}
}
