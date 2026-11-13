//go:build e2e

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE LIVE CHEAP STEP. The small work model is pinned through the app's own
// settings.models.set (every role on the one live model, [liveRecorder]
// refusing any other), the launch is in auto mode, and one real factory job
// runs a step marked cheap to its approve gate. The step opens on the small
// work model the pin names, and no other model is called.
func TestLiveFactoryCheapStepRunsOnTheSmallWorkModel(t *testing.T) {
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
	// One profile for the launch and the factory's steps (home.Join).
	profile := filepath.Join(root, "state")
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv(factoryPostureEnv, "")
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
	// The pins are written where settings.models.set writes them; the wire's
	// own check asks a live catalog this rig does not serve.
	// The router's catalog holds the live model only, so a seat nobody pinned
	// routes to it too and every role stays on the one model.
	seatCrewCatalog(t, []catalog.Model{{ID: liveFactoryModel, PromptPrice: 0.1e-6, CompletionPrice: 0.4e-6,
		CodingIndex: 50, AgenticIndex: 50, IntelligenceIndex: 50, Parameters: []string{"tools"}, ContextLength: 200000, OpenWeights: true}})
	for _, seat := range []crewroute.Seat{crewroute.Worker, crewroute.Planner, crewroute.Checker} {
		if err := config.SetCrewPin(profile, seat, liveFactoryModel); err != nil {
			t.Fatal(err)
		}
	}
	writeExternalCrewSettings(t, config.BudgetConfigPath(profile), map[string]string{
		config.KeyTierLowModel: liveFactoryModel, config.KeyTierReflexModel: liveFactoryModel, config.KeyTierMastermindModel: liveFactoryModel,
	})
	w := newFactoryWire(t, testFactoryHub(t))
	launch, err := openV3Launch(proc, v3Options{Model: liveFactoryModel, Workspace: g.workspace})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Config.OneModel {
		t.Fatal("the launch is on one model; a cheap step would keep it")
	}
	if home.Join() != profile {
		t.Fatalf("the factory's profile %q is not the launch's %q", home.Join(), profile)
	}
	if got := factoryOpenModel(profile, session.Config{}, factoryrun.ConversationSpec{StepEffort: "cheap"}); got != liveFactoryModel {
		t.Fatalf("a cheap step would open on %q, want the small work pin %q", got, liveFactoryModel)
	}
	stageSettle = 300 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })

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
	id, err := floor.Add(context.Background(), factory.Item{
		Title: "write the greeting", Repo: "acme/api", Tier: factory.TierOwner, Cap: liveFactoryBudget,
		Stages: []factory.Stage{
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done", Effort: "cheap",
				Ask: "Create the file HELLO.txt in the current directory with the write tool, its content exactly `hello factory` and a trailing newline. Then call stage_result with done true, findings 0 and a one-line output. Do nothing else."},
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var job api.Job
	w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
	if !st.Runner {
		var run appwire.FactoryLocalStatus
		w.must("factory.local_run", map[string]any{"caller": "live"}, &run)
	}
	var acted appwire.FactoryJobActed
	w.must("factory.job_start", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)
	ctx, cancel := context.WithTimeout(context.Background(), liveFactoryWall)
	defer cancel()
	var it factory.Item
	for {
		it, _ = floor.Get(id)
		if it.State == factory.StateLanded || it.Failure != nil || it.Question != "" || ctx.Err() != nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if it.State != factory.StateLanded {
		recorder.mu.Lock()
		t.Logf("recorder: requested %v answered %v calls %d problems %v", recorder.requests, recorder.replies, recorder.calls, recorder.problems)
		recorder.mu.Unlock()
		t.Fatalf("job is %s (failure %+v, question %q)", it.State, it.Failure, it.Question)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	t.Logf("live cheap step: job %d landed at its approve gate · small work pin %s · recorder: requested %v answered %v calls %d",
		id, liveFactoryModel, recorder.requests, recorder.replies, recorder.calls)
	if len(recorder.problems) > 0 || recorder.calls == 0 || recorder.requests[liveFactoryModel] != recorder.calls || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("model policy: problems %v requested %v answered %v", recorder.problems, recorder.requests, recorder.replies)
	}
}
