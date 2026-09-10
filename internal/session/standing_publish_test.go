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
// failed with the line that says why, recorded with its code.
func expectUnpublished(t *testing.T, workspace string, outcome standing.Outcome, code, line string) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != lastGoodReport || outcome.Published != nil {
		t.Fatalf("the last good report was replaced with %q (outcome %+v)", raw, outcome)
	}
	if outcome.Kind != standing.OutcomeFailed || !strings.Contains(outcome.Text, line) {
		t.Fatalf("outcome %s %q, want failed saying %q", outcome.Kind, outcome.Text, line)
	}
	if outcome.Withheld != code {
		t.Fatalf("withheld code %q, want %q", outcome.Withheld, code)
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
	expectUnpublished(t, workspace, outcome, "self-write", "tried to write its report instead of replying with it")
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
	expectUnpublished(t, workspace, outcome, "at-a-limit", "reached its step or spending limit")
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
	expectUnpublished(t, workspace, outcome, "unclosed-report", "has no closing line")
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
	expectUnpublished(t, workspace, outcome, "at-a-limit", "reached its step or spending limit")
}

// BLOCKER A. A report between both lines with nothing in it is no report. It
// replaced the last good one with an empty line.
func TestAnEmptyReportBetweenItsLinesIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{saying("<report>\n</report>")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "empty-report", "report was empty")
}

// cutAtTheLimit is a tool-free answer that stopped at the provider's output
// limit — the shape the loop continues twice and then gives up on.
func cutAtTheLimit(text string) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		provider.Emit(ctx, provider.StreamDelta, text)
		response := textResponse(text)
		response.Choices[0].FinishReason = "length"
		return response, nil
	}
}

// BLOCKER B. Three answers in a row stopped at the output limit: the loop asks
// for the rest twice, gives up, and ends the turn like any other. The partial
// answer — no report lines at all — was published as the report.
func TestAnAnswerCutAtTheOutputLimitIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{
		cutAtTheLimit("# Report\n- the first third"),
		cutAtTheLimit(" and the second"),
		cutAtTheLimit(" and still not the end"),
	}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "output-limit", "cut off at the model's output limit")
}

// BLOCKER B DURING THE CORRECTION. The rules check sent the run back once, and
// the correction ran out of continuations at the output limit.
func TestACorrectionCutAtTheOutputLimitIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{
		saying("<report>\n# Report\n- Ask alice@example.com to confirm.\n</report>"),
		cutAtTheLimit("# Report\n- Ask [redacted]"),
		cutAtTheLimit(" to confirm"),
		cutAtTheLimit(" the venue, and"),
	}}
	runner, _ := ruledRunner(t, root, model, brokenVerdict, `{"kept": true}`)
	outcome, err := runner.Run(context.Background(), reporting(workspace), admittedRun(t, root), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "output-limit", "cut off at the model's output limit")
}

// A report with a closing line and no opening line has no start anyone can
// tell. It used to fall through to "no lines" and publish the whole answer —
// the narration before it and the stray tag with it.
func TestAReportClosedButNeverOpenedIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{saying("Here is the update.\n# Report\n- ship Friday\n</report>")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "unopened-report", "has a closing line but no opening line")
}

// THE CODES ARE IDENTIFIERS. A front end reads occurrence.json's "withheld"
// instead of the line, so this set is pinned exactly: renaming, adding or
// dropping one fails here, and doing it on purpose means changing this test
// and saying so in a change entry.
func TestTheWithheldCodesArePinned(t *testing.T) {
	want := map[reportWithheld]string{
		withheldForAPerson:  "waiting-on-person",
		withheldCutOff:      "cut-off",
		withheldOutputLimit: "output-limit",
		withheldAtALimit:    "at-a-limit",
		withheldUnclosed:    "unclosed-report",
		withheldUnopened:    "unopened-report",
		withheldEmpty:       "empty-report",
		withheldSelfWrite:   "self-write",
		withheldNoReport:    "no-report",
		withheldByRules:     "held-by-rules",
		withheldStopped:     "stopped",
		withheldUnwritten:   "not-written",
	}
	if len(withheldCodes) != len(want) {
		t.Fatalf("withheldCodes has %d codes, the pinned set has %d", len(withheldCodes), len(want))
	}
	for reason, code := range want {
		if withheldCodes[reason] != code {
			t.Errorf("reason %d is coded %q, pinned as %q", reason, withheldCodes[reason], code)
		}
	}
	// Every reason but "not withheld" has a code, and that one has none.
	for reason := notWithheld + 1; reason <= withheldUnwritten; reason++ {
		if reason.code() == "" {
			t.Errorf("reason %d has no code", reason)
		}
	}
	if notWithheld.code() != "" {
		t.Errorf("a run nothing withheld is coded %q", notWithheld.code())
	}
}
