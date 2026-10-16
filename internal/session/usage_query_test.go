package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

// writeLedger writes lines to a fresh ledger file, one JSON object each, and
// answers its path. tail, when not empty, is appended without a newline — a
// write caught in flight.
func writeLedger(t *testing.T, lines []UsageLine, raw []string, tail string) string {
	t.Helper()
	var b strings.Builder
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	for _, r := range raw {
		b.WriteString(r)
		b.WriteByte('\n')
	}
	b.WriteString(tail)
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A unit of money every sum below can hold exactly, whatever the order it is
// added in: a power of two.
const cent = 1.0 / 1024

// bigLedger is 5 000 calls in 50 runs that alternate two models, and 6 000
// calls with no run — some auxiliary and unpriced, some unbilled, some late —
// spread over three local days.
func bigLedger(t *testing.T, zone *time.Location) ([]UsageLine, string) {
	t.Helper()
	base := time.Date(2026, 10, 12, 8, 0, 0, 0, zone)
	var lines []UsageLine
	for r := 0; r < 50; r++ {
		for c := 0; c < 100; c++ {
			at := base.Add(time.Duration(r)*time.Hour + time.Duration(c)*time.Second)
			lines = append(lines, UsageLine{
				Run: fmt.Sprintf("run%02d", r), At: at, Day: at.Format(usageDayLayout),
				Model: []string{"m/alpha", "m/beta"}[c%2], Calls: 1, Input: 10, Output: 3, USD: cent,
				Session: fmt.Sprintf("s%02d", r%5), Workspace: "/w",
			})
		}
	}
	for i := 0; i < 6000; i++ {
		at := base.Add(time.Duration(i) * 30 * time.Second)
		line := UsageLine{At: at, Day: at.Format(usageDayLayout), Model: "m/aux", Calls: 1, Input: 1, Output: 1, Session: "s00", Role: "title"}
		switch i % 3 {
		case 0:
			line.USD = 2 * cent
		case 1:
			line.Unbilled = true
		case 2:
			line.Task, line.Root, line.USD = "7", "s01", cent
		}
		lines = append(lines, line)
	}
	return lines, writeLedger(t, lines, nil, "")
}

func allPages(t *testing.T, path string, q UsageQuery, limit int) ([]UsageEntry, []UsageLedgerTotals) {
	t.Helper()
	var rows []UsageEntry
	var totals []UsageLedgerTotals
	cursor := ""
	for {
		page, err := QueryUsageLedger(path, q, cursor, limit, DefaultUsageLedgerBudget)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, page.Entries...)
		totals = append(totals, page.Totals)
		if page.Next == "" {
			return rows, totals
		}
		if len(page.Entries) != limit {
			t.Fatalf("a page with a next held %d rows, not %d", len(page.Entries), limit)
		}
		cursor = page.Next
	}
}

// A RUN IS ONE ROW WHATEVER MODELS IT USED, AND A CALL WITH NO RUN IS ALWAYS ITS
// OWN: eleven thousand calls are 50 run rows and 6 000 call rows, with every
// figure exact and the unpriced calls counted, not called free.
func TestLedgerGroupsRunsAndKeepsUnknownCallsApart(t *testing.T) {
	lines, path := bigLedger(t, time.UTC)
	page, err := QueryUsageLedger(path, UsageQuery{}, "", 500, DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	tot := page.Totals
	if tot.Runs != 50 || tot.UnknownRunRecords != 6000 || tot.Entries != 6050 || tot.Records != len(lines) || tot.Calls != len(lines) {
		t.Fatalf("totals %+v", tot)
	}
	if tot.RunUSD != 5000*cent || tot.UnknownRunUSD != 6000*cent || tot.USD != 11000*cent {
		t.Fatalf("money %+v", tot)
	}
	if tot.Unpriced != 2000 || tot.Unbilled != 2000 {
		t.Fatalf("unpriced %d unbilled %d", tot.Unpriced, tot.Unbilled)
	}
	if tot.Input != 50*100*10+6000 || tot.Output != 50*100*3+6000 {
		t.Fatalf("tokens %d/%d", tot.Input, tot.Output)
	}
	rows, _ := allPages(t, path, UsageQuery{}, 500)
	runs, calls := 0, 0
	for _, row := range rows {
		switch row.Grain {
		case UsageGrainRun:
			runs++
			if row.Records != 100 || row.USD != 100*cent || len(row.Models) != 2 || row.Models[0] != "m/alpha" || row.Models[1] != "m/beta" {
				t.Fatalf("run row %+v", row)
			}
			if row.End.Sub(row.Start) != 99*time.Second || row.Session == "" || row.Kind != SubjectConversation {
				t.Fatalf("run row span or identity %+v", row)
			}
		case UsageGrainCall:
			calls++
			if row.Records != 1 || row.Run != "" {
				t.Fatalf("call row %+v", row)
			}
			if row.Unbilled == 1 && (row.Priced() || row.Unpriced != 1) {
				t.Fatalf("an unbilled call reads as priced: %+v", row)
			}
		}
	}
	if runs != 50 || calls != 6000 {
		t.Fatalf("%d runs and %d calls", runs, calls)
	}
}

// THE TOTALS DO NOT DEPEND ON THE PAGE, and paging visits every row once in
// one order whatever the page size.
func TestLedgerPagesAreIndependentOfTheirSize(t *testing.T) {
	_, path := bigLedger(t, time.UTC)
	var want []string
	for _, limit := range []int{500, 137} {
		rows, totals := allPages(t, path, UsageQuery{}, limit)
		for _, tot := range totals {
			if tot != totals[0] {
				t.Fatalf("limit %d: totals moved between pages: %+v vs %+v", limit, tot, totals[0])
			}
		}
		var keys []string
		seen := map[string]bool{}
		for i, row := range rows {
			if seen[row.Key] {
				t.Fatalf("limit %d: %s twice", limit, row.Key)
			}
			seen[row.Key] = true
			keys = append(keys, row.Key)
			if i > 0 && !usageBefore(rows[i-1], row) {
				t.Fatalf("limit %d: out of order at %d", limit, i)
			}
		}
		if len(keys) != totals[0].Entries {
			t.Fatalf("limit %d: %d rows for %d entries", limit, len(keys), totals[0].Entries)
		}
		if want == nil {
			want = keys
		} else if strings.Join(want, ",") != strings.Join(keys, ",") {
			t.Fatalf("limit %d visits a different order", limit)
		}
	}
	// And a page of one, on a small ledger.
	small := writeLedger(t, []UsageLine{
		{Run: "a", At: time.Unix(100, 0), USD: cent}, {At: time.Unix(100, 0)}, {At: time.Unix(100, 0)}, {Run: "a", At: time.Unix(50, 0)},
	}, nil, "")
	rows, _ := allPages(t, small, UsageQuery{}, 1)
	if len(rows) != 3 {
		t.Fatalf("a page of one visited %d rows", len(rows))
	}
}

// THE FILE EXPORT AND THE PAGE ASK ONE QUESTION: every filter the page applies,
// the walk applies, and the two answer the same rows and the same totals.
func TestLedgerFiltersAreTheSameForPagesAndTheWholeWalk(t *testing.T) {
	_, path := bigLedger(t, time.UTC)
	for _, q := range []UsageQuery{
		{Model: "m/beta"},
		{Model: "m/aux"},
		{Kind: SubjectTask, Subject: "7"},
		{Kind: SubjectConversation, Subject: "s00"},
		{Root: "s01"},
		{Run: "run07"},
		{Session: " s03 "},
		{Workspace: "/w", From: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)},
	} {
		rows, totals := allPages(t, path, q, 500)
		var walked []UsageEntry
		walkTotals, _, err := EachUsageLedgerEntry(path, q, DefaultUsageLedgerBudget, func(e UsageEntry) error {
			walked = append(walked, e)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if walkTotals != totals[0] || len(walked) != len(rows) || len(rows) == 0 {
			t.Fatalf("%+v: walk %+v (%d rows), pages %+v (%d rows)", q, walkTotals, len(walked), totals[0], len(rows))
		}
		byKey := map[string]UsageEntry{}
		for _, e := range rows {
			byKey[e.Key] = e
		}
		for _, e := range walked {
			p, ok := byKey[e.Key]
			if !ok || p.Records != e.Records || p.USD != e.USD || p.Start != e.Start {
				t.Fatalf("%+v: walked row %s differs from its page row", q, e.Key)
			}
		}
	}
	// A model filter on a run keeps that run's other model out of it.
	page, err := QueryUsageLedger(path, UsageQuery{Model: "m/beta", Run: "run03"}, "", 0, DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Records != 50 || len(page.Entries[0].Models) != 1 {
		t.Fatalf("filtered run %+v", page.Entries)
	}
}

// THE WALK HANDS EVERY ROW OVER ONCE, IN LEDGER ORDER, a run where it begins.
func TestLedgerWalkIsInLedgerOrder(t *testing.T) {
	path := writeLedger(t, []UsageLine{
		{Run: "b", At: time.Unix(10, 0)},
		{At: time.Unix(11, 0)},
		{Run: "a", At: time.Unix(12, 0)},
		{Run: "b", At: time.Unix(13, 0)},
		{At: time.Unix(5, 0)}, // a late receipt: its own row, where it was written
	}, nil, "")
	var keys []string
	if _, _, err := EachUsageLedgerEntry(path, UsageQuery{}, DefaultUsageLedgerBudget, func(e UsageEntry) error {
		keys = append(keys, e.Grain+":"+e.Run)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keys, " "); got != "run:b call: run:a call:" {
		t.Fatalf("walk order %q", got)
	}
}

// A LOCAL DAY IS MIDNIGHT TO MIDNIGHT IN THE ASKER'S ZONE, both days included —
// including a day that is twenty-five hours long.
func TestLedgerDayRangeIsLocalCalendarDays(t *testing.T) {
	zone := time.FixedZone("EDT", -4*3600)
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	path := writeLedger(t, []UsageLine{
		{At: at("2026-10-14T23:59:59-04:00"), USD: 1},
		{At: at("2026-10-15T00:00:00-04:00"), USD: 2},
		{At: at("2026-10-16T03:59:59Z"), USD: 4}, // 23:59:59 on the 15th, local
		{At: at("2026-10-16T04:00:00Z"), USD: 8}, // midnight on the 16th, local
	}, nil, "")
	from, to, err := UsageDayRange("2026-10-15", "2026-10-15", zone)
	if err != nil {
		t.Fatal(err)
	}
	page, err := QueryUsageLedger(path, UsageQuery{From: from, To: to}, "", 0, DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	if page.Totals.USD != 6 || page.Totals.Records != 2 {
		t.Fatalf("one local day held %+v", page.Totals)
	}

	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	from, to, err = UsageDayRange("2026-11-01", "2026-11-01", toronto)
	if err != nil {
		t.Fatal(err)
	}
	if to.Sub(from) != 25*time.Hour {
		t.Fatalf("the day the clocks go back is %s long", to.Sub(from))
	}
	for _, bad := range [][2]string{{"2026-10-15", "2026-10-14"}, {"2026-13-01", "2026-13-02"}, {"yesterday", "2026-10-14"}, {"2026-10-15", ""}} {
		if _, _, err := UsageDayRange(bad[0], bad[1], zone); !errors.Is(err, ErrUsageQuery) {
			t.Fatalf("%v: %v", bad, err)
		}
	}
}

// WHAT CANNOT BE ASKED IS REFUSED, BY NAME: a page size out of bounds, a kind
// the ledger has no word for, a subject without its kind, an empty range, and a
// cursor this query did not make.
func TestLedgerRefusesWhatCannotBeAsked(t *testing.T) {
	_, path := bigLedger(t, time.UTC)
	for _, limit := range []int{-1, UsageLedgerPageMax + 1} {
		if _, err := QueryUsageLedger(path, UsageQuery{}, "", limit, DefaultUsageLedgerBudget); !errors.Is(err, ErrUsageQuery) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
	for _, q := range []UsageQuery{
		{Kind: "chat"},
		{Subject: "s00"},
		{From: time.Unix(10, 0), To: time.Unix(10, 0)},
	} {
		if _, err := QueryUsageLedger(path, q, "", 0, DefaultUsageLedgerBudget); !errors.Is(err, ErrUsageQuery) {
			t.Fatalf("%+v: %v", q, err)
		}
		if _, _, err := EachUsageLedgerEntry(path, q, DefaultUsageLedgerBudget, func(UsageEntry) error { return nil }); !errors.Is(err, ErrUsageQuery) {
			t.Fatalf("walk %+v: %v", q, err)
		}
	}
	page, err := QueryUsageLedger(path, UsageQuery{Model: "m/aux"}, "", 10, DefaultUsageLedgerBudget)
	if err != nil || page.Next == "" {
		t.Fatalf("%v %q", err, page.Next)
	}
	for _, cursor := range []string{"not a cursor", "eyJ2IjoxfQ", page.Next + "x", strings.Repeat("a", 2000)} {
		if _, err := QueryUsageLedger(path, UsageQuery{Model: "m/aux"}, cursor, 10, DefaultUsageLedgerBudget); !errors.Is(err, ErrUsageCursor) {
			t.Fatalf("cursor %.20q: %v", cursor, err)
		}
	}
	if _, err := QueryUsageLedger(path, UsageQuery{Model: "m/beta"}, page.Next, 10, DefaultUsageLedgerBudget); !errors.Is(err, ErrUsageCursor) {
		t.Fatalf("another query's cursor: %v", err)
	}
}

// A RANGE TOO LARGE TO HOLD IS REFUSED, NEVER TRUNCATED, and a third reader
// beside two is told to wait rather than queued.
func TestLedgerRefusesOverBudgetAndWhenBusy(t *testing.T) {
	_, path := bigLedger(t, time.UTC)
	small := UsageLedgerBudget{MaxRuns: 10, MaxBytes: 1 << 20}
	if _, err := QueryUsageLedger(path, UsageQuery{}, "", 0, small); !errors.Is(err, ErrUsageLedgerTooLarge) {
		t.Fatalf("over runs: %v", err)
	}
	if _, _, err := EachUsageLedgerEntry(path, UsageQuery{}, UsageLedgerBudget{MaxRuns: 1000, MaxBytes: 2048}, func(UsageEntry) error { return nil }); !errors.Is(err, ErrUsageLedgerTooLarge) {
		t.Fatalf("over bytes: %v", err)
	}
	if _, err := QueryUsageLedger(path, UsageQuery{Run: "run01"}, "", 0, small); err != nil {
		t.Fatalf("a narrower question fits: %v", err)
	}
	for range cap(usageScanSlots) {
		usageScanSlots <- struct{}{}
	}
	_, err := QueryUsageLedger(path, UsageQuery{}, "", 0, DefaultUsageLedgerBudget)
	for range cap(usageScanSlots) {
		<-usageScanSlots
	}
	if !errors.Is(err, ErrUsageScanBusy) {
		t.Fatalf("busy: %v", err)
	}
	if _, err := QueryUsageLedger(path, UsageQuery{}, "", 0, DefaultUsageLedgerBudget); err != nil {
		t.Fatalf("slots were not given back: %v", err)
	}
}

// THE READER SKIPS WHAT [ReadUsage] SKIPS AND STOPS WHERE IT STOPS: a line that
// does not parse, a line with no instant and a line too long to hold each cost
// their own record, and a last line still being written is not read at all.
func TestLedgerReaderMatchesReadUsageOnABadFile(t *testing.T) {
	good := []UsageLine{{At: time.Unix(1, 0), USD: cent}, {At: time.Unix(2, 0), Run: "r"}, {At: time.Unix(3, 0)}}
	huge := `{"at":"2026-10-15T00:00:00Z","model":"` + strings.Repeat("x", usageLineBytes) + `"}`
	exact := `{"at":"2026-10-15T00:00:00Z","model":"` // padded to exactly the limit, newline included
	exact += strings.Repeat("y", usageLineBytes-len(exact)-3) + `"}`
	path := writeLedger(t, good, []string{"not json", `{"model":"no instant"}`, huge, exact, "{\"at\":\"2026-10-15T00:00:01Z\"}\r"}, `{"at":"2026-10-15T00:00:02Z","usd":5}`)
	read, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := QueryUsageLedger(path, UsageQuery{}, "", 0, DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	if page.Totals.Records != len(read) || page.Totals.Records != 5 {
		t.Fatalf("read %d lines, ReadUsage %d", page.Totals.Records, len(read))
	}
	if page.Stats.Malformed != 2 || page.Stats.Oversized != 1 {
		t.Fatalf("stats %+v", page.Stats)
	}
	info, _ := os.Stat(path)
	tail := int64(len(`{"at":"2026-10-15T00:00:02Z","usd":5}`))
	if page.Stats.Bytes != info.Size()-tail {
		t.Fatalf("consumed %d of %d with a %d-byte tail", page.Stats.Bytes, info.Size(), tail)
	}
	if page.Totals.USD != cent {
		t.Fatalf("the partial tail was counted: %v", page.Totals.USD)
	}
	// An oversized line with no end is a tail too, not a skipped record.
	path = writeLedger(t, good, nil, huge)
	page, err = QueryUsageLedger(path, UsageQuery{}, "", 0, DefaultUsageLedgerBudget)
	if err != nil || page.Stats.Oversized != 0 || page.Totals.Records != 3 {
		t.Fatalf("%v %+v %+v", err, page.Stats, page.Totals)
	}
	// And no ledger at all is a machine that has spent nothing.
	page, err = QueryUsageLedger(filepath.Join(t.TempDir(), "absent.jsonl"), UsageQuery{}, "", 0, DefaultUsageLedgerBudget)
	if err != nil || page.Totals.Entries != 0 || page.Next != "" {
		t.Fatalf("%v %+v", err, page)
	}
}

// THE LEDGER'S SUBJECTS ARE THE SPEND PAGE'S: the same line is spent on the
// same thing whichever door asks.
func TestLedgerSubjectsAgreeWithUsageBySubject(t *testing.T) {
	lines := []UsageLine{
		{At: time.Unix(1, 0), Session: "a", USD: 1},
		{At: time.Unix(2, 0), Session: "node", Task: "3", Root: "a", USD: 2},
		{At: time.Unix(3, 0), Session: "hand", Root: "a", USD: 4},
		{At: time.Unix(4, 0), Session: "x", Automation: "nightly", Task: "9", USD: 8},
		{At: time.Unix(5, 0), Session: "y", Standing: "old", USD: 16},
	}
	want := map[string]float64{}
	for _, row := range UsageBySubject(lines) {
		want[row.Kind+"/"+row.ID] += row.USD
	}
	got := map[string]float64{}
	for _, line := range lines {
		kind, id := usageSubject(line)
		got[kind+"/"+id] += line.USD
	}
	if fmt.Sprint(want) != fmt.Sprint(got) {
		t.Fatalf("UsageBySubject %v, ledger %v", want, got)
	}
	// WHERE THEY PART IS A LINE THAT NAMES NOTHING: the spend page draws it as a
	// chat with no id, and the ledger says what is true — nobody knows.
	if kind, id := usageSubject(UsageLine{At: time.Unix(6, 0), USD: 32}); kind != "" || id != "" {
		t.Fatalf("a line naming nothing was spent on %s %q", kind, id)
	}
}

// manyRuns writes n runs of one call each, a second apart, and answers the path.
func manyRuns(t testing.TB, n int) string {
	t.Helper()
	var b strings.Builder
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		data, err := json.Marshal(UsageLine{Run: fmt.Sprintf("run%06d", i), At: base.Add(time.Duration(i) * time.Second), Model: "m/a", Calls: 1, Input: 1, Output: 1, USD: cent})
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A PAGE HOLDS ITS PAGE AND NOTHING MORE: a hundred thousand runs answered a
// row at a time hand back a slice of exactly one, with the totals and the next
// page exact.
func TestLedgerPageOfOneFromAHundredThousandRuns(t *testing.T) {
	path := manyRuns(t, 100_000)
	page, err := QueryUsageLedger(path, UsageQuery{}, "", 1, DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || cap(page.Entries) != 1 || page.Entries[0].Run != "run099999" || page.Next == "" {
		t.Fatalf("page of %d (cap %d): %+v next %q", len(page.Entries), cap(page.Entries), page.Entries, page.Next)
	}
	if page.Totals.Runs != 100_000 || page.Totals.Entries != 100_000 || page.Totals.USD != 100_000*cent {
		t.Fatalf("totals %+v", page.Totals)
	}
	next, err := QueryUsageLedger(path, UsageQuery{}, page.Next, 3, DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != 3 || cap(next.Entries) != 3 || next.Entries[0].Run != "run099998" || next.Entries[2].Run != "run099996" || next.Totals != page.Totals {
		t.Fatalf("second page %+v", next.Entries)
	}
}

// BenchmarkLedgerQueryHundredThousandRuns is one page of the first scan over a
// hundred thousand runs: linear in the file, a page held.
func BenchmarkLedgerQueryHundredThousandRuns(b *testing.B) {
	path := manyRuns(b, 100_000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		page, err := QueryUsageLedger(path, UsageQuery{}, "", 1, DefaultUsageLedgerBudget)
		if err != nil || cap(page.Entries) != 1 {
			b.Fatal(err, cap(page.Entries))
		}
	}
}

// A RUN THAT MET TEN THOUSAND MODELS IS STILL ONE ROW, its models every one
// and sorted, counted against the budget, and met without sorting each in.
func TestLedgerRunWithTenThousandModels(t *testing.T) {
	var lines []UsageLine
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 10_000; i > 0; i-- {
		lines = append(lines, UsageLine{Run: "r", At: at, Model: fmt.Sprintf("m/%05d", i), USD: cent})
		lines = append(lines, UsageLine{Run: "r", At: at, Model: fmt.Sprintf("m/%05d", i)})
	}
	path := writeLedger(t, lines, nil, "")
	page, err := QueryUsageLedger(path, UsageQuery{}, "", 1, DefaultUsageLedgerBudget)
	if err != nil {
		t.Fatal(err)
	}
	row := page.Entries[0]
	if len(row.Models) != 10_000 || !slices.IsSorted(row.Models) || row.Models[0] != "m/00001" || row.Records != 20_000 || row.modelSet != nil {
		t.Fatalf("%d models, sorted %v, first %q", len(row.Models), slices.IsSorted(row.Models), row.Models[0])
	}
	var walked UsageEntry
	if _, _, err := EachUsageLedgerEntry(path, UsageQuery{}, DefaultUsageLedgerBudget, func(e UsageEntry) error { walked = e; return nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(walked.Models, row.Models) {
		t.Fatal("the walk's models differ from the page's")
	}
	if _, err := QueryUsageLedger(path, UsageQuery{}, "", 1, UsageLedgerBudget{MaxRuns: 10, MaxBytes: 256 << 10}); !errors.Is(err, ErrUsageLedgerTooLarge) {
		t.Fatalf("ten thousand models were not counted against the budget: %v", err)
	}
}

// A FIGURE THAT CANNOT BE ADDED UP IS REFUSED: costs whose sum leaves the
// finite floats, counts whose sum would wrap, and a negative token count are
// each an error, never an infinity, a wrapped number or a zero.
func TestLedgerRefusesFiguresItCannotAdd(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, lines := range map[string][]UsageLine{
		"cost":     {{At: at, USD: math.MaxFloat64}, {Run: "r", At: at, USD: math.MaxFloat64}},
		"calls":    {{At: at, Calls: math.MaxInt}, {At: at, Calls: 2}},
		"tokens":   {{At: at, Input: math.MaxInt}, {Run: "r", At: at, Input: math.MaxInt}, {At: at, Input: 2}},
		"negative": {{At: at, Output: -5, USD: cent}},
	} {
		path := writeLedger(t, lines, nil, "")
		if _, err := QueryUsageLedger(path, UsageQuery{}, "", 0, DefaultUsageLedgerBudget); !errors.Is(err, ErrUsageLedgerFigures) {
			t.Fatalf("%s: page %v", name, err)
		}
		if _, _, err := EachUsageLedgerEntry(path, UsageQuery{}, DefaultUsageLedgerBudget, func(UsageEntry) error { return nil }); !errors.Is(err, ErrUsageLedgerFigures) {
			t.Fatalf("%s: walk %v", name, err)
		}
	}
	// One of the two huge figures alone is a figure, and is answered.
	path := writeLedger(t, []UsageLine{{At: at, USD: math.MaxFloat64}}, nil, "")
	if page, err := QueryUsageLedger(path, UsageQuery{}, "", 0, DefaultUsageLedgerBudget); err != nil || page.Totals.USD != math.MaxFloat64 {
		t.Fatalf("%v %+v", err, page.Totals)
	}
}

// EVERY PAGE OF ONE PAGING READS THE BYTES THE FIRST ONE READ: what is
// appended meanwhile is on no page and in no total until the next paging, and
// a ledger replaced, cut shorter or rewritten under a cursor refuses it.
func TestLedgerCursorReadsTheFirstPagesBytes(t *testing.T) {
	lines, path := bigLedger(t, time.UTC)
	first, err := QueryUsageLedger(path, UsageQuery{}, "", 100, DefaultUsageLedgerBudget)
	if err != nil || first.Next == "" {
		t.Fatalf("%v %q", err, first.Next)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	appended := original
	for i := 0; i < 50; i++ {
		data, _ := json.Marshal(UsageLine{Run: fmt.Sprintf("late%02d", i), At: lines[0].At.Add(-time.Hour), USD: cent})
		appended = append(append(appended, data...), '\n')
	}
	appended = append(appended, `{"at":"2026-10-1`...) // and a write in flight
	if err := os.WriteFile(path, appended, 0o600); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range first.Entries {
		seen[e.Key] = true
	}
	cursor := first.Next
	for cursor != "" {
		page, err := QueryUsageLedger(path, UsageQuery{}, cursor, 500, DefaultUsageLedgerBudget)
		if err != nil {
			t.Fatal(err)
		}
		if page.Totals != first.Totals {
			t.Fatalf("an append moved the totals of a paging: %+v vs %+v", page.Totals, first.Totals)
		}
		for _, e := range page.Entries {
			if seen[e.Key] || strings.HasPrefix(e.Run, "late") {
				t.Fatalf("%s twice, or from after the first page", e.Key)
			}
			seen[e.Key] = true
		}
		cursor = page.Next
	}
	if len(seen) != first.Totals.Entries {
		t.Fatalf("visited %d rows of %d", len(seen), first.Totals.Entries)
	}
	if fresh, err := QueryUsageLedger(path, UsageQuery{}, "", 1, DefaultUsageLedgerBudget); err != nil || fresh.Totals.Runs != first.Totals.Runs+50 {
		t.Fatalf("a new paging does not see the appended runs: %v %+v", err, fresh.Totals)
	}

	for name, data := range map[string][]byte{
		"shorter":   original[:len(original)/2],
		"rewritten": append([]byte(`{"at":"2026-10-12T08:00:00Z","usd":1}`+"\n"), original[38:]...),
		"replaced":  append(bytes.Repeat([]byte(" "), len(original)), '\n'),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := QueryUsageLedger(path, UsageQuery{}, first.Next, 10, DefaultUsageLedgerBudget); !errors.Is(err, ErrUsageLedgerChanged) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryUsageLedger(path, UsageQuery{}, first.Next, 10, DefaultUsageLedgerBudget); !errors.Is(err, ErrUsageLedgerChanged) {
		t.Fatalf("removed: %v", err)
	}
}
