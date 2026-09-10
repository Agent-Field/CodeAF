package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAStandingReportIsPublishedInsideItsProjectOnly(t *testing.T) {
	workspace := t.TempDir()
	published, err := publishStandingReport(workspace, "reports/inbox.md", "  # Report\nall quiet  ")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "reports", "inbox.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if string(raw) != "# Report\nall quiet\n" || published.Path != "reports/inbox.md" || published.Bytes != len(raw) || published.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("published %+v wrote %q", published, raw)
	}

	// A folder planted as a symlink out of the project must not carry the write.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := publishStandingReport(workspace, "escape/report.md", "x"); err == nil {
		t.Fatal("a report was written through a symlink outside the project")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("something landed outside the project: %v", entries)
	}
	// And a report path that is itself a symlink is refused rather than followed.
	if err := os.Symlink(filepath.Join(outside, "target.md"), filepath.Join(workspace, "reports", "linked.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := publishStandingReport(workspace, "reports/linked.md", "x"); err == nil {
		t.Fatal("a symlinked report path was followed")
	}
}

func TestAFiringIsToldWhyItRunsAndWhereItsReportGoes(t *testing.T) {
	workspace := t.TempDir()
	item := standing.Item{Workspace: workspace, Does: standing.Action{Kind: standing.ActionTask, Report: "reports/r.md"}}
	block := standingReportBlock(item, "reports/r.md")
	if !strings.Contains(block, "FINAL REPLY is the complete report") || strings.Contains(block, "previous version is at") {
		t.Fatalf("first report block: %s", block)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if block := standingReportBlock(item, "reports/r.md"); !strings.Contains(block, "previous version is at reports/r.md") {
		t.Fatalf("an existing report was not offered: %s", block)
	}
	prior := time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)
	text := standingOccurrenceBlock(standing.Occurrence{Spec: 3, Because: "the files you are watching changed", PreviousFired: prior, Attempt: 2})
	for _, want := range []string{"instructions: version 3", "woken because: the files you are watching changed", "previous occurrence: " + prior.Local().Format(time.RFC3339), "attempt 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("occurrence block lacks %q:\n%s", want, text)
		}
	}
	if first := standingOccurrenceBlock(standing.Occurrence{Spec: 1}); !strings.Contains(first, "none; this is the first") {
		t.Fatalf("a first occurrence claims a previous one:\n%s", first)
	}
}

// reporting is a watch whose final reply is published to reports/r.md.
func reporting(workspace string) standing.Item {
	item := nightly(workspace)
	item.When = standing.When{Kind: standing.WhenFile, Glob: "inbox/*", Words: "when inbox/* changes"}
	item.Does.Report = "reports/r.md"
	return item
}

// THE REPORT IS THE ANSWER, NOT THE NARRATION BEFORE THE READING. The words a
// model says before it calls a tool are about what it is about to do; only the
// last turn's words after its last tool call are published.
func TestAReportIsTheFinalAnswerNotTheNarration(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "inbox", "a.md"), []byte("Decision: ship Friday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "inbox/a.md"})
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "Let me read the inbox first.")
			return toolResponseWithText("call_1", "read", string(args), "Let me read the inbox first."), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "# Report\n- Decision: ship Friday")
			return textResponse("# Report\n- Decision: ship Friday"), nil
		},
	}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "# Report\n- Decision: ship Friday\n" || outcome.Kind != "landed" || outcome.Published == nil || !strings.HasPrefix(outcome.Text, "report updated: reports/r.md") {
		t.Fatalf("published %q outcome %+v", raw, outcome)
	}
}

// A RUN CUT OFF MID-ANSWER DID NOT LAND, AND THE LAST GOOD REPORT STAYS. The
// live journey found this: a pass deadline arrived while the final answer was
// streaming, and half a report was published as landed.
func TestARunCutOffMidAnswerPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := "# Report\nthe last good one\n"
	if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "# Report\n- Decision: ship Fri")
			return nil, context.DeadlineExceeded
		},
	}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if outcome.Kind != standing.OutcomeFailed || outcome.Published != nil || string(raw) != previous || !strings.Contains(outcome.Text, "cut off") {
		t.Fatalf("a cut-off run: outcome %+v report %q", outcome, raw)
	}
}
