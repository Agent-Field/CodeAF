package replangate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/agentjson"
)

func TestDecisionSchemaOperationUnion(t *testing.T) {
	valid := []byte(`{"action":"modify_dag","reason":"reshape","ops":[{"op":"add","title":"A","kind":"code","deps":null,"reason":"needed"},{"op":"cancel","id":"t1","reason":"obsolete"},{"op":"amend","id":"t2","prepend":"context"},{"op":"split","id":"t3","parts":[{"title":"A","description":"a","file_scope":["a.ts"]},{"title":"B","description":"b","file_scope":null}],"reason":"large"}],"drop_ids":null,"abort_summary":null}`)
	parsed := (DecisionSchema{}).SafeParse(valid)
	if !parsed.Success() || len(parsed.Data.Ops) != 4 {
		t.Fatalf("parsed=%#v", parsed)
	}
	for _, invalid := range []string{
		`{"action":"continue","reason":"x","ops":null,"drop_ids":null}`,
		`{"action":"modify_dag","reason":"x","ops":[{"op":"cancel","id":"t","reason":"x","extra":1}],"drop_ids":null,"abort_summary":null}`,
		`{"action":"modify_dag","reason":"x","ops":[{"op":"split","id":"t","parts":[{"title":"A","description":"a","file_scope":null}],"reason":"x"}],"drop_ids":null,"abort_summary":null}`,
	} {
		if got := (DecisionSchema{}).SafeParse([]byte(invalid)); got.Success() {
			t.Fatalf("invalid decision accepted: %s", invalid)
		}
	}
}

func TestCapturePlanDBSnapshot(t *testing.T) {
	runner := PlanDBRunnerFunc(func(args []string) (plandb.RunResult, error) {
		switch strings.Join(args, " ") {
		case "status --detail":
			return plandb.RunResult{Stdout: []byte("tasks")}, nil
		case "contexts --task a --kind blocker":
			return plandb.RunResult{Stdout: []byte("block A")}, nil
		case "contexts --task b --kind blocker":
			return plandb.RunResult{}, errors.New("skip")
		case "contexts --kind debt":
			return plandb.RunResult{Stdout: []byte("debt")}, nil
		case "contexts --kind frozen":
			return plandb.RunResult{Stdout: []byte("   ")}, nil
		}
		t.Fatalf("unexpected args %#v", args)
		return plandb.RunResult{}, nil
	})
	got := CapturePlanDBSnapshot("/repo", "db", []string{"a", "b"}, runner)
	if got.TaskList != "tasks" || got.Blockers != "Task a:\nblock A" ||
		got.Debt != "debt" || got.Frozen != "(no frozen leaves)" {
		t.Fatalf("snapshot=%#v", got)
	}
}

func TestCaptureSnapshotKeepsCLIContextTaskFilterBug(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	project := db.Init("snapshot")
	first, err := db.AddTask(plandb.AddTaskInput{Title: "first", Project: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.AddTask(plandb.AddTaskInput{Title: "second", Project: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	db.AddContext("block A", plandb.AddContextOpts{
		Project: project.ID, TaskID: first.ID, Kind: "blocker",
	})
	db.AddContext("block B", plandb.AddContextOpts{
		Project: project.ID, TaskID: second.ID, Kind: "blocker",
	})
	got := CapturePlanDBSnapshot("", "", []string{first.ID, second.ID}, nativePlanDBRunner{})
	if strings.Count(got.Blockers, "block A") != 2 ||
		strings.Count(got.Blockers, "block B") != 2 {
		t.Fatalf("blockers did not duplicate the unfiltered result: %s", got.Blockers)
	}
}

func TestDispatchReplannerUsesAgentJSONSnapshotAndHistory(t *testing.T) {
	workspace := t.TempDir()
	historyDir := filepath.Join(workspace, ".codeaf")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(historyDir, "replan-history.json"), []byte(
		`[{"timestamp":1,"action":"modify_dag","reason":"old"}]`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "fixture"
	output := filepath.Join(workspace, ".codeaf", "agents", "replanner", key+".json")
	var request agentjson.Request
	deps := Dependencies{
		PlanDB: PlanDBRunnerFunc(func(args []string) (plandb.RunResult, error) {
			if len(args) > 0 && args[0] == "status" {
				return plandb.RunResult{Stdout: []byte("state")}, nil
			}
			return plandb.RunResult{}, nil
		}),
		AgentJSON: agentjson.Dependencies{
			Resolver: agentjson.ResolverFunc(func(baked.Tier) []string { return []string{"p/m"} }),
			Client: agentjson.ClientFunc(func(_ context.Context, got agentjson.Request) error {
				request = got
				return os.WriteFile(output, []byte(
					`{"action":"continue","reason":"carry on","ops":null,"drop_ids":null,"abort_summary":null}`,
				), 0o644)
			}),
			NewID: func(prefix string) string { return prefix + "-id" },
		},
	}
	result, err := DispatchReplanner(context.Background(), DispatchReplannerInput{
		Workspace: workspace, ParentSessionID: "parent", UserGoal: "goal",
		EscalatedTaskIDs: []string{"t1"}, SessionKey: &key,
	}, deps)
	if err != nil || result.Data.Action != "continue" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if request.Agent != "replanner" ||
		!strings.Contains(request.TaskPrompt, "action=modify_dag, reason=old") ||
		!strings.Contains(request.TaskPrompt, "Escalated task IDs: t1") {
		t.Fatalf("request=%#v", request)
	}
}

func TestApplyReplanCountsNonzeroCLIResultsAndSplits(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	project := db.Init("replan")
	parent, err := db.AddTask(plandb.AddTaskInput{
		Title: "parent", Project: project.ID, Kind: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	runnerCalls := 0
	deps := Dependencies{
		PlanDB: PlanDBRunnerFunc(func([]string) (plandb.RunResult, error) {
			runnerCalls++
			return plandb.RunResult{Code: 1, Stderr: []byte("failed")}, nil
		}),
		NowMillis: func() int64 { return 1700000000123 },
	}
	decision := ReplanDecision{
		Action: "modify_dag", Reason: strings.Repeat("x", 205),
		Ops: []ReplanOp{
			AddOp{Op: "add", Title: "new", Kind: "code", Deps: []string{"a"}, Reason: "why"},
			CancelOp{Op: "cancel", ID: "a", Reason: "why"},
			AmendOp{Op: "amend", ID: "b", Prepend: "context"},
			SplitOp{Op: "split", ID: parent.ID, Reason: "large", Parts: []SplitPart{
				{Title: "A", Description: "part a", FileScope: []string{"a.ts"}},
				{Title: "B", Description: "part b", FileScope: nil},
			}},
		},
	}
	got := ApplyReplanDecision(decision, workspace, "unused", deps)
	if got.Abort || got.OpsApplied != 4 || runnerCalls != 3 ||
		got.Summary != "replanner:modify_dag — "+strings.Repeat("x", 200)+" (4 ops applied)" {
		t.Fatalf("result=%#v runnerCalls=%d", got, runnerCalls)
	}
	children := db.ListTasks(&plandb.ListTasksFilter{Parent: parent.ID})
	if len(children) != 2 || children[0].Description == nil ||
		!strings.HasPrefix(*children[0].Description, "file_scope: a.ts\n") {
		t.Fatalf("children=%#v", children)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, ".codeaf", "replan-history.json"))
	if err != nil {
		t.Fatal(err)
	}
	var history []map[string]any
	if json.Unmarshal(raw, &history) != nil || len(history) != 1 ||
		history[0]["opsApplied"] != float64(4) {
		t.Fatalf("history=%s", raw)
	}
}

func TestApplyAbortNullishSummary(t *testing.T) {
	empty := ""
	got := ApplyReplanDecision(ReplanDecision{
		Action: "abort", Reason: "fallback", AbortSummary: &empty,
	}, t.TempDir(), "", Dependencies{NowMillis: func() int64 { return 1 }})
	if !got.Abort || got.Summary != "" || got.OpsApplied != 0 {
		t.Fatalf("result=%#v", got)
	}
}

func TestApplyDoesNotEnforceFrozenConstraint(t *testing.T) {
	called := false
	got := ApplyReplanDecision(ReplanDecision{
		Action: "modify_dag", Reason: "replace signed-off leaf",
		Ops: []ReplanOp{
			CancelOp{Op: "cancel", ID: "frozen-task", Reason: "retry it"},
		},
	}, t.TempDir(), "", Dependencies{
		PlanDB: PlanDBRunnerFunc(func(args []string) (plandb.RunResult, error) {
			called = true
			if strings.Join(args, " ") != "task cancel frozen-task" {
				t.Fatalf("args=%#v", args)
			}
			return plandb.RunResult{}, nil
		}),
		NowMillis: func() int64 { return 1 },
	})
	if !called || got.OpsApplied != 1 {
		t.Fatalf("result=%#v called=%v", got, called)
	}
}

func TestShouldReplanNumberAndHistorySemantics(t *testing.T) {
	workspace := t.TempDir()
	write := func(entries string) {
		t.Helper()
		dir := filepath.Join(workspace, ".codeaf")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "replan-history.json"), []byte(entries), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`[{}, {}, {}]`)
	if !ShouldReplan(workspace, func(string) (string, bool) { return "", false }) {
		t.Fatal("default cap 4 rejected history length 3")
	}
	write(`[{}, {}, {}, {}]`)
	if ShouldReplan(workspace, func(string) (string, bool) { return "", false }) {
		t.Fatal("default cap 4 accepted history length 4")
	}
	for _, value := range []string{"0", "-1", "NaN", "Infinity", ""} {
		if ShouldReplan(workspace, func(string) (string, bool) { return value, true }) {
			t.Fatalf("invalid/disabled cap %q accepted", value)
		}
	}
	write(`[{}]`)
	if !ShouldReplan(workspace, func(string) (string, bool) { return "1.5", true }) {
		t.Fatal("fractional cap should accept length 1")
	}
}
