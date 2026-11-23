package session

import (
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

// A process writer may be joined by a caller with a shorter clock scope. Its
// journal must keep the writer's scope and retain the caller's charge once.
func TestJournalSyncStaysWithTheProcessWriterAcrossCallerScopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	usageWriterFor(path)
	defer StopUsageWriter(path)
	synctest.Test(t, func(t *testing.T) {
		RecordUsage(path, UsageLine{ID: "scoped-call", Input: 1, USD: .01})
		if !StopUsageWriter(path) {
			t.Fatal("the process writer did not join inside the caller's scope")
		}
	})
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 || rows[0].ID != "scoped-call" || rows[0].USD != .01 {
		t.Fatalf("rows=%+v, err=%v; want the caller's charge once", rows, err)
	}
}
