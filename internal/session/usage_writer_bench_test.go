package session_test

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// BenchmarkLedgerWriter20K measures enqueue through completed writer callbacks.
// Ledger reads and exact charge verification stay outside the timed region.
func BenchmarkLedgerWriter20K(b *testing.B) {
	const count = 20000
	for range b.N {
		b.StopTimer()
		path := filepath.Join(b.TempDir(), "usage.jsonl")
		var written atomic.Int64
		done := make(chan struct{})
		stop := session.OnUsageWritten(func() {
			if written.Add(1) == count {
				close(done)
			}
		})
		before := session.UsageDrops()
		b.StartTimer()
		for i := range count {
			session.RecordUsage(path, session.UsageLine{ID: fmt.Sprintf("charge-%d", i), Model: "fixture", Calls: 1, USD: .01})
		}
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			b.Fatal("writer did not complete")
		}
		b.StopTimer()
		stop()
		if !session.FlushUsage() {
			b.Fatal("flush")
		}
		session.StopUsageWriter(path)
		rows, err := session.ReadUsage(path, time.Time{})
		if err != nil {
			b.Fatal(err)
		}
		var cost float64
		for _, row := range rows {
			cost += row.USD
		}
		if len(rows) != count || cost < 199.999999 || cost > 200.000001 || session.UsageDrops() != before {
			b.Fatalf("rows=%d cost=%.9f drops=%d", len(rows), cost, session.UsageDrops()-before)
		}
	}
}
