package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestFactoryStageOwnsAnExplicitBackgroundCheckAfterCheckpoint(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv(factoryPostureEnv, "")
	workspace := filepath.Join(root, "work")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	gitT(t, workspace, "init", "-q")
	if err := os.WriteFile(filepath.Join(workspace, "source.txt"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitT(t, workspace, "add", "source.txt")
	gitT(t, workspace, "-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(root, "release")
	script := fmt.Sprintf("#!/bin/sh\nwhile [ ! -f %q ]; do sleep 0.01; done\necho PASS\n", release)
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	command := "export PATH=" + strconv.Quote(bin) + ":\"$PATH\"; go test ./..."
	args, _ := json.Marshal(map[string]any{"command": command, "background": true})
	model := newScriptedStageModel(t,
		stageCall{"stage_result", `{"done":false,"final":false,"output":"Beginning verification."}`},
		stageCall{"bash", string(args)},
		stageCall{"stage_result", `{"done":true,"final":true,"output":"The required check completed."}`},
	)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	t.Chdir(workspace)
	freshEngineProcess(t)
	oldSettle := stageSettle
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = oldSettle })
	process, err := openEngineProcess()
	if err != nil {
		t.Fatal(err)
	}
	launch, err := openV3Launch(process, engineLaunchOptions(remote.Hello{Version: remote.Version, Workspace: workspace}, workspace, ""))
	if err != nil {
		t.Fatal(err)
	}
	floor, err := store.Open(filepath.Join(root, "factory"))
	if err != nil {
		t.Fatal(err)
	}
	stage := factory.Stage{Name: "validate", Kind: factory.StageChat, Until: factory.UntilGreen, Ask: "run the required check", On: true}
	id, err := floor.Add(context.Background(), factory.Item{Title: "verify lifecycle", Repo: "work", Tier: factory.TierOwner, Model: "openai/gpt-4.1", Stages: []factory.Stage{stage}})
	if err != nil {
		t.Fatal(err)
	}
	if err := floor.Update(id, func(it *factory.Item) error { it.Stream = &factory.Stream{}; return nil }); err != nil {
		t.Fatal(err)
	}
	item, _ := floor.Get(id)
	executor := factoryChatStage(floor, "", factoryrun.NewChatExecutor(factoryStageMaker(floor, workspace, "", launch.Config)))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	type answer struct {
		result factory.StageResult
		err    error
	}
	done := make(chan answer, 1)
	running := make(chan *factory.StepVerification, 1)
	go func() {
		result, err := executor.Run(ctx, factoryrun.Job{Item: item, Stage: stage, Index: 0, Round: 1, Dir: workspace, Log: func(string) {}, Verification: func(v *factory.StepVerification) {
			if v != nil && len(v.Checks) > 0 {
				select {
				case running <- v:
				default:
				}
			}
		}})
		done <- answer{result, err}
	}()
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	select {
	case v := <-running:
		if v.State != "running" || v.Checks[0].StartedAt.IsZero() {
			t.Fatalf("missing typed running state: %+v", v)
		}
	case got := <-done:
		t.Fatalf("checkpoint or background start ended stage: %+v", got)
	case <-ctx.Done():
		t.Fatal("running check metadata never reached the coordinator")
	}
	select {
	case got := <-done:
		t.Fatalf("stage closed before check release: %+v", got)
	default:
	}
	model.mu.Lock()
	requests := 0
	for _, belt := range model.belts {
		for _, name := range belt {
			if name == "stage_result" {
				requests++
				break
			}
		}
	}
	model.mu.Unlock()
	if requests != 2 {
		t.Fatalf("running check was polled by model: %d stage requests", requests)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || !got.result.Done || got.result.Checkpoint || !factory.Met(stage, got.result) {
			t.Fatalf("completed check not accepted: %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("check completion did not settle the real stage")
	}
}

func TestStageVerificationProgressRequiresItsNativeCall(t *testing.T) {
	now := time.Now()
	current := []verification.CurrentCheck{
		{ID: "orphan", CallID: "old-killed-call", Outcome: "running", StartedAt: now},
		{ID: "legacy", Outcome: "running", StartedAt: now},
		{ID: "current", CallID: "live-call", Outcome: "running", StartedAt: now},
		{ID: "exited", CallID: "live-call", Outcome: "passed", StartedAt: now},
	}
	if got := liveStageVerification(current, nil); got != nil {
		t.Fatalf("orphan receipts advertised without physical work: %+v", got)
	}
	if got := liveStageVerification(current[:2], []session.StageCheckProgress{{CallID: "different-current-check"}}); got != nil {
		t.Fatalf("old receipt borrowed another check's liveness: %+v", got)
	}
	got := liveStageVerification(current, []session.StageCheckProgress{{CallID: "live-call"}})
	if got == nil || len(got.Checks) != 1 || got.Checks[0].ID != "current" {
		t.Fatalf("live invocation metadata lost or orphan leaked: %+v", got)
	}
	if got := liveStageVerification(current, []session.StageCheckProgress{{CallID: "live-call", Started: now.Add(time.Second)}}); got != nil {
		t.Fatal("reused provider call ID revived an older unfinished receipt")
	}
	if got := liveStageVerification(current, nil); got != nil {
		t.Fatal("settled native work did not clear running projection")
	}
}
