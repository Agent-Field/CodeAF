//go:build unix

package session

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func TestAgentSettlementDrainsQueuedUsage(t *testing.T) {
	for _, action := range []string{"close"} {
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
				done <- agent.Close()
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
	for _, action := range []string{"close"} {
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
				done <- agent.Close()
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
			if len(rows) != 0 || UsageDrops()-drops != 1 {
				t.Fatalf("blocked rows=%d short=%d, want 0 written and 1 short", len(rows), UsageDrops()-drops)
			}
			t.Logf("%s ceiling=%s elapsed=%s error=nil rows_at_return=%d drops=%d", action, usageFlushLimit, elapsed, len(rows), UsageDrops()-drops)
			release()
			usageFlushLimit = previous
			if !flushUsage(path) {
				t.Fatal("the row was not written once the ledger answered")
			}
			rows, err = ReadUsage(path, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].ID != "before-close" {
				t.Fatalf("recovered rows=%+v, want the queued call once", rows)
			}
			if UsageDrops() != drops {
				t.Fatalf("short by %d after the row was written", UsageDrops()-drops)
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
	go func() { flushUsage(path); close(firstSettled) }()
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
	go func() { flushUsage(path); close(secondSettled) }()
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

// A stalled ledger costs one close one ceiling, not one for each time the close
// settles its writes: two ceilings is a floor the second wait cannot get under.
func TestClosingAnAgentWaitsOneCeilingOnAStalledLedger(t *testing.T) {
	previous := usageFlushLimit
	usageFlushLimit = time.Second
	defer func() { usageFlushLimit = previous }()
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
	release := holdUsageLedger(t, path)
	defer StopUsageWriter(path)
	defer release()
	RecordUsage(path, UsageLine{ID: "before-close", Input: 1, USD: .001})
	start := time.Now()
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= 2*usageFlushLimit {
		t.Fatalf("close waited %s on a stalled ledger with a ceiling of %s", elapsed, usageFlushLimit)
	}
}

// A row the full queue could not take is reached while traffic keeps coming:
// steady writers must not hold the kept rows back until they stop.
func TestKeptUsageRowsAreReachedUnderSteadyTraffic(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	writer := usageWriterFor(path)
	// A slow disk: traffic outruns the writer, so the queue never empties alone.
	writer.beforeWrite = func() { time.Sleep(5 * time.Millisecond) }
	release := holdUsageLedger(t, path)
	defer StopUsageWriter(path)
	defer release()
	var issued atomic.Int64
	record := func() {
		RecordUsage(path, UsageLine{ID: fmt.Sprintf("call-%d", issued.Add(1)), Input: 1, USD: .001})
	}
	for range usageQueueDepth + 2 {
		record()
	}
	var oldest chan struct{}
	for oldest == nil {
		record()
		writer.overflowMu.Lock()
		if len(writer.overflowRows) > 0 {
			oldest = writer.overflowRows[0].done
		}
		writer.overflowMu.Unlock()
	}
	stop := make(chan struct{})
	var writers sync.WaitGroup
	for range 8 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					record()
					time.Sleep(50 * time.Microsecond)
				}
			}
		}()
	}
	released := time.Now()
	release()
	select {
	case <-oldest:
		t.Logf("oldest kept row written %s after the ledger answered, %d rows issued so far", time.Since(released), issued.Load())
	case <-time.After(3 * time.Second):
		close(stop)
		writers.Wait()
		t.Fatalf("the oldest kept row was still waiting after 3s of steady traffic, %d rows issued", issued.Load())
	}
	close(stop)
	writers.Wait()
	previous := usageFlushLimit
	usageFlushLimit = time.Minute
	defer func() { usageFlushLimit = previous }()
	if !FlushUsage() {
		t.Fatal("flush")
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int, len(rows))
	for _, row := range rows {
		seen[row.ID]++
	}
	total := int(issued.Load())
	if len(rows) != total || len(seen) != total {
		t.Fatalf("rows=%d distinct=%d, want %d of each", len(rows), len(seen), total)
	}
}

// Reaching the ceiling leaves one line and a count, with no figures in either.
// The count holds what is still short, so a row written later takes it back.
func TestReachingTheUsageCeilingIsLoggedAndCounted(t *testing.T) {
	previous := usageFlushLimit
	usageFlushLimit = 25 * time.Millisecond
	defer func() { usageFlushLimit = previous }()
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
	release := holdUsageLedger(t, path)
	defer StopUsageWriter(path)
	defer release()
	drops := UsageDrops()
	for i := range 3 {
		RecordUsage(path, UsageLine{ID: fmt.Sprintf("call-%d", i), Input: 1, USD: .125})
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if got := UsageDrops() - drops; got != 3 {
		t.Fatalf("drops after the ceiling = %d, want 3", got)
	}
	if flushUsage(path) {
		t.Fatal("flush answered true on a held ledger")
	}
	if got := UsageDrops() - drops; got != 3 {
		t.Fatalf("a second look counted the same rows again: %d", got)
	}
	line := logged.String()
	if !strings.Contains(line, path) || !strings.Contains(line, "3 rows") || strings.Contains(line, "0.125") || strings.Contains(line, ".125") {
		t.Fatalf("log = %q", line)
	}
	release()
	usageFlushLimit = previous
	if !flushUsage(path) {
		t.Fatal("flush after the ledger answered")
	}
	if got := UsageDrops() - drops; got != 0 {
		t.Fatalf("drops after the rows were written = %d, want 0", got)
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d, want 3", len(rows))
	}
}
