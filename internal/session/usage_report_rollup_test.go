package session

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func equalRollupScan(t *testing.T, path string, q UsageQuery, loc *time.Location, budget UsageReportBudget) {
	t.Helper()
	want, we := scanUsageReport(path, q, loc, budget)
	got, ge := QueryUsageReport(path, q, loc, budget)
	if fmt.Sprint(we) != fmt.Sprint(ge) || !reflect.DeepEqual(want, got) {
		t.Fatalf("rollup differs from scan: scan=%+v (%v), rollup=%+v (%v)", want, we, got, ge)
	}
}

func appendReportBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.Write(data)
	ce := f.Close()
	if e != nil {
		t.Fatal(e)
	}
	if ce != nil {
		t.Fatal(ce)
	}
}

func TestReportRollupEqualityAcrossLedgerChanges(t *testing.T) {
	var lines []UsageLine
	for i := range 2400 {
		l := reportLine(1+i%28, i%24, fmt.Sprintf("m%d", i%7), float64(i%5)/1024)
		l.Session = fmt.Sprintf("s%d", i%11)
		switch i % 4 {
		case 0:
			l.Task = fmt.Sprint(i % 13)
			l.Root = fmt.Sprintf("r%d", i%3)
		case 1:
			l.Automation = fmt.Sprint(i % 17)
		case 2:
			l.Standing = fmt.Sprint(i % 17)
		}
		if i%9 == 0 {
			l.Run = " run "
		}
		if i%11 == 0 {
			l.Day = ""
		}
		if i%19 == 0 {
			l.Unbilled = true
			l.USD = 0
		}
		lines = append(lines, l)
	}
	path := writeLedger(t, lines, []string{"not json", strings.Repeat("x", usageLineBytes+1)}, "")
	check := func() {
		for _, span := range [][2]string{{"2026-10-01", "2026-10-28"}, {"2026-10-03", "2026-10-07"}, {"2026-10-28", "2026-10-28"}, {"2026-11-01", "2026-11-02"}} {
			q := reportQuery(t, span[0], span[1])
			equalRollupScan(t, path, q, reportZone, UsageReportBudget{})
			equalRollupScan(t, path, q, reportZone, UsageReportBudget{MaxCells: 2})
			equalRollupScan(t, path, q, reportZone, UsageReportBudget{MaxBytes: 300})
		}
	}
	check()
	// A receipt appended last retains its original instant and stored day.
	late := reportLine(3, 10, "late", .125)
	raw, e := json.Marshal(late)
	if e != nil {
		t.Fatal(e)
	}
	appendReportBytes(t, path, append(raw, '\n'))
	check()
	q := reportQuery(t, "2026-10-03", "2026-10-03")
	r, e := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
	if e != nil {
		t.Fatal(e)
	}
	if cellOf(t, r, "2026-10-03", "late", "", "", "").Records != 1 {
		t.Fatal("late receipt lost")
	}
	appendReportBytes(t, path, raw[:len(raw)/2])
	check()
	appendReportBytes(t, path, append(raw[len(raw)/2:], '\n'))
	check()
	original, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(path, path+".old"); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, append(raw, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	check()
	// Same-size replacement, followed by a smaller replacement.
	replacement := append([]byte(nil), original...)
	middle := len(replacement) / 2
	at := middle + strings.Index(string(replacement[middle:]), `"model":"m`) + len(`"model":"`)
	replacement[at] = 'z' // Change a middle row outside the prefix-end samples.
	for _, data := range [][]byte{original, replacement, append(raw, '\n')} {
		if e = os.WriteFile(path+".new", data, 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.Rename(path+".new", path); e != nil {
			t.Fatal(e)
		}
		check()
	}
	if e = os.Truncate(path, 0); e != nil {
		t.Fatal(e)
	}
	check()
	appendReportBytes(t, path, original)
	check()
	// In-place same-size tail rewrite invalidates the retained prefix.
	appendReportBytes(t, path, append(raw, '\n'))
	check()
	info, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	offset := int64(strings.Index(string(raw), "late"))
	f, e := os.OpenFile(path, os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.WriteAt([]byte("Late"), info.Size()-int64(len(raw)+1)+offset)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	check()
}

func TestReportRollupFallbackEquality(t *testing.T) {
	q := reportQuery(t, "2026-10-14", "2026-10-15")
	for _, change := range []func(*UsageLine){
		func(l *UsageLine) { l.Day = "2026-10-15" },
		func(l *UsageLine) { l.USD = .01 },
		func(l *UsageLine) { l.Input = -1 },
	} {
		l := reportLine(14, 10, "m", .125)
		change(&l)
		path := writeLedger(t, []UsageLine{l, l, l}, nil, "")
		equalRollupScan(t, path, q, reportZone, UsageReportBudget{})
	}
	path := reportRows(t, 1000)
	equalRollupScan(t, path, q, reportZone, UsageReportBudget{})
	equalRollupScan(t, path, q, time.UTC, UsageReportBudget{})
	partial := q
	partial.From = partial.From.Add(time.Hour)
	equalRollupScan(t, path, partial, reportZone, UsageReportBudget{})
	for _, set := range []func(*UsageQuery){
		func(q *UsageQuery) { q.Model = "m/01" }, func(q *UsageQuery) { q.Kind = SubjectTask; q.Subject = "1" },
		func(q *UsageQuery) { q.Run = "run" }, func(q *UsageQuery) { q.Workspace = "w" }, func(q *UsageQuery) { q.Agent = "conv01" },
	} {
		filtered := q
		set(&filtered)
		equalRollupScan(t, path, filtered, reportZone, UsageReportBudget{})
	}
}

func TestReportRollupConcurrentReports(t *testing.T) {
	path := reportRows(t, 1000)
	q := reportQuery(t, "2026-10-01", "2026-10-31")
	want, e := scanUsageReport(path, q, reportZone, UsageReportBudget{})
	if e != nil {
		t.Fatal(e)
	}
	before := usageBytesScanned.Load()
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
			if e != nil || !reflect.DeepEqual(want, got) {
				t.Errorf("concurrent report differs: %v", e)
			}
		}()
	}
	wg.Wait()
	if read := usageBytesScanned.Load() - before; read != want.Stats.Bytes {
		t.Fatalf("concurrent reports scanned %d bytes, want one prefix of %d", read, want.Stats.Bytes)
	}
}

func BenchmarkUsageReportScanAndSecond100k(b *testing.B) {
	path := reportRows(b, 100000)
	q := reportQuery(b, "2026-10-01", "2026-10-31")
	want, e := QueryUsageReport(path, q, reportZone, UsageReportBudget{})
	if e != nil {
		b.Fatal(e)
	}
	for _, mode := range []string{"scan", "second"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			var got UsageReport
			var e error
			for range b.N {
				if mode == "scan" {
					got, e = scanUsageReport(path, q, reportZone, UsageReportBudget{})
				} else {
					got, e = QueryUsageReport(path, q, reportZone, UsageReportBudget{})
				}
				if e != nil {
					b.Fatal(e)
				}
			}
			b.StopTimer()
			if !reflect.DeepEqual(want, got) {
				b.Fatal("incorrect report")
			}
		})
	}
}

func TestReportRollupPreservesRoundingAtPrecisionLimit(t *testing.T) {
	lines := []UsageLine{reportLine(14, 10, "a", 1<<53), reportLine(15, 10, "a", 1), reportLine(15, 11, "a", 1)}
	path := writeLedger(t, lines, nil, "")
	equalRollupScan(t, path, reportQuery(t, "2026-10-14", "2026-10-15"), reportZone, UsageReportBudget{})
}

func TestReportRollupCacheBudgetFallsBackWithoutRefusingNarrowRange(t *testing.T) {
	var lines []UsageLine
	for i := range 9000 {
		lines = append(lines, reportLine(14+i/4500, 10, fmt.Sprint(i), .125))
	}
	path := writeLedger(t, lines, nil, "")
	for _, last := range []string{"2026-10-14", "2026-10-15"} {
		equalRollupScan(t, path, reportQuery(t, "2026-10-14", last), reportZone, UsageReportBudget{})
	}
}
