package session

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

var reportZone = time.FixedZone("EDT", -4*3600)

func reportQuery(t testing.TB, from, to string) UsageQuery {
	t.Helper()
	start, end, err := UsageDayRange(from, to, reportZone)
	if err != nil {
		t.Fatal(err)
	}
	return UsageQuery{From: start, To: end}
}

func reportLine(day, hour int, model string, usd float64) UsageLine {
	at := time.Date(2026, 10, day, hour, 0, 0, 0, reportZone)
	return UsageLine{At: at, Day: at.Format(usageDayLayout), Model: model, Calls: 1, Input: 10, Output: 3, USD: usd}
}

func cellOf(t *testing.T, r UsageReport, day, model, kind, subject, root string) UsageReportCell {
	t.Helper()
	for _, c := range r.Cells {
		if c.Day == day && c.Model == model && c.Kind == kind && c.Subject == subject && c.Root == root {
			return c
		}
	}
	t.Fatalf("no cell %s %q %s %q %q in %+v", day, model, kind, subject, root, r.Cells)
	return UsageReportCell{}
}

func TestReportAddsUpByDayModelAndSubject(t *testing.T) {
	task1 := reportLine(15, 12, "m/b", cent)
	task1.Task, task1.Root = "7", "convA"
	task2 := reportLine(15, 12, "m/b", cent)
	task2.Task, task2.Root = "7", "convB" // the same task id in another conversation
	old := reportLine(15, 13, "m/b", 2*cent)
	old.Calls = 41 // an older line covers a whole turn
	unbilled := reportLine(15, 14, "m/b", 0)
	unbilled.Unbilled = true
	auto := reportLine(15, 15, "m/b", cent)
	auto.Automation, auto.Run = "auto1", "run1"
	standing := reportLine(15, 16, "m/b", cent)
	standing.Standing = "auto1"
	lines := []UsageLine{
		reportLine(14, 9, "m/a", cent), reportLine(14, 10, "m/a", cent),
		reportLine(14, 11, "m/b", 0),    // unpriced: not free
		reportLine(15, 9, "", 2*cent),   // nobody named the model
		reportLine(15, 23, "m/a", cent), // late on the 15th
		reportLine(16, 1, "m/a", cent),  // outside the range
		reportLine(13, 23, "m/a", cent), // outside the range
		task1, task2, old, unbilled, auto, standing,
	}
	path := writeLedger(t, lines, []string{"not json", `{"at":"0001-01-01T00:00:00Z"}`}, `{"partial`)
	report, err := QueryUsageReport(path, reportQuery(t, "2026-10-14", "2026-10-15"), reportZone, UsageReportBudget{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Stats.Malformed != 2 {
		t.Fatalf("malformed = %d, want 2 visible", report.Stats.Malformed)
	}
	a14 := cellOf(t, report, "2026-10-14", "m/a", "", "", "")
	if a14.Records != 2 || a14.Calls != 2 || a14.USD != 2*cent || a14.Input != 20 || a14.Output != 6 {
		t.Fatalf("m/a on the 14th = %+v", a14)
	}
	if b := cellOf(t, report, "2026-10-14", "m/b", "", "", ""); b.Priced() || b.Unpriced != 1 {
		t.Fatalf("an unpriced cell must not read as a cost of nothing: %+v", b)
	}
	if u := cellOf(t, report, "2026-10-15", "", "", "", ""); u.Model != "" || u.USD != 2*cent {
		t.Fatalf("unnamed model cell = %+v", u)
	}
	if c := cellOf(t, report, "2026-10-15", "m/b", SubjectTask, "7", "convA"); c.Records != 1 {
		t.Fatalf("task in convA: %+v", c)
	}
	if c := cellOf(t, report, "2026-10-15", "m/b", SubjectTask, "7", "convB"); c.Records != 1 {
		t.Fatalf("task in convB: %+v", c)
	}
	// Calls and records stay apart: the old line is one record of 41 requests.
	c := cellOf(t, report, "2026-10-15", "m/b", "", "", "")
	if c.Records != 2 || c.Calls != 42 || c.Unbilled != 1 || c.Unpriced != 1 {
		t.Fatalf("legacy and unbilled lines: %+v", c)
	}
	// An automation, and a standing id read as one, are the same subject.
	if c := cellOf(t, report, "2026-10-15", "m/b", SubjectAutomation, "auto1", ""); c.Records != 2 {
		t.Fatalf("automation cell = %+v", c)
	}
	var cells, records, calls int
	var usd float64
	for _, c := range report.Cells {
		cells++
		records += c.Records
		calls += c.Calls
		usd += c.USD
	}
	if records != report.Totals.Records || calls != report.Totals.Calls || usd != report.Totals.USD {
		t.Fatalf("cells (%d, %d, %v) disagree with totals %+v", records, calls, usd, report.Totals)
	}
	if report.Totals.Unpriced != 2 || report.Totals.Unbilled != 1 || report.Totals.UnknownRunRecords != 10 {
		t.Fatalf("totals = %+v", report.Totals)
	}
	if strings.Join(report.Models, ",") != "m/a,m/b" {
		t.Fatalf("models = %v", report.Models)
	}
	for i := 1; i < len(report.Cells); i++ {
		p, q := report.Cells[i-1], report.Cells[i]
		if p.Day > q.Day || (p.Day == q.Day && p.Model > q.Model) {
			t.Fatalf("cells are not sorted at %d: %+v then %+v", i, p, q)
		}
	}
	// The same filters the ledger takes narrow the report.
	q := reportQuery(t, "2026-10-14", "2026-10-15")
	q.Model = "m/a"
	narrow, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
	if err != nil || narrow.Totals.Records != 3 || len(narrow.Models) != 1 {
		t.Fatalf("model filter: %+v %v", narrow.Totals, err)
	}
}

func TestReportUsesTheStoredDayAndSaysWhenItDidNot(t *testing.T) {
	stored := reportLine(14, 9, "m/a", cent)
	stored.Day = "2026-10-15" // the writer's zone called it the 15th
	blank := reportLine(14, 10, "m/a", cent)
	blank.Day = ""
	junk := reportLine(14, 11, "m/a", cent)
	junk.Day = "yesterday"
	path := writeLedger(t, []UsageLine{stored, blank, junk}, nil, "")
	report, err := QueryUsageReport(path, reportQuery(t, "2026-10-14", "2026-10-14"), reportZone, UsageReportBudget{})
	if err != nil {
		t.Fatal(err)
	}
	if report.DaysDerived != 2 {
		t.Fatalf("days derived = %d", report.DaysDerived)
	}
	if cellOf(t, report, "2026-10-15", "m/a", "", "", "").Records != 1 || cellOf(t, report, "2026-10-14", "m/a", "", "", "").Records != 2 {
		t.Fatalf("cells = %+v", report.Cells)
	}
}

func TestReportNeedsABoundedRange(t *testing.T) {
	path := writeLedger(t, nil, nil, "")
	day := reportQuery(t, "2026-01-01", "2026-01-01")
	cases := map[string]UsageQuery{
		"no bounds":  {},
		"no from":    {To: day.To},
		"no to":      {From: day.From},
		"too long":   reportQuery(t, "2025-01-01", "2026-01-02"),
		"bad kind":   {From: day.From, To: day.To, Kind: "crew"},
		"empty span": {From: day.To, To: day.From},
	}
	for name, q := range cases {
		if _, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{}); !errors.Is(err, ErrUsageQuery) {
			t.Errorf("%s: err = %v, want ErrUsageQuery", name, err)
		}
	}
	// 366 inclusive days is the longest, across a leap year.
	if _, err := QueryUsageReport(path, reportQuery(t, "2024-01-01", "2024-12-31"), reportZone, UsageReportBudget{}); err != nil {
		t.Fatalf("a 366 day range: %v", err)
	}
}

func TestReportRefusesGroupsPastItsCapInsteadOfTruncating(t *testing.T) {
	var lines []UsageLine
	for i := range 40 {
		lines = append(lines, reportLine(14, 9, fmt.Sprintf("m/%02d", i), cent))
	}
	path := writeLedger(t, lines, nil, "")
	q := reportQuery(t, "2026-10-14", "2026-10-14")
	if r, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{MaxCells: 40}); err != nil || len(r.Cells) != 40 {
		t.Fatalf("exactly at the cap must answer whole: %d cells, %v", len(r.Cells), err)
	}
	r, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{MaxCells: 39})
	if !errors.Is(err, ErrUsageReportTooLarge) || len(r.Cells) != 0 || r.Totals.Records != 0 {
		t.Fatalf("one past the cap must refuse and answer nothing: %+v %v", r, err)
	}
	// A larger budget than the ceiling is not honoured.
	if got := (UsageReportBudget{MaxCells: 1 << 30, MaxBytes: 1 << 40}).bounded(); got.MaxCells != UsageReportMaxCells || got.MaxBytes != UsageReportMaxBytes {
		t.Fatalf("budget = %+v", got)
	}
}

func TestReportRefusesHugeKeysBeforeCopyingThem(t *testing.T) {
	huge := reportLine(14, 9, strings.Repeat("m", 60<<10), cent)
	path := writeLedger(t, []UsageLine{huge}, nil, "")
	q := reportQuery(t, "2026-10-14", "2026-10-14")
	if _, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{MaxBytes: 32 << 10}); !errors.Is(err, ErrUsageReportTooLarge) {
		t.Fatalf("a 60 KiB model id under a 32 KiB budget: %v", err)
	}
	// Under a roomy budget the same line is reported whole, not clipped.
	r, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
	if err != nil || len(r.Cells) != 1 || len(r.Cells[0].Model) != 60<<10 {
		t.Fatalf("a long id must be kept whole or refused: %v", err)
	}
	// Many heavy groups exhaust the byte budget before the cell cap.
	var lines []UsageLine
	for i := range 100 {
		lines = append(lines, reportLine(14, 9, fmt.Sprintf("%s%03d", strings.Repeat("x", 50<<10), i), cent))
	}
	path = writeLedger(t, lines, nil, "")
	if _, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{}); !errors.Is(err, ErrUsageReportTooLarge) {
		t.Fatalf("100 groups of 50 KiB each: %v", err)
	}
}

func TestReportRefusesFiguresItCannotAdd(t *testing.T) {
	q := reportQuery(t, "2026-10-14", "2026-10-14")
	negative := reportLine(14, 9, "m/a", cent)
	negative.Input = -1
	huge := reportLine(14, 9, "m/a", math.MaxFloat64)
	for name, lines := range map[string][]UsageLine{
		"negative tokens": {negative},
		"cost overflow":   {huge, huge},
	} {
		path := writeLedger(t, lines, nil, "")
		if _, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{}); !errors.Is(err, ErrUsageLedgerFigures) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A cost the JSON decoder cannot read is a malformed line, counted, not a number.
	path := writeLedger(t, nil, []string{`{"at":"2026-10-14T09:00:00-04:00","day":"2026-10-14","usd":NaN}`, `{"at":"2026-10-14T09:00:00-04:00","day":"2026-10-14","usd":1e999}`}, "")
	r, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
	if err != nil || r.Stats.Malformed != 2 || len(r.Cells) != 0 {
		t.Fatalf("non-finite costs: %+v %v", r.Stats, err)
	}
}

func TestReportReadsOnePrefixAndMissingLedgerIsEmpty(t *testing.T) {
	q := reportQuery(t, "2026-10-14", "2026-10-14")
	r, err := QueryUsageReport(t.TempDir()+"/absent.jsonl", q, reportZone, UsageReportBudget{})
	if err != nil || len(r.Cells) != 0 || r.Stats.Bytes != 0 {
		t.Fatalf("absent ledger: %+v %v", r, err)
	}
	path := writeLedger(t, []UsageLine{reportLine(14, 9, "m/a", cent)}, nil, "")
	first, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
	if err != nil {
		t.Fatal(err)
	}
	// What is appended after the report is the next report's.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"at":"2026-10-14T10:00:00-04:00","day":"2026-10-14","model":"m/a","usd":0.5}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	second, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
	if err != nil || first.Totals.Records != 1 || second.Totals.Records != 2 || second.Stats.Bytes <= first.Stats.Bytes {
		t.Fatalf("first %+v second %+v %v", first.Totals, second.Totals, err)
	}
	// It is refused, not queued, when the scan slots are taken.
	usageScanSlots <- struct{}{}
	usageScanSlots <- struct{}{}
	defer func() { <-usageScanSlots; <-usageScanSlots }()
	if _, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{}); !errors.Is(err, ErrUsageScanBusy) {
		t.Fatalf("busy: %v", err)
	}
}

// reportRows writes n lines spread over 30 days, 12 models and 40 subjects.
func reportRows(tb testing.TB, n int) string {
	tb.Helper()
	var b strings.Builder
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, reportZone)
	for i := range n {
		at := base.Add(time.Duration(i%(30*24)) * time.Hour).Add(time.Duration(i) * time.Millisecond)
		fmt.Fprintf(&b, `{"at":%q,"day":%q,"model":"m/%02d","calls":1,"in":10,"out":3,"usd":0.0009765625,"session":"s%02d","task":"%d","root":"conv%02d"}`+"\n",
			at.Format(time.RFC3339Nano), at.Format(usageDayLayout), i%12, i%40, i%40, i%40)
	}
	path := tb.TempDir() + "/" + UsageLedgerName
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

func benchmarkReport(b *testing.B, n int) {
	path := reportRows(b, n)
	q := reportQuery(b, "2026-10-01", "2026-10-31")
	info, _ := os.Stat(path)
	b.SetBytes(info.Size())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r, err := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
		if err != nil || r.Totals.Records != n {
			b.Fatalf("records %d, want %d: %v", r.Totals.Records, n, err)
		}
	}
}

// The scan is linear in the ledger; these two sizes show the slope, not a bound.
func BenchmarkQueryUsageReport10kRows(b *testing.B)  { benchmarkReport(b, 10_000) }
func BenchmarkQueryUsageReport100kRows(b *testing.B) { benchmarkReport(b, 100_000) }
