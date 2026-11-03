package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The wire reads a real solo factory; the real runner finishes proof. Only
// the brief model is scripted, so these checks prove inputs and scheduling.
func TestAppFactoryBriefAfterProof(t *testing.T) {
	t.Run("publish wait refreshes despite cost guard", func(t *testing.T) {
		s, door, id, params := briefAfterProofSetup(t, factory.StateNew)
		proof := make(chan struct{})
		release := make(chan struct{})
		defer close(release)
		r := factoryrun.New(factoryrun.Options{Store: s.backend.local.host.floor, Exec: map[factory.StageKind]factoryrun.Executor{
			factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, job factoryrun.Job) (factory.StageResult, error) {
				if job.Stage.Name == "proof" {
					close(proof)
					select {
					case <-release:
					case <-ctx.Done():
						return factory.StageResult{}, ctx.Err()
					}
				}
				return factory.StageResult{Done: true, Output: "Checked the change", Claims: []factory.Claim{{Text: "The change works", OK: true, Evidence: "scripted check", Medium: "test"}}}, nil
			}),
		}})
		if err := r.Launch(id); err != nil {
			t.Fatal(err)
		}
		// Manual asks before proof; only the answer lets the runner reach it.
		reached := time.After(5 * time.Second)
	toProof:
		for {
			select {
			case <-proof:
				break toProof
			case <-reached:
				t.Fatal("runner never reached proof")
			case <-time.After(5 * time.Millisecond):
				if cur, _ := s.backend.local.host.floor.Get(id); cur.QKind == factory.QKindApprove {
					_ = r.Answer(id, true, "")
				}
			}
		}
		var written appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", params, &written)
		if !written.Kept || written.Current.Brief.AfterStep != "review" {
			t.Fatalf("seed brief = %+v", written)
		}
		cur, _ := s.backend.local.host.floor.Get(id)
		if factory.BriefCostAllowed(cur) {
			t.Fatal("fixture does not refuse step writes")
		}
		// Keep release owned by the defer, and let the executor continue now.
		release <- struct{}{}
		read := briefAfterProofRead(t, s, params, func(g map[string]any) bool { return briefRev(g) == 2 })
		if briefBody(read)["after_step"] != "proof" {
			t.Fatalf("after_step = %v", briefBody(read)["after_step"])
		}
		door.mu.Lock()
		input := door.asks[len(door.asks)-1].Messages[0].Text
		door.mu.Unlock()
		if !strings.Contains(input, `"waiting_on_proof_approval":true`) {
			t.Fatalf("writer lacks proof approval wait: %s", input)
		}
	})
	t.Run("publication refresh names the PR", func(t *testing.T) {
		s, door, id, params := briefAfterProofSetup(t, factory.StateLanded)
		var first appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", params, &first)
		// A publication must change the input even when proof already finished.
		before, _ := s.backend.local.host.floor.Get(id)
		briefPublishReceipt(t, s, id)
		after, _ := s.backend.local.host.floor.Get(id)
		if factory.BriefInputHash(factory.BriefInputOf(before)) == factory.BriefInputHash(factory.BriefInputOf(after)) {
			t.Error("publication leaves brief input hash unchanged")
		}
		read := briefAfterProofRead(t, s, params, func(g map[string]any) bool { return briefRev(g) == 2 })
		if read["verdict_behind"] != false {
			t.Errorf("fresh verdict_behind = %v", read["verdict_behind"])
		}
		door.mu.Lock()
		input := door.asks[len(door.asks)-1].Messages[0].Text
		door.mu.Unlock()
		if !strings.Contains(input, `"number":73`) || !strings.Contains(input, `"state":"open"`) {
			t.Fatalf("writer lacks PR receipt: %s", input)
		}
	})
	t.Run("failed end write keeps an out of date verdict", func(t *testing.T) {
		s, door, id, params := briefAfterProofSetup(t, factory.StateLanded)
		var first appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", params, &first)
		door.mu.Lock()
		door.answer = session.ErrandAnswer{Reason: session.ErrandDeadline}
		door.mu.Unlock()
		briefPublishReceipt(t, s, id)
		read := briefAfterProofRead(t, s, params, func(g map[string]any) bool { return g["status"] == factory.BriefStatusFailed })
		if briefRev(read) != 1 || briefBody(read)["verdict"] != first.Current.Brief.Verdict || read["verdict_behind"] != true {
			t.Fatalf("old verdict not marked behind: %+v", read)
		}
	})

	t.Run("lifecycle outcomes refresh", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			change func(*factory.Item)
		}{
			{"merged", func(it *factory.Item) { it.PullRequest = &factory.PullRequest{Number: 73, State: factory.PRMerged} }},
			{"closed without merge", func(it *factory.Item) { it.PullRequest = &factory.PullRequest{Number: 73, State: factory.PRClosed} }},
			{"shipped", func(it *factory.Item) { it.State = factory.StateShipped }},
			{"stopped", func(it *factory.Item) {
				it.State = factory.StateNew
				it.Failure = &factory.Failure{Code: factory.CodeStopped}
				it.Stream.Ended = time.Now()
			}},
			{"failed", func(it *factory.Item) {
				it.State = factory.StateNew
				it.Failure = &factory.Failure{Code: factory.CodeToolError, Detail: "check failed"}
				it.Stream.Ended = time.Now()
			}},
			{"comment posted", func(it *factory.Item) {
				it.State = factory.StateShipped
				it.CommentURL = "https://github.com/acme/api/issues/7#issuecomment-73"
			}},
			{"publish failed", func(it *factory.Item) {
				it.MarkPublishing("failed", "no_push_permission", "permission refused", time.Now())
			}},
			{"PR updated", func(it *factory.Item) {
				it.MarkPublishing("queued", "", "", time.Now())
				it.MarkPublishing("opened", "", "", time.Now())
			}},
		} {
			t.Run(c.name, func(t *testing.T) {
				s, _, id, params := briefAfterProofSetup(t, factory.StateLanded)
				var first appwire.FactoryJobBriefWritten
				s.must("factory.job_brief_write", params, &first)
				if !first.Kept {
					t.Fatalf("seed = %+v", first)
				}
				if err := s.backend.local.host.floor.Update(id, func(it *factory.Item) error { c.change(it); return nil }); err != nil {
					t.Fatal(err)
				}
				briefAfterProofRead(t, s, params, func(g map[string]any) bool { return briefRev(g) == 2 })
			})
		}
	})
	t.Run("Auto proof passage does not force a write", func(t *testing.T) {
		s, _, id, params := briefAfterProofSetup(t, factory.StateRunning)
		if err := s.backend.local.host.floor.Update(id, func(it *factory.Item) error {
			it.Autonomy = factory.AutonomyAuto
			it.Assignee = "owner"
			it.Cap = 5
			it.Proof = []factory.Claim{{Text: "checked", OK: true}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var first appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", params, &first)
		if err := s.backend.local.host.floor.Update(id, func(it *factory.Item) error {
			it.State = factory.StateLanded
			if why := factory.AutonomyBlockReason(*it); why != "" {
				t.Fatalf("Auto fixture is blocked: %s", why)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		var got map[string]any
		s.must("factory.job_brief", params, &got)
		if briefRev(got) != 1 {
			t.Fatalf("Auto forced an unreadable intermediate version: %+v", got)
		}
	})
	t.Run("read freshness follows newer steps and recovers", func(t *testing.T) {
		s, _, id, params := briefAfterProofSetup(t, factory.StateRunning)
		var got map[string]any
		s.must("factory.job_brief", params, &got)
		if got["verdict_behind"] != false {
			t.Fatalf("stub freshness = %+v", got)
		}
		if !strings.Contains(s.hello, appwire.FeatureFactoryJobBriefVerdictBehind) {
			t.Fatal("hello lacks brief freshness feature")
		}
		var first appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", params, &first)
		if first.Current.VerdictBehind {
			t.Fatal("fresh write marked behind")
		}
		if err := s.backend.local.host.floor.Update(id, func(it *factory.Item) error {
			it.Stream.Phases = append(it.Stream.Phases, factory.Phase{Name: "later", State: factory.PhaseRunning, Round: 1})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		s.must("factory.job_brief", params, &got)
		if got["verdict_behind"] != true {
			t.Fatalf("newer step not marked behind: %+v", got)
		}
		s.must("factory.job_brief_write", params, &first)
		if !first.Kept || first.Current.VerdictBehind {
			t.Fatalf("refresh = %+v", first)
		}
	})

	t.Run("end during a write is retried", func(t *testing.T) {
		s, door, id, params := briefAfterProofSetup(t, factory.StateLanded)
		held := &heldBriefDoor{briefDoor: door, entered: make(chan struct{}), release: make(chan struct{})}
		defer close(held.release)
		s.backend.open = func(ask appOpenAsk) (*appConn, error) {
			return &appConn{Conv: held, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
		}
		written := make(chan error, 1)
		go func() {
			_, err := s.backend.FactoryJobBriefWrite(context.Background(), appwire.FactoryJobBriefWriteParams{Factory: params["factory"].(string), Job: id})
			written <- err
		}()
		select {
		case <-held.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("model never entered the first write")
		}
		briefPublishReceipt(t, s, id)
		// The first answer remains blocked beyond the test host's debounce.
		time.Sleep(100 * time.Millisecond)
		held.release <- struct{}{}
		select {
		case err := <-written:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("first write never finished")
		}
		briefAfterProofRead(t, s, params, func(g map[string]any) bool { return briefRev(g) == 2 && g["verdict_behind"] == false })
	})
	t.Run("stop without an active round refreshes", func(t *testing.T) {
		s, _, id, params := briefAfterProofSetup(t, factory.StateQueued)
		var first appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", params, &first)
		r := factoryrun.New(factoryrun.Options{Store: s.backend.local.host.floor})
		if err := r.Stop(id); err != nil {
			t.Fatal(err)
		}
		cur, _ := s.backend.local.host.floor.Get(id)
		if cur.Failure != nil {
			t.Fatalf("stop cut a step unexpectedly: %+v", cur.Failure)
		}
		briefAfterProofRead(t, s, params, func(g map[string]any) bool { return briefRev(g) == 2 })
	})

	t.Run("stop before a run starts refreshes", func(t *testing.T) {
		s, door, id, params := briefAfterProofSetup(t, factory.StateNeedsYou)
		if err := s.backend.local.host.floor.Update(id, func(it *factory.Item) error {
			it.Stream = nil
			it.Body = "Retries send the message twice when a worker restarts in the middle of one send."
			it.QKind = factory.QKindAskFirst
			it.Question = "Start this job?"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var first appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", params, &first)
		r := factoryrun.New(factoryrun.Options{Store: s.backend.local.host.floor})
		if err := r.Stop(id); err != nil {
			t.Fatal(err)
		}
		briefAfterProofRead(t, s, params, func(g map[string]any) bool { return briefRev(g) == 2 })
		door.mu.Lock()
		input := door.asks[len(door.asks)-1].Messages[0].Text
		door.mu.Unlock()
		if !strings.Contains(input, `"end":"stopped"`) {
			t.Fatalf("writer lacks stopped outcome: %s", input)
		}
	})
	t.Run("plain step still costs too much", func(t *testing.T) {
		it := factory.Item{Stream: &factory.Stream{Spent: 0.01, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}}}, Briefs: []factory.BriefVersion{{AfterStep: "plan", CostUSD: 0.02, InputHash: "old"}}}
		if factory.BriefCostAllowed(it) {
			t.Fatal("plain step allowed over the spend share")
		}
		if got := factory.BriefBeginSkip(it, factory.BriefTriggerStep, "new", time.Now()); got != factory.BriefSkipCost {
			t.Fatalf("step skip = %q", got)
		}
	})
	t.Run("PR number requires PR evidence", func(t *testing.T) {
		it := factory.Item{PullRequest: &factory.PullRequest{Number: 73, State: factory.PROpen}}
		for _, c := range []struct {
			draft string
			valid bool
		}{
			{`{"verdict":"PR 73 is open.","verdict_evidence":[{"kind":"pr"}],"blocks":[]}`, true},
			{`{"verdict":"PR 74 is open.","verdict_evidence":[{"kind":"pr"}],"blocks":[]}`, false},
			{`{"verdict":"PR 73 is open.","blocks":[]}`, false},
		} {
			_, _, fault := factory.CheckBriefDraft(it, []byte(c.draft))
			if (fault == "") != c.valid {
				t.Errorf("valid=%v fault=%q", c.valid, fault)
			}
		}
	})
}

func briefAfterProofSetup(t *testing.T, state factory.State) (*localSetup, *briefDoor, int, map[string]any) {
	t.Helper()
	s := newLocalSetup(t)
	door := &briefDoor{fakeConversation: &fakeConversation{}, answer: session.ErrandAnswer{Text: `{"verdict":"The change is checked.","blocks":[]}`, Model: "scripted", Usage: session.ErrandUsage{CostUSD: 0.02}}}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: door, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	s.backend.briefs.stop()
	s.backend.briefs = newBriefDebounce(5*time.Millisecond, s.backend.writeBriefLater)
	it, err := s.backend.local.host.floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Fix retries", Kind: factory.KindIssue, State: state, Autonomy: factory.AutonomyManual,
		Stages: []factory.Stage{{Name: "review", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageChat, Until: factory.UntilProven, On: true}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, door, it.ID, map[string]any{"factory": st.Factory, "job": it.ID}
}

func briefPublishReceipt(t *testing.T, s *localSetup, id int) {
	t.Helper()
	if err := s.backend.local.host.floor.Update(id, func(it *factory.Item) error {
		it.PullRequest = &factory.PullRequest{Number: 73, URL: "https://github.com/acme/api/pull/73", State: factory.PROpen}
		return factoryrun.ShipPublished(it, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
}

func briefBody(g map[string]any) map[string]any { return g["brief"].(map[string]any) }
func briefRev(g map[string]any) float64         { return briefBody(g)["rev"].(float64) }
func briefAfterProofRead(t *testing.T, s *localSetup, params map[string]any, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	var got map[string]any
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.must("factory.job_brief", params, &got)
		if ok(got) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, _ := json.Marshal(got)
	t.Fatalf("brief never reached expected version/status: %s", raw)
	return nil
}

// heldBriefDoor holds the first answer while the job reaches publication.
type heldBriefDoor struct {
	*briefDoor
	first   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (d *heldBriefDoor) RunErrand(ctx context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	if d.first.CompareAndSwap(false, true) {
		close(d.entered)
		select {
		case <-d.release:
		case <-ctx.Done():
			return session.ErrandAnswer{Reason: session.ErrandDeadline}, nil
		}
	}
	return d.briefDoor.RunErrand(ctx, ask)
}
