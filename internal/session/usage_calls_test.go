package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// callFixture is one local day of calls across two agents: a chat's own lines
// (Session only), the same agent's task lines (a node session with Root set),
// another agent's, a run-less late receipt, an older line covering many
// requests, and an unpriced and an unbilled one.
func callFixture(zone *time.Location) []UsageLine {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 14, h, m, 0, 0, zone) }
	day := func(t time.Time) string { return t.Format(usageDayLayout) }
	line := func(l UsageLine) UsageLine { l.Day = day(l.At); return l }
	return []UsageLine{
		line(UsageLine{At: at(8, 0), Session: "chatA", Model: "m/one", Calls: 1, Input: 10, Output: 2, USD: 0.0123456789012345, Run: "r1"}),
		line(UsageLine{At: at(8, 5), Session: "node1", Root: "chatA", Task: "3", Model: "m/two", Calls: 1, Input: 7, Output: 1, USD: cent, Run: "r2"}),
		line(UsageLine{At: at(8, 6), Session: "chatB", Model: "m/one", Calls: 1, Input: 5, Output: 5, USD: 2 * cent}),
		line(UsageLine{At: at(9, 0), Session: "chatA", Model: "m/one", Calls: 41, Input: 400, Output: 40, USD: 4 * cent}),
		line(UsageLine{At: at(9, 1), Session: "chatA", Calls: 1, Input: 1, Output: 1}),
		line(UsageLine{At: at(9, 2), Session: "node1", Root: "chatA", Task: "3", Model: "m/two", Calls: 1, Input: 2, Output: 2, Unbilled: true}),
		line(UsageLine{At: at(23, 59), Session: "chatB", Root: "", Model: "m/two", Calls: 0, Input: 3, Output: 3, USD: cent}),
		// The next local day: outside a one-day range.
		line(UsageLine{At: time.Date(2026, 10, 15, 0, 0, 0, 0, zone), Session: "chatA", Model: "m/one", Calls: 1, Input: 1, Output: 1, USD: cent}),
	}
}

type collected struct {
	calls  []UsageCall
	totals UsageCallTotals
	stats  UsageScanStats
}

func collectCalls(t *testing.T, path string, q UsageQuery) collected {
	t.Helper()
	var c collected
	var err error
	c.totals, c.stats, err = EachUsageCall(path, q, func(call UsageCall) error {
		c.calls = append(c.calls, call)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func dayQuery(t *testing.T, zone *time.Location, from, to string) UsageQuery {
	t.Helper()
	start, end, err := UsageDayRange(from, to, zone)
	if err != nil {
		t.Fatal(err)
	}
	return UsageQuery{From: start, To: end}
}

// THE VISITOR AND THE LEDGER AND THE REPORT ARE ONE QUESTION: for every filter
// the same lines are selected, so records, requests, tokens, cost, unpriced and
// unbilled add up identically — and the agent filter takes the chat's own
// lines AND its tasks' lines, never the other agent's.
func TestSessionCallExportAgreesWithLedgerAndReport(t *testing.T) {
	zone := time.FixedZone("Toronto", -4*3600)
	path := writeLedger(t, callFixture(zone), nil, "")
	base := dayQuery(t, zone, "2026-10-14", "2026-10-14")
	for name, mutate := range map[string]func(UsageQuery) UsageQuery{
		"range":      func(q UsageQuery) UsageQuery { return q },
		"agent":      func(q UsageQuery) UsageQuery { q.Agent = "chatA"; return q },
		"other":      func(q UsageQuery) UsageQuery { q.Agent = "chatB"; return q },
		"model":      func(q UsageQuery) UsageQuery { q.Model = "m/two"; return q },
		"task":       func(q UsageQuery) UsageQuery { q.Kind, q.Subject, q.Root = SubjectTask, "3", "chatA"; return q },
		"run":        func(q UsageQuery) UsageQuery { q.Run = "r1"; return q },
		"agent+mod":  func(q UsageQuery) UsageQuery { q.Agent, q.Model = "chatA", "m/two"; return q },
		"no matches": func(q UsageQuery) UsageQuery { q.Agent = "nobody"; return q },
	} {
		q := mutate(base)
		got := collectCalls(t, path, q)
		page, err := QueryUsageLedger(path, q, "", UsageLedgerPageMax, DefaultUsageLedgerBudget)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		report, err := QueryUsageReport(path, q, zone, UsageReportBudget{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		g := got.totals
		for label, totals := range map[string]UsageLedgerTotals{"ledger": page.Totals, "report": report.Totals} {
			if g.Records != totals.Records || g.Requests != totals.Calls || g.Input != totals.Input || g.Output != totals.Output ||
				g.USD != totals.USD || g.Unpriced != totals.Unpriced || g.Unbilled != totals.Unbilled {
				t.Fatalf("%s vs %s: calls %+v, totals %+v", name, label, g, totals)
			}
		}
		if len(got.calls) != g.Records {
			t.Fatalf("%s: %d calls handed over for %d records", name, len(got.calls), g.Records)
		}
		var usd float64
		var requests int
		for i, call := range got.calls {
			if i > 0 && call.Offset <= got.calls[i-1].Offset {
				t.Fatalf("%s: not in ledger order", name)
			}
			if call.Priced {
				usd += call.USD
			}
			requests += call.Requests
		}
		if usd != g.USD || requests != g.Requests {
			t.Fatalf("%s: rows sum to %v/%d, totals say %v/%d", name, usd, requests, g.USD, g.Requests)
		}
	}

	agent := collectCalls(t, path, func() UsageQuery { q := base; q.Agent = "chatA"; return q }())
	if len(agent.calls) != 5 {
		t.Fatalf("chatA's chat and task lines: %d", len(agent.calls))
	}
	for _, call := range agent.calls {
		if call.Owner != "chatA" {
			t.Fatalf("a line of %q came with chatA", call.Owner)
		}
	}
	if all := collectCalls(t, path, base); len(all.calls) != 7 {
		t.Fatalf("the day holds %d, want 7 (the next day is outside the range)", len(all.calls))
	}
}

// A LINE THAT SAYS MANY REQUESTS STAYS ONE RECORD, and a line that says none is
// the one request every line is at least. An unpriced line is not free.
func TestSessionCallExportKeepsLegacyRequestsAndUnpriced(t *testing.T) {
	zone := time.UTC
	got := collectCalls(t, writeLedger(t, callFixture(zone), nil, ""), dayQuery(t, zone, "2026-10-14", "2026-10-14"))
	requests := map[int]int{}
	for _, c := range got.calls {
		requests[c.Requests]++
		if c.Offset < 0 {
			t.Fatal("no offset")
		}
	}
	if requests[41] != 1 || requests[1] != 6 {
		t.Fatalf("requests per record: %v", requests)
	}
	if got.totals.Requests != 41+6 || got.totals.Records != 7 {
		t.Fatalf("%+v", got.totals)
	}
	var unpriced, unbilled int
	for _, c := range got.calls {
		if !c.Priced {
			unpriced++
			if c.USD != 0 {
				t.Fatalf("an unpriced call carries %v", c.USD)
			}
		}
		if c.Unbilled {
			unbilled++
		}
	}
	if unpriced != 2 || unbilled != 1 || got.totals.Unpriced != 2 || got.totals.Unbilled != 1 || !got.totals.Priced() {
		t.Fatalf("unpriced %d unbilled %d %+v", unpriced, unbilled, got.totals)
	}
	// A range of unpriced lines has no cost at all.
	only := collectCalls(t, writeLedger(t, []UsageLine{{At: time.Now(), Session: "s", Input: 1}}, nil, ""), UsageQuery{})
	if only.totals.Priced() || only.totals.Records != 1 {
		t.Fatalf("%+v", only.totals)
	}
}

// The full precision of a cost reaches the visitor untouched.
func TestSessionCallExportCostIsFullPrecision(t *testing.T) {
	got := collectCalls(t, writeLedger(t, callFixture(time.UTC), nil, ""), dayQuery(t, time.UTC, "2026-10-14", "2026-10-14"))
	if got.calls[0].USD != 0.0123456789012345 {
		t.Fatalf("%v", got.calls[0].USD)
	}
}

// A figure that cannot be added up ends the pass with the ledger's own
// refusal and no totals, and so does an error from the caller.
func TestSessionCallExportRefusesFiguresItCannotAdd(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, lines := range map[string][]UsageLine{
		"cost":     {{At: at, USD: math.MaxFloat64}, {At: at, USD: math.MaxFloat64}},
		"calls":    {{At: at, Calls: math.MaxInt}, {At: at, Calls: 2}},
		"tokens":   {{At: at, Input: math.MaxInt}, {At: at, Input: math.MaxInt}, {At: at, Input: 2}},
		"negative": {{At: at, Output: -5, USD: cent}},
		"negusd":   {{At: at, Input: 1, USD: -cent}},
		"negcalls": {{At: at, Input: 1, Calls: -3}},
	} {
		path := writeLedger(t, lines, nil, "")
		totals, _, err := EachUsageCall(path, UsageQuery{}, func(UsageCall) error { return nil })
		if !errors.Is(err, ErrUsageLedgerFigures) || totals != (UsageCallTotals{}) {
			t.Fatalf("%s: %v %+v", name, err, totals)
		}
	}
	// A huge figure alone is a figure.
	path := writeLedger(t, []UsageLine{{At: at, USD: math.MaxFloat64}}, nil, "")
	if totals, _, err := EachUsageCall(path, UsageQuery{}, func(UsageCall) error { return nil }); err != nil || totals.USD != math.MaxFloat64 {
		t.Fatalf("%v %+v", err, totals)
	}
	// A hand-written line with a number JSON cannot hold as a float is malformed,
	// not wrapped: it is skipped and counted, as everywhere.
	path = writeLedger(t, []UsageLine{{At: at, Input: 1, USD: cent}}, []string{`{"at":"2026-01-01T00:00:00Z","usd":1e999}`, `{"at":"2026-01-01T00:00:00Z","in":1e30}`}, "")
	totals, stats, err := EachUsageCall(path, UsageQuery{}, func(UsageCall) error { return nil })
	if err != nil || stats.Malformed != 2 || totals.Records != 1 {
		t.Fatalf("%v %+v %+v", err, stats, totals)
	}

	boom := errors.New("boom")
	totals, _, err = EachUsageCall(path, UsageQuery{}, func(UsageCall) error { return boom })
	if !errors.Is(err, boom) || totals != (UsageCallTotals{}) {
		t.Fatalf("%v %+v", err, totals)
	}
}

// AN UNASKABLE QUERY, A MISSING LEDGER, A WRITE IN FLIGHT AND A LEDGER THAT
// GROWS MID-PASS: the pass reads whole lines of the length the file had when it
// opened, and nothing else.
func TestSessionCallExportReadsAFixedPrefix(t *testing.T) {
	if _, _, err := EachUsageCall("", UsageQuery{Subject: "x"}, func(UsageCall) error { return nil }); !errors.Is(err, ErrUsageQuery) {
		t.Fatalf("%v", err)
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "absent.jsonl")} {
		totals, stats, err := EachUsageCall(path, UsageQuery{}, func(UsageCall) error { t.Fatal("a call from nothing"); return nil })
		if err != nil || totals != (UsageCallTotals{}) || stats != (UsageScanStats{}) {
			t.Fatalf("%q: %v %+v", path, err, totals)
		}
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	line := UsageLine{At: at, Session: "s", Input: 1, USD: cent}
	partial := `{"at":"2026-01-01T00:00:00Z","in":9`
	path := writeLedger(t, []UsageLine{line, line, line}, []string{"not json"}, partial)
	appended := false
	var seen int
	totals, stats, err := EachUsageCall(path, UsageQuery{}, func(UsageCall) error {
		seen++
		if !appended {
			appended = true
			data, _ := json.Marshal(line)
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.Write(append(data, '\n')); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	})
	if err != nil || seen != 3 || totals.Records != 3 || stats.Malformed != 1 {
		t.Fatalf("%v seen %d %+v %+v", err, seen, totals, stats)
	}
	// The appended line landed after the half line, so the two are one malformed
	// line: the next pass counts it and neither is a call.
	if again := collectCalls(t, path, UsageQuery{}); again.totals.Records != 3 || again.stats.Malformed != 2 {
		t.Fatalf("%+v %+v", again.totals, again.stats)
	}
}

// ONE SCAN SLOT PAIR FOR EVERY LEDGER READER: with both taken the visitor is
// refused at once, takes no slot of its own, and leaks nothing.
func TestSessionCallExportSharesTheScanSlots(t *testing.T) {
	path := writeLedger(t, callFixture(time.UTC), nil, "")
	before := runtime.NumGoroutine()
	usageScanSlots <- struct{}{}
	usageScanSlots <- struct{}{}
	_, _, err := EachUsageCall(path, UsageQuery{}, func(UsageCall) error { t.Fatal("ran while busy"); return nil })
	if !errors.Is(err, ErrUsageScanBusy) {
		t.Fatalf("%v", err)
	}
	<-usageScanSlots
	<-usageScanSlots
	if len(usageScanSlots) != 0 {
		t.Fatalf("%d slots held", len(usageScanSlots))
	}
	// Both slots are free again, and a failed pass gives its slot back.
	for range 5 {
		_, _, _ = EachUsageCall(path, UsageQuery{}, func(UsageCall) error { return errors.New("stop") })
	}
	if len(usageScanSlots) != 0 {
		t.Fatalf("%d slots leaked", len(usageScanSlots))
	}
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("goroutines %d -> %d", before, after)
	}
}

// callsLedger writes n priced one-request lines over many agents and tasks.
func callsLedger(tb testing.TB, n int) string {
	tb.Helper()
	var b strings.Builder
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		data, err := json.Marshal(UsageLine{At: at, Day: at.Format(usageDayLayout), Model: fmt.Sprintf("m/%d", i%7), Calls: 1, Input: 3, Output: 2, USD: cent,
			Session: fmt.Sprintf("s%04d", i%1000), Root: fmt.Sprintf("a%03d", i%250), Task: fmt.Sprint(i % 9)})
		if err != nil {
			tb.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	path := filepath.Join(tb.TempDir(), UsageLedgerName)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

// BenchmarkSessionCallExportVisitor shows the visitor's time is linear in the
// ledger and that it keeps nothing: the LIVE heap (after a forced collection,
// sampled ten times through the pass) beyond what it was before the pass is
// reported beside the time, and does not grow with the ledger. It claims
// nothing about the process's resident memory.
func BenchmarkSessionCallExportVisitor(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		b.Run(fmt.Sprintf("%dk", n/1000), func(b *testing.B) {
			path := callsLedger(b, n)
			info, _ := os.Stat(path)
			b.ReportAllocs()
			b.SetBytes(info.Size())
			var peak uint64
			for i := 0; i < b.N; i++ {
				var m runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&m)
				baseline := m.HeapAlloc
				visited := 0
				totals, _, err := EachUsageCall(path, UsageQuery{}, func(UsageCall) error {
					visited++
					if visited%(n/10) == 0 {
						runtime.GC()
						runtime.ReadMemStats(&m)
						peak = max(peak, m.HeapAlloc-min(baseline, m.HeapAlloc))
					}
					return nil
				})
				if err != nil || totals.Records != n || visited != n {
					b.Fatalf("%v %d %d", err, totals.Records, visited)
				}
			}
			b.ReportMetric(float64(peak)/(1<<20), "live-heap-growth-MiB")
		})
	}
}
