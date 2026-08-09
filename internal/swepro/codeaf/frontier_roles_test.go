package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/session/auditorgate"
	"github.com/Agent-Field/swe-pro-go/internal/session/contract"
	"github.com/Agent-Field/swe-pro-go/internal/session/fixgenerator"
	"github.com/Agent-Field/swe-pro-go/internal/session/frontierplanning"
)

func frontierRoleRunner(
	t *testing.T,
	workspace string,
	backend backend,
	notes io.Writer,
) *pipeline {
	t.Helper()
	runner := newPipeline(cliArgs{
		High: "provider/high", Low: "provider/low", Frontier: "provider/frontier",
		EntryAgent: "coder",
	}, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(io.Discard), Notes: notes,
	})
	t.Cleanup(runner.runtime.Close)
	runner.entryAgent = "coder"
	if err := runner.runtime.ensureRootSession(
		context.Background(), runner.sessionID, "frontier roles", "coder",
	); err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestPlanArbiterTriggerBudgetAndCoderConsumption(t *testing.T) {
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_HARD", "1")
	t.Setenv("CODEAF_PLAN_ARB", "1")

	t.Run("structural disagreement dispatches frontier and seeds coder", func(t *testing.T) {
		var mu sync.Mutex
		requests := []turn{}
		backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			mu.Lock()
			requests = append(requests, request)
			mu.Unlock()
			switch request.Agent {
			case "plan-sketch":
				if request.ModelID == "low" {
					return turnResult{Text: `{"files":["internal/b.go"],"approach":"change B"}`}, nil
				}
				return turnResult{Text: `{"files":["internal/a.go"],"approach":"change A"}`}, nil
			case "plan-arbiter":
				return turnResult{Text: "Use internal/a.go and verify the registry first."}, nil
			case "coder":
				return turnResult{Text: "implemented"}, nil
			default:
				return turnResult{}, errors.New("unexpected agent " + request.Agent)
			}
		})
		var notes bytes.Buffer
		runner := frontierRoleRunner(t, t.TempDir(), backend, &notes)
		block := runner.runPlanArbitration(context.Background(), "Wire the remaining role.")
		if !strings.Contains(block, "Use internal/a.go and verify the registry first.") {
			t.Fatalf("plan block = %q", block)
		}
		if !strings.Contains(notes.String(), "structural disagreement (jaccard=0.00) — frontier arbitration") {
			t.Fatalf("notes = %q", notes.String())
		}
		runner.initialPlanBlock = block
		if err := runner.runDirectLeaf(context.Background(), "Wire the remaining role.", "coder", "", ""); err != nil {
			t.Fatal(err)
		}

		mu.Lock()
		got := append([]turn(nil), requests...)
		mu.Unlock()
		arbiterCalls := 0
		coderPrompt := ""
		for _, request := range got {
			switch request.Agent {
			case "plan-arbiter":
				arbiterCalls++
				if request.ModelID != "frontier" ||
					!strings.Contains(request.Prompt, "files: internal/a.go") ||
					!strings.Contains(request.Prompt, "files: internal/b.go") {
					t.Fatalf("arbiter request = %+v", request)
				}
			case "coder":
				coderPrompt = request.Prompt
			}
		}
		if arbiterCalls != 1 {
			t.Fatalf("arbiter calls = %d, requests=%#v", arbiterCalls, got)
		}
		if !strings.Contains(coderPrompt, block) {
			t.Fatalf("coder prompt omitted arbitrated plan:\n%s", coderPrompt)
		}
	})

	t.Run("exhausted arbitration budget uses sketch A and proceeds", func(t *testing.T) {
		var mu sync.Mutex
		agents := []string{}
		backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			mu.Lock()
			agents = append(agents, request.Agent)
			mu.Unlock()
			if request.Agent != "plan-sketch" {
				return turnResult{}, errors.New("unexpected dispatch " + request.Agent)
			}
			if request.ModelID == "low" {
				return turnResult{Text: `{"files":["b.go"],"approach":"B"}`}, nil
			}
			return turnResult{Text: `{"files":["a.go"],"approach":"A"}`}, nil
		})
		runner := frontierRoleRunner(t, t.TempDir(), backend, io.Discard)
		if !runner.frontierPlanning.TryTake(frontierplanning.RoleSketchArbitration) {
			t.Fatal("failed to pre-consume arbitration budget")
		}
		block := runner.runPlanArbitration(context.Background(), "Goal")
		if !strings.Contains(block, "files: a.go") || strings.Contains(block, "files: b.go") {
			t.Fatalf("budget fallback block = %q", block)
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(strings.Join(agents, ","), "plan-arbiter") || len(agents) != 2 {
			t.Fatalf("agents = %v", agents)
		}
	})

	t.Run("arbiter dispatch failure falls back to sketch A", func(t *testing.T) {
		backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			if request.Agent == "plan-arbiter" {
				return turnResult{}, errors.New("provider unavailable")
			}
			if request.ModelID == "low" {
				return turnResult{Text: `{"files":["b.go"],"approach":"B"}`}, nil
			}
			return turnResult{Text: `{"files":["a.go"],"approach":"A"}`}, nil
		})
		runner := frontierRoleRunner(t, t.TempDir(), backend, io.Discard)
		block := runner.runPlanArbitration(context.Background(), "Goal")
		if !strings.Contains(block, "files: a.go") || strings.Contains(block, "files: b.go") {
			t.Fatalf("dispatch failure fallback block = %q", block)
		}
	})

	t.Run("without hard-mode trigger behavior is unchanged", func(t *testing.T) {
		t.Setenv("CODEAF_HARD", "0")
		calls := 0
		runner := frontierRoleRunner(t, t.TempDir(), backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			calls++
			return turnResult{}, errors.New("unexpected dispatch " + request.Agent)
		}), io.Discard)
		if block := runner.runPlanArbitration(context.Background(), "Goal"); block != "" || calls != 0 {
			t.Fatalf("block=%q calls=%d", block, calls)
		}
	})
}

func TestContractReviewerTriggerBudgetConsumptionAndFallback(t *testing.T) {
	t.Setenv("CODEAF_CONTRACT_REVIEW", "1")
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "acceptance.test"), []byte("assert exact behavior\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registered := &contract.Contract{Command: "go test ./...", Paths: []string{"acceptance.test"}}
	result := &contract.ContractResult{Pass: true, ExitCode: 0, TailOutput: "PASS\n"}

	t.Run("one shot renders into auditor contract evidence", func(t *testing.T) {
		calls := 0
		backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			calls++
			if request.Agent != "contract-reviewer" || request.ModelID != "frontier" ||
				!strings.Contains(request.Prompt, "assert exact behavior") ||
				!strings.Contains(request.Prompt, `"command":"go test ./..."`) ||
				!strings.Contains(request.Prompt, "pass=true exit=0\nPASS") {
				return turnResult{}, errors.New("bad contract-review request")
			}
			return turnResult{Text: `{"verdict":"weak","reasons":["misses the error path"],"suggestion":"add a failing case"}`}, nil
		})
		var notes bytes.Buffer
		runner := frontierRoleRunner(t, workspace, backend, &notes)
		block, attempted := runner.runContractReview(context.Background(), "Handle the error path.", registered, result)
		if !attempted || calls != 1 || !strings.Contains(block, "## Frontier contract review: WEAK") {
			t.Fatalf("attempted=%v calls=%d block=%q", attempted, calls, block)
		}
		contractEvidence := contract.ContractEvidenceBlock(*registered, *result) + "\n\n" + block
		auditorPrompt := auditorgate.BuildAuditPrompt(auditorgate.AuditPromptArgs{
			UserPrompt: "Handle the error path.", Workspace: workspace,
		}) + "\n\n" + contractEvidence
		if !strings.Contains(auditorPrompt, "misses the error path") ||
			!strings.Contains(auditorPrompt, "Weigh contract-pass evidence accordingly") {
			t.Fatalf("auditor prompt omitted review evidence:\n%s", auditorPrompt)
		}
		if !strings.Contains(notes.String(), "W12a contract-reviewer: dispatching (frontier one-shot)") {
			t.Fatalf("notes = %q", notes.String())
		}
	})

	t.Run("exhausted budget suppresses dispatch and proceeds", func(t *testing.T) {
		calls := 0
		runner := frontierRoleRunner(t, workspace, backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			calls++
			return turnResult{}, errors.New("unexpected dispatch " + request.Agent)
		}), io.Discard)
		runner.frontierPlanning.TryTake(frontierplanning.RoleContractReview)
		block, attempted := runner.runContractReview(context.Background(), "Goal", registered, result)
		if block != "" || attempted || calls != 0 {
			t.Fatalf("block=%q attempted=%v calls=%d", block, attempted, calls)
		}
	})

	t.Run("without both contract and machine result behavior is unchanged", func(t *testing.T) {
		calls := 0
		runner := frontierRoleRunner(t, workspace, backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			calls++
			return turnResult{}, errors.New("unexpected dispatch " + request.Agent)
		}), io.Discard)
		block, attempted := runner.runContractReview(context.Background(), "Goal", registered, nil)
		if block != "" || attempted || calls != 0 || runner.frontierPlanning.Used.ContractReview != 0 {
			t.Fatalf("block=%q attempted=%v calls=%d usage=%+v", block, attempted, calls, runner.frontierPlanning.Used)
		}
	})

	t.Run("dispatch failure is a consumed empty advisory", func(t *testing.T) {
		runner := frontierRoleRunner(t, workspace, backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			return turnResult{}, errors.New("provider unavailable")
		}), io.Discard)
		block, attempted := runner.runContractReview(context.Background(), "Goal", registered, result)
		if block != "" || !attempted || runner.frontierPlanning.Used.ContractReview != 1 {
			t.Fatalf("block=%q attempted=%v usage=%+v", block, attempted, runner.frontierPlanning.Used)
		}
	})
}

func TestRootCauseTriggerBudgetFixGeneratorConsumptionAndFallback(t *testing.T) {
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_ROOT_CAUSE", "1")
	workspace, base := guardWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, "broken.go"), []byte("package broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := "broken.go"
	line := 7.0
	originalHint := "keep the existing repair hint"
	verdict := auditorgate.AuditorVerdict{
		Verdict:     auditorgate.VerdictFail,
		Blockers:    []auditorgate.Blocker{{File: &file, Line: &line, Detail: "still broken"}},
		RepairHints: []string{originalHint},
	}
	contractResult := &contract.ContractResult{TailOutput: "latest contract failure"}

	t.Run("fix cycle two prepends clamped diagnosis to fix-generator", func(t *testing.T) {
		diagnosis := "  root\n\tcause  " + strings.Repeat("x", 1000)
		calls := 0
		backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			calls++
			if request.Agent != "root-cause" || request.ModelID != "frontier" ||
				!strings.Contains(request.Prompt, "entering fix cycle 2") ||
				!strings.Contains(request.Prompt, "broken.go:7 — still broken") ||
				!strings.Contains(request.Prompt, "latest contract failure") {
				return turnResult{}, errors.New("bad root-cause request")
			}
			return turnResult{Text: diagnosis}, nil
		})
		var notes bytes.Buffer
		runner := frontierRoleRunner(t, workspace, backend, &notes)
		modified := runner.withRootCauseDiagnosis(
			context.Background(), "Fix the broken behavior.", base, 2, verdict, contractResult,
		)
		if calls != 1 || len(modified.RepairHints) != 2 {
			t.Fatalf("calls=%d hints=%#v", calls, modified.RepairHints)
		}
		hint := modified.RepairHints[0]
		if !strings.HasPrefix(hint, "[frontier root-cause diagnosis] root cause ") ||
			strings.ContainsAny(hint, "\n\t") || len(utf16.Encode([]rune(strings.TrimPrefix(hint, "[frontier root-cause diagnosis] ")))) != 900 ||
			modified.RepairHints[1] != originalHint {
			t.Fatalf("root-cause hints = %#v", modified.RepairHints)
		}
		fixPrompt := fixgenerator.BuildFixGenPrompt(fixgenerator.PromptInput{
			Workspace: workspace, UserGoal: "Fix the broken behavior.", Verdict: modified, Cycle: 1,
		}, filepath.Join(workspace, ".codeaf", "fix.json"), "(no frozen leaves)")
		if !strings.Contains(fixPrompt, hint) || strings.Index(fixPrompt, hint) > strings.Index(fixPrompt, originalHint) {
			t.Fatalf("fix-generator prompt did not receive diagnosis as lead hint:\n%s", fixPrompt)
		}
		if !strings.Contains(notes.String(), "W12d root-cause: diagnosis attached as lead repair hint") {
			t.Fatalf("notes = %q", notes.String())
		}
	})

	t.Run("exhausted two-call budget suppresses dispatch", func(t *testing.T) {
		calls := 0
		runner := frontierRoleRunner(t, workspace, backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			calls++
			return turnResult{}, errors.New("unexpected dispatch " + request.Agent)
		}), io.Discard)
		runner.frontierPlanning.TryTake(frontierplanning.RoleRootCause)
		runner.frontierPlanning.TryTake(frontierplanning.RoleRootCause)
		modified := runner.withRootCauseDiagnosis(context.Background(), "Goal", base, 3, verdict, contractResult)
		if calls != 0 || len(modified.RepairHints) != 1 || modified.RepairHints[0] != originalHint {
			t.Fatalf("calls=%d hints=%#v", calls, modified.RepairHints)
		}
	})

	t.Run("before fix cycle two and on dispatch failure proceed unchanged", func(t *testing.T) {
		calls := 0
		runner := frontierRoleRunner(t, workspace, backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			calls++
			return turnResult{}, errors.New("provider unavailable")
		}), io.Discard)
		beforeTrigger := runner.withRootCauseDiagnosis(context.Background(), "Goal", base, 1, verdict, contractResult)
		if calls != 0 || len(beforeTrigger.RepairHints) != 1 {
			t.Fatalf("pre-trigger calls=%d hints=%#v", calls, beforeTrigger.RepairHints)
		}
		afterFailure := runner.withRootCauseDiagnosis(context.Background(), "Goal", base, 2, verdict, contractResult)
		if calls != 1 || len(afterFailure.RepairHints) != 1 || afterFailure.RepairHints[0] != originalHint {
			t.Fatalf("failure fallback calls=%d hints=%#v", calls, afterFailure.RepairHints)
		}
	})
}
