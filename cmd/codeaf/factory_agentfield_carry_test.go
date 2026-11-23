package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

// afPausedStep is a job copy on a runner whose agentfield step "build" waits
// on its run's plan, and a control plane that holds that pause.
func afPausedStep(t *testing.T, decisions ...factory.AgentFieldDecision) (*store.Store, int, *afFakeCP, agentfield.TreeNode) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wait := &factory.AgentFieldWait{Execution: "exec-plan", Step: "build", RequestID: "plan-r1", Question: "Approve the plan?", Body: "# Plan", At: time.Now()}
	it, err := st.Create(factory.Item{Title: "Add Len", State: factory.StateNeedsYou, QKind: factory.QKindAgentField, Question: "Approve the plan?", Stream: &factory.Stream{},
		Stages:          []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "planner.build", On: true}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {ExecutionID: "exec-root", RunID: "run-1", Waiting: wait, Decisions: decisions}}})
	if err != nil {
		t.Fatal(err)
	}
	reason := afWaitingForApproval
	plan := afNode("exec-plan", "plan", "waiting", "2026-11-07T12:00:01Z", "")
	plan.StatusReason = &reason
	cp := &afFakeCP{approval: map[string]agentfield.Approval{"exec-plan": {Status: "pending", RequestID: "plan-r1", Question: "Approve the plan?", Body: "# Plan"}}}
	return st, it.ID, cp, afNode("exec-root", "build", "running", "2026-11-07T12:00:00Z", "", plan)
}

// THE RUNNER THAT STARTED THE RUN CARRIES A PERSON'S ANSWER. A factory that
// took the answer signals it to this runner, which sends it to the control
// plane as the run's owner, with the person named beside the words, and takes
// the question down on its own copy so its next reading does not ask again.
func TestTheRunnerCarriesAnAnswerGivenOnAnyFactory(t *testing.T) {
	st, job, cp, _ := afPausedStep(t)
	r := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "org-runner", true }}
	d := factory.AgentFieldDecision{RequestID: "plan-r1", Execution: "exec-plan", Decision: agentfield.DecisionRequestChanges, Words: "use sqlite", By: "sam"}
	if err := r.Answer(context.Background(), job, d); err != nil {
		t.Fatal(err)
	}
	if want := "exec-plan: request_changes: use sqlite (by sam)"; !slices.Contains(cp.resolved, want) {
		t.Fatalf("the control plane was told %q, want %q", cp.resolved, want)
	}
	it, _ := st.Get(job)
	if it.State == factory.StateNeedsYou || it.AgentFieldSteps["build"].Waiting != nil {
		t.Fatalf("the runner's copy still asks: state %s waiting %+v", it.State, it.AgentFieldSteps["build"].Waiting)
	}
}

// AN ANSWER SURVIVES A RESTART. The factory kept the person's decision on the
// job; the runner that takes the job again (a fresh process, nothing in
// memory) reads the control plane's pause, finds the decision it still owes
// for that very pause, sends it, and does not put the question up again.
func TestARunnerThatTakesTheJobAgainSendsTheAnswerItStillOwes(t *testing.T) {
	d := factory.AgentFieldDecision{RequestID: "plan-r1", Execution: "exec-plan", Decision: agentfield.DecisionApproved, Words: "go", By: "sam", At: time.Now()}
	st, job, cp, tree := afPausedStep(t, d)
	r := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "org-runner", true }}
	if _, err := r.syncTo(context.Background(), cp, st, job, tree, afPlace{step: "build"}); err != nil {
		t.Fatal(err)
	}
	if want := "exec-plan: approved: go (by sam)"; !slices.Contains(cp.resolved, want) {
		t.Fatalf("the owed answer was not sent: %q", cp.resolved)
	}
	it, _ := st.Get(job)
	if it.State == factory.StateNeedsYou || it.QKind == factory.QKindAgentField {
		t.Fatalf("the answered pause was asked again: state %s qkind %q", it.State, it.QKind)
	}
	// Read again at once: the control plane already has it, so it is not
	// sent twice in a row.
	if _, err := r.syncTo(context.Background(), cp, st, job, tree, afPlace{step: "build"}); err != nil {
		t.Fatal(err)
	}
	if n := len(cp.resolved); n != 1 {
		t.Fatalf("the answer was sent %d times: %q", n, cp.resolved)
	}
}

// A REFUSED ANSWER IS SAID, NEVER RETRIED FOREVER. The control plane refuses
// the answer outright (another node owns the run): the runner writes the
// refusal on the run record, so the question stands again on the factory
// with its words, and the signal is done.
func TestAnAnswerTheControlPlaneRefusesIsWrittenOnTheRun(t *testing.T) {
	st, job, cp, _ := afPausedStep(t)
	cp.resolveErr = &agentfield.Error{Code: 403, Op: "answer the approval", Message: "this node does not own the run"}
	r := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "org-runner", true }}
	d := factory.AgentFieldDecision{RequestID: "plan-r1", Execution: "exec-plan", Decision: agentfield.DecisionApproved, By: "sam", At: time.Now()}
	if err := r.Answer(context.Background(), job, d); err != nil {
		t.Fatalf("a refusal is not worth trying again: %v", err)
	}
	it, _ := st.Get(job)
	f := it.AgentFieldSteps["build"].Refused
	if f == nil || f.RequestID != "plan-r1" || !strings.Contains(f.Words, "does not own the run") {
		t.Fatalf("refusal = %+v", f)
	}
	if factory.AgentFieldAnswerFor(it.AgentFieldSteps["build"], "exec-plan", "plan-r1") != nil {
		t.Fatal("a refused answer still reads as owed")
	}
}

// A STOP NO RUNNER HELD IS CARRIED BY THE NEXT ONE. Job 71 of the third live
// run: the person's Stop landed while no runner held the job, so its run went
// on at the control plane and opened a pull request four seconds later. The
// runner that next takes the stopped job cancels the whole run and writes its
// end on the factory's own job, so the job is owed nothing more.
func TestARunnerCarriesAStopThatLandedWithNoRunnerHoldingTheJob(t *testing.T) {
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	floor, id, cp, tree := afPausedStep(t)
	stopped := true
	if err := floor.Update(id, func(it *factory.Item) error {
		it.PersonStopped = &stopped
		it.State, it.QKind, it.Question = factory.StateNew, "", ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cp.tree = tree
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := &afRemote{items: mirror, floor: floor, conn: func() (afClient, string, bool) { return cp, "workaf", true }}
	it, _ := floor.Get(id)
	if err := remote.CarryStop(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	cp.mu.Lock()
	cancelled := append([]string(nil), cp.cancelled...)
	cp.mu.Unlock()
	if len(cancelled) != 1 || cancelled[0] != "run-1" {
		t.Fatalf("the stop did not reach the control plane: cancelled %v", cancelled)
	}
	got, _ := floor.Get(id)
	if run := got.AgentFieldSteps["build"]; run.Ended.IsZero() || run.Status != "cancelled" {
		t.Fatalf("the run's end is not on the factory's job: %+v", run)
	}
	if !factory.StoppedByPerson(got) || factory.AgentFieldAnyRunLive(got) {
		t.Fatalf("after the carry the job still owes its run a runner: stopped %v", factory.StoppedByPerson(got))
	}
}

type afLeaseQueueRig struct {
	url        string
	st         *store.Store
	desk       *lease.Desk
	floor      factory.Seam
	cp         *afFakeCP
	advance    func(time.Duration)
	now        func() time.Time
	afterServe func(*http.Request)
	start      func(slots int) (mirror *store.Store, runner *factoryrun.Runner, releases <-chan api.WorkerLease, quit func())
}

func newAFLeaseQueueRig(t *testing.T, intercept func(http.ResponseWriter, *http.Request) bool) *afLeaseQueueRig {
	t.Helper()
	appDayProfile(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	prev := afRemoteEvery
	afRemoteEvery = 20 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })

	var clockMu sync.Mutex
	var offset time.Duration
	now := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return time.Now().Add(offset)
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		offset += d
		clockMu.Unlock()
	}

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, now(), factory.WithRunner(lease.NewDoors(desk)))
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var rig *afLeaseQueueRig
	handler := serve.New(seam, serve.Config{Address: "http://test", Store: st, Root: st.Root(), Record: history, PullWait: 250 * time.Millisecond, Now: now, LeaseOptions: lease.Options{Now: now}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if intercept != nil && intercept(w, req) {
			return
		}
		handler.ServeHTTP(w, req)
		if rig != nil && rig.afterServe != nil {
			rig.afterServe(req)
		}
	}))
	t.Cleanup(func() { server.Close(); handler.Close() })

	cp := &afFakeCP{
		tree: afNode("exec-root", "build_issue", "running", "2026-11-07T12:00:00Z", "",
			afNode("e-plan", "plan", "running", "2026-11-07T12:00:01Z", "")),
		result: json.RawMessage(`{"success":true,"summary":"done","pr_results":[{"pr_url":"https://github.com/santoshkumarradha/factory-demo/pull/42","pr_number":42}]}`),
	}
	workspace := t.TempDir()
	start := func(slots int) (*store.Store, *factoryrun.Runner, <-chan api.WorkerLease, func()) {
		mirror, err := store.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		remote := &afRemote{
			items: mirror,
			floor: st,
			conn:  func() (afClient, string, bool) { return cp, "workaf-node", true },
		}
		r := factoryrun.New(factoryrun.Options{
			Store: mirror,
			Exec: map[factory.StageKind]factoryrun.Executor{
				factory.StageAgentField: factoryrun.ExecutorFunc(remote.Step),
			},
			Benches:          api.MaxRunnerSlots,
			Remote:           remote.Run,
			AgentFieldAnswer: remote.Answer,
		})

		releases := make(chan api.WorkerLease, 1024)
		var mu sync.Mutex
		creds := map[int]string{}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		machine := "laptop"
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
				Attach: api.WorkerAttach{
					Machine:         machine,
					Name:            machine,
					Handle:          "me",
					TakesUnassigned: map[string]bool{lease.EveryRepo: true},
				},
				Runner:    r,
				Mirror:    mirror,
				CarryStop: remote.CarryStop,
				LocalLease: func(_ string, held api.WorkerLease) {
					select {
					case releases <- held:
					default:
					}
				},
				Now:         now,
				Slots:       slots,
				ReportEvery: 25 * time.Millisecond,
				BeatEvery:   30 * time.Millisecond,
			})
		}()
		var once sync.Once
		quit := func() {
			once.Do(func() {
				cancel()
				<-done
			})
		}
		t.Cleanup(quit)
		_ = workspace
		return mirror, r, releases, quit
	}

	rig = &afLeaseQueueRig{
		url:     server.URL,
		st:      st,
		desk:    desk,
		floor:   client.Open(server.URL, "").Seam(),
		cp:      cp,
		advance: advance,
		now:     now,
		start:   start,
	}
	return rig
}

func addAFBuildJob(t *testing.T, st *store.Store) int {
	t.Helper()
	stage := factory.Stage{
		Name:   "build",
		Label:  "Build with SWE-AF",
		Kind:   factory.StageAgentField,
		Target: "swe-planner.build_issue",
		On:     true,
		Input:  map[string]string{"title": "{title}", "body": "{body}", "repo_url": "{repo_url}", "issue_number": "{issue_number}"},
	}
	id, err := st.Add(context.Background(), factory.Item{
		Autonomy: factory.AutonomyManual,
		Title:    "Add a Len() method to Ledger",
		Body:     "ledger.go needs Len",
		Num:      12,
		Repo:     "santoshkumarradha/factory-demo",
		Tier:     factory.TierOwner,
		Stages:   []factory.Stage{stage},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(id, func(it *factory.Item) error {
		it.Autonomy = ""
		it.DecidePipeline(&factory.PipelineCopy{Text: "steps:\n  - name: build\n    target: swe-planner.build_issue\n"}, time.Now())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func afBuildRunning(it factory.Item) bool {
	step := it.AgentFieldSteps["build"]
	return it.State == factory.StateRunning &&
		it.Stream != nil && len(it.Stream.Phases) > 0 && it.Stream.Phases[0].State == factory.PhaseRunning &&
		step != nil && step.RunID == "run-1" && len(step.Stages) > 0
}

func afWaitJob(t *testing.T, st *store.Store, id int, what string, want func(factory.Item) bool) factory.Item {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		it, err := st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if want(it) {
			return it
		}
		if time.Now().After(deadline) {
			t.Fatalf("never %s (last state=%s)", what, it.State)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func waitLeaseDrop(t *testing.T, releases <-chan api.WorkerLease, id int) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	sawPull := false
	emptyCount := 0
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
					return
				}
			}
		case <-deadline:
			t.Fatal("the worker never finished dropping its lease")
		}
	}
}

// C1, C2, C6, C7: When a worker running an AgentField step loses its lease
// because a heartbeat answers "lease gone" (the lease expired at the queue),
// the control plane receives NO cancel-tree call for that run (C1), the
// dropped worker joins what it started (C6), the job is re-queued rather than
// shown as stopped by a person (C7), and the next lease of the same job
// follows the SAME remote run (run-1) to its end without dispatching a second
// build (C2).
func TestLostLeaseOnAgentFieldStepDoesNotCancelRemoteRunAndNextLeaseReattaches(t *testing.T) {
	var holdPull atomic.Bool
	var firstWorkerAuth atomic.Value
	g := newAFLeaseQueueRig(t, func(w http.ResponseWriter, req *http.Request) bool {
		if strings.HasSuffix(req.URL.Path, "/workers/pull") {
			auth := req.Header.Get("Authorization")
			if prev, ok := firstWorkerAuth.Load().(string); !ok || prev == "" {
				firstWorkerAuth.Store(auth)
			} else if auth == prev && holdPull.Load() {
				w.WriteHeader(http.StatusNoContent)
				return true
			}
		}
		return false
	})
	id := addAFBuildJob(t, g.st)
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	mirror1, runner1, releases1, quit1 := g.start(1)

	// Wait until Worker 1 has dispatched run-1 on the control plane and reported its running step.
	afWaitJob(t, g.st, id, "run-1 recorded on the job", afBuildRunning)
	holdPull.Store(true)

	// Expire the lease at the queue so the next heartbeat answers HTTP 409 "lease is no longer live".
	g.advance(lease.DefaultTTL + time.Second)
	waitLeaseDrop(t, releases1, id)

	// C6: Dropping the job joined what it started within its bound.
	if runner1.Running(id) {
		t.Fatal("C6: runner1 is still running the job after the lease was dropped")
	}
	quit1()

	// C1: The control plane received NO cancel-tree call for run-1.
	g.cp.mu.Lock()
	cancelled := append([]string(nil), g.cp.cancelled...)
	dispatched := len(g.cp.dispatched)
	g.cp.mu.Unlock()
	if len(cancelled) != 0 {
		t.Fatalf("C1: a lost lease cancelled the remote run on the control plane: %v", cancelled)
	}
	if dispatched != 1 {
		t.Fatalf("expected 1 dispatch before re-attach, got %d", dispatched)
	}

	// C7: The job is re-queued ("Waiting to start"), never shown as stopped by a person.
	itAfterLoss, err := g.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	localAfterLoss, _ := mirror1.Get(id)
	if factory.StoppedByPerson(itAfterLoss) || factory.StoppedByPerson(localAfterLoss) {
		t.Fatalf("C7: lost lease marked the job as stopped by a person: factory=%v mirror=%v",
			factory.StoppedByPerson(itAfterLoss), factory.StoppedByPerson(localAfterLoss))
	}
	if itAfterLoss.State != factory.StateQueued {
		t.Fatalf("C7: lost lease left job in state %s, want %s", itAfterLoss.State, factory.StateQueued)
	}
	if itAfterLoss.Stream != nil {
		for _, entry := range itAfterLoss.Stream.Log {
			if strings.Contains(entry.Text, "Stopped by you") {
				t.Fatalf("C7: lost lease logged person stop: %+v", itAfterLoss.Stream.Log)
			}
		}
	}

	// C2: The next lease of the same job follows the SAME remote run (run-1)
	// to its end and does not dispatch a second build.
	end := "2026-11-07T12:00:09Z"
	g.cp.set(func() {
		g.cp.tree.Status = "succeeded"
		g.cp.tree.CompletedAt = &end
		g.cp.tree.Children[0].Status = "succeeded"
		g.cp.tree.Children[0].CompletedAt = &end
	})
	_, runner2, _, quit2 := g.start(1)
	defer quit2()

	done := afWaitJob(t, g.st, id, "re-attached run finished", func(it factory.Item) bool {
		return it.PullRequest != nil && it.PullRequest.Number == 42 &&
			it.AgentFieldSteps["build"] != nil && !it.AgentFieldSteps["build"].Ended.IsZero()
	})
	g.cp.mu.Lock()
	cancelledAfter := append([]string(nil), g.cp.cancelled...)
	dispatchedAfter := len(g.cp.dispatched)
	g.cp.mu.Unlock()
	if len(cancelledAfter) != 0 {
		t.Fatalf("C1/C2: remote run was cancelled during re-attach: %v", cancelledAfter)
	}
	if dispatchedAfter != 1 {
		t.Fatalf("C2: next lease dispatched %d runs, want 1 (re-attach to run-1)", dispatchedAfter)
	}
	if gotRun := done.AgentFieldSteps["build"]; gotRun == nil || gotRun.RunID != "run-1" || gotRun.Status != "succeeded" {
		t.Fatalf("C2: job did not finish on run-1: %+v", gotRun)
	}
	quit2()
	if runner2.Running(id) {
		t.Fatal("C6: runner2 is still running after worker quit")
	}
}

// C1, C2, C6, C7: When a worker running an AgentField step cannot reach the
// heartbeat queue past its bound, it drops the job locally without calling
// cancel-tree on the control plane (C1, C6, C7), and once the queue is
// reachable again the next lease re-attaches to the same run (C2).
func TestUnreachableHeartbeatQueuePastBoundDoesNotCancelRemoteRunAndReattaches(t *testing.T) {
	var unreachable atomic.Bool
	unblockPull := make(chan struct{})
	g := newAFLeaseQueueRig(t, func(w http.ResponseWriter, req *http.Request) bool {
		if !unreachable.Load() {
			return false
		}
		if strings.HasSuffix(req.URL.Path, "/workers/pull") {
			select {
			case <-unblockPull:
				return false
			case <-req.Context().Done():
				return true
			}
		}
		if strings.HasPrefix(req.URL.Path, "/v1/workers/") {
			http.Error(w, "queue unreachable", http.StatusBadGateway)
			return true
		}
		return false
	})
	id := addAFBuildJob(t, g.st)
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	mirror, runner, releases, quit := g.start(1)

	afWaitJob(t, g.st, id, "run-1 recorded on the job", afBuildRunning)

	// Cut off the heartbeat queue past its bound.
	unreachable.Store(true)
	g.advance(lease.DefaultTTL + time.Second)
	waitLeaseDrop(t, releases, id)

	// C6: Runner joined what it started when the lease was dropped.
	if runner.Running(id) {
		t.Fatal("C6: runner still running after heartbeat failed past bound")
	}

	// C1: No cancel-tree call was sent to the control plane.
	g.cp.mu.Lock()
	cancelled := append([]string(nil), g.cp.cancelled...)
	dispatched := len(g.cp.dispatched)
	g.cp.mu.Unlock()
	if len(cancelled) != 0 {
		t.Fatalf("C1: unreachable heartbeat queue cancelled remote run: %v", cancelled)
	}
	if dispatched != 1 {
		t.Fatalf("expected 1 dispatch, got %d", dispatched)
	}

	// C7: Neither store nor mirror shows the job as stopped by a person.
	itAfterLoss, _ := g.st.Get(id)
	localAfterLoss, _ := mirror.Get(id)
	if factory.StoppedByPerson(itAfterLoss) || factory.StoppedByPerson(localAfterLoss) {
		t.Fatalf("C7: unreachable heartbeat marked the job as stopped by a person: factory=%v mirror=%v",
			factory.StoppedByPerson(itAfterLoss), factory.StoppedByPerson(localAfterLoss))
	}

	// C2: Restore the queue and complete run-1; the next lease follows run-1 to its end.
	end := "2026-11-07T12:00:09Z"
	g.cp.set(func() {
		g.cp.tree.Status = "succeeded"
		g.cp.tree.CompletedAt = &end
		g.cp.tree.Children[0].Status = "succeeded"
		g.cp.tree.Children[0].CompletedAt = &end
	})
	unreachable.Store(false)
	close(unblockPull)

	done := afWaitJob(t, g.st, id, "re-attached run finished after queue returned", func(it factory.Item) bool {
		return it.PullRequest != nil && it.PullRequest.Number == 42 &&
			it.AgentFieldSteps["build"] != nil && !it.AgentFieldSteps["build"].Ended.IsZero()
	})
	g.cp.mu.Lock()
	cancelledAfter := append([]string(nil), g.cp.cancelled...)
	dispatchedAfter := len(g.cp.dispatched)
	g.cp.mu.Unlock()
	if len(cancelledAfter) != 0 {
		t.Fatalf("C1/C2: remote run was cancelled after queue returned: %v", cancelledAfter)
	}
	if dispatchedAfter != 1 {
		t.Fatalf("C2: next lease dispatched %d runs, want 1", dispatchedAfter)
	}
	if gotRun := done.AgentFieldSteps["build"]; gotRun == nil || gotRun.RunID != "run-1" || gotRun.Status != "succeeded" {
		t.Fatalf("C2: job did not finish on run-1: %+v", gotRun)
	}
	quit()
	if runner.Running(id) {
		t.Fatal("C6: runner still running after worker quit")
	}
}

// C3: A person's Stop on the job (both the factory's stop door and the app
// wire's factory.job_stop call) still posts cancel-tree for the remote run,
// exactly once.
func TestPersonStopOnAgentFieldStepPostsCancelTreeExactlyOnce(t *testing.T) {
	runCase := func(t *testing.T, stopJob func(g *afLeaseQueueRig, id int, startWorkers func(), onStop func())) {
		var stopCalled atomic.Bool
		secondPulled := make(chan struct{}, 1)
		carryDone := make(chan struct{}, 1)
		g := newAFLeaseQueueRig(t, nil)
		id := addAFBuildJob(t, g.st)
		g.afterServe = func(req *http.Request) {
			if !stopCalled.Load() {
				return
			}
			if strings.HasSuffix(req.URL.Path, "/workers/pull") {
				if live, threadOnly, _ := g.desk.LiveHold(id); live && threadOnly {
					select {
					case secondPulled <- struct{}{}:
					default:
					}
				}
			}
			if strings.HasSuffix(req.URL.Path, "/done") {
				select {
				case carryDone <- struct{}{}:
				default:
				}
			}
		}
		// Hold CancelTree briefly so the second worker slot pulls the
		// ThreadOnly stop-carry lease while the first slot is also stopping.
		g.cp.afterCancel = func() {
			select {
			case <-secondPulled:
			case <-time.After(500 * time.Millisecond):
			}
		}
		var runner *factoryrun.Runner
		var quit func()
		startWorkers := func() {
			_, runner, _, quit = g.start(2)
		}
		defer func() {
			if quit != nil {
				quit()
			}
		}()

		stopJob(g, id, startWorkers, func() { stopCalled.Store(true) })

		select {
		case <-carryDone:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for stop-carry lease to finish")
		}
		stopped := afWaitJob(t, g.st, id, "remote run marked ended after stop", func(it factory.Item) bool {
			return factory.StoppedByPerson(it) && !factory.AgentFieldAnyRunLive(it) && !runner.Running(id)
		})

		g.cp.mu.Lock()
		cancelled := append([]string(nil), g.cp.cancelled...)
		g.cp.mu.Unlock()
		if !slices.Equal(cancelled, []string{"run-1"}) {
			t.Fatalf("C3: stop posted cancel-tree %v, want [run-1] exactly once", cancelled)
		}
		if stopped.State != factory.StateNew {
			t.Fatalf("C3: stopped job is in state %s, want %s", stopped.State, factory.StateNew)
		}
	}

	t.Run("factory stop door", func(t *testing.T) {
		runCase(t, func(g *afLeaseQueueRig, id int, startWorkers func(), onStop func()) {
			if err := g.floor.Launch(id); err != nil {
				t.Fatal(err)
			}
			startWorkers()
			afWaitJob(t, g.st, id, "run-1 running on control plane", afBuildRunning)
			onStop()
			if err := g.floor.Stop(id); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("app wire stop call", func(t *testing.T) {
		runCase(t, func(g *afLeaseQueueRig, id int, startWorkers func(), onStop func()) {
			hub := newAppFactoryHub()
			dir := filepath.Join(t.TempDir(), "app")
			hub.dir = func() string { return dir }
			t.Cleanup(hub.close)
			backend := newFakeBackend(newAppRecorder(), nil)
			warmed := make(chan struct{})
			close(warmed)
			backend.warmed = warmed
			backend.factoryOnce.Do(func() { backend.factories = hub; hub.resume = backend.resumeLocal })
			t.Cleanup(func() { _ = backend.Close() })
			w := wireOver(t, backend)
			var conn appwire.FactoryConnection
			w.must("factory.attach", map[string]any{"url": g.url}, &conn)

			var read0 api.Job
			w.must("factory.job", map[string]any{"factory": conn.ID, "job": id}, &read0)
			w.must("factory.job_start", map[string]any{"factory": conn.ID, "job": id, "expect": map[string]any{"version": read0.Version}}, nil)
			startWorkers()

			afWaitJob(t, g.st, id, "run-1 started via app wire", afBuildRunning)
			var readRun api.Job
			w.must("factory.job", map[string]any{"factory": conn.ID, "job": id}, &readRun)
			onStop()
			w.must("factory.job_stop", map[string]any{"factory": conn.ID, "job": id, "expect": map[string]any{"version": readRun.Version}}, nil)
		})
	})
}
