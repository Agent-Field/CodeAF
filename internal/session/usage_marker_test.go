package session

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMissingReceiptMarkersAreNotModelCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	t.Cleanup(func() { StopUsageWriter(path) })
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	RecordUnbilledCall(path, UsageLine{At: at, Day: "2026-11-01", Session: "chat", Model: "m", FactoryJob: "o/r#1"})
	if !FlushUsage() {
		t.Fatal("flush")
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	q := UsageQuery{From: at.Add(-time.Hour), To: at.Add(time.Hour)}
	page, err := QueryUsageLedger(path, q, "", 10, DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	if page.Totals.Calls != 0 || page.Totals.Unbilled != 1 || len(page.Entries) != 1 || page.Entries[0].Calls != 0 {
		t.Errorf("ledger=%+v", page)
	}
	walked, _, err := EachUsageLedgerEntry(path, q, DefaultUsageLedgerBudget, func(entry UsageEntry) error {
		if entry.Calls != 0 || entry.Unbilled != 1 {
			t.Errorf("ledger visitor=%+v", entry)
		}
		return nil
	})
	if err != nil || walked.Calls != 0 || walked.Unbilled != 1 {
		t.Errorf("ledger visitor totals=%+v err=%v", walked, err)
	}
	report, err := QueryUsageReport(path, q, time.UTC, UsageReportBudget{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.Calls != 0 || report.Totals.Unbilled != 1 || len(report.Cells) != 1 || report.Cells[0].Calls != 0 {
		t.Errorf("report=%+v", report)
	}
	calls, _, err := EachUsageCall(path, q, func(UsageCall) error { t.Error("marker listed as a model call"); return nil })
	if err != nil || calls.Records != 0 || calls.Requests != 0 {
		t.Errorf("calls=%+v err=%v", calls, err)
	}
	factory := FactoryUsageByModel(rows, "2026-11-01", "2026-11-01")
	if len(factory.Models) != 1 || factory.Models[0].Calls != 0 || factory.Models[0].Unbilled != 1 || factory.Jobs["o/r#1"][0].Calls != 0 {
		t.Errorf("factory=%+v", factory)
	}
	for _, got := range UsageByDay(rows, LastDays(at, 1)) {
		if got.Calls != 0 || got.USD != 0 {
			t.Errorf("daily spend=%+v", got)
		}
	}
	if got := UsageTotals(rows); got.Calls != 0 || got.USD != 0 {
		t.Errorf("spend totals=%+v", got)
	}
	for _, got := range UsageByModel(rows) {
		if got.Calls != 0 {
			t.Errorf("model spend=%+v", got)
		}
	}
	for _, got := range UsageBySubject(rows) {
		if got.Calls != 0 {
			t.Errorf("subject spend=%+v", got)
		}
	}
	receipt := UsageTree(rows, "chat")
	if receipt.Calls != 0 || receipt.Unbilled != 1 || receipt.Direct != 0 {
		t.Errorf("receipt=%+v", receipt)
	}
}
