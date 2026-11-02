//go:build e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/home"
)

// liveLaunchDefault is a launch default no provider serves. The recorder
// refuses any request for it, so a step that opened on it fails the lane.
const liveLaunchDefault = "codeaf-test/launch-default-never-sent"

// THE FACTORY'S DEFAULT MODEL IS WHAT A REAL STEP RUNS ON. One bounded live run
// of the real runner and stage maker: the launch's own default is a model no
// provider serves and the launch is not on one model, the factory keeps
// deepseek/deepseek-v4.1-flash as its default, and the job (no model of its
// own) carries that default as queueing stamps it. Every model call goes
// through [liveRecorder], which refuses any model but the pinned one, so the
// step reaching its approve gate proves it opened on the factory's default.
func TestLiveFactoryStepRunsOnTheFactorysDefaultModel(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends a few cents on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads (OPENROUTER_API_KEY, profile api_key); not faking a pass")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("BLOCKED: git is not installed")
	}
	started := time.Now()
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	// Off one model, the small calls beside a step ride the profile's tier
	// rows; this lane runs every role on the one allowed model, so it pins them.
	writeExternalCrewSettings(t, config.BudgetConfigPath(profile), map[string]string{config.KeyTierLowModel: liveFactoryModel, config.KeyTierReflexModel: liveFactoryModel})
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv(factoryPostureEnv, "")
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)

	g := newRunRig(t)
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Model: liveLaunchDefault, Workspace: g.workspace})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Config.OneModel {
		t.Fatal("the launch is on one model; the factory's default would not apply")
	}
	if _, err := g.st.ChangeSpending(func(s *factory.Spending) error { s.DefaultModel = liveFactoryModel; return nil }); err != nil {
		t.Fatal(err)
	}
	spending, err := g.st.Spending()
	if err != nil {
		t.Fatal(err)
	}

	stageSettle = 300 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
	r := buildFactoryRunner(g.st, g.st, g.workspace, "", factoryStageMaker(g.st, g.workspace, "", launch.Config), nil)
	stopAll(t, g.st, r)
	seam := factory.LocalSeam(g.st, time.Now(), factory.WithRunner(r))
	const want = "hello factory"
	id, err := g.st.Add(context.Background(), factory.Item{
		Title: "write the greeting", Repo: "api", Tier: factory.TierOwner, Cap: liveFactoryBudget,
		// The stamp a lease's queueing makes (lease.Doors.Launch); this lane's
		// local seam queues for its own runner and has no lease.
		FactoryModel: spending.DefaultModel,
		Stages: []factory.Stage{
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done",
				Ask: "Create the file HELLO.txt in the current directory with the write tool, its content exactly `" + want + "` and a trailing newline. Then call stage_result with done true, findings 0 and a one-line output. Do nothing else."},
			{Name: "verify", Kind: factory.StageCheck, On: true, Ask: "grep -qx '" + want + "' HELLO.txt"},
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveFactoryWall)
	defer cancel()
	if err := seam.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := waitLiveItem(ctx, g.st.Get, id)

	recorder.mu.Lock()
	summary := map[string]any{
		"launch_default": liveLaunchDefault, "factory_default": spending.DefaultModel,
		"request_models": recorder.requests, "response_models": recorder.replies, "calls": recorder.calls,
		"http_statuses": recorder.statuses, "cost_usd": recorder.cost, "state": string(it.State),
		"problems": recorder.problems, "seconds": int(time.Since(started).Seconds()), "one_model": false,
		"github": "none: no remote, no token, no publish",
	}
	problems, calls, requests, cost := append([]string(nil), recorder.problems...), recorder.calls, recorder.requests[liveFactoryModel], recorder.cost
	recorder.mu.Unlock()
	writeLiveFactoryLog(t, summary)

	if it.State != factory.StateLanded {
		t.Fatalf("item is %s (failure %+v, question %q); summary %v", it.State, it.Failure, it.Question, summary)
	}
	got, err := os.ReadFile(filepath.Join(g.st.Root(), "work", "api-"+strconv.Itoa(id), "HELLO.txt"))
	if err != nil || strings.TrimSpace(string(got)) != want {
		t.Fatalf("HELLO.txt in the item's worktree = %q, %v", got, err)
	}
	if calls == 0 || len(problems) > 0 || requests != calls {
		t.Fatalf("calls %d, on the factory default %d, problems %v", calls, requests, problems)
	}
	if cost > liveFactoryBudget {
		t.Fatalf("cost $%.4f exceeds the $%.2f budget", cost, liveFactoryBudget)
	}
}

// waitLiveItem polls one item until it lands, fails, asks, is dropped or ctx ends.
func waitLiveItem(ctx context.Context, get func(int) (factory.Item, error), id int) factory.Item {
	for {
		it, _ := get(id)
		if it.State == factory.StateLanded || it.Failure != nil || it.Question != "" || it.State == factory.StateDismissed || ctx.Err() != nil {
			return it
		}
		time.Sleep(250 * time.Millisecond)
	}
}
