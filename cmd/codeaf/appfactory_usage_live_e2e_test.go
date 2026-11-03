//go:build e2e

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
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

// THE LIVE USAGE BY MODEL. One real factory job on one real model, every role
// on it ([assertFactoryPin], [liveRecorder]), through the app's own doors; then
// factory.usage_by_model answers that job's calls from this computer's ledger:
// the model the provider named, its calls, its tokens and its priced cost.
// No GitHub; the job stops at its approve gate. Opt-in, a few cents.
func TestLiveFactoryJobUsageByModel(t *testing.T) {
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
	launch, err := openV3Launch(proc, v3Options{Model: liveFactoryModel, OneModel: true, Workspace: g.workspace})
	if err != nil {
		t.Fatal(err)
	}
	assertFactoryPin(t, launch.Config, liveFactoryModel)
	stageSettle = 300 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })

	w := newFactoryWire(t, testFactoryHub(t))
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
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done",
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
		t.Fatalf("job is %s (failure %+v, question %q)", it.State, it.Failure, it.Question)
	}
	session.FlushUsage()
	today := time.Now().Format(time.DateOnly)
	var usage appwire.FactoryUsage
	w.must("factory.usage_by_model", map[string]any{"factory": st.Factory, "from": today, "to": today, "job": id}, &usage)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, m := range usage.Models {
		cost := "not priced"
		if m.CostUSD != nil {
			cost = "$" + formatUSD(*m.CostUSD)
		}
		t.Logf("live usage_by_model: job %s model %s paid %q calls %d tokens in %d out %d cost %s · recorder: requested %v answered %v calls %d",
			usage.Jobs[0].Ref, m.Model, m.Paid, m.Calls, m.TokensIn, m.TokensOut, cost, recorder.requests, recorder.replies, recorder.calls)
	}
	if len(recorder.problems) > 0 || recorder.requests[liveFactoryModel] != recorder.calls || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("model policy: problems %v requested %v answered %v", recorder.problems, recorder.requests, recorder.replies)
	}
	if len(usage.Jobs) != 1 || usage.Jobs[0].Job != id || len(usage.Models) != 1 {
		t.Fatalf("usage by model = %+v", usage)
	}
	if m := usage.Models[0]; m.Model != liveFactoryModel || m.Calls == 0 || m.TokensIn == 0 || m.TokensOut == 0 || m.CostUSD == nil || *m.CostUSD <= 0 {
		t.Fatalf("the job's model row = %+v", m)
	}
}

func formatUSD(usd float64) string { return strconv.FormatFloat(usd, 'f', 6, 64) }
