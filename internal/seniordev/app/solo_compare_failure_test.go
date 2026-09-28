//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// A failed tree comparison gives no evidence about the checkout left behind.
// The terminal and the ending must both say it could not be checked.
func TestFailedCandidateComparisonEndsUnchecked(t *testing.T) {
	runner, state, _, _ := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "candidate\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "untested later bytes\n"); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(runner.workspace, ".git")
	hidden := filepath.Join(runner.workspace, ".git-paused")
	if err := os.Rename(gitDir, hidden); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(hidden, gitDir)
	outcome := &soloOutcome{Status: "pass", Frozen: state.candidate()}
	runner.soloRestoreIfDiverged(state, outcome)
	runner.soloTerminal(outcome, "submitted, and its build and tests passed")
	ending := endingOf(pipelineResult{Status: delegate.StatusPass, Terminal: outcome.TerminalData})
	if outcome.Status == "pass" || strings.Contains(ending.Message, "build and tests passed") {
		t.Fatalf("comparison failed but status=%q, ending=%q, restore_failed=%q", outcome.Status, ending.Message, outcome.RestoreFailed)
	}
	if reason := outcome.TerminalData["reason"].(string); strings.Contains(reason, "build and tests passed") || !strings.Contains(reason, "could not be checked") {
		t.Fatalf("terminal reason disagrees with unchecked status: %q", reason)
	}
	if !strings.Contains(outcome.RestoreFailed, "could not compare the folder with the submitted candidate") {
		t.Fatalf("comparison failure was not recorded: %q", outcome.RestoreFailed)
	}
	if !strings.Contains(ending.Message, "could not be checked against what was verified") {
		t.Fatalf("ending hid the failed comparison: %q", ending.Message)
	}
}
