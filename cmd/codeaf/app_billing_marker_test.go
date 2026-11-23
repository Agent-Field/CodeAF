package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestDesktopSpendingLabelsMarkersWithoutModelCalls(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	item, err := s.backend.local.host.floor.Create(factory.Item{Repo: "o/r", Num: 1, Title: "fixture", Tier: factory.TierOwner})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ledgerAt(t, time.UTC, []session.UsageLine{{At: at, Day: "2026-11-01", Session: "chat", Model: "m", FactoryJob: "o/r#1", Kind: executor.UnbilledMarkerKind, Unbilled: true}}, "")
	oldKinds := appFactoryModelKinds
	appFactoryModelKinds = func() map[string]string { return nil }
	t.Cleanup(func() { appFactoryModelKinds = oldKinds })
	filter := map[string]any{"from": "2026-11-01", "to": "2026-11-01"}
	var page appwire.SpendLedgerResult
	s.must("spend.ledger", filter, &page)
	if page.Totals.Calls != 0 || page.Totals.UnbilledRecords != 1 || len(page.Rows) != 1 || page.Rows[0].Calls != 0 || page.Rows[0].UnbilledRecords != 1 {
		t.Fatalf("ledger=%+v", page)
	}
	var report appwire.SpendReportResult
	s.must("spend.report", filter, &report)
	if report.Totals.Calls != 0 || report.Totals.UnbilledRecords != 1 || len(report.Cells) != 1 || report.Cells[0].Calls != 0 || report.Cells[0].UnbilledRecords != 1 {
		t.Fatalf("report=%+v", report)
	}
	var exported appwire.SpendLedgerExportResult
	s.must("spend.ledger.export", map[string]any{"from": "2026-11-01", "to": "2026-11-01", "destination": filepath.Join(t.TempDir(), "ledger.csv")}, &exported)
	if exported.Totals.Calls != 0 || exported.Totals.UnbilledRecords != 1 || len(readCSV(t, exported.Path)) != 2 {
		t.Fatalf("ledger export=%+v", exported)
	}
	var calls appwire.SpendCallsExportResult
	s.must("spend.calls.export", map[string]any{"from": "2026-11-01", "to": "2026-11-01", "destination": filepath.Join(t.TempDir(), "calls.csv")}, &calls)
	if calls.Rows != 0 || calls.Totals.Calls != 0 || len(readCSV(t, calls.Path)) != 1 {
		t.Fatalf("call export=%+v", calls)
	}
	var usage appwire.FactoryUsage
	s.must("factory.usage_by_model", map[string]any{"factory": st.Factory, "job": item.ID, "from": "2026-11-01", "to": "2026-11-01"}, &usage)
	if len(usage.Models) != 1 || usage.Models[0].Calls != 0 || usage.Models[0].UnbilledCalls != 1 || len(usage.Jobs) != 1 || usage.Jobs[0].Models[0].Calls != 0 {
		t.Fatalf("Factory=%+v", usage)
	}
}

func TestDesktopLegacyCallExportSkipsMarkers(t *testing.T) {
	backend := settingsBackend(t)
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	path := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(path) })
	session.RecordUnbilledCall(path, session.UsageLine{At: at, Model: "missing"})
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	out, err := backend.ExportSpend(at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rows := readCSV(t, out); len(rows) != 1 {
		t.Fatalf("marker listed as a model call: %v", rows)
	}
}
