package worker

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// proofWorker drives work itself, with real admission, report and receipt HTTP
// doors. The only replacement is a scripted executor; no provider is called.
func proofWorker(t *testing.T, exec run.Executor, wrap func(http.Handler) http.Handler, every time.Duration, clock ...func() time.Time) (*store.Store, *store.Store, *run.Runner, int, context.CancelFunc, <-chan struct{}, factory.Seam) {
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
	handler := serve.New(seam, serve.Config{Address: "http://test", Store: st, PullWait: time.Millisecond})
	server := httptest.NewServer(wrap(handler))
	t.Cleanup(func() { server.Close(); handler.Close() })
	it, err := st.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew, Autonomy: factory.AutonomyAuto, Assignee: "me", Cap: 10, Stages: []factory.Stage{{Name: "first", Kind: factory.StageCheck, On: true}, {Name: "second", Kind: factory.StageCheck, On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := seam.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	w := client.Open(server.URL, "").Worker("")
	if _, err := w.Attach(t.Context(), api.WorkerAttach{Machine: "proof", Name: "proof", Handle: "me", TakesUnassigned: map[string]bool{lease.EveryRepo: true}}); err != nil {
		t.Fatal(err)
	}
	pulled, ok, err := w.Pull(t.Context())
	if err != nil || !ok {
		t.Fatalf("pull: ok=%t err=%v", ok, err)
	}
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := run.New(run.Options{Store: mirror, RepoDir: func(string) string { return mirror.Root() }, Exec: map[factory.StageKind]run.Executor{factory.StageCheck: exec}})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	root := t.TempDir()
	var now func() time.Time
	if len(clock) > 0 {
		now = clock[0]
	}
	go func() {
		defer close(done)
		Options{cap: &capacity{jobs: map[int]chan struct{}{}}, Runner: runner, Mirror: mirror, BeatEvery: 20 * time.Millisecond, ReportEvery: every, DeliveryRoot: root, Now: now}.work(ctx, w, pulled)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(12 * time.Second):
			t.Error("work cleanup exceeded 12s")
		}
	})
	return st, mirror, runner, it.ID, cancel, done, seam
}

func awaitProof(t *testing.T, ch <-chan struct{}, bound time.Duration, why string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(bound):
		t.Fatal(why)
	}
}

func TestRefusedStepRetriesWhenFactoryAnswersAgain(t *testing.T) {
	var armed, stalled atomic.Bool
	refused := make(chan struct{})
	var rounds atomic.Int32
	st, mirror, runner, id, _, done, _ := proofWorker(t, run.ExecutorFunc(func(_ context.Context, j run.Job) (factory.StageResult, error) {
		rounds.Add(1)
		if j.Stage.Name == "first" {
			armed.Store(true)
		}
		return factory.StageResult{Done: true, Spent: .125}, nil
	}), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/report") && armed.Load() && stalled.CompareAndSwap(false, true) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
				close(refused)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, time.Hour)
	awaitProof(t, refused, 7*time.Second, "report did not refuse admission")
	visible, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if visible.State != factory.StateRunning || visible.Question != "" || visible.QKind != "" || visible.Failure != nil || visible.Stream.Spent != 0 || visible.Stream.Phases[0].State != factory.PhaseRunning || visible.Stream.Phases[0].Round != 1 || len(visible.Stream.Log) != 1 || visible.Stream.Log[0].Text != "queued" {
		t.Fatalf("factory stall view: state=%s question=%q stream=%+v", visible.State, visible.Question, visible.Stream)
	}
	t.Logf("factory stall view: running; first running round 1; cost $0; question empty; log queued")

	cur, err := mirror.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stall state=%s question=%q kind=%q log=%+v", cur.State, cur.Question, cur.QKind, cur.Stream.Log)
	if cur.State != factory.StateRunning || cur.Question != "" || cur.QKind != "" || cur.Failure != nil || len(cur.Stream.Log) != 1 || cur.Stream.Log[0].Text != "queued" {
		t.Errorf("stall parked or failed job: %+v", cur)
	}
	awaitProof(t, done, 9*time.Second, "refused step did not retry without a person")
	cur, err = st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if rounds.Load() != 2 || cur.Stream.Spent != .25 || cur.Stream.Phases[0].Round != 1 || cur.Stream.Phases[1].Round != 1 || cur.Question != "" || cur.Failure != nil || (cur.State != factory.StateShipped && cur.State != factory.StateLanded) || runner.Running(id) {
		t.Fatalf("retry lost spend/rounds or failed: calls=%d item=%+v stream=%+v", rounds.Load(), cur, cur.Stream)
	}
}

func TestAdmissionWaitHasATotalBound(t *testing.T) {
	entered, advance, periodic := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var stalls atomic.Int32
	var armed atomic.Bool
	var requested atomic.Int64
	var paid atomic.Int32
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	var mirror *store.Store
	var id int
	_, mirror, runner, id, cancel, _, _ := proofWorker(t, run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
		if j.Stage.Name == "first" {
			close(entered)
			select {
			case <-advance:
			case <-ctx.Done():
				return factory.StageResult{}, ctx.Err()
			}
			requested.Store(time.Now().UnixNano())
			return factory.StageResult{Done: true, Spent: .125}, nil
		}
		paid.Add(1)
		return factory.StageResult{Done: true}, nil
	}), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/report") && armed.Load() {
				n := stalls.Add(1)
				if n == 1 {
					_, _ = io.Copy(io.Discard, r.Body)
					close(periodic)
					<-r.Context().Done()
					return
				}
				// Read before stalling: a replay may bypass the decoder, but
				// request cancellation must still reach the server's context.
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
				if n <= 3 {
					select {
					case <-time.After(4900 * time.Millisecond):
					case <-r.Context().Done():
						return
					}
				}

				if n == 4 {
					<-r.Context().Done()
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}, 100*time.Millisecond, func() time.Time { return time.Unix(0, clock.Add(int64(3*time.Second))) })
	awaitProof(t, entered, time.Second, "first step did not start")
	if err := mirror.Update(id, func(it *factory.Item) error { it.Stream.Spent = .0625; return nil }); err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	awaitProof(t, periodic, time.Second, "periodic report did not stall")
	close(advance)
	deadline := time.Now().Add(23 * time.Second)
	for runner.Running(id) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	elapsed := time.Since(time.Unix(0, requested.Load()))
	t.Logf("total admission wait observed=%s limit=5.5s scheduling_allowance=0.5s stalled_requests=%d configured=periodic(5s)+pending_retry(4.9s)+fresh_report(4.9s)+admission(5s)", elapsed, stalls.Load())
	cancel()
	if elapsed > 6*time.Second || paid.Load() != 0 || stalls.Load() < 1 {
		t.Fatalf("total admission wait exceeded 6s: %s", elapsed)
	}
}

func TestReportGoroutineEndsWithoutLosingSpend(t *testing.T) {
	for _, mode := range []string{"cancel", "report-lease-loss", "heartbeat-lease-loss", "cancel-undelivered", "report-lease-loss-undelivered", "heartbeat-lease-loss-undelivered"} {
		t.Run(mode, func(t *testing.T) {
			entered, stalled := make(chan struct{}), make(chan struct{})
			var armed, applied atomic.Bool
			var reports atomic.Int32
			var reportKey string
			var mu sync.Mutex
			var deltas []float64
			var recovery atomic.Int32
			var st *store.Store
			var id int
			var doors factory.Seam
			st, _, _, id, cancel, done, doors := proofWorker(t, run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
				j.Spend(.125)
				close(entered)
				<-ctx.Done()
				j.Spend(.0625)
				return factory.StageResult{}, ctx.Err()
			}), func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/heartbeat") && applied.Load() && strings.HasPrefix(mode, "heartbeat-lease-loss") {
						if err := doors.Stop(id); err != nil {
							t.Error(err)
						}
						next.ServeHTTP(w, r)
						return
					}
					if strings.HasSuffix(r.URL.Path, "/report") && armed.Load() {
						n := reports.Add(1)
						key := r.Header.Get("Idempotency-Key")
						mu.Lock()
						if n == 1 {
							reportKey = key
						}
						if key == "" || key != reportKey {
							t.Errorf("report retry identity changed: %q -> %q", reportKey, key)
						}
						mu.Unlock()
						before, _ := st.Get(id)
						if !strings.Contains(mode, "undelivered") {
							rec := httptest.NewRecorder()
							next.ServeHTTP(rec, r)
							if rec.Code != 200 {
								t.Errorf("apply: %d %s", rec.Code, rec.Body.String())
							}
						} else {
							_, _ = io.Copy(io.Discard, r.Body)
						}
						// A replay response can bypass decoding. Drain the body
						// so server cancellation observes the stalled retry too.
						_, _ = io.Copy(io.Discard, r.Body)
						after, _ := st.Get(id)
						mu.Lock()
						deltas = append(deltas, factory.JobSpent(after)-factory.JobSpent(before))
						mu.Unlock()
						if n == 1 {
							panic(http.ErrAbortHandler)
						}
						applied.Store(true)
						close(stalled)
						if strings.HasPrefix(mode, "report-lease-loss") {
							if err := doors.Stop(id); err != nil {
								t.Error(err)
							}
							http.Error(w, "lease gone", http.StatusConflict)
							return
						}
						<-r.Context().Done()
						return
					}

					if strings.Contains(r.URL.Path, "/workers/recover/") {
						before, _ := st.Get(id)
						rec := httptest.NewRecorder()
						next.ServeHTTP(rec, r)
						after, _ := st.Get(id)
						mu.Lock()
						deltas = append(deltas, after.Stream.Spent-before.Stream.Spent)
						mu.Unlock()
						if rec.Code != 200 {
							t.Errorf("recover: %d %s", rec.Code, rec.Body.String())
						}
						if recovery.Add(1) == 1 {
							panic(http.ErrAbortHandler)
						}
						for k, vs := range rec.Header() {
							for _, v := range vs {
								w.Header().Add(k, v)
							}
						}
						w.WriteHeader(rec.Code)
						_, _ = w.Write(rec.Body.Bytes())
						return
					}
					next.ServeHTTP(w, r)
				})
			}, 100*time.Millisecond)
			awaitProof(t, entered, time.Second, "executor did not start")
			armed.Store(true)
			awaitProof(t, stalled, 5*time.Second, "paid report did not stall")
			reporterCount := func() int {
				var stacks bytes.Buffer
				if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
					t.Fatal(err)
				}
				return strings.Count(stacks.String(), "worker.Options.work.func")
			}
			if reporterCount() == 0 {
				t.Fatal("stalled reporter was not observed")
			}
			before := time.Now()
			if strings.HasPrefix(mode, "cancel") {
				cancel()
			}
			awaitProof(t, done, 7*time.Second, "work/reporter did not end within 7s")
			if n := reporterCount(); n != 0 {
				t.Fatalf("work ended with %d reporter frames still alive", n)
			}
			cur, err := st.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			sum := 0.0
			for _, delta := range deltas {
				sum += delta
			}
			mu.Unlock()
			if sum != .1875 || cur.Stream.Spent != .1875 || recovery.Load() != 2 || reports.Load() != 2 {
				t.Fatalf("spent=%g want .1875; applied_deltas=%v sum=%g; report_requests=%d recovery_requests=%d", cur.Stream.Spent, deltas, sum, reports.Load(), recovery.Load())
			}
			t.Logf("shutdown=%s work_and_reporter=%s received_spend=%g recovery_calls=%d", mode, time.Since(before), cur.Stream.Spent, recovery.Load())
		})
	}
}
