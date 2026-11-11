package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

const callsCent = 1.0 / 1024

// callsFixture is two agents' day: chatA's own lines and its task's lines
// (the task's session is a node's, rooted at chatA), chatB's, an older line
// covering 41 requests, an unpriced one, an unbilled one, a call a minute
// before local midnight and one just after.
func callsFixture(zone *time.Location) []session.UsageLine {
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, zone) }
	line := func(l session.UsageLine) session.UsageLine { l.Day = l.At.Format("2006-01-02"); return l }
	return []session.UsageLine{
		line(session.UsageLine{At: at(14, 8, 0), Session: "chatA", Model: "m/one", Calls: 1, Input: 10, Output: 2, USD: 0.0123456789012345, Run: "r1"}),
		line(session.UsageLine{At: at(14, 8, 5), Session: "node1", Root: "chatA", Task: "3", Model: "m/two", Calls: 1, Input: 7, Output: 1, USD: callsCent, Run: "r2"}),
		line(session.UsageLine{At: at(14, 8, 6), Session: "chatB", Model: "m/one", Calls: 1, Input: 5, Output: 5, USD: 2 * callsCent}),
		line(session.UsageLine{At: at(14, 9, 0), Session: "chatA", Model: "m/one", Calls: 41, Input: 400, Output: 40, USD: 4 * callsCent}),
		line(session.UsageLine{At: at(14, 9, 1), Session: "chatA", Calls: 1, Input: 1, Output: 1}),
		line(session.UsageLine{At: at(14, 9, 2), Session: "node1", Root: "chatA", Task: "3", Model: "m/two", Calls: 1, Input: 2, Output: 2, Unbilled: true}),
		line(session.UsageLine{At: at(14, 23, 59), Session: "chatB", Model: "m/two", Input: 3, Output: 3, USD: callsCent}),
		line(session.UsageLine{At: at(15, 0, 0), Session: "chatA", Model: "m/one", Calls: 1, Input: 1, Output: 1, USD: callsCent}),
	}
}

func callsFilter() appwire.SpendLedgerFilter {
	return appwire.SpendLedgerFilter{From: "2026-10-14", To: "2026-10-14"}
}

func sumColumn(t *testing.T, rows [][]string, col int) (sum float64, blanks int) {
	t.Helper()
	for _, r := range rows[1:] {
		if r[col] == "" {
			blanks++
			continue
		}
		v, err := strconv.ParseFloat(r[col], 64)
		if err != nil {
			t.Fatal(err)
		}
		sum += v
	}
	return sum, blanks
}

func sumInts(t *testing.T, rows [][]string, col int) (sum int64) {
	t.Helper()
	for _, r := range rows[1:] {
		v, err := strconv.ParseInt(r[col], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		sum += v
	}
	return sum
}

// THE FILE IS WHAT THE FILTER SAYS, in the order it was recorded: twelve
// columns, the agent's chat and task lines together and not the other
// agent's, the date and time in the engine's zone, the amount at full
// precision and blank where nobody priced it, the older line's 41 requests
// kept as 41, and crew, provider and outcome blank on every row.
func TestAppSpendCallsExportWritesTheFilteredCalls(t *testing.T) {
	zone := time.FixedZone("Toronto", -4*3600)
	ledgerAt(t, zone, callsFixture(zone), "")
	b := &appBackend{}
	dir := t.TempDir()
	dest := filepath.Join(dir, "codeaf-spending-2026-10.csv")
	filter := callsFilter()
	filter.Agent = "chatA"
	out, err := b.ExportSpendCalls(appwire.SpendCallsExportAsk{Filter: filter, Destination: dest})
	if err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, dest)
	if got := strings.Join(rows[0], ","); got != "date,time,crew,agent,task,model,provider,outcome,amount_usd,requests,tokens_in,tokens_out" {
		t.Fatalf("header %q", got)
	}
	if len(rows) != 6 || out.Rows != 5 || out.Totals.Records != 5 {
		t.Fatalf("%d rows, answer %+v", len(rows), out)
	}
	want := [][]string{
		{"2026-10-14", "08:00:00", "", "chatA", "", "m/one", "", "", "0.0123456789012345", "1", "10", "2"},
		{"2026-10-14", "08:05:00", "", "chatA", "3", "m/two", "", "", "0.0009765625", "1", "7", "1"},
		{"2026-10-14", "09:00:00", "", "chatA", "", "m/one", "", "", "0.00390625", "41", "400", "40"},
		{"2026-10-14", "09:01:00", "", "chatA", "", "", "", "", "", "1", "1", "1"},
		{"2026-10-14", "09:02:00", "", "chatA", "3", "m/two", "", "", "", "1", "2", "2"},
	}
	for i, w := range want {
		if strings.Join(rows[i+1], "|") != strings.Join(w, "|") {
			t.Fatalf("row %d:\n got %q\nwant %q", i, rows[i+1], w)
		}
	}
	usd, blank := sumColumn(t, rows, 8)
	if out.Totals.CostUSD == nil || *out.Totals.CostUSD != usd || out.Totals.UnpricedRecords != blank || blank != 2 ||
		out.Totals.UnbilledRecords != 1 || out.Totals.Calls != int(sumInts(t, rows, 9)) || out.Totals.Calls != 45 ||
		out.Totals.TokensIn != sumInts(t, rows, 10) || out.Totals.TokensOut != sumInts(t, rows, 11) {
		t.Fatalf("the answer and the file disagree: %+v (usd %v, blanks %d)", out.Totals, usd, blank)
	}
	if info, err := os.Stat(dest); err != nil || info.Size() != out.Bytes || info.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v bytes %d", info, err, out.Bytes)
	}
	if out.Path != dest || out.Skipped != (appwire.SpendLedgerSkipped{}) {
		t.Fatalf("%+v", out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("%d entries beside the export", len(entries))
	}

	// chatB is the other agent's; the whole day is both, and the midnight edge
	// is the engine's local midnight, not UTC's.
	for agent, count := range map[string]int{"chatB": 2, "": 7, "nobody": 0} {
		f := callsFilter()
		f.Agent = agent
		res, err := b.ExportSpendCalls(appwire.SpendCallsExportAsk{Filter: f, Destination: filepath.Join(dir, "x-"+agent+".csv")})
		if err != nil || res.Rows != count || len(readCSV(t, res.Path)) != count+1 {
			t.Fatalf("agent %q: %v %+v", agent, err, res)
		}
	}
}

// EVERY FILTER IS THE LEDGER'S AND THE REPORT'S: the file's sums are the same
// figures spend.ledger and spend.report state for the same filter.
func TestAppSpendCallsExportAgreesWithLedgerAndReport(t *testing.T) {
	zone := time.FixedZone("Toronto", -4*3600)
	ledgerAt(t, zone, callsFixture(zone), "")
	b := &appBackend{}
	dir := t.TempDir()
	for name, mutate := range map[string]func(*appwire.SpendLedgerFilter){
		"day":   func(*appwire.SpendLedgerFilter) {},
		"agent": func(f *appwire.SpendLedgerFilter) { f.Agent = "chatA" },
		"model": func(f *appwire.SpendLedgerFilter) { f.Model = "m/two" },
		"task":  func(f *appwire.SpendLedgerFilter) { f.Kind, f.Subject, f.Root = appwire.SpendKindTask, "3", "chatA" },
		"run":   func(f *appwire.SpendLedgerFilter) { f.Run = "r1" },
		"two days": func(f *appwire.SpendLedgerFilter) {
			f.To = "2026-10-15"
		},
	} {
		filter := callsFilter()
		mutate(&filter)
		out, err := b.ExportSpendCalls(appwire.SpendCallsExportAsk{Filter: filter, Destination: filepath.Join(dir, name+".csv")})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		page, err := b.SpendLedger(appwire.SpendLedgerAsk{Filter: filter, Limit: appwire.SpendLedgerLimitMax})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		report, err := b.SpendReport(appwire.SpendReportAsk{Filter: filter})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		lt, rt, ct := page.Totals, report.Totals, out.Totals
		if ct.Records != lt.Records || ct.Records != rt.Records || ct.Calls != lt.Calls || ct.Calls != rt.Calls ||
			ct.TokensIn != lt.TokensIn || ct.TokensIn != rt.TokensIn || ct.TokensOut != lt.TokensOut || ct.TokensOut != rt.TokensOut ||
			ct.UnpricedRecords != lt.UnpricedRecords || ct.UnpricedRecords != rt.UnpricedRecords ||
			ct.UnbilledRecords != lt.UnbilledRecords || ct.UnbilledRecords != rt.UnbilledRecords {
			t.Fatalf("%s: file %+v, ledger %+v, report %+v", name, ct, lt, rt)
		}
		if (ct.CostUSD == nil) != (lt.CostUSD == nil) || (ct.CostUSD == nil) != (rt.CostUSD == nil) ||
			(ct.CostUSD != nil && (math.Abs(*ct.CostUSD-*lt.CostUSD) > 1e-12 || math.Abs(*ct.CostUSD-*rt.CostUSD) > 1e-12)) {
			t.Fatalf("%s: cost file %v, ledger %v, report %v", name, ct.CostUSD, lt.CostUSD, rt.CostUSD)
		}
		rows := readCSV(t, out.Path)
		if usd, _ := sumColumn(t, rows, 8); (ct.CostUSD != nil && usd != *ct.CostUSD) || (ct.CostUSD == nil && usd != 0) {
			t.Fatalf("%s: the file sums to %v, the answer says %v", name, usd, ct.CostUSD)
		}
	}
}

// THE EXACT BYTES OF AWKWARD TEXT SURVIVE: an id with a comma, quotes, a
// newline and multibyte text, and a destination with spaces, a quote, multibyte
// text and a TRAILING SPACE are used as given. (A model id is trimmed, as the
// filters trim, so its trailing space is the one thing not kept.)
func TestAppSpendCallsExportKeepsAwkwardTextAndPathsExact(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	odd := "mødel, \"q\"\nnew line é日本 "
	ledgerAt(t, time.UTC, []session.UsageLine{
		{At: at, Day: "2026-10-14", Session: "chat é", Task: "t,1", Model: odd, Calls: 1, Input: 1, Output: 1, USD: callsCent},
		{At: at, Day: "2026-10-14", Session: "=chat", Model: "-m", Calls: 1, Input: 1, Output: 1},
	}, "")
	b := &appBackend{}
	dir := t.TempDir()
	dest := filepath.Join(dir, " spend é \"q\", 日本.csv ")
	if _, err := b.ExportSpendCalls(appwire.SpendCallsExportAsk{Filter: callsFilter(), Destination: dest}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != filepath.Base(dest) {
		t.Fatalf("the file is %q, not %q", entries[0].Name(), filepath.Base(dest))
	}
	rows := readCSV(t, dest)
	if rows[1][3] != "chat é" || rows[1][4] != "t,1" || rows[1][5] != strings.TrimSpace(odd) {
		t.Fatalf("%q", rows[1])
	}
	// A cell a spreadsheet would run is written inert, as the ledger's export does.
	if rows[2][3] != "'=chat" || rows[2][5] != "'-m" || rows[2][8] != "" {
		t.Fatalf("%q", rows[2])
	}
}

// AN EMPTY RANGE IS A VALID FILE: the header alone, no cost, no rows — also
// from a machine that has never spent anything.
func TestAppSpendCallsExportOfNothingIsJustTheHeader(t *testing.T) {
	zone := time.UTC
	for name, ledger := range map[string]func(){
		"empty range":    func() { ledgerAt(t, zone, callsFixture(zone), "") },
		"no ledger file": func() { appLedgerPath = func() string { return filepath.Join(t.TempDir(), "absent.jsonl") } },
	} {
		ledger()
		dir := t.TempDir()
		dest := filepath.Join(dir, "spend.csv")
		filter := appwire.SpendLedgerFilter{From: "2025-01-01", To: "2025-01-02"}
		if name == "no ledger file" {
			filter = callsFilter()
		}
		out, err := (&appBackend{}).ExportSpendCalls(appwire.SpendCallsExportAsk{Filter: filter, Destination: dest})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw, _ := os.ReadFile(dest)
		const header = "date,time,crew,agent,task,model,provider,outcome,amount_usd,requests,tokens_in,tokens_out\n"
		if string(raw) != header || out.Rows != 0 || out.Bytes != int64(len(header)) || out.Totals != (appwire.SpendCallsTotals{}) {
			t.Fatalf("%s: %q %+v", name, raw, out)
		}
		if len(readCSV(t, dest)) != 1 {
			t.Fatalf("%s: not a CSV of one record", name)
		}
	}
}

// NO OVERWRITE IS NEVER A REPLACEMENT: an existing file is kept, a file created
// between the check and the move is kept (by a link that cannot replace, or by
// a refusal where links are not served), and nothing of ours is left behind.
func TestAppSpendCallsExportNeverReplacesAnExistingOrConcurrentFile(t *testing.T) {
	ledgerAt(t, time.UTC, callsFixture(time.UTC), "")
	b := &appBackend{}
	ask := func(dest string, overwrite bool) appwire.SpendCallsExportAsk {
		return appwire.SpendCallsExportAsk{Filter: callsFilter(), Destination: dest, Overwrite: overwrite}
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "spend.csv")
	if err := os.WriteFile(dest, []byte("the person's own file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := b.ExportSpendCalls(ask(dest, false))
	if wireErrorCode(err) != appwire.CodeBadParams || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("%v", err)
	}
	keptIntact(t, dir, dest, "the person's own file\n")

	oldLink := appLinkFile
	t.Cleanup(func() { appLinkFile = oldLink })
	for name, link := range map[string]func(string, string) error{
		"links":    os.Link,
		"no links": func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} },
	} {
		dir := t.TempDir()
		dest := filepath.Join(dir, "spend.csv")
		appLinkFile = func(from, to string) error {
			if err := os.WriteFile(to, []byte("theirs\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return link(from, to)
		}
		if _, err := b.ExportSpendCalls(ask(dest, false)); wireErrorCode(err) != appwire.CodeBadParams {
			t.Fatalf("%s: %v", name, err)
		}
		keptIntact(t, dir, dest, "theirs\n")
	}

	// Where links are not served and nothing is there, it is refused, not
	// written by looking first and renaming after.
	dir = t.TempDir()
	dest = filepath.Join(dir, "spend.csv")
	appLinkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} }
	if _, err := b.ExportSpendCalls(ask(dest, false)); wireErrorCode(err) != appwire.CodeBadParams {
		t.Fatalf("%v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("%d entries left by a refused export", len(entries))
	}
	appLinkFile = oldLink

	// With overwrite the old file is replaced whole.
	dest = filepath.Join(t.TempDir(), "spend.csv")
	if err := os.WriteFile(dest, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := b.ExportSpendCalls(ask(dest, true))
	if err != nil || len(readCSV(t, dest)) != out.Rows+1 || out.Rows != 7 {
		t.Fatalf("%v %+v", err, out)
	}
	// A destination that is not a file, or has no folder, is the asker's to fix.
	for _, bad := range []string{filepath.Join(dir, "missing", "spend.csv"), dir} {
		if _, err := b.ExportSpendCalls(ask(bad, true)); wireErrorCode(err) != appwire.CodeBadParams {
			t.Fatalf("%s: %v", bad, err)
		}
	}
}

// A figure the ledger cannot add up is refused by name after the file was
// begun: the part is removed and the destination is exactly as it was.
func TestAppSpendCallsExportRefusalCleansItsPartAndKeepsTheDestination(t *testing.T) {
	at := time.Date(2026, 10, 14, 6, 0, 0, 0, time.UTC)
	good := session.UsageLine{At: at, Session: "s", Input: 1, USD: callsCent}
	for name, bad := range map[string]session.UsageLine{
		"cost overflow":   {At: at, Session: "s", USD: math.MaxFloat64},
		"tokens overflow": {At: at, Session: "s", Input: math.MaxInt},
		"negative tokens": {At: at, Session: "s", Output: -1},
		"negative cost":   {At: at, Session: "s", Input: 1, USD: -1},
	} {
		lines := []session.UsageLine{good, bad}
		switch name {
		case "cost overflow":
			lines = []session.UsageLine{bad, bad}
		case "tokens overflow":
			lines = []session.UsageLine{bad, bad}
		}
		ledgerAt(t, time.UTC, lines, "")
		dir := t.TempDir()
		dest := filepath.Join(dir, "spend.csv")
		if err := os.WriteFile(dest, []byte("kept\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := (&appBackend{}).ExportSpendCalls(appwire.SpendCallsExportAsk{Filter: callsFilter(), Destination: dest, Overwrite: true})
		if wireErrorCode(err) != appwire.CodeBadParams || !errors.Is(err, session.ErrUsageLedgerFigures) && !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("%s: %v", name, err)
		}
		keptIntact(t, dir, dest, "kept\n")
	}
}

// failingWriter accepts n bytes and then fails, as a full disk does.
type failingWriter struct {
	w io.Writer
	n int
}

var errDiskFull = errors.New("no space left on device")

func (f *failingWriter) Write(p []byte) (int, error) {
	if len(p) > f.n {
		wrote := f.n
		f.n = 0
		if wrote > 0 {
			_, _ = f.w.Write(p[:wrote])
		}
		return wrote, errDiskFull
	}
	f.n -= len(p)
	return f.w.Write(p)
}

// A WRITE THAT FAILS PART WAY, through the real whole-file writer and the real
// pass: the error is the writer's, the part is removed, the old destination is
// untouched — whether the failure comes before the header, in the middle, or at
// the final flush.
func TestAppSpendCallsExportWriterFailureCleansUp(t *testing.T) {
	zone := time.UTC
	var lines []session.UsageLine
	for i := range 400 {
		at := time.Date(2026, 10, 14, 6, 0, 0, 0, zone).Add(time.Duration(i) * time.Second)
		lines = append(lines, session.UsageLine{At: at, Session: "chatA", Task: strconv.Itoa(i), Model: "m/some-long-model-name", Calls: 1, Input: i + 1, Output: 1, USD: callsCent})
	}
	ledgerAt(t, zone, lines, "")
	query, err := appLedgerQuery(callsFilter())
	if err != nil {
		t.Fatal(err)
	}
	// How long the whole file is, so the failures can be spread across it.
	var whole bytes.Buffer
	var probe appSpendCallsRun
	if err := appSpendCallsFill(appLedgerPath(), query, zone, &probe)(&whole); err != nil || probe.rows != 400 {
		t.Fatalf("%v %d", err, probe.rows)
	}
	for _, at := range []int{0, 3, 100, 4096, whole.Len() / 2, whole.Len() - 1} {
		dir := t.TempDir()
		dest := filepath.Join(dir, "spend.csv")
		if err := os.WriteFile(dest, []byte("kept\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var run appSpendCallsRun
		fill := appSpendCallsFill(appLedgerPath(), query, zone, &run)
		_, err := appWriteWhole(dest, true, func(w io.Writer) error { return fill(&failingWriter{w: w, n: at}) })
		if !errors.Is(err, errDiskFull) {
			t.Fatalf("fails at %d: %v", at, err)
		}
		keptIntact(t, dir, dest, "kept\n")
		// And with nothing at the destination, nothing is left at all.
		fresh := filepath.Join(dir, "fresh.csv")
		_, err = appWriteWhole(fresh, false, func(w io.Writer) error { return fill(&failingWriter{w: w, n: at}) })
		if !errors.Is(err, errDiskFull) {
			t.Fatalf("fails at %d: %v", at, err)
		}
		keptIntact(t, dir, dest, "kept\n")
		if _, err := os.Stat(fresh); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a failed export left %s", fresh)
		}
	}
	// A pass that is stopped by its caller is the same failure.
	stop := errors.New("stopped")
	dir := t.TempDir()
	dest := filepath.Join(dir, "spend.csv")
	_, err = appWriteWhole(dest, false, func(w io.Writer) error {
		out := appSpendCallsFill(appLedgerPath(), query, zone, &appSpendCallsRun{})
		if err := out(&failingWriter{w: w, n: 1 << 30}); err != nil {
			return err
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("%v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("%d entries left", len(entries))
	}
}

// ONE PAIR OF SCAN SLOTS FOR EVERY LEDGER READER: busy is the asker's retry, the
// destination is untouched, and the slots are all free afterwards.
func TestAppSpendCallsExportIsBusyWhenTheSlotsAreTaken(t *testing.T) {
	ledgerAt(t, time.UTC, callsFixture(time.UTC), "")
	dir := t.TempDir()
	dest := filepath.Join(dir, "spend.csv")
	if err := os.WriteFile(dest, []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	release := fillScanSlots(t)
	_, err := (&appBackend{}).ExportSpendCalls(appwire.SpendCallsExportAsk{Filter: callsFilter(), Destination: dest, Overwrite: true})
	release()
	if wireErrorCode(err) != appwire.CodeBusy {
		t.Fatalf("%v", err)
	}
	keptIntact(t, dir, dest, "kept\n")
	if _, err := (&appBackend{}).ExportSpendCalls(appwire.SpendCallsExportAsk{Filter: callsFilter(), Destination: dest, Overwrite: true}); err != nil {
		t.Fatalf("after release: %v", err)
	}
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("goroutines %d -> %d", before, after)
	}
}

// THE RUN-GRAIN EXPORT IS UNTOUCHED: same eight-plus-fifteen columns, same
// rows, and this file adds a method without changing it.
func TestAppSpendCallsLeavesTheLedgerExportAlone(t *testing.T) {
	ledgerAt(t, time.UTC, callsFixture(time.UTC), "")
	dest := filepath.Join(t.TempDir(), "ledger.csv")
	out, err := (&appBackend{}).ExportSpendLedger(appwire.SpendLedgerExportAsk{Filter: callsFilter(), Destination: dest})
	if err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, dest)
	if len(rows[0]) != 23 || rows[0][0] != "time" || rows[0][7] != "by" || len(rows) != out.Rows+1 || len(appwire.SpendLedgerCSVHeader) != 23 {
		t.Fatalf("%q", rows[0])
	}
	// Two runs (r1, r2) are one row each and five lines carry no run: 7 rows.
	if out.Rows != 7 {
		t.Fatalf("the run-grain export changed: %d rows", out.Rows)
	}
}

func BenchmarkAppSpendCallsExport(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		b.Run(fmt.Sprintf("%dk", n/1000), func(b *testing.B) {
			var sb strings.Builder
			base := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
			for i := range n {
				at := base.Add(time.Duration(i) * 400 * time.Millisecond)
				fmt.Fprintf(&sb, `{"at":%q,"day":"2026-10-14","model":"m/%d","calls":1,"in":3,"out":2,"usd":0.0009765625,"session":"s%04d","root":"a%03d","task":"%d"}`+"\n",
					at.Format(time.RFC3339Nano), i%7, i%1000, i%250, i%9)
			}
			ledger := filepath.Join(b.TempDir(), session.UsageLedgerName)
			if err := os.WriteFile(ledger, []byte(sb.String()), 0o600); err != nil {
				b.Fatal(err)
			}
			sb.Reset()
			oldPath, oldZone := appLedgerPath, appLedgerZone
			appLedgerPath = func() string { return ledger }
			appLedgerZone = func() *time.Location { return time.UTC }
			b.Cleanup(func() { appLedgerPath, appLedgerZone = oldPath, oldZone })
			dest := filepath.Join(b.TempDir(), "spend.csv")
			info, _ := os.Stat(ledger)
			b.ReportAllocs()
			b.SetBytes(info.Size())
			var peak uint64
			for i := 0; i < b.N; i++ {
				// Sample the LIVE heap (after a forced collection) from inside the
				// pass, through the real writer, ten times; the CSV writer writes in
				// 4 KiB pieces, so the samples are spread by write, not by row.
				var m runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&m)
				baseline := m.HeapAlloc
				writes := 0
				query, _ := appLedgerQuery(callsFilter())
				var run appSpendCallsRun
				fill := appSpendCallsFill(appLedgerPath(), query, time.UTC, &run)
				_, err := appWriteWhole(dest, true, func(w io.Writer) error {
					return fill(sampler{w: w, tick: func() {
						writes++
						if writes%(n/200) == 0 {
							runtime.GC()
							runtime.ReadMemStats(&m)
							peak = max(peak, m.HeapAlloc-min(baseline, m.HeapAlloc))
						}
					}})
				})
				if err != nil || run.rows != n {
					b.Fatalf("%v %d", err, run.rows)
				}
			}
			b.ReportMetric(float64(peak)/(1<<20), "live-heap-growth-MiB")
		})
	}
}

// sampler calls tick on every write.
type sampler struct {
	w    io.Writer
	tick func()
}

func (s sampler) Write(p []byte) (int, error) { s.tick(); return s.w.Write(p) }
