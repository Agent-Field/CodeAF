//go:build unix

package session

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Independent appenders share the generation lock without waiting for one another.
func TestRecordUsageSharesLedgerLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	prepareUsage(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = filelock.Lock(file, false, false); err != nil {
		t.Fatal(err)
	}
	defer StopUsageWriter(path)
	defer filelock.Unlock(file)
	done := make(chan struct{})
	go func() { RecordUsage(path, UsageLine{ID: "shared-lock", Input: 1}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RecordUsage blocked behind another shared lock")
	}
}

// Journal handoff shares the ledger lock, so it must wait before claiming durability.
func TestRecordingUsageWaitsForTheDurableJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.Lock(file, true, false); err != nil {
		file.Close()
		t.Fatal(err)
	}
	locked := true
	release := func() {
		if locked {
			if err := filelock.Unlock(file); err != nil {
				t.Error(err)
			}
			locked = false
		}
	}
	t.Cleanup(func() { release(); StopUsageWriter(path); file.Close() })
	at := usageAt(t, "2026-08-25 08:00")
	drops := UsageDrops()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		RecordUsage(path, UsageLine{At: at, Model: "opus-4.1", Role: "title", Calls: 2,
			Input: 900, Output: 120, USD: 0.31, Session: "aaaa1111aaaa1111", Workspace: "/repo"})
		for i := 0; i < 4*usageQueueDepth; i++ {
			RecordUsage(path, UsageLine{At: at.Add(time.Duration(i) * time.Second),
				Model: "opus-4.1", Calls: 1, Input: 10, USD: 0.01})
		}
	}()
	select {
	case <-returned:
		t.Fatal("RecordUsage returned before acquiring the journal lock")
	case <-time.After(50 * time.Millisecond):
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatal("the writer bypassed the held ledger lock")
	}
	release()
	select {
	case <-returned:
	case <-time.After(time.Minute):
		t.Fatal("journal did not resume")
	}
	if !FlushUsage() {
		t.Fatal("the flush hit its deadline rather than draining; the rows in front of it are still queued")
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1+4*usageQueueDepth || UsageDrops() != drops {
		t.Fatalf("durable rows=%d want=%d dropped=%d", len(rows), 1+4*usageQueueDepth, UsageDrops()-drops)
	}
	row := rows[0]
	if !row.At.Equal(at) || row.Day != at.Format(usageDayLayout) {
		t.Fatalf("the row's day is %q at %s", row.Day, row.At)
	}
	if row.Model != "opus-4.1" || row.Role != "title" || row.Calls != 2 {
		t.Fatalf("the row is %+v", row)
	}
	if row.Input != 900 || row.Output != 120 || row.USD != 0.31 {
		t.Fatalf("the figures are %+v", row)
	}
	if row.Session != "aaaa1111aaaa1111" || row.Workspace != "/repo" {
		t.Fatalf("the ids are %+v", row)
	}
	var total float64
	for _, row := range rows {
		total += row.USD
	}
	if math.Abs(total-(.31+4*usageQueueDepth*.01)) > 1e-9 {
		t.Fatalf("drained spend=$%.6f want=$%.6f", total, .31+4*usageQueueDepth*.01)
	}
}

// A flush keeps its ceiling even when journaled rows cannot reach the ledger.
func TestFlushingUsageGivesUpOnAStalledLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	release := holdUsageProjection(t, path)
	defer func() { release(); StopUsageWriter(path) }()
	RecordUsage(path, UsageLine{At: usageAt(t, "2026-08-25 08:00"),
		Model: "opus-4.1", Calls: 1, Input: 10, USD: 0.01})

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		FlushUsage()
	}()
	select {
	case <-returned:
	case <-time.After(usageFlushLimit + 3*time.Second):
		t.Fatal("FlushUsage never came back from a ledger nobody is reading")
	}
}

// A FLUSH THAT HIT ITS DEADLINE IS NOT A JOIN, AND SAYS SO.
//
// FlushUsage waits under a ceiling and returns either way, which is the right
// bargain: a stalled ledger must not hold a terminal open. What a caller cannot
// work out for itself is WHICH of the two happened, and a caller that reads a
// deadline as a join goes on to do what the queued rows are in the way of. The
// deadline is shrunk here rather than provoked with load, because a test that
// needs a timer to fire should say so in one line.
func TestAFlushThatHitsItsDeadlineSaysSoAndStoppingTheWriterIsTheJoin(t *testing.T) {
	previous := usageFlushLimit
	usageFlushLimit = time.Millisecond
	defer func() { usageFlushLimit = previous }()

	home := t.TempDir()
	path := filepath.Join(home, UsageLedgerName)
	release := holdUsageProjection(t, path)
	defer release()
	at := usageAt(t, "2026-08-25 08:00")
	// The projection is held after the durable handoff.
	RecordUsage(path, UsageLine{At: at, Model: "opus-4.1", Calls: 1, Input: 10, USD: 0.01})

	if FlushUsage() {
		t.Fatal("the flush claimed a drain while its writer was parked on a fifo nobody reads")
	}

	// Releasing projection lets stop join the writer after the timed-out flush.
	release()
	StopUsageWriter(path)
	usageWritersMu.Lock()
	_, kept := usageWriters[path]
	usageWritersMu.Unlock()
	if kept {
		t.Fatal("the stopped path is still on the writer registry, so a later row would queue for a home that is going away")
	}
}
