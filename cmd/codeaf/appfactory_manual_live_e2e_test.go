//go:build e2e

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE LIVE MANUAL RUN: the app's own doors on one real model. A Manual job's
// Start runs its first step, then every later step waits for Approve; Request
// changes sends back the step before and the ask comes again. No GitHub.
// Opt-in (CODEAF_LIVE_FACTORY=1), a few cents.
func TestLiveManualAsksBeforeEveryStepOnOneModel(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends a few cents on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	keys := config.NewAppCredentials()
	t.Cleanup(config.UseAppCredentials(keys))
	keys.Set(modelsource.DefaultID, liveFactoryKey)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)

	g := newRunRig(t)
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Model: liveFactoryModel, OneModel: true, Workspace: g.workspace})
	if err != nil {
		t.Fatal(err)
	}
	assertFactoryPin(t, launch.Config, liveFactoryModel)
	stageSettle = 300 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })

	w := newFactoryWire(t, testFactoryHub(t))
	// This wire opens no agents, so the background brief writer stays off.
	w.backend.briefs.stop()
	w.backend.briefs = newBriefDebounce(time.Hour, func(int, string) {})
	local := &w.backend.local
	local.githubHTTP = (&refusingGitHub{}).client()
	local.rules = func(context.Context, string, string) (pipeline.File, error) { return pipeline.File{}, os.ErrNotExist }
	local.parent = func() session.Config { return launch.Config }
	local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
		return seam
	}
	var st appwire.FactoryLocalStatus
	w.must("factory.local_start", map[string]any{"caller": "live"}, &st)
	floor := local.host.floor
	if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
		t.Fatal(err)
	}
	const say = "Then call stage_result with done true, findings 0 and a one-line output. Do nothing else."
	id, err := floor.Add(context.Background(), factory.Item{
		Title: "write the greeting", Repo: "acme/api", Tier: factory.TierOwner, Cap: 2, Assignee: "owner", Autonomy: factory.AutonomyManual,
		Stages: []factory.Stage{
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done", Ask: "Create the file greet/HELLO.txt with the write tool, its content exactly `hello factory`. " + say},
			{Name: "verify", Kind: factory.StageCheck, On: true, Ask: "grep -qx 'hello factory' greet/HELLO.txt"},
			{Name: "review", Kind: factory.StageChat, On: true, Until: "done", Ask: "Read greet/HELLO.txt. " + say},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := func() api.Job {
		var j api.Job
		w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &j)
		return j
	}
	waitFor := func(what string, ok func(factory.Item) bool) factory.Item {
		t.Helper()
		for end := time.Now().Add(4 * time.Minute); ; time.Sleep(250 * time.Millisecond) {
			it, err := floor.Get(id)
			if err == nil && ok(it) {
				return it
			}
			if time.Now().After(end) {
				var lines []string
				if it.Stream != nil {
					for _, ph := range it.Stream.Phases {
						lines = append(lines, fmt.Sprintf("%s %s round %d of %d note %q", ph.Name, ph.State, ph.Round, ph.RoundLimit, ph.Note))
					}
					for _, l := range it.Stream.Log[max(0, len(it.Stream.Log)-12):] {
						lines = append(lines, l.Tone+": "+l.Text)
					}
				}
				t.Fatalf("never %s: %+v question %q failure %+v\n%s", what, it.State, it.Question, it.Failure, strings.Join(lines, "\n"))
			}
		}
	}
	act := func(method string, extra map[string]any) {
		t.Helper()
		j := job()
		params := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": j.Version, "state": j.State}}
		for k, v := range extra {
			params[k] = v
		}
		var acted appwire.FactoryJobActed
		w.must(method, params, &acted)
	}
	asking := func(step string) func(factory.Item) bool {
		return func(it factory.Item) bool {
			return it.State == factory.StateNeedsYou && it.Question == factory.NextStepQuestion(step)
		}
	}

	var run appwire.FactoryLocalStatus
	w.must("factory.local_run", map[string]any{"caller": "live"}, &run)
	act("factory.job_start", nil)

	var trail []string
	held := waitFor("asked before verify", asking("verify"))
	if p := held.Stream.Phases; p[0].State != factory.PhaseDone || p[1].State != factory.PhaseWaiting || p[1].Round != 0 || p[1].Result != nil {
		t.Fatalf("verify ran before the word: %+v", p)
	}
	j := job()
	if j.NeedsYou == nil || j.NeedsYou.Kind != factory.NeedsApprovePlan || !j.Can.Approve || !j.Can.RejectPlan {
		t.Fatalf("the page cannot answer: needs=%+v can=%+v", j.NeedsYou, j.Can)
	}
	trail = append(trail, "asked: "+held.Question)
	act("factory.job_approve", map[string]any{"commit": j.HeadCommit})

	held = waitFor("asked before review", asking("review"))
	if p := held.Stream.Phases; p[1].State != factory.PhaseDone || p[1].Round != 1 || p[2].Round != 0 {
		t.Fatalf("after the first yes: %+v", p)
	}
	trail = append(trail, "asked: "+held.Question)
	act("factory.job_reject_plan", map[string]any{"words": "check it once more"})

	held = waitFor("asked again after the send-back", func(it factory.Item) bool {
		return asking("review")(it) && it.Stream.Phases[1].Round == 2
	})
	trail = append(trail, "sent back, verify round 2, asked: "+held.Question)
	act("factory.job_approve", map[string]any{"commit": job().HeadCommit})

	landed := waitFor("landed", func(it factory.Item) bool { return it.State == factory.StateLanded })
	if p := landed.Stream.Phases; p[2].State != factory.PhaseDone || p[2].Round != 1 {
		t.Fatalf("review after the last yes: %+v", p)
	}
	stepModels := map[string]string{}
	for _, s := range job().Steps {
		stepModels[s.Name] = s.Model
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	writeLiveFactoryLog(t, map[string]any{
		"requested_model": liveFactoryModel, "request_models": recorder.requests, "response_models": recorder.replies,
		"calls": recorder.calls, "cost_usd": recorder.cost, "problems": recorder.problems, "state": string(landed.State),
		"step_models": stepModels, "trail": trail, "autonomy": landed.Autonomy,
	})
	if recorder.calls == 0 || len(recorder.problems) > 0 || recorder.requests[liveFactoryModel] != recorder.calls || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("model policy: calls %d requests %v replies %v problems %v", recorder.calls, recorder.requests, recorder.replies, recorder.problems)
	}
	if stepModels["write"] != liveFactoryModel || stepModels["review"] != liveFactoryModel {
		t.Fatalf("recorded step models: %v", stepModels)
	}
}
