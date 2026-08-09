package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/specclauses"
)

type clauseJudgerFunc func(context.Context, string, string) (specclauses.SpecClauseJudgment, error)

func (f clauseJudgerFunc) JudgeSpecClauses(
	ctx context.Context, spec, workspace string,
) (specclauses.SpecClauseJudgment, error) {
	return f(ctx, spec, workspace)
}

func TestDispatchReminderContainsClauseInventoryAndRootDemandsContract(t *testing.T) {
	// Validation contract C2: a coder leaf receives both the dense task clause
	// inventory and the complete root demands used by the auditor.
	h := newTerminalHarness(t, LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "implemented"}}})
	h.scheduler.adaptiveCuts = true
	issuePath := filepath.Join(h.scheduler.workspace, "leaf-issue.md")
	if err := os.WriteFile(issuePath, []byte("dense issue clauses"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskClauses := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	rootClauses := []string{"root one", "root two"}
	h.scheduler.clauses = clauseJudgerFunc(func(_ context.Context, spec, _ string) (specclauses.SpecClauseJudgment, error) {
		clauses := rootClauses
		if strings.Contains(spec, "dense issue clauses") {
			clauses = taskClauses
		}
		return specclauses.SpecClauseJudgment{Clauses: clauses, Count: float64(len(clauses)), Source: "llm"}, nil
	})
	task := phase2Task("root")
	description := "issue_file: " + issuePath + "\nfile_scope: src/leaf.go\nImplement the leaf."
	task.Description = &description
	result := h.dispatch(task)
	if !result.Success {
		t.Fatalf("dispatch = %+v", result)
	}
	if len(h.step.requests) != 1 {
		t.Fatalf("leaf requests = %d, want 1", len(h.step.requests))
	}
	reminder := h.step.requests[0].SystemReminder
	for _, want := range []string{
		"# Verification contract — spec clause inventory",
		"5. epsilon",
		"Complete spec demand list (survives excerpt truncation):",
		"2. root two",
	} {
		if !strings.Contains(reminder, want) {
			t.Fatalf("reminder missing %q:\n%s", want, reminder)
		}
	}
}
