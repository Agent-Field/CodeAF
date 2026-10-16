package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The wire and the engine state their caps in two packages; they are one law.
func TestAppSpendReportCapsAreOneLaw(t *testing.T) {
	if appwire.SpendReportCellsMax != session.UsageReportMaxCells || appwire.SpendReportDaysMax != session.UsageReportMaxDays ||
		appwire.SpendReportReplyMax != session.UsageReportMaxBytes {
		t.Fatalf("wire (%d cells, %d days, %d bytes) disagrees with the engine (%d, %d, %d)",
			appwire.SpendReportCellsMax, appwire.SpendReportDaysMax, appwire.SpendReportReplyMax,
			session.UsageReportMaxCells, session.UsageReportMaxDays, session.UsageReportMaxBytes)
	}
}

// THE REPORT AND THE LEDGER ARE THE SAME BILL: the report's totals, and the sum
// of its cells, are what spend.ledger says for the same filter.
func TestAppSpendReportAgreesWithTheLedger(t *testing.T) {
	zone := time.FixedZone("EDT", -4*3600)
	ledgerAt(t, zone, appLedgerFixture(zone), "")
	b := &appBackend{}
	for _, filter := range []appwire.SpendLedgerFilter{
		{From: "2026-10-14", To: "2026-10-15"},
		{From: "2026-10-14", To: "2026-10-14", Model: "m/aux"},
		{From: "2026-10-14", To: "2026-10-15", Kind: appwire.SpendKindAgent, Subject: "conv0000000000002"},
	} {
		report, err := b.SpendReport(appwire.SpendReportAsk{Filter: filter})
		if err != nil {
			t.Fatal(err)
		}
		page, err := b.SpendLedger(appwire.SpendLedgerAsk{Filter: filter, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		lt, rt := page.Totals, report.Totals
		if rt.Records != lt.Records || rt.Calls != lt.Calls || rt.TokensIn != lt.TokensIn || rt.TokensOut != lt.TokensOut ||
			rt.UnpricedRecords != lt.UnpricedRecords || rt.UnbilledRecords != lt.UnbilledRecords || rt.RunlessRecords != lt.UnknownRunRecords ||
			(rt.CostUSD == nil) != (lt.CostUSD == nil) || (rt.CostUSD != nil && *rt.CostUSD != *lt.CostUSD) {
			t.Fatalf("%+v: report totals %+v, ledger totals %+v", filter, rt, lt)
		}
		var records, calls int
		var usd float64
		days := map[string]bool{}
		for _, c := range report.Cells {
			records += c.Records
			calls += c.Calls
			days[c.Day] = true
			if c.CostUSD != nil {
				usd += *c.CostUSD
			}
			if c.Kind != "" && c.Kind != appwire.SpendKindAgent {
				t.Fatalf("kind %q on the wire", c.Kind)
			}
		}
		if records != rt.Records || calls != rt.Calls || len(report.Cells) != rt.Cells || (rt.CostUSD != nil && usd != *rt.CostUSD) {
			t.Fatalf("%+v: cells add to %d/%d/%v, totals %+v", filter, records, calls, usd, rt)
		}
		if report.From != filter.From || report.To != filter.To {
			t.Fatalf("range echoed as %s..%s", report.From, report.To)
		}
		if filter.Model == "" && (!days["2026-10-14"] || !days["2026-10-15"]) {
			t.Fatalf("days = %v", days)
		}
	}
}

func TestAppSpendReportRefusesInTheWiresWords(t *testing.T) {
	zone := time.FixedZone("EDT", -4*3600)
	ledgerAt(t, zone, appLedgerFixture(zone), "")
	b := &appBackend{}
	// A report with no bounded range is a query error, not an all-history scan.
	if _, err := b.SpendReport(appwire.SpendReportAsk{Filter: appwire.SpendLedgerFilter{From: "2000-01-01", To: "2026-10-15"}}); wireErrorCode(err) != appwire.CodeBadParams {
		t.Fatalf("all history: %v", err)
	}
	// Too many groups is refused as bad_params, not cut.
	var lines []session.UsageLine
	base := time.Date(2026, 10, 14, 6, 0, 0, 0, zone)
	for i := range appwire.SpendReportCellsMax + 1 {
		lines = append(lines, session.UsageLine{At: base, Day: "2026-10-14", Model: "m/" + time.Duration(i).String(), Calls: 1, USD: 1.0 / 256})
	}
	ledgerAt(t, zone, lines, "")
	_, err := b.SpendReport(appwire.SpendReportAsk{Filter: appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}})
	if wireErrorCode(err) != appwire.CodeBadParams {
		t.Fatalf("too many groups: %v", err)
	}
	// A third reader beside two is busy.
	release := fillScanSlots(t)
	defer release()
	if _, err := b.SpendReport(appwire.SpendReportAsk{Filter: appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}}); wireErrorCode(err) != appwire.CodeBusy {
		t.Fatalf("busy: %v", err)
	}
}

func TestAppSpendReportOmitsWhatNoLineCarried(t *testing.T) {
	zone := time.FixedZone("EDT", -4*3600)
	at := time.Date(2026, 10, 14, 6, 0, 0, 0, zone)
	ledgerAt(t, zone, []session.UsageLine{{At: at, Day: "2026-10-14", Calls: 1, Unbilled: true}}, "")
	report, err := (&appBackend{}).SpendReport(appwire.SpendReportAsk{Filter: appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}})
	if err != nil || len(report.Cells) != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	c := report.Cells[0]
	if c.CostUSD != nil || c.Model != "" || c.Kind != "" || c.Subject != "" || c.UnbilledRecords != 1 || report.Totals.CostUSD != nil || len(report.Models) != 0 {
		t.Fatalf("an unpriced, unnamed, unattributed line must stay absent: %+v / %+v", c, report.Totals)
	}
}
