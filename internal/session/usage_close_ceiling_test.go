package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// isolateUsageRegistry swaps in a fresh writer registry for one test and puts
// the process's own writers back afterward, so a test that drives CloseUsage or
// FlushUsage (both of which act on EVERY writer) touches only its own.
func isolateUsageRegistry(t *testing.T) {
	t.Helper()
	usageWritersMu.Lock()
	saved := usageWriters
	usageWriters = map[string]*usageWriter{}
	usageWritersMu.Unlock()
	t.Cleanup(func() {
		usageWritersMu.Lock()
		usageWriters = saved
		usageWritersMu.Unlock()
	})
}

// TestCloseUsageReturnsUnderTheCeilingWhenAWriterStalls is the failing-first
// guard for the same bargain [FlushUsage] keeps: a writer parked inside the
// write path never returns from its run loop, and CloseUsage must not wait for
// it forever, because the caller is [v3Process.closeAll] and an unbounded wait
// there is a hung ~/.codeaf holding the exit open. Without one ceiling for the
// whole call this blocks forever and the test trips its own bound.
func TestCloseUsageReturnsUnderTheCeilingWhenAWriterStalls(t *testing.T) {
	isolateUsageRegistry(t)

	blocked := make(chan struct{})
	var blockedOnce sync.Once
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }

	ledger := filepath.Join(t.TempDir(), "v3", "usage.jsonl")
	writer := &usageWriter{
		queue:   make(chan usageWrite, usageQueueDepth),
		stopped: make(chan struct{}),
		beforeWrite: func() {
			blockedOnce.Do(func() { close(blocked) })
			<-release // park the run loop inside the write path
		},
	}
	usageWritersMu.Lock()
	usageWriters[ledger] = writer
	usageWritersMu.Unlock()
	go writer.run(ledger)
	t.Cleanup(func() {
		closeRelease()
		<-writer.stopped
	})

	// One real row drives the writer into beforeWrite, where it stalls.
	RecordUsage(ledger, UsageLine{At: time.Now(), Model: "opus", Role: "talk",
		Input: 1, Output: 1, USD: 0.01})
	<-blocked // the writer is now stuck inside the write path

	start := time.Now()
	done := make(chan struct{})
	go func() { CloseUsage(); close(done) }()
	select {
	case <-done:
	case <-time.After(usageFlushLimit + 10*time.Second):
		t.Fatal("CloseUsage did not return while a writer was stalled: the wait has no ceiling")
	}
	if elapsed := time.Since(start); elapsed > usageFlushLimit+3*time.Second {
		t.Fatalf("CloseUsage took %v, well past the %v ceiling", elapsed, usageFlushLimit)
	}
}

// TestCloseUsageJoinsAndLeavesTheLastRowOnDisk is the normal case the ceiling
// must not cost: when the writer is not stalled, CloseUsage joins it and the
// last row handed to [RecordUsage] is on disk after Close returns.
func TestCloseUsageJoinsAndLeavesTheLastRowOnDisk(t *testing.T) {
	isolateUsageRegistry(t)

	ledger := filepath.Join(t.TempDir(), "v3", "usage.jsonl")
	RecordUsage(ledger, UsageLine{At: time.Now(), Model: "opus-4.1", Role: "talk",
		Input: 900, Output: 120, USD: 0.31, Session: "aaaa1111aaaa1111"})

	CloseUsage()

	data, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("CloseUsage returned before the row reached disk: %v", err)
	}
	if !strings.Contains(string(data), "opus-4.1") {
		t.Fatalf("the last row is not on disk after CloseUsage: %q", string(data))
	}
}

// TestFlushUsageAndCloseUsageDoNotRace fires a flush and a close at the same
// writers, over and over, so the race detector sees the one interleaving that
// used to panic: FlushUsage sending a marker on a queue CloseUsage had just
// closed. Both now enqueue and close under the same lock, so there is no window;
// run this with -race. On the old code (a send outside the lock) this either
// panics on a closed channel or trips -race.
func TestFlushUsageAndCloseUsageDoNotRace(t *testing.T) {
	isolateUsageRegistry(t)
	base := t.TempDir()

	for iter := 0; iter < 200; iter++ {
		for w := 0; w < 4; w++ {
			ledger := filepath.Join(base, fmt.Sprintf("u-%d-%d.jsonl", iter, w))
			writer := &usageWriter{
				queue:   make(chan usageWrite, usageQueueDepth),
				stopped: make(chan struct{}),
			}
			usageWritersMu.Lock()
			usageWriters[ledger] = writer
			usageWritersMu.Unlock()
			go writer.run(ledger)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})
		go func() { defer wg.Done(); <-start; FlushUsage() }()
		go func() { defer wg.Done(); <-start; CloseUsage() }()
		close(start)
		wg.Wait()
	}
}
