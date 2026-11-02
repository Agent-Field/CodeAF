package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestARunnerOnItsOwnHomeReadsTheLeasedJobsBudget: a runner whose home holds
// no factory of its own runs a leased job over its mirror. Its step's meter
// must read the job's budget from that mirror (the copy the factory handed
// over), not from the runner's own empty factory folder, and book what the
// step spent there so the worker's report carries it back.
func TestARunnerOnItsOwnHomeReadsTheLeasedJobsBudget(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	model := newStageModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
	proc, err := openEngineProcess()
	if err != nil {
		t.Fatal(err)
	}
	launch, err := openV3Launch(proc, engineLaunchOptions(remote.Hello{Version: remote.Version, Workspace: workspace}, workspace, ""))
	if err != nil {
		t.Fatal(err)
	}

	// The runner's own home: a factory folder with no jobs in it.
	floor, err := store.Open(filepath.Join(root, "factory"))
	if err != nil {
		t.Fatal(err)
	}
	// The leased job, as the worker saves it on its mirror.
	mirror, err := store.Open(filepath.Join(root, "factory", "workers", "1", "remote"))
	if err != nil {
		t.Fatal(err)
	}
	leased := factory.Item{
		ID: 7, Num: 7, Title: "tidy the ledger", Repo: "work", Tier: factory.TierOwner, Cap: 5,
		Stages: []factory.Stage{{Name: "review", Kind: factory.StageChat, Ask: "look it over", Until: "done", On: true}},
		Stream: &factory.Stream{Spent: 1},
	}
	if err := mirror.Save(leased); err != nil {
		t.Fatal(err)
	}
	it, err := mirror.Get(leased.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := floor.Get(it.ID); err == nil {
		t.Fatal("the runner's own home already holds the job; the test proves nothing")
	}

	host := &appFactoryHost{floor: floor, workspace: workspace}
	source := &factoryParentSource{raw: func() session.Config { return launch.Config }}
	exec := factoryChatStage(mirror, "", factoryrun.NewChatExecutor(host.stepMaker(mirror, source)))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := exec.Run(ctx, factoryrun.Job{Item: it, Stage: it.Stages[0], Index: 0, Round: 1, Dir: workspace, Log: func(string) {}})
	if err != nil {
		t.Fatalf("the step failed on a runner with its own home: %v", err)
	}
	if !res.Done || res.Output != "looked it over" {
		t.Fatalf("result = done %v, output %q; want the reported round", res.Done, res.Output)
	}
}
