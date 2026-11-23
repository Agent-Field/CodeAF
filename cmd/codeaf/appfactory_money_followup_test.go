package main

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A paid Follow PR attempt and a later manager turn retain one job identity.
// The repair reports needs_you, so this fixture neither publishes nor pushes.
func TestAppFactoryFollowPRAndPostPublishMoney(t *testing.T) {
	s := newLocalSetup(t)
	t.Setenv("OPENROUTER_API_KEY", "fixture")
	var calls atomic.Int64
	server := moneyProvider(t, &calls)
	g := newRunRig(t)
	freshEngineProcess(t)
	freshEngineFactoryStore(t)
	t.Setenv(factoryBenchesEnv, "")
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
	s.backend.emit = s.server
	s.backend.local.parent = func() session.Config {
		return session.Config{Workspace: g.workspace, Model: "deepseek/deepseek-v4.1-flash", OneModel: true, APIKey: "fixture", BaseURL: server.URL, DeterministicNames: true}
	}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		agent := moneyAgent(t, s, ask, server.URL)
		return &appConn{Conv: agent, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = agent.Close() }}, nil
	}
	// This local runner uses the captured PR snapshot. Keep the automatic
	// worker off so it cannot also lease the same fixture job.
	var connected atomic.Bool
	var runner *factoryrun.Runner
	hook := s.backend.local.seamHook
	s.backend.local.seamHook = func(seam factory.Seam) factory.Seam {
		seam = hook(seam)
		seam.ConnectGitHub = func(context.Context, string) error { return nil }
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
			if !connected.Load() {
				return factory.GitHubLink{}, nil
			}
			return factory.GitHubLink{Login: "owner"}, nil
		}
		seam.Launch = func(id int) error { return runner.Launch(id) }
		return seam
	}
	st := s.start()
	if st.Runner {
		t.Fatal("fixture started an automatic worker")
	}
	connected.Store(true)
	if err := s.backend.local.host.seam.ConnectGitHub(context.Background(), "fixture"); err != nil {
		t.Fatal(err)
	}
	s.backend.briefs.stop()
	s.backend.briefs = newBriefDebounce(time.Hour, s.backend.writeBriefLater)
	floor := s.backend.local.host.floor
	parent := s.backend.local.parent()
	maker := factoryStageMaker(floor, g.workspace, "", parent)
	runner = factoryrun.New(factoryrun.Options{Store: floor, Workdir: func(factory.Item) (string, error) { return g.workspace, nil }, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryChatStage(floor, "", factoryrun.NewChatExecutor(maker))}})
	stopAll(t, floor, runner)
	if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
		t.Fatal(err)
	}
	item := factory.Item{Repo: "acme/api", Num: 7, Kind: factory.KindIssue, Title: "Repair the PR retry policy", Tier: factory.TierOwner, Model: "deepseek/deepseek-v4.1-flash", Cap: 1, State: factory.StateNew,
		PullRequest: &factory.PullRequest{Number: 73, State: factory.PROpen}, FollowUp: &factory.PRFollowUp{Kind: "pr", State: "queued", Attempt: 1, Limit: 3}}
	factory.EnsureFollowPR(&item)
	it, err := floor.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"factory": st.Factory, "job": it.ID}
	var job api.Job
	s.must("factory.job", params, &job)
	s.must("factory.job_start", map[string]any{"factory": st.Factory, "job": it.ID, "expect": map[string]any{"version": job.Version}}, &appwire.FactoryJobActed{})
	deadline := time.Now().Add(15 * time.Second)
	for {
		s.must("factory.job", params, &job)
		if calls.Load() > 0 && job.State == api.StateNeedsYou {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("paid repair never settled: calls=%d job=%+v", calls.Load(), job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	moneyTotals(t, s, st.Factory, it, &calls, float64(calls.Load())*.01)
	// A captured publication receipt is a fixture fact, with no forge writes.
	if err := floor.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateShipped
		cur.Stream.Ended = time.Now()
		cur.PullRequest = &factory.PullRequest{Number: 73, State: factory.PROpen}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	makeJobManager(t, s, it.ID)
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", params, &chats)
	before := calls.Load()
	var turn appwire.TurnResult
	s.must("agents.send", map[string]any{"id": chats.Agent, "text": "Explain the published repair."}, &turn)
	deadline = time.Now().Add(5 * time.Second)
	for {
		finished := func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, line := range s.lines {
				var event struct {
					Event string           `json:"event"`
					Data  appwire.TurnDone `json:"data"`
				}
				if json.Unmarshal([]byte(line), &event) == nil && event.Event == appwire.EventTurnDone && event.Data.Agent == chats.Agent && event.Data.Turn == turn.Turn {
					if !event.Data.OK {
						t.Fatalf("post-publish turn failed: %+v", event.Data)
					}
					return true
				}
			}
			return false
		}()
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("post-publish turn never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != before+1 {
		t.Fatalf("post-publish calls=%d, want %d", calls.Load(), before+1)
	}

	moneyTotals(t, s, st.Factory, it, &calls, float64(calls.Load())*.01)
}
