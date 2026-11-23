package cellindex

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestUsageUnownedChargeSurvivesDeadlineExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), session.UsageLedgerName)
	queueChargeThroughExit(t, path, session.UsageLine{ID: "last-charge", Model: "fixture/model", Calls: 1, Input: 10, Output: 1, USD: .009})
	before, err := session.ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("startup did not recover the journaled charge: %+v", before)
	}
	// Start the next writer as another independent call would.
	session.RecordUsage(path, session.UsageLine{ID: "next-process-call", Input: 1, USD: .001})
	if !session.StopUsageWriter(path) {
		t.Fatal("reopened writer did not stop")
	}
	rows, err := session.ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.ID == "last-charge" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unowned charge survived exit %d times, want once; ledger rows=%d", count, len(rows))
	}
}
