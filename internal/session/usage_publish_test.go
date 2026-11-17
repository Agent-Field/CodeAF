package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// Contract 5: real recorder rows survive a replacement while its append handle
// is held, and its next charge reaches the new history exactly once.
func TestPublishContract5UsageSurvivesReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	t.Cleanup(func() { StopUsageWriter(path) })
	before := UsageDrops()
	RecordUsage(path, UsageLine{At: time.Now(), Model: "test/model", Calls: 1, USD: 1.5})
	if !FlushUsage() {
		t.Fatal("first charge did not settle")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		t.Fatalf("first charge missing: %q, %v", raw, err)
	}
	next := path + ".next"
	if err := os.WriteFile(next, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := fsreplace.Rename(next, path); err != nil {
		t.Fatalf("replace held ledger: %v", err)
	}
	RecordUsage(path, UsageLine{At: time.Now(), Model: "test/model", Calls: 1, USD: 0.25})
	if !FlushUsage() {
		t.Fatal("next charge did not settle")
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("history = %+v, %v", rows, err)
	}
	if rows[0].USD != 1.5 || rows[1].USD != 0.25 || UsageDrops() != before {
		t.Fatalf("lost spend: rows=%+v dropped=%d", rows, UsageDrops()-before)
	}
}
