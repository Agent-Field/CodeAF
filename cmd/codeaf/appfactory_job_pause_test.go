package main

import (
	"context"
	"strings"
	"sync"
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
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

// The desk pauses through the app wire; the real lease worker cancels its
// conversation and resumes the same step without losing its chat or spending.
func TestFactoryRunningJobPause(t *testing.T) {
	t.Run("leased round", func(t *testing.T) {
		s := newLocalSetup(t)
		status := s.start()
		floor := s.backend.local.host.floor
		mirror, err := store.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		started := make(chan *factoryrun.FakeConversation, 4)
		cancelled := make(chan struct{})
		finish := make(chan struct{})
		var calls atomic.Int32
		maker := &factoryrun.FakeMaker{Script: func(ctx context.Context, c *factoryrun.FakeConversation) {
			n := calls.Add(1)
			started <- c
			if n == 1 {
				<-ctx.Done()
				close(cancelled)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-finish:
			}
			_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
		}}
		runner := factoryrun.New(factoryrun.Options{Store: mirror, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryrun.NewChatExecutor(maker)}})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		var mu sync.Mutex
		credential := ""
		go func() {
			defer close(done)
			worker.Run(ctx, worker.Options{
				Dial: func(int) *client.WorkerClient {
					mu.Lock()
					defer mu.Unlock()
					return client.Open(status.URL, s.backend.local.host.token).Worker(credential)
				},
				Keep:   func(_ int, w *client.WorkerClient) { mu.Lock(); defer mu.Unlock(); credential = w.Credential() },
				Attach: api.WorkerAttach{Machine: "pause-fixture", Name: "pause fixture", Handle: "owner", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
				Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: 20 * time.Millisecond, BeatEvery: 20 * time.Millisecond,
			})
		}()
		t.Cleanup(func() { cancel(); <-done })
		it, err := floor.Create(factory.Item{Repo: "acme/api", Title: "Pause this round", Kind: factory.KindIssue, Autonomy: factory.AutonomyManual, Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write", Until: factory.UntilDone, On: true}}})
		if err != nil {
			t.Fatal(err)
		}
		params := map[string]any{"factory": status.Factory, "job": it.ID}
		read := func() api.Job { var j api.Job; s.must("factory.job", params, &j); return j }
		act := func(method string) {
			j := read()
			params["expect"] = map[string]any{"version": j.Version}
			s.must(method, params, &appwire.FactoryJobActed{})
		}
		wait := func(what string, predicate func(api.Job) bool) api.Job {
			t.Helper()
			deadline := time.Now().Add(10 * time.Second)
			for {
				j := read()
				if predicate(j) {
					return j
				}
				if time.Now().After(deadline) {
					t.Fatalf("never %s: %+v", what, j)
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		next := func() *factoryrun.FakeConversation {
			t.Helper()
			select {
			case c := <-started:
				return c
			case <-time.After(10 * time.Second):
				t.Fatal("model round did not start")
				return nil
			}
		}
		desk, err := lease.Open(floor, lease.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.NewDoors(desk).Launch(it.ID); err != nil {
			t.Fatal(err)
		}
		first := next()
		wait("running with chat", func(j api.Job) bool {
			return j.State == api.StateRunning && j.Desk != nil && j.Desk.Run != nil && len(j.Steps) > 0
		})
		j := read()
		if !j.Can.Hold || j.Can.Resume {
			t.Fatalf("running controls: hold=%v resume=%v", j.Can.Hold, j.Can.Resume)
		}
		act("factory.job_hold")
		select {
		case <-cancelled:
		case <-time.After(5 * time.Second):
			t.Fatal("pause did not cancel the model round")
		}
		paused := wait("paused", func(j api.Job) bool { return j.StatePresentation.Kind == "paused" && j.Can.Resume })
		if paused.StatePresentation.Title != "Paused" || paused.Can.Hold || paused.NeedsYou != nil || paused.Failure != nil {
			t.Fatalf("paused job: %+v", paused)
		}
		time.Sleep(100 * time.Millisecond)
		if calls.Load() != 1 {
			t.Fatalf("paused job made %d model calls", calls.Load())
		}
		if j = read(); j.Cost != paused.Cost {
			t.Fatalf("paused cost grew: %v -> %v", paused.Cost, j.Cost)
		}
		act("factory.job_resume")
		second := next()
		if second.Spec.Resume != first.ID() {
			t.Fatalf("resumed chat %q, want %q", second.Spec.Resume, first.ID())
		}
		close(finish)
		wait("finished step", func(j api.Job) bool { return len(j.Steps) > 0 && j.Steps[0].State == api.StepDone })
		history, err := record.Open(floor, record.Options{})
		if err != nil {
			t.Fatal(err)
		}
		events, err := history.Since(context.Background(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var receipts []string
		for _, e := range events {
			if e.Job == it.ID && (e.Kind == record.Held || e.Kind == record.Resumed) {
				receipts = append(receipts, e.Kind+":"+e.Handle)
			}
		}
		if strings.Join(receipts, ",") != "held:owner,resumed:owner" {
			t.Fatalf("pause receipts: %v", receipts)
		}
		if !strings.Contains(s.hello, `"factory.job_pause"`) {
			t.Fatalf("hello lacks factory.job_pause: %s", s.hello)
		}
		j = read()
		var words []string
		for _, line := range j.Desk.Run.Log {
			if line.Text == "paused" || line.Text == "resumed" || strings.Contains(line.Text, "stopped") {
				words = append(words, line.Text)
			}
		}
		if strings.Join(words, ",") != "paused,resumed" {
			t.Fatalf("pause chat lines: %v", words)
		}
	})
	t.Run("owner", func(t *testing.T) {
		f := startActionFactory(t)
		alice := signedIn(t, f, 1, "alice")
		bob := signedIn(t, f, 2, "bob")
		it := f.create(t, factory.Item{State: factory.StateRunning, Assignee: "alice", Stream: &factory.Stream{}})
		bob.w.mustFail("factory.job_hold", bob.params(t, it.ID, nil), appwire.CodeForbidden)
		alice.act(t, "factory.job_hold", it.ID, nil)
		bob.w.mustFail("factory.job_resume", bob.params(t, it.ID, nil), appwire.CodeForbidden)
		alice.act(t, "factory.job_resume", it.ID, nil)
	})
	t.Run("permissions", func(t *testing.T) {
		for _, tc := range []struct {
			name               string
			state              factory.State
			publishing, paused bool
			hold, resume       bool
		}{
			{name: "running", state: factory.StateRunning, hold: true},
			{name: "paused", state: factory.StateRunning, paused: true, resume: true},
			{name: "needs you", state: factory.StateNeedsYou},
			{name: "done", state: factory.StateShipped},
			{name: "publishing", state: factory.StateRunning, publishing: true},
		} {
			it := factory.Item{State: tc.state, Assignee: "owner", Publishing: tc.publishing, Stream: &factory.Stream{Paused: tc.paused}}
			can := api.CanFor(it, "owner")
			if can.Hold != tc.hold || can.Resume != tc.resume {
				t.Errorf("%s: hold=%v resume=%v", tc.name, can.Hold, can.Resume)
			}
			can = api.CanFor(it, "other")
			if can.Hold || can.Resume {
				t.Errorf("%s allows another person", tc.name)
			}
		}
	})
	t.Run("no lease and refusals", func(t *testing.T) {
		s := newLocalSetup(t)
		status := s.start()
		floor := s.backend.local.host.floor
		for _, state := range []factory.State{factory.StateRunning, factory.StateNew, factory.StateNeedsYou, factory.StateLanded} {
			it, err := floor.Create(factory.Item{Repo: "acme/api", Title: "Saved job", Kind: factory.KindIssue, State: state, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, Chat: "saved-chat"}}, Spent: 0.25}})
			if err != nil {
				t.Fatal(err)
			}
			params := map[string]any{"factory": status.Factory, "job": it.ID}
			var before api.Job
			s.must("factory.job", params, &before)
			params["expect"] = map[string]any{"version": before.Version}
			if state != factory.StateRunning {
				s.mustFail("factory.job_hold", params, appwire.CodeConflict)
				continue
			}
			s.must("factory.job_hold", params, &appwire.FactoryJobActed{})
			var j api.Job
			s.must("factory.job", params, &j)
			if j.StatePresentation.Kind != "paused" || !j.Can.Resume || j.Cost != 0.25 {
				t.Fatalf("unleased pause: %+v", j)
			}
			params["expect"] = map[string]any{"version": j.Version}
			s.must("factory.job_resume", params, &appwire.FactoryJobActed{})
			s.must("factory.job", params, &j)
			if j.State != api.StateQueued || j.Desk.Run.Paused || j.Cost != 0.25 {
				t.Fatalf("unleased resume: %+v", j)
			}
		}
	})
}
