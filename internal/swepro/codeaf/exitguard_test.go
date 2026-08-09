package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
)

func TestRunFailureLedgerAdapterTranslatesFieldsAndLimit(t *testing.T) {
	severity, file, confidence := "high", "a.go", "medium"
	line, attempt := jscompat.JSNumber(17), jscompat.JSNumber(3)
	wantLimit := float64(50)
	called := false
	adapter := runFailureLedgerAdapter{
		recent: func(root string, limit ...float64) []*ledgers.LeafFailure {
			called = true
			if root != "root" || !reflect.DeepEqual(limit, []float64{wantLimit}) {
				t.Fatalf("recent args = %q, %#v", root, limit)
			}
			return []*ledgers.LeafFailure{{
				TaskID: "task", Title: "title", Reason: "reason",
				Bugs: []ledgers.FailureBug{{
					Severity: &severity, File: &file, Line: &line, Detail: "detail",
				}},
				RepairHints: []string{"split it"}, Confidence: &confidence,
				Attempt: &attempt, Timestamp: jscompat.JSNumber(1234),
			}}
		},
	}

	// Contract 1: the production ledger adapter preserves every field and the recency window.
	got := adapter.RecentFailures("root", int(wantLimit))
	lineWant, attemptWant := float64(17), float64(3)
	want := []steploop.LeafFailure{{
		TaskID: "task", Title: "title", Reason: "reason",
		Bugs: []steploop.FailureBug{{
			Severity: &severity, File: &file, Line: &lineWant, Detail: "detail",
		}},
		RepairHints: []string{"split it"}, Confidence: &confidence,
		Attempt: &attemptWant, Timestamp: 1234,
	}}
	if !called || !reflect.DeepEqual(got, want) {
		t.Fatalf("translated = %#v, want %#v", got, want)
	}
}

type exitGuardBackend struct {
	workspace string
	calls     int
	beforeRun func()
}

func (backend *exitGuardBackend) Run(ctx context.Context, request turn) (turnResult, error) {
	backend.calls++
	if backend.beforeRun != nil {
		backend.beforeRun()
	}
	if !strings.Contains(request.Prompt, "RECOVERY REQUIRED") {
		return turnResult{}, fmt.Errorf("recovery reminder missing from prompt")
	}
	path := filepath.Join(backend.workspace, ".codeaf", "agents", "exit-guard", "recovery.json")
	raw, _ := json.Marshal(recoveryDecision{Operations: []recoveryOperation{{
		Action: "cancel", TaskID: "failed", Reason: "accepted as impossible",
	}}})
	input, _ := json.Marshal(map[string]any{"filePath": path, "content": string(raw)})
	if request.Execute == nil {
		return turnResult{}, fmt.Errorf("write tool unavailable")
	}
	if _, err := request.Execute(ctx, steploop.ToolCall{Name: "write", Input: input}); err != nil {
		return turnResult{}, err
	}
	return turnResult{Text: "recovery planned"}, nil
}

func newExitGuardTestPipeline(t *testing.T, backend backend) *pipeline {
	t.Helper()
	workspace := t.TempDir()
	args, err := parseArgs([]string{"run", "--dir", workspace, "test exit guard"})
	if err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(args, workspace, pipelineDeps{Backend: backend})
	runner.entryAgent = "orchestrator"
	return runner
}

func restoreOpenFailureGraph() {
	plandb.GetPlanDB().Restore(plandb.Snapshot{
		Tasks: []*plandb.Task{
			{ID: "failed", Status: plandb.StatusFailed, Title: "failed leaf"},
			{ID: "dependent", Status: plandb.StatusPending, Title: "blocked dependent"},
		},
		Dependencies: []plandb.Dependency{{
			FromTask: "failed", ToTask: "dependent", Kind: plandb.DepFeedsInto,
		}},
	})
}

func TestExitGuardRecoversMarksAndRedispatchesOnce(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	restoreOpenFailureGraph()
	root := "guard-root-fires"
	ledgers.ClearLedger(root)
	ledgers.RecordLeafFailure(root, &ledgers.LeafFailure{
		TaskID: "failed", Title: "failed leaf", Reason: "repair cap exhausted",
		Bugs: []ledgers.FailureBug{}, RepairHints: []string{"split it"},
	})
	backend := &exitGuardBackend{}
	runner := newExitGuardTestPipeline(t, backend)
	backend.workspace = runner.workspace
	backend.beforeRun = func() {
		if !ledgers.WasFailureNudged(root, "failed") {
			t.Fatal("failure was not marked before the recovery turn")
		}
	}
	redispatches := 0

	// Contract 2: guard fires before audit, marks first, runs one recovery turn, then redispatches once.
	err := runner.exitGuardBeforeAudit(context.Background(), "project", root,
		func(context.Context, string, string, string) error {
			redispatches++
			if !ledgers.WasFailureNudged(root, "failed") {
				t.Fatal("failure was not marked before redispatch")
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if backend.calls != 1 || redispatches != 1 {
		t.Fatalf("recovery calls = %d, redispatches = %d", backend.calls, redispatches)
	}
	if !ledgers.WasFailureNudged(root, "failed") {
		t.Fatal("failure was not marked nudged")
	}
	if task := plandb.GetPlanDB().GetTask("failed"); task == nil || task.Status != plandb.StatusCancelled {
		t.Fatalf("recovery mutation did not cancel failure: %#v", task)
	}
}

func TestExitGuardAlreadyNudgedDoesNotFireTwice(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	restoreOpenFailureGraph()
	root := "guard-root-nudged"
	ledgers.ClearLedger(root)
	ledgers.RecordLeafFailure(root, &ledgers.LeafFailure{TaskID: "failed"})
	ledgers.MarkFailureNudged(root, "failed")
	backend := &exitGuardBackend{}
	runner := newExitGuardTestPipeline(t, backend)
	backend.workspace = runner.workspace
	redispatches := 0

	// Contract 3: a previously nudged open failure proceeds without another turn.
	err := runner.exitGuardBeforeAudit(context.Background(), "project", root,
		func(context.Context, string, string, string) error { redispatches++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if backend.calls != 0 || redispatches != 0 {
		t.Fatalf("recovery calls = %d, redispatches = %d", backend.calls, redispatches)
	}
}

func TestExitGuardNoOpenFailuresIsNoOp(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	restoreOpenFailureGraph()
	backend := &exitGuardBackend{}
	runner := newExitGuardTestPipeline(t, backend)
	backend.workspace = runner.workspace

	// Contract 4: without this-run failure evidence the quiet-to-audit transition is unchanged.
	err := runner.exitGuardBeforeAudit(context.Background(), "project", "guard-root-empty",
		func(context.Context, string, string, string) error { t.Fatal("unexpected redispatch"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if backend.calls != 0 {
		t.Fatalf("recovery calls = %d", backend.calls)
	}
}

func TestExitGuardRecoverySkipsUnrelatedHealthyTask(t *testing.T) {
	// Validation contract 3: recovery operations outside the failed candidate's
	// downstream graph are logged and skipped without mutating the healthy task.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	restoreOpenFailureGraph()
	plandb.GetPlanDB().Restore(plandb.Snapshot{
		Tasks: []*plandb.Task{
			{ID: "failed", Status: plandb.StatusFailed, Title: "failed leaf"},
			{ID: "dependent", Status: plandb.StatusPending, Title: "blocked dependent"},
			{ID: "healthy", Status: plandb.StatusDone, Title: "healthy task"},
		},
		Dependencies: []plandb.Dependency{{
			FromTask: "failed", ToTask: "dependent", Kind: plandb.DepFeedsInto,
		}},
	})
	var notes strings.Builder
	runner := newExitGuardTestPipeline(t, &exitGuardBackend{})
	runner.notes = &notes
	applied, failed := runner.applyRecoveryDecision(
		[]steploop.OpenFailure{{TaskID: "failed"}},
		recoveryDecision{Operations: []recoveryOperation{{
			Action: "cancel", TaskID: "healthy", Reason: "unrelated",
		}}},
	)
	if applied != 0 || failed != 1 {
		t.Fatalf("applied=%d failed=%d", applied, failed)
	}
	if task := plandb.GetPlanDB().GetTask("healthy"); task == nil || task.Status != plandb.StatusDone {
		t.Fatalf("healthy task was mutated: %#v", task)
	}
	if !strings.Contains(notes.String(), "out-of-scope recovery op: cancel healthy") {
		t.Fatalf("missing out-of-scope log: %q", notes.String())
	}
}

func TestExitGuardRecoveryAppliesSplitOnFailedCandidate(t *testing.T) {
	// Validation contract 3: the failed candidate itself remains eligible for
	// split and the children created by that operation become part of its subtree.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	restoreOpenFailureGraph()
	runner := newExitGuardTestPipeline(t, &exitGuardBackend{})
	applied, failed := runner.applyRecoveryDecision(
		[]steploop.OpenFailure{{TaskID: "failed"}},
		recoveryDecision{Operations: []recoveryOperation{{
			Action: "split", TaskID: "failed", Into: "smaller A, smaller B",
		}}},
	)
	if applied != 1 || failed != 0 {
		t.Fatalf("applied=%d failed=%d", applied, failed)
	}
	children := plandb.GetPlanDB().ListTasks(&plandb.ListTasksFilter{Parent: "failed"})
	if len(children) != 2 {
		t.Fatalf("split children = %#v", children)
	}
}
