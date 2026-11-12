package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
)

func TestFactoryAgentFieldPipelineHandsPullRequestToReview(t *testing.T) {
	for _, opened := range []bool{true, false} {
		t.Run(fmt.Sprintf("opened=%t", opened), func(t *testing.T) {
			g := newAFLeaseQueueRig(t, nil)
			h := serve.New(factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "me"}, nil }}, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "me", Store: g.st})
			server := httptest.NewServer(h)
			t.Cleanup(func() { server.Close(); h.Close() })
			w := newFactoryWire(t, testFactoryHub(t))
			var conn appwire.FactoryConnection
			w.must("factory.attach", map[string]any{"url": server.URL, "token": testHostToken}, &conn)
			text := "pipeline:\n  build-review: [build, review]\nsteps:\n  build:\n    kind: agentfield\n    target: swe-planner.build_issue\n    input:\n      title: '{title}'\n  review:\n    kind: agentfield\n    target: pr-af.review\n    input:\n      pr_url: '{pr_url}'\n"
			var library appwire.FactoryLibrary
			w.must("factory.library_set", map[string]any{"factory": conn.ID, "text": text, "expect": map[string]any{"version": 1}}, &library)
			file, err := withFactoryHouseRules(g.st, pipeline.Defaults(), nil)
			if err != nil {
				t.Fatal(err)
			}
			resolved, ok := file.Resolve("build-review")
			if !ok || len(resolved.Stages) != 2 {
				t.Fatalf("saved pipeline did not resolve: %+v", resolved)
			}
			const link = "https://github.com/acme/api/pull/42"
			g.cp.set(func() {
				g.cp.tree = afNode("exec-root", "build_issue", "succeeded", "2026-11-07T12:00:00Z", "2026-11-07T12:00:02Z")
				g.cp.result = json.RawMessage(`{"success":true,"summary":"The answer is ready."}`)
				if opened {
					g.cp.result = json.RawMessage(`{"success":true,"pr_url":"` + link + `"}`)
				}
			})
			job, err := g.st.Create(factory.Item{Kind: factory.KindIssue, Stages: resolved.Stages, Title: "Build then review", Repo: "acme/api", State: factory.StateNew, Tier: factory.TierOwner, Assignee: "me"})
			if err != nil {
				t.Fatal(err)
			}
			_, _, releases, quit := g.start(1)
			if err := lease.NewDoors(g.desk).WithStartRead(func(context.Context, factory.Item) (pipeline.File, error) { return file, nil }).Launch(job.ID); err != nil {
				t.Fatal(err)
			}
			got := afWaitJob(t, g.st, job.ID, "completed build and review", func(it factory.Item) bool { return it.State == factory.StateShipped || it.Failure != nil })
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
		released:
			for {
				select {
				case held := <-releases:
					if held.Job == job.ID && held.ID == "" {
						break released
					}
				case <-timer.C:
					t.Fatal("the completed worker did not release its lease")
				}
			}
			quit()
			g.cp.mu.Lock()
			defer g.cp.mu.Unlock()
			if !opened {
				if len(g.cp.dispatched) != 1 || got.Failure == nil || !strings.Contains(got.Failure.Detail, "this step needs {pr_url}") {
					t.Fatalf("missing PR did not stop review before dispatch: calls=%+v failure=%+v", g.cp.dispatched, got.Failure)
				}
				return
			}
			if got.State != factory.StateShipped || len(g.cp.dispatched) != 2 {
				t.Fatalf("pipeline did not finish both stages: state=%s calls=%+v failure=%+v", got.State, g.cp.dispatched, got.Failure)
			}
			if g.cp.dispatched[0]["_target"] != "swe-planner.build_issue" || g.cp.dispatched[1]["_target"] != "pr-af.review" || g.cp.dispatched[1]["pr_url"] != link {
				t.Fatalf("build-to-review handoff: %+v", g.cp.dispatched)
			}
			if got.PullRequest == nil || got.PullRequest.URL != link || got.Stream == nil || len(got.Stream.Phases) != 2 {
				t.Fatalf("pipeline result: %+v", got)
			}
			for _, ph := range got.Stream.Phases {
				if ph.State != factory.PhaseDone {
					t.Errorf("%s state=%s", ph.Name, ph.State)
				}
			}
			t.Run("completion wording", func(t *testing.T) {
				for i, want := range []string{"finished and opened " + link, "finished"} {
					ph := got.Stream.Phases[i]
					if ph.Result == nil || ph.Result.Output != want {
						t.Errorf("%s result=%+v, want %q", ph.Name, ph.Result, want)
					}
					wantLog := ph.Name + ": " + want
					found := false
					for _, line := range got.Stream.Log {
						if strings.HasPrefix(line.Text, ph.Name+": finished") {
							found = true
							if line.Text != wantLog {
								t.Errorf("run log=%q, want %q", line.Text, wantLog)
							}
						}
					}
					if !found {
						t.Errorf("run log is missing %q", wantLog)
					}
				}
			})
		})
	}
}

func TestFactoryAgentFieldResumedStepKeepsPullRequestAttribution(t *testing.T) {
	const link = "https://github.com/acme/api/pull/42"
	for _, tc := range []struct {
		name, result, want string
		existing           bool
		wantPullRequest    bool
	}{
		{"opening step", `{"success":true,"pr_url":"` + link + `"}`, "finished and opened " + link, false, true},
		{"no pull request", `{"success":true}`, "finished", false, false},
		{"review echoes pull request", `{"success":true,"pr_url":"` + link + `"}`, "finished", true, true},
		{"review names another pull request", `{"success":true,"pr_url":"https://github.com/acme/api/pull/43"}`, "finished", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, it, stage := afStepJob(t)
			if tc.existing {
				stage = factory.Stage{Name: "review", Kind: factory.StageAgentField, Target: "pr-af.review", On: true, Input: map[string]string{"pr_url": "{pr_url}"}}
				if err := st.Update(it.ID, func(it *factory.Item) error {
					it.Stages = append(it.Stages, stage)
					it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "build", State: factory.PhaseDone}, {Name: "review", State: factory.PhaseRunning}}}
					it.PullRequest = &factory.PullRequest{URL: link, Number: 42, State: factory.PROpen}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			cp := &afFakeCP{tree: afNode("exec-root", "work", "running", "2026-11-07T12:00:00Z", "",
				afNode("exec-child", "work", "succeeded", "2026-11-07T12:00:00Z", "2026-11-07T12:00:01Z")), result: json.RawMessage(tc.result)}
			remote := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "fixture-node", true }}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			guard.Go("test interrupted AgentField step", func() {
				_, err := remote.Step(ctx, factoryrun.Job{Item: it, Stage: stage})
				done <- err
			})
			afUntil(t, st, it.ID, "run recorded before restart", func(it factory.Item) bool {
				return it.AgentFieldSteps[stage.Name] != nil && (!tc.wantPullRequest || it.PullRequest != nil)
			})
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("interrupted step did not return")
			}
			cp.set(func() {
				ended := "2026-11-07T12:00:02Z"
				cp.tree.Status, cp.tree.CompletedAt = "succeeded", &ended
			})
			reopened, err := store.Open(st.Root())
			if err != nil {
				t.Fatal(err)
			}
			it, _ = reopened.Get(it.ID)
			remote = &afRemote{items: reopened, floor: reopened, conn: func() (afClient, string, bool) { return cp, "fixture-node", true }}
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			res, err := remote.Step(ctx, factoryrun.Job{Item: it, Stage: stage, Continuing: true})
			if err != nil || !res.Done || res.Output != tc.want {
				t.Errorf("resumed step result=%+v error=%v, want %q", res, err, tc.want)
			}
			end, _ := reopened.Get(it.ID)
			if tc.wantPullRequest {
				if end.PullRequest == nil || end.PullRequest.URL != link {
					t.Errorf("first pull request changed: %+v", end.PullRequest)
				}
			} else if end.PullRequest != nil {
				t.Errorf("invented a pull request: %+v", end.PullRequest)
			}
			if len(cp.dispatched) != 1 {
				t.Errorf("restart dispatched another run: %+v", cp.dispatched)
			}
		})
	}
}

func TestFactoryAgentFieldLegacyStepKeepsPullRequestAttribution(t *testing.T) {
	const link = "https://github.com/acme/api/pull/42"
	for _, tc := range []struct {
		name, step string
		receipt    bool
	}{{"opening build", "build", false}, {"review with receipt", "review", true}, {"review without receipt", "review", false}} {
		t.Run(tc.name, func(t *testing.T) {
			st, it, build := afStepJob(t)
			review := factory.Stage{Name: "review", Kind: factory.StageAgentField, Target: "pr-af.review", On: true, Input: map[string]string{"pr_url": "{pr_url}"}}
			if tc.receipt {
				review.Input = nil
			}
			stage, want := build, "finished and opened "+link
			phases := []factory.Phase{{Name: "build", State: factory.PhaseRunning}, {Name: "review", State: factory.PhasePending}}
			if tc.step == "review" {
				stage, want = review, "finished"
				phases[0].State = factory.PhaseDone
				if tc.receipt {
					phases[0].Result = &factory.StageResult{Done: true, Output: "finished and opened " + link}
				}
				phases[1].State = factory.PhaseRunning
			}
			if err := st.Update(it.ID, func(it *factory.Item) error {
				it.Stages, it.Stream = []factory.Stage{build, review}, &factory.Stream{Phases: phases}
				it.PullRequest = &factory.PullRequest{URL: link, Number: 42, State: factory.PROpen}
				it.AgentFieldSteps = map[string]*factory.AgentFieldRun{tc.step: {ExecutionID: "exec-root", RunID: "run-1", EndOwed: true}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			reopened, err := store.Open(st.Root())
			if err != nil {
				t.Fatal(err)
			}
			it, _ = reopened.Get(it.ID)
			cp := &afFakeCP{tree: afNode("exec-root", "work", "succeeded", "2026-11-07T12:00:00Z", "2026-11-07T12:00:02Z"), result: json.RawMessage(`{"success":true,"pr_url":"` + link + `"}`)}
			remote := &afRemote{items: reopened, floor: reopened, conn: func() (afClient, string, bool) { return cp, "fixture-node", true }}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			res, err := remote.Step(ctx, factoryrun.Job{Item: it, Stage: stage, Continuing: true})
			if err != nil || !res.Done || res.Output != want || len(cp.dispatched) != 0 {
				t.Errorf("legacy %s result=%+v error=%v dispatches=%d, want %q", tc.step, res, err, len(cp.dispatched), want)
			}
		})
	}
}

func TestAgentFieldReviewRefusesUnavailablePullRequestBinding(t *testing.T) {
	for _, why := range []string{"no PR", "build still running", "review is first"} {
		t.Run(why, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			review := factory.Stage{Name: "review", Kind: factory.StageAgentField, Target: "pr-af.review", On: true, Input: map[string]string{"pr_url": "{pr_url}"}}
			phases := []factory.Phase{{Name: "build", Kind: factory.StageAgentField, State: factory.PhaseDone}, {Name: "review", Kind: factory.StageAgentField, State: factory.PhaseRunning}}
			pr := &factory.PullRequest{URL: "https://github.com/acme/api/pull/42", Number: 42}
			switch why {
			case "no PR":
				pr = nil
			case "build still running":
				phases[0].State = factory.PhaseRunning
			case "review is first":
				phases[0], phases[1] = phases[1], phases[0]
			}
			it, err := st.Create(factory.Item{Title: "Review", Repo: "acme/api", State: factory.StateRunning, Stages: []factory.Stage{{Name: "build", Kind: factory.StageAgentField, On: true}, review}, Stream: &factory.Stream{Phases: phases}, PullRequest: pr})
			if err != nil {
				t.Fatal(err)
			}
			cp := &afFakeCP{tree: afNode("exec-root", "review", "succeeded", "2026-11-07T12:00:00Z", "2026-11-07T12:00:02Z"), result: json.RawMessage(`{"success":true}`)}
			remote := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return cp, "fixture-node", true }}
			_, err = remote.Step(context.Background(), factoryrun.Job{Item: it, Stage: review})
			if err == nil || !strings.Contains(err.Error(), "pull request") || len(cp.dispatched) != 0 {
				t.Fatalf("unavailable PR binding dispatched review: error=%v calls=%+v", err, cp.dispatched)
			}
		})
	}
}
