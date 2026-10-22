package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
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
		name, check     string
		done            bool
		rerun           string
		repeatedReports int
	}{
		{"zero-match reporter", "go test -v ./... | grep -c -e '--- FAIL:'", true, "", 0},
		{"real test failure", "CHECK_EXIT=7 go test -v ./... | tail -2", false, "", 0},
		{"failed build after passing tests", "go test -v ./... && false", false, "", 0},
		{"audit failure hidden by shell success", "CHECK_EXIT=1 npm audit || true", false, "", 0},
		{"audit corrected in the same conversation", "CHECK_EXIT=1 npm audit || true", true, "npm audit", 0},
		{"saved exit corrected in the same conversation", "CHECK_EXIT=127 go test ./... > /dev/null 2>&1; ec=$?; echo \"EXIT=$ec\"; grep -c '^FAIL' /dev/null", true, "go test ./...", 0},
		{"cargo discovered before its check wrapper", "cargo test --test cli regression", true, "", 0},
		{"repeated rejected report stops the model", "CHECK_EXIT=7 go test ./...", false, "", stageVerificationRejectLimit},
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
			for _, program := range []string{"go", "npm"} {
				if err := os.WriteFile(filepath.Join(bin, program), []byte("#!/bin/sh\nprintf '%s\\n' '--- PASS: TestA (0s)' PASS\nexit \"${CHECK_EXIT:-0}\"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CARGO_HOME", filepath.Join(root, "home", ".cargo"))
			cargoBin := filepath.Join(root, "home", ".cargo", "bin")
			if err := os.MkdirAll(cargoBin, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cargoBin, "cargo"), []byte("#!/bin/sh\necho PASS\n"), 0700); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(tc.check, "cargo ") {
				// Do not accidentally test the laptop's rustup executable. This
				// PATH has only the tools used by the shell and receipt observer.
				for _, name := range []string{"bash", "git", "mktemp", "readlink"} {
					path, err := exec.LookPath(name)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("PATH", bin)
			}
			command := "export PATH=" + strconv.Quote(bin) + ":\"$PATH\"; " + tc.check
			args, _ := json.Marshal(map[string]string{"command": command})
			fixed := stageCall{"stage_result", `{"done":true,"output":"repaired","repair":{"decision":"fixed","reason":"checked"}}`}
			calls := []stageCall{{"bash", string(args)}, fixed}
			for n := 1; n < tc.repeatedReports; n++ {
				calls = append(calls, fixed)
			}
			if tc.rerun != "" {
				retryArgs, _ := json.Marshal(map[string]string{"command": "export PATH=" + strconv.Quote(bin) + ":\"$PATH\"; " + tc.rerun})
				calls = append(calls, stageCall{"bash", string(retryArgs)}, fixed)
			}
			model := newScriptedStageModel(t, calls...)
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
			if tc.repeatedReports > 0 && (result.Repair == nil || result.Repair.Decision != "needs_you" || !strings.Contains(result.Repair.Reason, "same unresolved evidence")) {
				t.Fatalf("repeated refusal did not stop recoverably: %+v", result.Repair)
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
