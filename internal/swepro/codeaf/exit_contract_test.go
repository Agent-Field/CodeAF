package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/scheduler"
)

type failingTerminalBackend struct{}

func (failingTerminalBackend) Run(
	_ context.Context, request turn,
) (turnResult, error) {
	switch request.Agent {
	case "coder":
		if err := os.WriteFile(
			filepath.Join(request.Workspace, "terminal-contract.txt"),
			[]byte("changed\n"), 0o600,
		); err != nil {
			return turnResult{}, err
		}
		return turnResult{Text: "implementation finished"}, nil
	case "auditor", "auditor-light":
		body, err := json.Marshal(map[string]any{
			"verdict": "fail",
			"commands": []any{map[string]any{
				"cmd": "go test ./...", "exit": 1,
			}},
			"blockers": []any{map[string]any{
				"detail": "the validation suite still fails", "severity": "correctness",
			}},
			"repair_hints": []any{},
		})
		if err != nil {
			return turnResult{}, err
		}
		path := filepath.Join(request.Workspace, ".codeaf", "auditor-verdict.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return turnResult{}, err
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			return turnResult{}, err
		}
		return turnResult{Parts: []scheduler.LeafPart{{
			Type: "tool", Tool: "bash", ArgsKey: `{"command":"go test ./..."}`,
			Status: "completed",
		}}}, nil
	default:
		return turnResult{Text: "completed"}, nil
	}
}

func TestSupervisedFailWritesTerminalRecordAndReturnsSuccess(t *testing.T) {
	// Validation contract B1: a supervised child ending in fail records the
	// terminal outcome and exits zero so its parent can take another snapshot.
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	t.Setenv("CODEAF_ADMISSIBILITY", "0")
	t.Setenv("CODEAF_TAMPER", "0")
	t.Setenv("CODEAF_SPEC_IDS", "0")
	t.Setenv("CODEAF_SUPERVISED", "1")

	var stdout, stderr bytes.Buffer
	err := runCLI(context.Background(), []string{
		"run", "--dir", workspace, "--high", "provider/high",
		"--entry-agent", "coder", "Implement the requested behavior.",
	}, failingTerminalBackend{}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("supervised fail returned a process error: %v", err)
	}
	checkpoint := readResumeCheckpoint(workspace)
	if checkpoint == nil || checkpoint.FinalStatus != "fail" ||
		checkpoint.Reason != "the validation suite still fails" {
		t.Fatalf(
			"terminal checkpoint = %#v\nstdout:\n%s\nstderr:\n%s",
			checkpoint, stdout.String(), stderr.String(),
		)
	}
	if !strings.Contains(stdout.String(), `"type":"terminal"`) ||
		!strings.Contains(stdout.String(), `"status":"fail"`) {
		t.Fatalf("terminal event not written:\n%s", stdout.String())
	}
}

func TestRootDrainStallWritesFailTerminalAndReturnsSuccess(t *testing.T) {
	// C3/C4: the hard drain bound is a terminal work failure that a supervisor
	// can ratchet. It names the open row, emits fail (never crashed), and exits 0.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("CODEAF_PRE_GATES", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	t.Setenv("CODEAF_AUDITOR", "0")
	t.Setenv("CODEAF_AUTORESUME", "0")
	t.Setenv("CODEAF_DISPATCH_MAX_LOOPS", "1")
	t.Setenv("CODEAF_MAX_PARALLEL", "0")

	childAdded := false
	backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
		if request.Agent == "root-orchestrator" && !childAdded {
			childAdded = true
			if _, err := plandb.GetPlanDB().AddTask(plandb.AddTaskInput{
				Title: "unfinished bookkeeping", Project: request.PlanDB.ProjectID,
				Parent: plandb.TaskID(request.PlanDB.RootTaskID), CustomID: "t-stalled",
			}); err != nil {
				return turnResult{}, err
			}
		}
		return turnResult{SessionID: request.SessionID, Text: "completed"}, nil
	})
	var stdout, stderr bytes.Buffer
	err := runCLI(context.Background(), []string{
		"run", "--dir", workspace, "--high", "provider/high",
		"Implement the requested behavior.",
	}, backend, &stdout, &stderr)
	if err != nil {
		t.Fatalf("root drain stall returned a process error: %v", err)
	}
	checkpoint := readResumeCheckpoint(workspace)
	if checkpoint == nil || checkpoint.FinalStatus != "fail" ||
		!strings.Contains(checkpoint.Reason, "t-stalled") ||
		!strings.Contains(checkpoint.Reason, "unfinished bookkeeping") {
		t.Fatalf("stall checkpoint=%#v\nstdout:\n%s\nstderr:\n%s", checkpoint, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status":"fail"`) ||
		strings.Contains(stdout.String(), `"status":"crashed"`) {
		t.Fatalf("wrong terminal event:\n%s", stdout.String())
	}
}
