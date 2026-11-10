//go:build unix

package session

import (
	"math"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Holding the ledger's file lock models a stalled disk without replacing the
// seekable ledger with a pipe. Enqueue must finish while the lock is held;
// releasing it must durably drain every charge, including queue overflow.
func TestRecordingUsageNeverWaitsOnTheDisk(t *testing.T) {
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
	case <-time.After(2 * time.Second):
		t.Fatal("RecordUsage was still inside the disk two seconds later: the turn path is waiting on the ledger")
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatal("the writer bypassed the held ledger lock")
	}
	release()
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

// A FLUSH ANSWERS EVEN WHEN THE DISK NEVER WILL. [FlushUsage] used to send into
// a writer's queue and wait on the answer with no ceiling on either, so a writer
// parked inside [openUsageLedger] on a stalled mount held the flush for good —
// and the caller holding the bag is [v3Process.closeAll], which is to say the
// terminal never came back. A fifo nobody ever reads is that mount.
func TestFlushingUsageGivesUpOnAStalledLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("this filesystem has no fifos to stall on: %v", err)
	}
	// Nobody ever opens the reading end, so the writer's own open blocks for the
	// life of the process and its queue is never drained.
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
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("this filesystem has no fifos to stall on: %v", err)
	}
	at := usageAt(t, "2026-08-25 08:00")
	// Nobody is reading the fifo, so the writer parks inside its open and every
	// row stays queued behind it.
	RecordUsage(path, UsageLine{At: at, Model: "opus-4.1", Calls: 1, Input: 10, USD: 0.01})

	if FlushUsage() {
		t.Fatal("the flush claimed a drain while its writer was parked on a fifo nobody reads")
	}

	// The writer is parked inside an open that will never answer, so the join
	// reports that it gave up rather than pretending. Either way the path is off
	// the registry, so no later row can be queued for this home.
	StopUsageWriter(path)
	usageWritersMu.Lock()
	_, kept := usageWriters[path]
	usageWritersMu.Unlock()
	if kept {
		t.Fatal("the stopped path is still on the writer registry, so a later row would queue for a home that is going away")
	}
}
