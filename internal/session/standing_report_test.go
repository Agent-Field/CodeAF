package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
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
