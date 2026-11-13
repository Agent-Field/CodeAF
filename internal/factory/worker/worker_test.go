package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	base  string
	st    *store.Store
	floor factory.Seam // the floor as any window reaches it: over HTTP
	doors factory.Seam // the server's own seam, past the record's approval binding
	run   *run.Runner
	// record is the factory's record, where the service writes approvals.
	record *record.Record
	mirror *store.Store
	seen   *reportLog
	// quit ends the worker's process, as quitting the app does, and waits
	// for it to return.
	quit func()
}

func newRig(t *testing.T, localLease ...func(string, api.WorkerLease)) rig {
	t.Helper()
	var keepLocal func(string, api.WorkerLease)
	if len(localLease) > 0 {
		keepLocal = localLease[0]
	}
	return newRigWith(t, keepLocal, nil)
}

// newRigWith is newRig whose worker's runner options tune may change.
func newRigWith(t *testing.T, keepLocal func(string, api.WorkerLease), tune func(*run.Options), identity ...string) rig {
	return newRigWithReportEvery(t, keepLocal, tune, 20*time.Millisecond, identity...)
}

func newRigWithReportEvery(t *testing.T, keepLocal func(string, api.WorkerLease), tune func(*run.Options), reportEvery time.Duration, identity ...string) rig {
	return newRigWithDelivery(t, keepLocal, tune, reportEvery, nil, nil, 2, identity...)
}

func newRigWithDelivery(t *testing.T, keepLocal func(string, api.WorkerLease), tune func(*run.Options), reportEvery time.Duration, intercept func(http.ResponseWriter, *http.Request) bool, clock func() time.Time, slots int, identity ...string) rig {
	return newRigWithReady(t, keepLocal, tune, reportEvery, intercept, clock, slots, nil, identity...)
}

func newRigWithReady(t *testing.T, keepLocal func(string, api.WorkerLease), tune func(*run.Options), reportEvery time.Duration, intercept func(http.ResponseWriter, *http.Request) bool, clock func() time.Time, slots int, ready <-chan struct{}, identity ...string) rig {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(lease.NewDoors(desk)))
	if len(identity) > 0 {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: identity[0]}, nil }
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	handler := serve.New(seam, serve.Config{Address: "http://test", Store: st, Record: history, PullWait: 50 * time.Millisecond, Now: clock, LeaseOptions: lease.Options{Now: clock}})
	seen := &reportLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if intercept != nil && intercept(w, req) {
			return
		}
		body := ""
		if req.Method == http.MethodPost {
			body = peek(req)
		}
		captured := httptest.NewRecorder()
		handler.ServeHTTP(captured, req)
		if isReport(req) || strings.HasSuffix(req.URL.Path, "/done") {
			seen.add(fmt.Sprintf("%s status=%d request=%s", req.URL.Path, captured.Code, body))
		}
		if strings.HasSuffix(req.URL.Path, "/heartbeat") && strings.Contains(captured.Body.String(), `"verb"`) {
			var beat api.WorkerHeartbeat
			if err := json.Unmarshal(captured.Body.Bytes(), &beat); err == nil {
				seen.add(fmt.Sprintf("heartbeat signals=%+v", beat.Signals))
			}
		}
		for key, values := range captured.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(captured.Code)
		_, _ = w.Write(captured.Body.Bytes())
	}))
	t.Cleanup(func() { server.Close(); handler.Close() })

	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	opts := run.Options{
		Store:   mirror,
		Exec:    map[factory.StageKind]run.Executor{factory.StageCheck: run.NewCheckExecutor(run.CheckOptions{})},
		RepoDir: func(string) string { return dir },
	}
	if tune != nil {
		tune(&opts)
	}
	r := run.New(opts)
	var mu sync.Mutex
	creds := map[int]string{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if ready != nil {
			select {
			case <-ready:
			case <-ctx.Done():
				return
			}
		}
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
			Attach: api.WorkerAttach{Machine: "laptop", Name: "laptop (pid 123)", Handle: "me", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
			Runner: r,
			Mirror: mirror,
			LocalLease: func(base string, held api.WorkerLease) {
				if held.ID == "" {
					local, err := mirror.Get(held.Job)
					seen.add(fmt.Sprintf("worker release job=%d state=%s stream=%+v error=%v", held.Job, local.State, local.Stream, err))
				}
				if keepLocal != nil {
					keepLocal(base, held)
				}
			},
			Now:         clock,
			Slots:       slots,
			ReportEvery: reportEvery,
			BeatEvery:   25 * time.Millisecond,
		})
	}()
	quit := func() { cancel(); <-done }
	t.Cleanup(quit)
	return rig{base: server.URL, st: st, floor: client.Open(server.URL, "").Seam(), doors: seam, run: r, record: history, mirror: mirror, seen: seen, quit: quit}
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
	id, err := g.st.Add(context.Background(), factory.Item{Autonomy: factory.AutonomyManual, Title: "prove it builds", Repo: "api", Tier: factory.TierOwner, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	// Existing protocol fixtures predate the persisted approval modes.
	// Explicit-mode publication behavior has its own real service fixtures.
	if err := g.st.Update(id, func(it *factory.Item) error { it.Autonomy = ""; return nil }); err != nil {
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

// A WORKER THAT ENDS LETS GO OF ITS STEPS; ONLY A STOP STOPS THEM. Quitting
// the app ends the worker's process: a step it was running is let go, so
// work that runs elsewhere (an AgentField run on a control plane) goes on and
// the next start follows it again (run.StoppedHere is false). A person's Stop
// on the factory still stops the step as theirs (StoppedHere is true).
func TestAWorkerThatEndsLetsGoAndOnlyAStopStops(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stopped bool
	}{{"the worker quits", false}, {"the person stops it", true}} {
		t.Run(tc.name, func(t *testing.T) {
			ended, began := make(chan bool, 1), make(chan struct{}, 1)
			g := newRigWith(t, nil, func(o *run.Options) {
				o.Exec[factory.StageCheck] = run.ExecutorFunc(func(ctx context.Context, _ run.Job) (factory.StageResult, error) {
					began <- struct{}{}
					<-ctx.Done()
					ended <- run.StoppedHere(ctx)
					return factory.StageResult{}, ctx.Err()
				})
			})
			id := g.add(t, check("elsewhere"))
			if err := g.floor.Launch(id); err != nil {
				t.Fatal(err)
			}
			select {
			case <-began:
			case <-time.After(5 * time.Second):
				it, _ := g.st.Get(id)
				t.Fatalf("the worker never began the step: %s %+v", it.State, it.Stream)
			}
			if tc.stopped {
				if err := g.floor.Stop(id); err != nil {
					t.Fatal(err)
				}
			} else {
				go g.quit()
			}
			select {
			case got := <-ended:
				if got != tc.stopped {
					t.Fatalf("the step's end read as a person's stop = %t, want %t", got, tc.stopped)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the step never ended")
			}
			if !tc.stopped {
				if local, _ := g.mirror.Get(id); factory.StoppedByPerson(local) {
					t.Fatal("quitting marked the job as stopped by its person")
				}
			}
		})
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
			it, _ := g.st.Get(id)
			local, _ := g.mirror.Get(id)
			t.Fatalf("the worker's runner never said paused: factory=%+v worker=%+v running=%t reports=%q", it.Stream, local.Stream, g.run.Running(id), g.seen.all())
		}
	}
	if err := g.floor.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateShipped)
}

// A resumed job carries completed steps over HTTP and continues beside the
// files they left, even when a new worker process takes the lease.
func TestAResumedRunKeepsItsOriginalMachine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "from-first-run"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := newRigWith(t, nil, func(o *run.Options) {
		o.RepoDir = func(string) string { return dir }
	})
	first := check("false")
	first.Name = "already-done"
	second := check("test -f from-first-run")
	it, err := g.st.Create(factory.Item{
		Repo: "api", Title: "resume beside the files", State: factory.StateQueued,
		WorktreeMachine: "laptop", Stages: []factory.Stage{first, second},
		Stream: &factory.Stream{Cur: 1, Started: time.Now(), Phases: []factory.Phase{
			{Name: first.Name, State: factory.PhaseDone, Result: &factory.StageResult{Done: true}},
			{Name: second.Name, State: factory.PhasePending},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	other := client.Open(g.base, "").Worker("")
	if _, err := other.Attach(context.Background(), api.WorkerAttach{Machine: "desktop", Name: "desktop", Handle: "me", TakesUnassigned: map[string]bool{"*": true}}); err != nil {
		t.Fatal(err)
	}
	if pulled, ok, err := other.Pull(context.Background()); err != nil || ok {
		t.Fatalf("another machine pulled the resumed job: %+v, %v, %v", pulled, ok, err)
	}
	shipped := g.waitFor(t, it.ID, factory.StateShipped)
	if shipped.WorktreeMachine != "laptop" || shipped.Stream.Phases[0].Round != 0 {
		t.Fatalf("resumed run restarted or moved: %+v", shipped)
	}
	if result := shipped.Stream.Phases[1].Result; result == nil || result.Exit != 0 {
		t.Fatalf("the resumed worker could not read the first run's file: %+v", result)
	}
}

// TestAFactoryStageRoomReachesTheFactoryThroughTheWorkersReports: the room the
// factory's stage wrapper records on the worker's mirror (factoryChatStage
// does it with Annotate) crosses the lease to the factory's own copy of the
// job, and stays on it when the job ships. Before the report carried it, the
// engine's item had an empty Stream.Room whatever the worker had recorded.
func TestAFactoryStageRoomReachesTheFactoryThroughTheWorkersReports(t *testing.T) {
	const room = "0a1b2c3d4e5f"
	g := newRigWith(t, nil, func(o *run.Options) {
		mirror, inner := o.Store, o.Exec[factory.StageCheck]
		o.Exec[factory.StageCheck] = run.ExecutorFunc(func(ctx context.Context, job run.Job) (factory.StageResult, error) {
			if err := mirror.Annotate(job.Item.ID, func(it *factory.Item) error {
				if it.Stream != nil {
					it.Stream.Room = room
				}
				return nil
			}); err != nil {
				return factory.StageResult{}, err
			}
			return inner.Run(ctx, job)
		})
	})
	id := g.add(t, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateShipped)
	if it.Stream == nil || it.Stream.Room != room {
		t.Fatalf("the factory's copy lost the stage room after the report: %+v", it.Stream)
	}
}

// C4, C5, C6, C7: A local (non-AgentField) step stops promptly when its lease
// is withdrawn (C4), when its lease expires at the queue ("lease gone", C5),
// or when the heartbeat cannot reach the queue past its bound (C5). In every
// case the worker joins what it started within its bound (C6), and neither the
// step context nor the job is marked as stopped by a person (C7).
func TestLostAndWithdrawnLeaseStopLocalStepPromptlyWithoutPersonStop(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind string
	}{
		{name: "withdrawn lease", kind: "withdrawn"},
		{name: "expired lease gone", kind: "expired"},
		{name: "heartbeat unreachable past bound", kind: "unreachable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var clockMu sync.Mutex
			var offset time.Duration
			now := func() time.Time {
				clockMu.Lock()
				defer clockMu.Unlock()
				return time.Now().Add(offset)
			}
			advance := func(d time.Duration) {
				clockMu.Lock()
				defer clockMu.Unlock()
				offset += d
			}

			launched := make(chan struct{})
			var holdPull, unreachable atomic.Bool
			intercept := func(w http.ResponseWriter, req *http.Request) bool {
				if unreachable.Load() && strings.Contains(req.URL.Path, "/workers/") {
					http.Error(w, "queue unreachable", http.StatusBadGateway)
					return true
				}
				if holdPull.Load() && strings.HasSuffix(req.URL.Path, "/workers/pull") {
					w.WriteHeader(http.StatusNoContent)
					return true
				}
				return false
			}

			began := make(chan struct{}, 1)
			ended := make(chan bool, 1)
			releases := make(chan api.WorkerLease, 1024)
			var mirrorPtr atomic.Pointer[store.Store]
			var localMu sync.Mutex
			var localAtDrop factory.Item
			g := newRigWithReady(t,
				func(_ string, held api.WorkerLease) {
					if held.ID == "" {
						if m := mirrorPtr.Load(); m != nil {
							if it, err := m.Get(held.Job); err == nil {
								localMu.Lock()
								localAtDrop = it
								localMu.Unlock()
							}
						}
					}
					select {
					case releases <- held:
					default:
					}
				},
				func(o *run.Options) {
					o.Exec[factory.StageCheck] = run.ExecutorFunc(func(ctx context.Context, _ run.Job) (factory.StageResult, error) {
						select {
						case began <- struct{}{}:
						default:
						}
						<-ctx.Done()
						ended <- run.StoppedHere(ctx)
						return factory.StageResult{}, ctx.Err()
					})
				},
				25*time.Millisecond,
				intercept,
				now,
				1,
				launched,
			)
			mirrorPtr.Store(g.mirror)

			id := g.add(t, check("step-1"), factory.Stage{Name: "second", Kind: factory.StageCheck, Ask: "true", On: true})
			if err := g.floor.Launch(id); err != nil {
				t.Fatal(err)
			}
			close(launched)
			select {
			case <-began:
			case <-time.After(10 * time.Second):
				t.Fatal("the worker never began the local step")
			}
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
				if it, _ := g.st.Get(id); it.State == factory.StateRunning && it.Stream != nil && len(it.Stream.Phases) > 0 && it.Stream.Phases[0].State == factory.PhaseRunning {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the worker's running state never reached the factory store")
				}
			}

			holdPull.Store(true)
			switch tc.kind {
			case "withdrawn":
				if _, _, err := g.doors.Edit(context.Background(), id, factory.RunEdit{
					Skip: []string{"second"},
					By:   factory.ByYou,
					Why:  "skip second step",
				}); err != nil {
					t.Fatal(err)
				}
			case "expired":
				advance(lease.DefaultTTL + time.Second)
			case "unreachable":
				unreachable.Store(true)
				advance(lease.DefaultTTL + time.Second)
			}

			// C4, C5, C7: The local step stops promptly and is not marked StoppedHere.
			select {
			case stoppedHere := <-ended:
				if stoppedHere {
					t.Fatalf("C7 (%s): lost/withdrawn lease marked step context StoppedHere = true", tc.name)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("C5 (%s): local step did not stop promptly when lease was lost", tc.name)
			}

			// Wait for the worker to finish dropping the lease (j.drop + defer keepLocal).
			sawPull := false
			emptyCount := 0
			deadline := time.After(3 * time.Second)
			for {
				select {
				case held := <-releases:
					if held.Job != id {
						continue
					}
					if held.ID != "" {
						sawPull = true
						continue
					}
					if sawPull {
						emptyCount++
						if emptyCount >= 2 {
							goto dropped
						}
					}
				case <-deadline:
					t.Fatalf("C6 (%s): worker never released local lease receipt", tc.name)
				}
			}
		dropped:
			// C6: Dropping the job joined what it started within its bound.
			if g.run.Running(id) {
				t.Fatalf("C6 (%s): runner is still running after lease drop", tc.name)
			}

			// C7: Neither the factory item nor the worker mirror shows a person's stop.
			it, _ := g.st.Get(id)
			localMu.Lock()
			local := localAtDrop
			localMu.Unlock()
			if factory.StoppedByPerson(it) || factory.StoppedByPerson(local) {
				t.Fatalf("C7 (%s): job marked StoppedByPerson: factory=%v mirror=%v",
					tc.name, factory.StoppedByPerson(it), factory.StoppedByPerson(local))
			}
			if logHas(it, "Stopped by you") > 0 || logHas(local, "Stopped by you") > 0 {
				t.Fatalf("C7 (%s): job log says 'Stopped by you': factory=%+v mirror=%+v",
					tc.name, it.Stream, local.Stream)
			}
			switch tc.kind {
			case "withdrawn":
				if it.State != factory.StateQueued {
					t.Fatalf("C4 (%s): factory job state = %s, want %s", tc.name, it.State, factory.StateQueued)
				}
			case "expired":
				if it.State != factory.StateNeedsYou || it.QKind != factory.NeedsRunnerOffline || it.Question != "Runner went offline mid-step" {
					t.Fatalf("C7 (%s): factory job = (%s, %s, %q), want (%s, %s, %q)",
						tc.name, it.State, it.QKind, it.Question, factory.StateNeedsYou, factory.NeedsRunnerOffline, "Runner went offline mid-step")
				}
			}

			g.quit()
			if g.run.Running(id) {
				t.Fatalf("C6 (%s): runner still running after worker quit", tc.name)
			}
		})
	}
}
