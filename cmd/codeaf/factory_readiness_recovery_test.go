package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// Replay the observed failure through the real model/tool/journal path. The
// fixture executable is a deterministic passing check; its zero-match grep
// still exits 1 under the product's actual shell. A missing readiness report
// must be corrected at the tool call, without opening another conversation.
func TestFactoryReadinessRecoversLiveCountsAndChecklistThroughRealChat(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	workspace, bin := filepath.Join(root, "work"), filepath.Join(root, "bin")
	for _, dir := range []string{workspace, bin} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nprintf '%s\\n' '--- PASS: TestFixture (0.00s)' PASS\n"), 0700); err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf(`export PATH=%q:"$PATH"; go test ./... -count=1 -v 2>&1 | grep -cE '^\s*--- PASS:' && echo "---" && go test ./... -count=1 -v 2>&1 | grep -cE '^\s*--- FAIL:' && echo "---" && go test ./... -count=1 -v 2>&1 | grep -cE '^\s*--- SKIP:'`, bin)
	args, _ := json.Marshal(map[string]string{"command": command})
	head := strings.Repeat("a", 40)
	model := newScriptedStageModel(t,
		stageCall{"bash", string(args)},
		stageCall{"stage_result", `{"done":true,"findings":0,"repair":{"decision":"no_change","reason":"Diff and description match, no remaining findings"}}`},
		stageCall{"stage_result", fmt.Sprintf(`{"done":true,"findings":0,"repair":{"decision":"no_change","reason":"Diff and description match, no remaining findings","readiness":{"head":%q,"diff_reviewed":true,"description_matches":true,"feedback_addressed":true,"no_unfinished_work":true}}}`, head)},
	)
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
	stage := factory.Stage{Name: factory.FollowPRStep, Kind: factory.StageChat, Ask: "Check the final diff and description", Until: factory.UntilProven, On: true}
	id, err := floor.Add(context.Background(), factory.Item{Title: "review the fixture", Repo: "work", Kind: factory.KindIssue, Tier: factory.TierOwner, Cap: 10, Autonomy: factory.AutonomyAuto, HeadCommit: head,
		Stages: []factory.Stage{stage}, Stream: &factory.Stream{},
		PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen, Draft: true, HeadSHA: head, ReadinessKey: "snapshot", ChecksRead: true, ReviewsRead: true, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, Mergeable: factory.MergeableYes},
		FollowUp:    &factory.PRFollowUp{Kind: "readiness", State: "running", Readiness: &factory.PRReadiness{Key: "snapshot", Head: head, State: "reviewing"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	it, _ := floor.Get(id)
	maker := factoryStageMaker(floor, workspace, "", launch.Config)
	exec := factoryChatStage(floor, "", factoryrun.NewChatExecutor(maker))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	res, err := exec.Run(ctx, factoryrun.Job{Item: it, Stage: stage, Index: 0, Round: 1, Dir: workspace, Log: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := factory.ValidateReadinessReview(it, res); err != nil {
		t.Fatalf("real chat did not produce acceptable readiness: %v; output=%s", err, res.Output)
	}
	if res.Reports != 1 {
		t.Fatalf("accepted %d reports, want only the corrected one", res.Reports)
	}
	checks, err := verification.Read(res.Chat)
	if err != nil || len(checks) != 2 {
		t.Fatalf("actual shell receipts: count=%d err=%v", len(checks), err)
	}
	for _, check := range checks {
		if check.Exit == nil || *check.Exit != 0 {
			t.Fatal("fixture check did not actually pass")
		}
	}
	raw, err := os.ReadFile(res.Chat)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Command exited with code 1") || !strings.Contains(string(raw), "include repair.readiness") {
		t.Fatal("did not exercise both live shell failure and in-turn checklist correction")
	}
}
