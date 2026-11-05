package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// TestAdmissionStall measures the real loop at its HTTP and executor boundaries.
// The executor's first completion requests the next paid round; no provider runs.
func TestAdmissionStall(t *testing.T) {
	for _, mode := range []string{"admission", "admission-stop", "periodic"} {
		t.Run(mode, func(t *testing.T) {
			mirror, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			item, err := mirror.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew, Autonomy: factory.AutonomyAuto, Stages: []factory.Stage{{Name: "first", Kind: factory.StageCheck, On: true}, {Name: "second", Kind: factory.StageCheck, On: true}}})
			if err != nil {
				t.Fatal(err)
			}
			entered, advance, next := make(chan time.Time, 1), make(chan struct{}), make(chan time.Time, 1)
			runner := run.New(run.Options{Store: mirror, RepoDir: func(string) string { return mirror.Root() }, Exec: map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
				if j.Stage.Name == "first" {
					entered <- time.Now()
					select {
					case <-advance:
						return factory.StageResult{Done: true, Spent: .1}, nil
					case <-ctx.Done():
						return factory.StageResult{}, ctx.Err()
					}
				}
				next <- time.Now()
				<-ctx.Done()
				return factory.StageResult{}, ctx.Err()
			})}})
			var armed, stalled, queued atomic.Bool
			stall := make(chan time.Time, 1)
			var mu sync.Mutex
			var beats []time.Time
			var queuedAt, deliveredAt time.Time
			observeModel := func() {
				if cur, err := mirror.Get(item.ID); err == nil && cur.Model == "chosen" {
					mu.Lock()
					if deliveredAt.IsZero() {
						deliveredAt = time.Now()
					}
					mu.Unlock()
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if strings.HasSuffix(r.URL.Path, "/report") && armed.Load() && stalled.CompareAndSwap(false, true) {
					stall <- time.Now()
					<-r.Context().Done()
					return
				}
				if strings.HasSuffix(r.URL.Path, "/heartbeat") {
					mu.Lock()
					beats = append(beats, time.Now())
					mu.Unlock()
					hb := api.WorkerHeartbeat{}
					if queued.Load() {
						hb.Signals = []api.WorkerSignal{{Verb: "model", Words: "chosen", Delivery: "lease/model"}}
						if mode != "admission" {
							hb.Signals = append(hb.Signals, api.WorkerSignal{Verb: "stop", Delivery: "lease/stop"})
						}
					}
					_ = json.NewEncoder(w).Encode(hb)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			done := make(chan struct{})
			reportEvery := time.Hour
			if mode == "periodic" {
				reportEvery = 300 * time.Millisecond
			}
			go func() {
				defer close(done)
				Options{cap: &capacity{jobs: map[int]chan struct{}{}}, Runner: runner, Mirror: mirror, BeatEvery: 100 * time.Millisecond, ReportEvery: reportEvery, DeliveryRoot: t.TempDir(), LocalLease: func(_ string, _ api.WorkerLease) { observeModel() }}.work(ctx, client.Open(server.URL, "").Worker("worker"), api.WorkerPull{Lease: api.WorkerLease{ID: "lease_test", Job: item.ID}, Job: api.FromItem(item)})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("first executor did not start")
			}
			// Observe renewals before introducing the report stall.
			time.Sleep(220 * time.Millisecond)
			requested := time.Now()
			armed.Store(true)
			if strings.HasPrefix(mode, "admission") {
				close(advance)
			} else {
				if err := mirror.Update(item.ID, func(it *factory.Item) error { it.Stream.Spent = .1; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			var stallAt time.Time
			select {
			case stallAt = <-stall:
			case <-ctx.Done():
				t.Fatal("report did not stall")
			}
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			queuedAt = time.Now()
			mu.Unlock()
			queued.Store(true)
			var outcomeAt time.Time
			outcome := "refused"
			for outcomeAt.IsZero() {
				observeModel()
				select {
				case outcomeAt = <-next:
					outcome = "started"
				default:
				}
				if !runner.Running(item.ID) {
					outcomeAt = time.Now()
				}
				if ctx.Err() != nil {
					t.Fatal("runner did not stop")
				}
				time.Sleep(time.Millisecond)
			}
			for runner.Running(item.ID) && ctx.Err() == nil {
				time.Sleep(time.Millisecond)
			}
			stoppedAt := time.Now()
			mu.Lock()
			longest := time.Duration(0)
			for i := 1; i < len(beats); i++ {
				if gap := beats[i].Sub(beats[i-1]); gap > longest {
					longest = gap
				}
			}
			deliveryDelay := deliveredAt.Sub(queuedAt)
			stopDelay := stoppedAt.Sub(queuedAt)
			if mode == "admission" {
				stopDelay = 0
			}
			mu.Unlock()
			admissionDelay := outcomeAt.Sub(requested)
			if mode == "periodic" {
				admissionDelay, outcome = 0, "already_running"
			}
			t.Logf("mode=%s admission_seconds=%.6f outcome=%s heartbeat_gap_seconds=%.6f stop_seconds=%.6f other_delivery_seconds=%.6f report_stall_seconds=%.6f heartbeat_period_seconds=0.100000 request_timeout_seconds=5", mode, admissionDelay.Seconds(), outcome, longest.Seconds(), stopDelay.Seconds(), deliveryDelay.Seconds(), stoppedAt.Sub(stallAt).Seconds())
			if longest >= time.Second {
				t.Errorf("heartbeat gap %s, want under 1s", longest)
			}
			if deliveryDelay < 0 || deliveryDelay >= time.Second {
				t.Errorf("other signal delivery %s, want under 1s", deliveryDelay)
			}
			if mode != "admission" && stopDelay >= time.Second {
				t.Errorf("Stop delivery %s, want under 1s", stopDelay)
			}
			if strings.HasPrefix(mode, "admission") && outcome != "refused" {
				t.Errorf("step %s, want refused", outcome)
			}
			if mode == "admission" && (outcomeAt.Sub(stallAt) < deliveryRequestTimeout || admissionDelay > deliveryRequestTimeout+time.Second) {
				t.Errorf("admission refused after %s (stall %s), want report timeout %s through 6s", admissionDelay, outcomeAt.Sub(stallAt), deliveryRequestTimeout)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("worker reporter did not end within 1s of cancellation")
			}
		})
	}
}

// A reply can finish while the final snapshot is in flight. Done must wait
// for its spending and delivery receipt too, without holding up heartbeats.
func TestFinalReportIncludesReplyDeliveredDuringFlush(t *testing.T) {
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := mirror.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew, Autonomy: factory.AutonomyAuto, Stages: []factory.Stage{{Name: "check", Kind: factory.StageCheck, On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	var finished atomic.Bool
	runner := run.New(run.Options{Store: mirror, RepoDir: func(string) string { return mirror.Root() }, Exec: map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
		finished.Store(true)
		return factory.StageResult{Done: true}, nil
	})}})
	var stalled atomic.Bool
	replyDone, release := make(chan struct{}), make(chan struct{})
	var spending atomic.Int64
	doneSpend := make(chan int64, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/report"):
			var report api.WorkerReport
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
				t.Error(err)
			}
			if finished.Load() && stalled.CompareAndSwap(false, true) {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			spending.Add(int64(report.Cost * 100))
		case strings.HasSuffix(r.URL.Path, "/heartbeat"):
			hb := api.WorkerHeartbeat{}
			if stalled.Load() {
				hb.Signals = []api.WorkerSignal{{Verb: "reply", ID: "reply", Words: "hello", Delivery: "lease/reply"}}
			}
			_ = json.NewEncoder(w).Encode(hb)
			return
		case strings.HasSuffix(r.URL.Path, "/done"):
			doneSpend <- spending.Load()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		Options{cap: &capacity{jobs: map[int]chan struct{}{}}, Runner: runner, Mirror: mirror, BeatEvery: 10 * time.Millisecond, ReportEvery: time.Hour, DeliveryRoot: t.TempDir(), Reply: func(_ context.Context, _ factory.Item, _ string) error {
			err := mirror.Update(item.ID, func(it *factory.Item) error { it.Stream.Spent = .2; return nil })
			close(replyDone)
			return err
		}}.work(ctx, client.Open(server.URL, "").Worker("worker"), api.WorkerPull{Lease: api.WorkerLease{ID: "lease_test", Job: item.ID}, Job: api.FromItem(item)})
	}()
	select {
	case <-replyDone:
	case <-ctx.Done():
		t.Fatal("reply did not finish during final reporting")
	}
	// Let the reply return and the worker observe its completion before the
	// older snapshot is acknowledged.
	time.Sleep(30 * time.Millisecond)
	close(release)
	select {
	case got := <-doneSpend:
		if got != 20 {
			t.Errorf("Done after reporting %d cents, want 20 from the concurrent reply", got)
		}
	case <-ctx.Done():
		t.Error("worker did not release lease")
	}
	cancel()
	<-done
}
