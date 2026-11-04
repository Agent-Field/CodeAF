//go:build unix

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func TestAgentSettlementDrainsQueuedUsage(t *testing.T) {
	for _, action := range []string{"settle", "close"} {
		t.Run(action, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), UsageLedgerName)
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
			release := holdUsageLedger(t, path)
			defer StopUsageWriter(path)
			// Release before joining the writer, including on a failed assertion.
			defer release()
			const calls = 513
			for i := range calls {
				RecordUsage(path, UsageLine{ID: fmt.Sprintf("call-%d", i), Input: 1, USD: .001})
			}
			done := make(chan error, 1)
			go func() {
				if action == "close" {
					done <- agent.Close()
				} else {
					agent.SettleWrites()
					done <- nil
				}
			}()
			select {
			case err := <-done:
				t.Fatalf("%s returned with queued usage still blocked on the ledger lock: %v", action, err)
			case <-time.After(50 * time.Millisecond):
			}
			release()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("settlement did not drain after releasing the ledger")
			}
			rows, err := ReadUsage(path, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			seen := make(map[string]int)
			for _, row := range rows {
				seen[row.ID]++
			}
			if len(rows) != calls {
				t.Fatalf("rows=%d, want one for each of %d queued calls", len(rows), calls)
			}
			for i := range calls {
				if n := seen[fmt.Sprintf("call-%d", i)]; n != 1 {
					t.Fatalf("call-%d written %d times, want once", i, n)
				}
			}
		})
	}
}

func TestAgentUsageDrainHasACeilingAndEventuallyWrites(t *testing.T) {
	for _, action := range []string{"settle", "close"} {
		t.Run(action, func(t *testing.T) {
			previous := usageFlushLimit
			usageFlushLimit = 25 * time.Millisecond
			defer func() { usageFlushLimit = previous }()
			path := filepath.Join(t.TempDir(), UsageLedgerName)
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
			release := holdUsageLedger(t, path)
			defer StopUsageWriter(path)
			defer release()
			drops := UsageDrops()
			RecordUsage(path, UsageLine{ID: "before-close", Input: 1, USD: .001})
			start := time.Now()
			done := make(chan error, 1)
			go func() {
				if action == "close" {
					done <- agent.Close()
				} else {
					agent.SettleWrites()
					done <- nil
				}
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("usage timeout surfaced as a close error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("usage drain did not respect its ceiling")
			}
			elapsed := time.Since(start)
			if elapsed < usageFlushLimit {
				t.Fatalf("%s returned before trying to drain: %s", action, elapsed)
			}
			rows, err := ReadUsage(path, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 || UsageDrops() != drops {
				t.Fatalf("blocked rows=%d drops=%d", len(rows), UsageDrops()-drops)
			}
			t.Logf("%s ceiling=%s elapsed=%s error=nil rows_at_return=%d drops=%d", action, usageFlushLimit, elapsed, len(rows), UsageDrops()-drops)
			release()
			usageFlushLimit = previous
			agent.SettleWrites()
			rows, err = ReadUsage(path, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].ID != "before-close" {
				t.Fatalf("recovered rows=%+v, want the queued call once", rows)
			}
		})
	}
}

func TestClosingAnAgentKeepsSharedUsageWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	first, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
	second, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
	defer StopUsageWriter(path)
	// A different ledger must not make either agent's settlement time out.
	other := filepath.Join(t.TempDir(), UsageLedgerName)
	release := holdUsageLedger(t, other)
	defer StopUsageWriter(other)
	defer release()
	RecordUsage(other, UsageLine{ID: "other", Input: 1})
	RecordUsage(path, UsageLine{ID: "first", Input: 1})
	done := make(chan error, 1)
	go func() { done <- first.Close() }()
	RecordUsage(path, UsageLine{ID: "second", Input: 1})
	settled := make(chan error, 1)
	go func() { second.SettleWrites(); settled <- nil }()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for _, completion := range []<-chan error{done, settled} {
		select {
		case err := <-completion:
			if err != nil {
				t.Fatal(err)
			}
		case <-deadline.C:
			t.Fatal("settling one agent waited for an unrelated ledger")
		}
	}
	RecordUsage(path, UsageLine{ID: "after-close", Input: 1})
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int)
	for _, row := range rows {
		seen[row.ID]++
	}
	if len(rows) != 3 || seen["first"] != 1 || seen["second"] != 1 || seen["after-close"] != 1 {
		t.Fatalf("shared ledger calls=%v, want each call once", seen)
	}
}

// A held file lock stalls the real append path without replacing disk writes.
func holdUsageLedger(t *testing.T, path string) func() {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.Lock(file, true, false); err != nil {
		file.Close()
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(func() { _ = filelock.Unlock(file); _ = file.Close() }) }
	t.Cleanup(release)
	return release
}

func TestOverlappingSettlementsDrainAFullUsageQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	first, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
	second, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
	writer := usageWriterFor(path)
	initial := make(chan struct{})
	tail := make(chan struct{})
	releaseInitial, releaseTail := make(chan struct{}), make(chan struct{})
	var initialOnce, tailOnce sync.Once
	unblockInitial := func() { initialOnce.Do(func() { close(releaseInitial) }) }
	unblockTail := func() { tailOnce.Do(func() { close(releaseTail) }) }
	writes := 0
	writer.beforeWrite = func() {
		writes++
		if writes == 1 {
			close(initial)
			<-releaseInitial
		}
		if writes == 3 {
			close(tail)
			<-releaseTail
		}
	}
	defer StopUsageWriter(path)
	defer unblockInitial()
	defer unblockTail()
	RecordUsage(path, UsageLine{ID: "initial", Input: 1})
	<-initial
	firstSettled := make(chan struct{})
	go func() { first.SettleWrites(); close(firstSettled) }()
	// Arrange an older flush marker ahead of a full queue and a newer barrier.
	waitFor := func(ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for !ready() {
			if time.Now().After(deadline) {
				t.Fatal("could not arrange overlapping settlements")
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitFor(func() bool { return len(writer.queue) == 1 })
	for i := range usageQueueDepth {
		RecordUsage(path, UsageLine{ID: fmt.Sprintf("queued-%d", i), Input: 1})
	}
	secondSettled := make(chan struct{})
	go func() { second.SettleWrites(); close(secondSettled) }()
	waitFor(func() bool {
		writer.overflowMu.Lock()
		defer writer.overflowMu.Unlock()
		return len(writer.overflowRows) == 2
	})
	unblockInitial()
	select {
	case <-tail:
	case <-time.After(time.Second):
		t.Fatal("writer did not reach its queued tail")
	}
	select {
	case <-secondSettled:
		t.Fatal("newer settlement returned while its queued tail was still blocked")
	case <-time.After(50 * time.Millisecond):
	}
	unblockTail()
	for _, done := range []<-chan struct{}{firstSettled, secondSettled} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("settlement did not finish after releasing its tail")
		}
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int)
	for _, row := range rows {
		seen[row.ID]++
	}
	if len(rows) != 1+usageQueueDepth || len(seen) != len(rows) {
		t.Fatalf("rows=%d distinct=%d, want %d calls once", len(rows), len(seen), 1+usageQueueDepth)
	}
}
