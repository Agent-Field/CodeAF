package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUnreadableStageJobKeepsConfigReason(t *testing.T) {
	testUnreadableStageJobKeepsConfigReason(t, false)
}

func TestInitiallyUnreadableStageJobKeepsConfigReason(t *testing.T) {
	testUnreadableStageJobKeepsConfigReason(t, true)
}

func testUnreadableStageJobKeepsConfigReason(t *testing.T, initiallyUnreadable bool) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("mode 000 requires nonroot")
	}
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv(factoryPostureEnv, "")
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "NOTES.md")
	writeArgs, _ := json.Marshal(map[string]string{"path": target, "content": "written by the stage\n"})
	model := newScriptedStageModel(t, stageCall{"write", string(writeArgs)}, stageCall{"stage_result", stageModelReport})
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
	st, err := store.Open(filepath.Join(root, "factory"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{
		Title: "write the notes", Repo: "work", Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write the notes", Until: "done", On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(id, func(it *factory.Item) error { it.Stream = &factory.Stream{}; return nil }); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)

	profile := ""
	file := config.BudgetConfigPath(profile)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"tools.approvals":"write:deny"}`), 0600); err != nil {
		t.Fatal(err)
	}
	reason := "saved approval rules could not be read"
	if initiallyUnreadable {
		if err := os.Chmod(file, 0); err != nil {
			t.Fatal(err)
		}
		reason = "permission denied"
	} else {
		oldHandler := model.server.Config.Handler
		var once sync.Once
		model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte(`"stage_result"`)) {
				once.Do(func() {
					if err := os.Chmod(file, 0); err != nil {
						t.Error(err)
					}
				})
			}
			oldHandler.ServeHTTP(w, r)
		})
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0600) })
	maker := factoryStageMaker(st, workspace, profile, launch.Config)
	exec := factoryChatStage(st, profile, factoryrun.NewChatExecutor(maker))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	res, err := exec.Run(ctx, factoryrun.Job{Item: it, Stage: it.Stages[0], Index: 0, Round: 1, Dir: workspace, Log: func(line string) {
		t.Logf("JOB LOG: %s", line)
		if strings.Contains(line, "reminded") {
			t.Error("terminal profile refusal must not retry reporting")
		}
	}})
	if err != nil {
		t.Fatalf("ROUND ERROR: %v", err)
	}
	t.Logf("ROUND RESULT: %+v", res)
	if res.Done || res.Exit != 1 || !strings.Contains(res.Output, file) || !strings.Contains(res.Output, reason) {
		t.Errorf("job lost the terminal profile-read reason: %+v", res)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if initiallyUnreadable {
		t.Logf("STAGE MODEL REQUESTS: %d", len(model.briefs))
		if len(model.briefs) != 0 || res.Spent != 0 || res.Reports != 0 {
			t.Fatalf("refused admission did work: requests=%d spent=%v reports=%d", len(model.briefs), res.Spent, res.Reports)
		}
	}
	for _, brief := range model.briefs {
		if i := strings.Index(brief, "no resolver is attached"); i >= 0 {
			t.Logf("MODEL RECEIVED REFUSAL: %.650s", brief[i:])
		}
	}
	got, readErr := os.ReadFile(target)
	t.Logf("TARGET: content=%q err=%v", got, readErr)
	if !os.IsNotExist(readErr) {
		t.Fatalf("unreadable stage wrote file")
	}
	if ctx.Err() != nil {
		t.Fatalf("stage failed to stop before timeout: %v", ctx.Err())
	}
}
