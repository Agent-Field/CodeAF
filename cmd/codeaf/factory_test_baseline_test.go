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
	"github.com/Agent-Field/codeaf/internal/factory/verification"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestFactoryTestStageComparesPinnedBaseFailures(t *testing.T) {
	cache := t.TempDir()
	for _, tc := range []struct {
		name, change string
		strict       bool
		done         bool
	}{
		{name: "same failure", change: "const newWork = true", done: true},
		{name: "new failure", change: "func TestRegression(t *testing.T) { t.Fatal(\"introduced here\") }"},
		{name: "strict Test", change: "const newWork = true", strict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := resolvedTempDir(t)
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv(home.EnvVar, filepath.Join(root, "state"))
			t.Setenv(config.ProfileDirEnv, "")
			t.Setenv("OPENROUTER_API_KEY", "test-key")
			t.Setenv(factoryPostureEnv, "")
			t.Setenv("GOCACHE", cache)
			t.Setenv("GOTOOLCHAIN", "local")
			t.Setenv("GOWORK", "off")
			t.Setenv("GOFLAGS", "")
			workspace := filepath.Join(root, "job")
			if err := os.Mkdir(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			gitT(t, workspace, "init", "-q")
			write := func(name, text string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(workspace, name), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/fixture\n\ngo 1.23\n")
			const tests = "package fixture\nimport \"testing\"\nfunc TestExisting(t *testing.T) { t.Fatal(\"fixture service is unavailable\") }\n"
			write("fixture_test.go", tests)
			gitT(t, workspace, "add", ".")
			gitT(t, workspace, "commit", "-qm", "base")
			pin := gitT(t, workspace, "rev-parse", "HEAD")
			base := filepath.Join(root, "base")
			gitT(t, workspace, "worktree", "add", "--detach", base, pin)
			write("fixture_test.go", tests+tc.change+"\n")
			const check = "go test -json -count=1 ./..."
			bash := func(command string) stageCall {
				raw, _ := json.Marshal(map[string]string{"command": command})
				return stageCall{"bash", string(raw)}
			}
			model := newScriptedStageModel(t, bash(check), bash("cd "+strconv.Quote(base)+" && "+check),
				stageCall{"stage_result", `{"done":true,"output":"Test assessed"}`})
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
			stage, _ := factory.ShippedStage("test")
			if tc.strict {
				stage.Until = factory.UntilGreen
			}
			item, err := floor.Create(factory.Item{
				Title: "Compare Test evidence", Repo: "fixture", Kind: factory.KindIssue, Tier: factory.TierOwner,
				Model: "openai/gpt-4.1", ChangeBase: pin, Stages: []factory.Stage{stage}, Stream: &factory.Stream{},
			})
			if err != nil {
				t.Fatal(err)
			}
			maker := factoryStageMaker(floor, workspace, "", launch.Config)
			executor := factoryChatStage(floor, "", factoryrun.NewChatExecutor(maker))
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			result, err := executor.Run(ctx, factoryrun.Job{Item: item, Stage: stage, Index: 0, Round: 1, Dir: workspace, Log: func(string) {}})
			if err != nil || result.Done != tc.done {
				t.Fatalf("done=%v want %v, err=%v; notes=%v output=%s", result.Done, tc.done, err, result.Notes, result.Output)
			}
			if tc.done {
				if !strings.Contains(result.Output, verification.BaselineNotePrefix+pin) {
					t.Fatalf("missing successful comparison in stage result: %s", result.Output)
				}
				notes := strings.Join(result.Notes, "\n")
				if !strings.Contains(notes, `"base_commit":"`+pin+`"`) || !strings.Contains(notes, `"exit":1`) || !strings.Contains(notes, `"outcome":"failed"`) {
					t.Fatalf("missing original failed receipt or base proof: %v", result.Notes)
				}
			}
		})
	}
}
