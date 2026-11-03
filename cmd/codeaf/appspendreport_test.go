package main

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
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

// A row made outside any job and any chat turn still reaches the page: the
// report has it, and spend.today moves without a turn ending.
func TestLedgerRowWithoutATurnMovesSpendToday(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	previous := appLedgerPath
	appLedgerPath = func() string { return path }
	t.Cleanup(func() { appLedgerPath = previous })
	t.Cleanup(func() { session.StopUsageWriter(path) })
	now := time.Now()
	day := now.Format(time.DateOnly)
	got := make(chan appwire.SpendChanged, 8)
	b := &appBackend{now: time.Now, today: func(at time.Time) float64 {
		usd, err := session.DailySpendUSD(path, at)
		if err != nil {
			t.Error(err)
		}
		return usd
	}, emit: appEmitFunc(func(event string, payload any) {
		if changed, ok := payload.(appwire.SpendChanged); ok && event == appwire.EventSpendChanged {
			got <- changed
		}
	})}
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	b.dayAuth.profile, b.dayAuth.settled = config.ProfileDir(), true
	t.Cleanup(b.watchSpendRows())
	rows := []session.UsageLine{
		{Role: "repo_ask", USD: .0061, Calls: 1, Session: "page", Input: 9},
		{Role: "pipeline_ai", USD: .0041, Calls: 1, Session: "page", Input: 9},
		{Role: "view_topic", USD: .0014, Calls: 1, Session: "page", Input: 9},
		{Role: "factory triage", USD: .1103, Calls: 1, Input: 9},
		{Role: "factory scout", USD: .0015, Calls: 1, Input: 9},
	}
	want := 0.0
	for _, row := range rows {
		row.At, row.Day, row.Model = now, day, "m/one"
		want += row.USD
		session.RecordUsage(path, row)
	}
	deadline := time.After(5 * time.Second)
	last := 0.0
	for math.Abs(last-want) > 1e-9 {
		select {
		case changed := <-got:
			last = changed.Spend.TodayUSD
		case <-deadline:
			t.Fatalf("spend.changed last read %.4f, ledger has %.4f", last, want)
		}
	}
	report, err := b.SpendReport(appwire.SpendReportAsk{Filter: appwire.SpendLedgerFilter{From: day, To: day}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.CostUSD == nil || math.Abs(*report.Totals.CostUSD-want) > 1e-9 || report.Totals.Records != len(rows) {
		t.Fatalf("spend.report totals %+v, ledger has %.4f in %d rows", report.Totals, want, len(rows))
	}
}

type appEmitFunc func(event string, payload any)

func (f appEmitFunc) Emit(event string, payload any) { f(event, payload) }

// Factory's own AI has its own figure on spend.today, so it isn't called a chat.
// A job's rows stay the job's, whatever their role.
func TestSpendTodayNamesFactoryAI(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	path := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(path) })
	b := &appBackend{now: time.Now}
	b.dayAuth.profile, b.dayAuth.settled = config.ProfileDir(), true
	now := time.Now()
	for _, row := range []session.UsageLine{
		{Role: factoryTriageName, USD: .1103, Input: 9},
		{Role: factoryScoutName, USD: .0015, Input: 9},
		{Role: "repo_ask", USD: .0061, Input: 9},
		{Role: "pipeline_ai", USD: .0041, Input: 9},
		{Role: "view_topic", USD: .0014, Input: 9},
		{Role: "markreader", USD: .0109, Input: 9},
		{USD: .02, Input: 9},
		{Role: "factory triage", USD: .03, Input: 9, FactoryJob: "acme/api#1"},
	} {
		row.At, row.Model = now, "m/one"
		session.RecordUsage(path, row)
	}
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	spend, err := b.SpendToday()
	if err != nil {
		t.Fatal(err)
	}
	if spend.FactoryAIUSD == nil || math.Abs(*spend.FactoryAIUSD-.1234) > 1e-9 || math.Abs(spend.TodayUSD-.1843) > 1e-9 {
		t.Fatalf("spend.today = %+v", spend)
	}
}
