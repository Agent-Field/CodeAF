package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestUsageMillionRowLedgerFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	b := bufio.NewWriterSize(out, 1024*1024)
	for i := range 1000000 {
		if _, err := fmt.Fprintf(b, "{\"at\":\"2026-11-03T12:00:00Z\",\"day\":\"2026-11-03\",\"id\":\"%024d\",\"session\":\"chat-%06d\",\"model\":\"fixture/model\",\"input\":100,\"output\":10,\"usd\":0.001,\"billing\":\"metered\"}\n", i, i/100); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	defer StopUsageWriter(path)
	done := make(chan struct{}, 1)
	unwatch := OnUsageWritten(func() {
		select {
		case done <- struct{}{}:
		default:
		}
	})
	defer unwatch()
	var baseline, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&baseline)
	began := time.Now()
	RecordUsage(path, UsageLine{ID: "new-paid-call", Input: 1, USD: .002})
	enqueue := time.Since(began)
	flushBegan := time.Now()
	flushed := FlushUsage()
	flushElapsed := time.Since(flushBegan)
	readNew := func() float64 {
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if _, e = f.Seek(185000000, 0); e != nil {
			t.Fatal(e)
		}
		var row UsageLine
		if e = json.NewDecoder(f).Decode(&row); e != nil {
			return 0
		}
		return row.USD
	}
	atFlush := readNew()
	runtime.ReadMemStats(&after)
	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatal("write not done")
	}
	t.Logf("rows=1000000 enqueue=%s flush_ok=%v flush_elapsed=%s first_write=%s drops=%d; new charge expected=$0.002000 recorded_at_flush=$%.6f recorded_after_join=$%.6f", enqueue, flushed, flushElapsed, time.Since(began), UsageDrops(), atFlush, readNew())
	// Replay through the public writer must retain exactly one new charge.
	RecordUsage(path, UsageLine{ID: "new-paid-call", Input: 1, USD: .002})
	for _, old := range []int{0, 999999} {
		RecordUsage(path, UsageLine{ID: fmt.Sprintf("%024d", old), Input: 100, USD: .001})
	}
	if !FlushUsage() {
		t.Fatal("replay flush")
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e = f.Seek(185000000, 0); e != nil {
		t.Fatal(e)
	}
	dec := json.NewDecoder(f)
	var tailTotal float64
	var tailRows int
	for {
		var row UsageLine
		e = dec.Decode(&row)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		tailTotal += row.USD
		tailRows++
	}
	totals, stats, err := EachUsageLedgerEntry(path, UsageQuery{}, UsageLedgerBudget{}, func(UsageEntry) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("total expected=$1000.002000 recorded=$%.6f; at_flush expected=$1000.002000 recorded=$%.6f new_rows=%d scanned_bytes=%d records=%d allocated_at_flush=%d", totals.USD, 1000+atFlush, tailRows, stats.Bytes, totals.Records, after.TotalAlloc-baseline.TotalAlloc)
	if math.Abs(totals.USD-1000.002) > 1e-6 || totals.Records != 1000001 || stats.Bytes != info.Size() {
		t.Fatal("full public ledger read disagrees with expected calls, bytes or total")
	}
	if after.TotalAlloc-baseline.TotalAlloc > 256<<20 {
		t.Fatal("initial writer exceeded 256 MiB allocation budget")
	}
	if tailRows != 1 || tailTotal != .002 {
		t.Fatal("new call missing or replay repeated")
	}
	if !flushed || atFlush != .002 {
		t.Fatal("default FlushUsage deadline expires scanning valid 1M-row ledger")
	}
}
