package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// The real factory maker, session bash belt, journal collector and completion
// guard must agree, even when the model incorrectly reports a failed test as
// fixed or a reporter exits nonzero after passing tests. No live provider or
// GitHub write is needed to exercise those exact product doors.
func TestFactoryVerificationThroughTheRealStageConversation(t *testing.T) {
	for _, tc := range []struct {
		name, check string
		done        bool
	}{
		{"zero-match reporter", "go test -v ./... | grep -c -e '--- FAIL:'", true},
		{"real test failure", "CHECK_EXIT=7 go test -v ./... | tail -2", false},
		{"failed build after passing tests", "go test -v ./... && false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := resolvedTempDir(t)
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv(home.EnvVar, filepath.Join(root, "state"))
			t.Setenv(config.ProfileDirEnv, "")
			t.Setenv("OPENROUTER_API_KEY", "test-key")
			t.Setenv(factoryPostureEnv, "")
			workspace := filepath.Join(root, "work")
			if err := os.MkdirAll(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nprintf '%s\\n' '--- PASS: TestA (0s)' PASS\nexit \"${CHECK_EXIT:-0}\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			command := "export PATH=" + strconv.Quote(bin) + ":\"$PATH\"; " + tc.check
			args, _ := json.Marshal(map[string]string{"command": command})
			model := newScriptedStageModel(t, stageCall{"bash", string(args)}, stageCall{"stage_result", `{"done":true,"output":"repaired","repair":{"decision":"fixed","reason":"checked"}}`})
			t.Setenv("CODEAF_BASE_URL", model.server.URL)
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
			floor, err := store.Open(filepath.Join(root, "factory"))
			if err != nil {
				t.Fatal(err)
			}
			stage := factory.Stage{Name: "followpr", Kind: factory.StageChat, Ask: "repair the existing pull request", Until: factory.UntilDone, On: true}
			id, err := floor.Add(context.Background(), factory.Item{Title: "repair CI", Repo: "work", Tier: factory.TierOwner, Model: "openai/gpt-4.1", Stages: []factory.Stage{stage}})
			if err != nil {
				t.Fatal(err)
			}
			if err := floor.Update(id, func(it *factory.Item) error { it.Stream = &factory.Stream{}; return nil }); err != nil {
				t.Fatal(err)
			}
			item, _ := floor.Get(id)
			maker := factoryStageMaker(floor, workspace, "", launch.Config)
			executor := factoryChatStage(floor, "", factoryrun.NewChatExecutor(maker))
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			result, err := executor.Run(ctx, factoryrun.Job{Item: item, Stage: stage, Index: 0, Round: 1, Dir: workspace, Log: func(string) {}})
			if err != nil || result.Done != tc.done {
				t.Fatalf("stage done=%v want %v, err=%v; notes=%v output=%s", result.Done, tc.done, err, result.Notes, result.Output)
			}
			if !tc.done && !strings.HasPrefix(result.Output, "Verification blocked:") {
				t.Fatalf("missing concrete rejection: %s", result.Output)
			}
			found := false
			for _, note := range result.Notes {
				found = found || strings.HasPrefix(note, "Check receipt:")
			}
			if !found {
				t.Fatalf("session did not persist actual check exit: %v", result.Notes)
			}
		})
	}
}
