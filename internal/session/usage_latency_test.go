package session

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// This measures the caller's handoff, including journal sync when supported.
func TestRecordUsageLatencyMeasure(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	const n = 1000
	samples := make([]time.Duration, n)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for i := range n {
		row := UsageLine{ID: fmt.Sprintf("measure-%d", i), At: at, Model: "fixture/model", Session: "fixture-chat", Task: "17", Root: "fixture-root", Workspace: "fixture-workspace", FactoryJob: "fixture-job", Input: 1000, Output: 200, USD: .0090004, Calls: 1}
		start := time.Now()
		RecordUsage(path, row)
		samples[i] = time.Since(start)
	}
	if !StopUsageWriter(path) {
		t.Fatal("writer did not drain")
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != n {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.ID] || r.USD != .0090004 {
			t.Fatal("incorrect output")
		}
		seen[r.ID] = true
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("rows=%d p50=%s p95=%s", n, samples[n/2], samples[n*95/100])
}
