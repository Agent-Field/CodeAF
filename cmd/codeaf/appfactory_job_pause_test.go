package main

import (
	"context"
	"errors"
	"reflect"
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
		s, status := newPauseSetup(t)
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
		chat := factoryrun.NewChatExecutor(maker)
		runner := factoryrun.New(factoryrun.Options{Store: mirror, Talk: sessionTalk{}, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, job factoryrun.Job) (factory.StageResult, error) {
			job.Spend(0.125)
			return chat.Run(ctx, job)
		})}})
		startPauseWorker(t, s, status, runner, mirror)
		it, err := floor.Create(factory.Item{Repo: "acme/api", Title: "Pause this round", Kind: factory.KindIssue, Autonomy: factory.AutonomyManual, Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write", Until: factory.UntilDone, On: true}}})
		if err != nil {
			t.Fatal(err)
		}
		params := map[string]any{"factory": status.Factory, "job": it.ID}
		read := func() api.Job { var j api.Job; s.must("factory.job", params, &j); return j }
		act := func(method string) {
			t.Helper()
			params["expect"] = map[string]any{"version": 1}
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
		s.backend.open = func(ask appOpenAsk) (*appConn, error) {
			return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
		}
		var chats appwire.FactoryJobChats
		s.must("factory.job_chats", params, &chats)
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
		paused := wait("paused", func(j api.Job) bool { return j.StatePresentation.Kind == "paused" && j.Can.Resume && j.Cost == 0.125 })
		if paused.StatePresentation.Title != "Paused" || paused.Can.Hold || paused.NeedsYou != nil || paused.Failure != nil || paused.Cost != 0.125 {
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
		wait("finished step", func(j api.Job) bool { return len(j.Steps) > 0 && j.Steps[0].State == api.StepDone && j.Cost == 0.25 })
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
		var page appwire.EntriesResult
		s.must("agents.entries", map[string]any{"id": chats.Agent, "limit": 200}, &page)
		resumed := 0
		for _, entry := range page.Entries {
			if entry.Text == "resumed" {
				resumed++
			}
		}
		if resumed != 1 {
			t.Errorf("manager chat has %d resumed lines, want 1", resumed)
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
		s, status := newPauseSetup(t)
		floor := s.backend.local.host.floor
		for _, state := range []factory.State{factory.StateRunning, factory.StateNew, factory.StateNeedsYou, factory.StateLanded} {
			it, err := floor.Create(factory.Item{Repo: "acme/api", Title: "Saved job", Kind: factory.KindIssue, State: state, Autonomy: factory.AutonomyManual, Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write", Until: factory.UntilDone, On: true}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, Chat: "saved-chat"}}, Spent: 0.25}})
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
			j = api.Job{}
			s.must("factory.job", params, &j)
			if j.State != api.StateQueued || j.Desk.Run.Paused || j.Cost != 0.25 {
				t.Fatalf("unleased resume: %+v", j)
			}
			mirror, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			opened := make(chan factoryrun.ConversationSpec, 1)
			maker := &factoryrun.FakeMaker{Script: func(ctx context.Context, c *factoryrun.FakeConversation) {
				opened <- c.Spec
				_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
			}}
			runner := factoryrun.New(factoryrun.Options{Store: mirror, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryrun.NewChatExecutor(maker)}})
			startPauseWorker(t, s, status, runner, mirror)
			select {
			case spec := <-opened:
				if spec.Resume != "saved-chat" {
					t.Fatalf("unleased resumed chat %q, want saved-chat", spec.Resume)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("unleased resume never ran its saved step")
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				j = api.Job{}
				s.must("factory.job", params, &j)
				if len(j.Steps) > 0 && j.Steps[0].State == api.StepDone {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("unleased resumed step never finished: %+v", j)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if j.Cost != 0.25 {
				t.Fatalf("unleased resume changed recorded spend: %v", j.Cost)
			}
		}
	})
}

// A paused job gives the only slot to another job and rejoins the ordinary
// queue on resume, keeping completed work and the interrupted conversation.
func TestFactoryPausedJobReleasesCapacity(t *testing.T) {
	for _, busy := range []bool{true, false} {
		name := "free slot"
		if busy {
			name = "occupied slot"
		}
		t.Run(name, func(t *testing.T) {
			s, status := newPauseSetup(t)
			floor := s.backend.local.host.floor
			mirror, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			started := make(chan *factoryrun.FakeConversation, 4)
			finishB := make(chan struct{})
			finishA := make(chan struct{})
			cancelledA := make(chan struct{})
			drainA := make(chan struct{})
			releaseDrain := sync.OnceFunc(func() { close(drainA) })
			var calls, prepared atomic.Int32
			maker := &factoryrun.FakeMaker{Script: func(ctx context.Context, c *factoryrun.FakeConversation) {
				calls.Add(1)
				started <- c
				finish := finishA
				if c.Spec.Item.Title == "B" {
					finish = finishB
				}
				select {
				case <-ctx.Done():
					return
				case <-finish:
				}
				_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
			}}
			chat := factoryrun.NewChatExecutor(maker)
			runner := factoryrun.New(factoryrun.Options{Store: mirror, Benches: 1, Exec: map[factory.StageKind]factoryrun.Executor{
				factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, job factoryrun.Job) (factory.StageResult, error) {
					if job.Stage.Name == "prepare" {
						prepared.Add(1)
						return factory.StageResult{Done: true, Notes: []string{"prepared once"}}, nil
					}
					job.Spend(0.125)
					result, err := chat.Run(ctx, job)
					if !busy && job.Resume == "" && ctx.Err() != nil {
						// Keep the cancelled executor in flight until Resume has
						// queued the job, before its final report and lease release.
						close(cancelledA)
						<-drainA
					}
					return result, err
				}),
			}})
			startPauseWorker(t, s, status, runner, mirror)
			t.Cleanup(releaseDrain)
			create := func(title string, prepare bool) int {
				t.Helper()
				stages := []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write", Until: factory.UntilDone, On: true}}
				if prepare {
					stages = append([]factory.Stage{{Name: "prepare", Kind: factory.StageChat, Ask: "prepare", Until: factory.UntilDone, On: true}}, stages...)
				}
				it, err := floor.Create(factory.Item{Repo: "acme/api", Title: title, Kind: factory.KindIssue, Assignee: "owner", Autonomy: factory.AutonomyAuto, Stages: stages})
				if err != nil {
					t.Fatal(err)
				}
				return it.ID
			}
			read := func(id int) api.Job {
				var j api.Job
				s.must("factory.job", map[string]any{"factory": status.Factory, "job": id}, &j)
				return j
			}
			act := func(id int, method string) {
				t.Helper()
				j := read(id)
				s.must(method, map[string]any{"factory": status.Factory, "job": id, "expect": map[string]any{"version": j.Version}}, &appwire.FactoryJobActed{})
			}
			wait := func(id int, what string, predicate func(api.Job) bool) api.Job {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for {
					j := read(id)
					if predicate(j) {
						return j
					}
					if time.Now().After(deadline) {
						t.Fatalf("job %d never %s: state=%s presentation=%+v", id, what, j.State, j.StatePresentation)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			next := func(title string) *factoryrun.FakeConversation {
				t.Helper()
				select {
				case c := <-started:
					if c.Spec.Item.Title != title {
						t.Fatalf("started %s, want %s", c.Spec.Item.Title, title)
					}
					return c
				case <-time.After(10 * time.Second):
					t.Fatalf("job %s never started its model round", title)
					return nil
				}
			}
			desk, err := lease.Open(floor, lease.Options{})
			if err != nil {
				t.Fatal(err)
			}
			launch := func(id int) {
				t.Helper()
				if err := lease.NewDoors(desk).Launch(id); err != nil {
					t.Fatal(err)
				}
			}
			a := create("A", true)
			launch(a)
			first := next("A")
			wait(a, "running", func(j api.Job) bool {
				return j.State == api.StateRunning && j.Cost == 0.125 && len(j.Steps) == 2 && j.Steps[0].State == api.StepDone
			})
			var b int
			var waiting api.StatePresentation
			if busy {
				b = create("B", false)
				launch(b)
				queued := wait(b, "queued behind A", func(j api.Job) bool {
					return j.State == api.StateQueued && j.StatePresentation.Kind == "waiting_for_runner"
				})
				waiting = queued.StatePresentation
			}
			act(a, "factory.job_hold")
			paused := wait(a, "paused", func(j api.Job) bool { return j.StatePresentation.Kind == "paused" && j.Can.Resume })
			if paused.State != api.StateRunning || paused.Cost != 0.125 || paused.Can.Hold {
				t.Fatalf("paused job: %+v", paused)
			}
			if busy {
				next("B")
				wait(b, "running while A is paused", func(j api.Job) bool { return j.State == api.StateRunning })
				if j := read(a); j.StatePresentation.Kind != "paused" || !j.Can.Resume || j.Cost != paused.Cost {
					t.Fatalf("A lost its pause while B runs: %+v", j)
				}
			}
			if !busy {
				select {
				case <-cancelledA:
				case <-time.After(10 * time.Second):
					t.Fatal("pause did not cancel A's executor")
				}
			}
			time.Sleep(100 * time.Millisecond)
			before := calls.Load()
			wantCalls := int32(1)
			if busy {
				wantCalls++
			}
			if before != wantCalls {
				t.Fatalf("model calls while A is paused: got %d, want %d", before, wantCalls)
			}
			act(a, "factory.job_resume")
			if busy {
				queued := wait(a, "waiting for the occupied slot", func(j api.Job) bool { return j.State == api.StateQueued && j.StatePresentation.Kind == waiting.Kind })
				if queued.StatePresentation.Title != waiting.Title || queued.StatePresentation.Detail != waiting.Detail || queued.Cost != paused.Cost || queued.Desk.Run.Paused {
					t.Fatalf("resumed job is not an ordinary queued job: %+v", queued)
				}
				time.Sleep(100 * time.Millisecond)
				if calls.Load() != before {
					t.Fatal("resumed job made a model call without a free slot")
				}
				close(finishB)
			} else {
				queued := read(a)
				if queued.State != api.StateQueued || queued.Desk.Run.Paused || queued.Cost != paused.Cost || calls.Load() != before {
					t.Fatalf("resume while cancellation drains: %+v", queued)
				}
				releaseDrain()
			}
			second := next("A")
			if second.Spec.Resume != first.ID() || second.Spec.Item.Stream.Phases[1].Round != 1 {
				t.Fatalf("resume lost conversation or round: resume=%q want=%q", second.Spec.Resume, first.ID())
			}
			close(finishA)
			wait(a, "finished with spending kept", func(j api.Job) bool { return j.Cost == 0.25 && len(j.Steps) == 2 && j.Steps[1].State == api.StepDone })
			if prepared.Load() != 1 {
				t.Fatalf("finished preparation ran %d times", prepared.Load())
			}
		})
	}
}

func startPauseWorker(t *testing.T, s *localSetup, status appwire.FactoryLocalStatus, runner *factoryrun.Runner, mirror *store.Store) {
	t.Helper()
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
}

// The fixture opens the real solo server without starting the automatic worker;
// each test supplies its own real lease worker with a scripted model instead.
func newPauseSetup(t *testing.T) (*localSetup, appwire.FactoryLocalStatus) {
	t.Helper()
	s := newLocalSetup(t)
	if err := s.backend.startLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, s.backend.localStatus()
}

// Stop is committed first: this covers that ordering, not every concurrent interleaving.
func TestFactoryPauseResumeRule(t *testing.T) {
	for _, paused := range []bool{false, true} {
		method := "factory.job_hold"
		if paused {
			method = "factory.job_resume"
		}
		t.Run(method+" stale progress", func(t *testing.T) {
			s, status := newPauseSetup(t)
			floor := s.backend.local.host.floor
			it, err := floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateRunning,
				Stream: &factory.Stream{Paused: paused, Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1}}}})
			if err != nil {
				t.Fatal(err)
			}
			params := map[string]any{"factory": status.Factory, "job": it.ID}
			var before api.Job
			s.must("factory.job", params, &before)
			if err := floor.Update(it.ID, func(it *factory.Item) error { it.Stream.Spent = .25; return nil }); err != nil {
				t.Fatal(err)
			}
			params["expect"] = map[string]any{"version": before.Version}
			var acted appwire.FactoryJobActed
			s.must(method, params, &acted)
			if acted.Current == nil || acted.Current.Desk.Run.Paused == paused || acted.Current.Cost != .25 {
				t.Fatalf("stale action did not take effect: %+v", acted.Current)
			}
		})
	}
	t.Run("pause finished job", func(t *testing.T) {
		s, status := newPauseSetup(t)
		it, err := s.backend.local.host.floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateShipped})
		if err != nil {
			t.Fatal(err)
		}
		params := map[string]any{"factory": status.Factory, "job": it.ID, "expect": map[string]any{"version": 999}}
		var before api.Job
		s.must("factory.job", params, &before)
		_, code, words := s.call("factory.job_hold", params)
		if code != appwire.CodeConflict || !strings.Contains(words, "is not running") {
			t.Fatalf("finished pause: %s %s", code, words)
		}
		var after api.Job
		s.must("factory.job", params, &after)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("finished job changed")
		}
	})

	for _, method := range []string{"factory.job_hold", "factory.job_resume"} {
		t.Run(method+" after stop", func(t *testing.T) {
			s, status := newPauseSetup(t)
			floor := s.backend.local.host.floor
			it, err := floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateRunning,
				Stream: &factory.Stream{Paused: true, Spent: .25, Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, Chat: "saved-chat"}}}})
			if err != nil {
				t.Fatal(err)
			}
			params := map[string]any{"factory": status.Factory, "job": it.ID}
			var before api.Job
			s.must("factory.job", params, &before)
			params["expect"] = map[string]any{"version": before.Version}
			s.must("factory.job_stop", params, &appwire.FactoryJobActed{})
			var stopped api.Job
			s.must("factory.job", params, &stopped)
			var activity appwire.FactoryActivity
			s.must("factory.activity", map[string]any{"factory": status.Factory}, &activity)
			_, code, words := s.call(method, params)
			want := "is not paused"
			if method == "factory.job_hold" {
				want = "is not running"
			}
			if code != appwire.CodeConflict || !strings.Contains(words, want) {
				t.Errorf("%s: code=%s words=%q, want conflict containing %q", method, code, words, want)
			}
			var after api.Job
			s.must("factory.job", params, &after)
			var recorded appwire.FactoryActivity
			s.must("factory.activity", map[string]any{"factory": status.Factory}, &recorded)
			if !reflect.DeepEqual(stopped, after) || !reflect.DeepEqual(activity, recorded) {
				t.Fatal("refused action changed job or record")
			}
			desk, err := lease.Open(floor, lease.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := desk.Pull(lease.Worker{ID: "resume-fixture", Handle: "owner", Solo: true}); err != lease.ErrNoJob {
				t.Fatalf("stopped job became runnable: %v", err)
			}
		})
	}
}

func TestFactoryResumeChat(t *testing.T) {
	for _, leased := range []bool{false, true} {
		name := "unleased"
		if leased {
			name = "leased"
		}
		t.Run(name, func(t *testing.T) {
			s, status := newPauseSetup(t)
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
			}
			floor := s.backend.local.host.floor
			it, err := floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateQueued,
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, Chat: "saved-chat"}}}})
			if err != nil {
				t.Fatal(err)
			}
			if leased {
				desk, err := lease.Open(floor, lease.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := desk.Pull(lease.Worker{ID: "chat-fixture", Handle: "owner", Solo: true}); err != nil {
					t.Fatal(err)
				}
			}
			params := map[string]any{"factory": status.Factory, "job": it.ID}
			var chats appwire.FactoryJobChats
			s.must("factory.job_chats", params, &chats)
			act := func(method string) {
				t.Helper()
				var j api.Job
				s.must("factory.job", params, &j)
				params["expect"] = map[string]any{"version": j.Version}
				s.must(method, params, &appwire.FactoryJobActed{})
			}
			act("factory.job_hold")
			act("factory.job_resume")
			act("factory.job_resume")
			var page appwire.EntriesResult
			s.must("agents.entries", map[string]any{"id": chats.Agent, "limit": 200}, &page)
			count := 0
			for _, e := range page.Entries {
				if e.Text == "resumed" {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("manager chat has %d resumed lines, want 1", count)
			}
		})
	}
}

// The journal is the external failure seam; a refused append must stay owed.
type retryResumeTalk struct {
	sessionTalk
	failed atomic.Bool
}

func (talk *retryResumeTalk) Say(path, line string, step *factory.StepProgress) error {
	if line == "resumed" && !talk.failed.Swap(true) {
		return errors.New("journal temporarily unavailable")
	}
	return talk.sessionTalk.Say(path, line, step)
}

func TestFactoryResumeChatRetries(t *testing.T) {
	s, status := newPauseSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateRunning, Stream: &factory.Stream{Paused: true}})
	if err != nil {
		t.Fatal(err)
	}
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": status.Factory, "job": it.ID}, &chats)
	desk, err := lease.Open(floor, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(desk).WithTalk(&retryResumeTalk{})
	if err := doors.Hold(it.ID, false); err != nil {
		t.Fatalf("Resume failed after queueing: %v", err)
	}
	if err := doors.Hold(it.ID, false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var page appwire.EntriesResult
		s.must("agents.entries", map[string]any{"id": chats.Agent, "limit": 200}, &page)
		count := 0
		for _, e := range page.Entries {
			if e.Text == "resumed" {
				count++
			}
		}
		if count == 1 {
			break
		}
		if count > 1 || time.Now().After(deadline) {
			t.Fatalf("retry wrote %d resumed lines, want 1", count)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
