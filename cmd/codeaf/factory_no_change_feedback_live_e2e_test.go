//go:build e2e

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/home"
)

// A captured local PR snapshot carries the same comment revisions as GitHub.
// The runner, conversation, tools and model are real; nothing writes to GitHub.
func TestLiveFollowPRConsidersNothingToChangeWithoutDiff(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skip("opt-in live factory lane")
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key")
	}
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv("CODEAF_MODEL_POOL", "off")
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv(factoryPostureEnv, "")
	profile := v3Profile(t, map[string]any{
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.reflex": liveFactoryModel,
		"models.tiers.high": liveFactoryModel, "models.tiers.worker": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel, "models.fallbacks": "",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	g := newRunRig(t)
	base := strings.TrimSpace(gitT(t, g.workspace, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(g.workspace, "README.md"), []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitT(t, g.workspace, "add", "README.md")
	gitT(t, g.workspace, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-qm", "Add greeting")
	head := strings.TrimSpace(gitT(t, g.workspace, "rev-parse", "HEAD"))
	freshEngineProcess(t)
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
	item := factory.Item{Title: "Add hello to README", Body: "README.md contains hello on its own line.", Repo: "api", Kind: factory.KindIssue, Cap: .50, Autonomy: factory.AutonomyAuto, Model: liveFactoryModel, HeadCommit: head, ChangeBase: base, Publish: "pr", State: factory.StateRunning,
		PullRequest: &factory.PullRequest{Number: 11, State: factory.PROpen, HeadSHA: head, BaseSHA: base, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, FeedbackTracked: true, Feedback: "PR conversation comment 1: Nothing to change.", FeedbackItems: []factory.PRFeedback{{ID: "conversation:1", Revision: "v1"}}},
		FollowUp:    &factory.PRFollowUp{Kind: "pr", State: "queued", HeadSHA: head, Attempt: 1, Limit: 3, Inspecting: map[string]string{"conversation:1": "v1"}, Details: "Current PR description: Add hello to README. PR conversation comment 1: Nothing to change.", Request: "Read the current checkout and comment. Assess it against the diff; do not manufacture an edit. No GitHub access is needed: the captured PR snapshot is authoritative."},
		Stream:      &factory.Stream{Paused: true},
	}
	factory.EnsureFollowPR(&item)
	made, err := g.st.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	id := made.ID
	maker := factoryStageMaker(g.st, g.workspace, profile, launch.Config)
	r := factoryrun.New(factoryrun.Options{Store: g.st, Workdir: func(factory.Item) (string, error) { return g.workspace, nil }, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryChatStage(g.st, profile, factoryrun.NewChatExecutor(maker))}, Push: func(string, string) error { t.Error("no-change review tried to push"); return factory.ErrReadOnly }})
	stopAll(t, g.st, r)
	if err := r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(liveFactoryWall)
	var got factory.Item
	for time.Now().Before(deadline) {
		got, _ = g.st.Get(id)
		if got.State == factory.StateShipped || got.Question != "" || got.Failure != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	recorder.mu.Lock()
	t.Logf("LIVE calls=%d cost_usd=%.6f requests=%v recorded_models=%v problems=%v", recorder.calls, recorder.cost, recorder.requests, recorder.replies, recorder.problems)
	if recorder.calls == 0 || len(recorder.problems) != 0 || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 || recorder.cost > liveFactoryBudget {
		t.Error("live model evidence or budget failed")
	}
	recorder.mu.Unlock()
	if got.State != factory.StateShipped || got.FollowUp.State != "watching" || got.FollowUp.Reviewed["conversation:1"] != "v1" {
		t.Fatalf("no-change feedback did not finish: state=%s question=%q follow=%+v", got.State, got.Question, got.FollowUp)
	}
	ph := got.Stream.Phases[len(got.Stream.Phases)-1]
	if ph.Result == nil || ph.Result.Model != liveFactoryModel || ph.Result.Repair == nil || ph.Result.Repair.Decision != "no_change" {
		t.Fatalf("wrong recorded result: %+v", ph.Result)
	}
	if strings.TrimSpace(gitT(t, g.workspace, "rev-parse", "HEAD")) != head || strings.TrimSpace(gitT(t, g.workspace, "status", "--porcelain", "--untracked-files=all")) != "" {
		t.Fatal("no-change review produced a diff")
	}
	if v := factory.MergeGate(got, factory.MergeAsk{By: "sam"}); !v.OK {
		t.Fatalf("Merge remains blocked: %s", v.Why)
	}
	t.Logf("LIVE finished=true no_new_diff=true considered=%s model=%s spent_usd=%.6f", got.FollowUp.Reviewed["conversation:1"], ph.Result.Model, got.Stream.Spent)
}
