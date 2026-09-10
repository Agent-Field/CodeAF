package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// These are the final review's two blockers of 2026-09-10, each a run whose
// turns were not evidence of a finished report and which published anyway.
// Every case starts from a last good report and asserts its bytes survive.

const lastGoodReport = "# Report\nthe last good one\n"

// withLastGoodReport is a workspace whose report already holds lastGoodReport.
func withLastGoodReport(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte(lastGoodReport), 0o644); err != nil {
		t.Fatal(err)
	}
	return workspace
}

// expectUnpublished asserts the last good report is untouched and the run
// failed with the line that says why.
func expectUnpublished(t *testing.T, workspace string, outcome standing.Outcome, line string) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != lastGoodReport || outcome.Published != nil {
		t.Fatalf("the last good report was replaced with %q (outcome %+v)", raw, outcome)
	}
	if outcome.Kind != standing.OutcomeFailed || !strings.Contains(outcome.Text, line) {
		t.Fatalf("outcome %s %q, want failed saying %q", outcome.Kind, outcome.Text, line)
	}
}

// readingStep is a turn that says text and then calls read — one step of work
// after the words, so a step limit of one stops the run there.
func readingStep(workspace, text string) step {
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(workspace, "reports", "r.md")})
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		if text != "" {
			provider.Emit(ctx, provider.StreamDelta, text)
		}
		return toolResponseWithText("call_r", "read", string(args), text), nil
	}
}

// BLOCKER 1. The run tried to write its own report — refused, as every
// unattended write is — and then apologised, with no report between the lines.
// The apology is not a report, and the refused write's content never is: the
// report's authority is aforge's alone.
func TestARefusedWriteOfItsOwnReportWithNoReportLinesPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(workspace, "reports", "r.md"), "content": "the model's own copy"})
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "Now I'll write the report to the file.")
			return toolResponseWithText("call_w", "write", string(args), "Now I'll write the report to the file."), nil
		},
		saying("I cannot write the file; someone with permission needs to place it."),
	}}
	runner := standingChildRunner(t, root, model)
	runner.parent.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "tried to write its report instead of replying with it")
}

// BLOCKER 2, THE LIMIT. A step limit ends the run's turn normally — the
// interrupt closes it with no error — so a report written before the stopped
// work was published as though the run had finished.
func TestARunStoppedAtItsStepLimitPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{
		readingStep(workspace, "<report>\n# Report\n- written before the stopped work\n</report>"),
		saying("<report>\n# Report\n- after the limit\n</report>"),
	}}
	item := reporting(workspace)
	item.Does.MaxSteps = 1
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), item, filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "reached its step or spending limit")
}

// BLOCKER 2, THE UNCLOSED REPORT. A report opened and never closed ends
// wherever the run stopped writing, which is half a page.
func TestAReportOpenedAndNeverClosedIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{saying("<report>\n# Report\n- half a page, and then")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "has no closing line")
}

// BLOCKER 2, THE LIMIT DURING THE CORRECTION. The rules check sent the run
// back once, and the correction turn was stopped at the step limit after it
// wrote a report. The check was handed "not capped" before the correction ran,
// so the stop went unseen and the corrected report was published.
func TestACorrectionStoppedAtTheStepLimitPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{
		saying("<report>\n# Report\n- Ask alice@example.com to confirm.\n</report>"),
		readingStep(workspace, "<report>\n# Report\n- Ask [redacted] to confirm.\n</report>"),
		saying("<report>\n# Report\n- after the limit\n</report>"),
	}}
	runner, _ := ruledRunner(t, root, model, brokenVerdict, `{"kept": true}`)
	item := reporting(workspace)
	item.Does.MaxSteps = 1
	outcome, err := runner.Run(context.Background(), item, admittedRun(t, root), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "reached its step or spending limit")
}
