package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTrafficLog writes n entries straight to a team's two files, the first
// rotatedN of them into the rotated file, each with text of textLen bytes, so a
// test or a benchmark can build a big log without taking the lock n times.
func writeTrafficLog(tb testing.TB, dir, team string, n, rotatedN, textLen int) {
	tb.Helper()
	path := TrafficPath(dir, team)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		tb.Fatal(err)
	}
	var rotated, current strings.Builder
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		text := strings.Repeat(string(rune('a'+i%26)), textLen+i%7)
		raw, err := json.Marshal(Entry{ID: fmt.Sprintf("%012d", i), At: at.Add(time.Duration(i) * time.Second),
			Kind: KindNote, From: "web", To: ToRoom, Text: text})
		if err != nil {
			tb.Fatal(err)
		}
		if i <= rotatedN {
			rotated.Write(raw)
			rotated.WriteByte('\n')
		} else {
			current.Write(raw)
			current.WriteByte('\n')
		}
	}
	if rotatedN > 0 {
		if err := os.WriteFile(trafficRotated(path), []byte(rotated.String()), 0o600); err != nil {
			tb.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(current.String()), 0o600); err != nil {
		tb.Fatal(err)
	}
}

// referenceTraffic is what ReadTraffic answers, computed the slow way: every
// entry of both files, filtered and cut.
func referenceTraffic(t *testing.T, dir, team, after string, limit int) []string {
	t.Helper()
	all, err := ReadTraffic(dir, team, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range all {
		if after == "" || e.ID > after {
			ids = append(ids, e.ID)
		}
	}
	if limit > 0 && len(ids) > limit {
		if after == "" {
			ids = ids[len(ids)-limit:]
		} else {
			ids = ids[:limit]
		}
	}
	return ids
}

// THE BOUNDED READS ANSWER WHAT THE WHOLE READ ANSWERS, across window edges,
// across the rotation, past a line too long for one window, around a line
// that does not parse, and with a last line still being written.
func TestTrafficBoundedReadsAgreeWithTheWholeRead(t *testing.T) {
	defer func(n int64) { trafficWindow = n }(trafficWindow)
	trafficWindow = 700
	dir := t.TempDir()
	writeTrafficLog(t, dir, "t1", 120, 50, 60)
	path := TrafficPath(dir, "t1")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	long, _ := json.Marshal(Entry{ID: "000000000121", Kind: KindNote, From: "web", To: ToRoom, Text: strings.Repeat("x", 3000)})
	_, _ = file.WriteString("not json\n" + string(long) + "\n")
	later, _ := json.Marshal(Entry{ID: "000000000122", Kind: KindNote, From: "web", To: ToRoom, Text: "after the long one"})
	_, _ = file.WriteString(string(later) + "\n" + `{"id":"000000000123","kind":"note"`)
	_ = file.Close()

	afters := []string{"", "000000000000", "000000000001", "000000000049", "000000000050", "000000000051", "000000000077",
		"000000000119", "000000000120", "000000000121", "000000000122", "000000000200"}
	for _, after := range afters {
		for _, limit := range []int{1, 3, 20, 71, 100, 500} {
			got, err := ReadTraffic(dir, "t1", after, limit)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, e := range got {
				ids = append(ids, e.ID)
			}
			want := referenceTraffic(t, dir, "t1", after, limit)
			if strings.Join(ids, ",") != strings.Join(want, ",") {
				t.Errorf("after %q limit %d read %v, want %v", after, limit, ids, want)
			}
		}
	}
}

// benchLog is a log the size the audit measured: 4.4 MB over the two files.
func benchLog(b *testing.B) (string, string) {
	dir := b.TempDir()
	writeTrafficLog(b, dir, "t1", 22000, 20000, 140)
	return dir, fmt.Sprintf("%012d", 22000)
}

// BenchmarkTrafficNothingNew is a reader at the end of the log asking again.
func BenchmarkTrafficNothingNew(b *testing.B) {
	dir, last := benchLog(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := ReadTraffic(dir, "t1", last, 100); err != nil || len(got) != 0 {
			b.Fatal(len(got), err)
		}
	}
}

// BenchmarkTrafficTail is a digest's look at the newest entries.
func BenchmarkTrafficTail(b *testing.B) {
	dir, _ := benchLog(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := ReadTraffic(dir, "t1", "", 200); err != nil || len(got) != 200 {
			b.Fatal(len(got), err)
		}
	}
}

// BenchmarkTrafficPageFromTheMiddle is a reader catching up a page at a time.
func BenchmarkTrafficPageFromTheMiddle(b *testing.B) {
	dir, _ := benchLog(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := ReadTraffic(dir, "t1", fmt.Sprintf("%012d", 11000), 100); err != nil || len(got) != 100 {
			b.Fatal(len(got), err)
		}
	}
}
