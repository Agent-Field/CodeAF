package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUsageJournalCrashAfterLedgerAppend(t *testing.T) {
	testUsageJournalCrash(t, false)
}
func TestUsageJournalCrashMidRewrite(t *testing.T) {
	testUsageJournalCrash(t, true)
}
func testUsageJournalCrash(t *testing.T, temp bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	row := UsageLine{ID: "crash-charge", SharedID: true, At: time.Date(2026, 11, 4, 23, 59, 0, 0, time.UTC), Day: "2026-11-04", USD: .0090004, Input: 10, Session: "chat", Task: "17", FactoryJob: "job", Billing: "included", BillingBasis: "plan", Price: "provider", RemoteEvent: "remote-event"}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	for _, name := range []string{path, path + usagePendingSuffix} {
		if err := os.WriteFile(name, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if temp {
		if err := os.WriteFile(path+usagePendingSuffix+".tmp-dead", []byte("partial replacement"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 || rows[0] != row {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("ledger changed: %v", err)
	}
	pending, err := os.ReadFile(path + usagePendingSuffix)
	if err != nil || len(pending) != 0 {
		t.Fatalf("journal not compacted: %q %v", pending, err)
	}
	temps, err := filepath.Glob(path + usagePendingSuffix + ".tmp-*")
	if err != nil || len(temps) != 0 {
		t.Fatalf("stale temps: %v %v", temps, err)
	}
}

func TestUsageJournalReplaysExactMissingRowsAndRepairsTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	row := UsageLine{ID: "journal-charge", SharedID: true, At: time.Date(2026, 11, 4, 23, 59, 0, 0, time.UTC), Day: "2026-11-04", USD: .0090004, Input: 10, Session: "chat", Root: "root", Task: "17", FactoryJob: "job", Billing: "included", BillingBasis: "plan", Price: "provider"}
	raw, _ := json.Marshal(row)
	raw = append(raw, '\n')
	raw = append(raw, []byte("{torn")...)
	if err := os.WriteFile(path+usagePendingSuffix, raw, 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 || rows[0] != row {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	RecordUsage(path, UsageLine{ID: "next", Input: 1, USD: .001})
	if !StopUsageWriter(path) {
		t.Fatal("did not drain")
	}
	rows, err = ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	names, _ := filepath.Glob(path + usagePendingSuffix + ".torn-*")
	if len(names) != 1 {
		t.Fatalf("quarantine=%v", names)
	}
	tail, err := os.ReadFile(names[0])
	if err != nil || string(tail) != "{torn" {
		t.Fatalf("tail=%q %v", tail, err)
	}
}

func TestUsageJournalKeepsUnknownRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	raw := []byte("{\"future\":true}\ninvalid\n{\"id\":\"future-row\",\"at\":\"2026-11-04T12:00:00Z\",\"future\":true}\n")
	if err := os.WriteFile(path+usagePendingSuffix, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path + usagePendingSuffix)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("unknown records lost: %q %v", after, err)
	}
}

type usageLogBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *usageLogBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}
func (b *usageLogBuffer) text() string { b.Lock(); defer b.Unlock(); return b.Buffer.String() }

func TestRecordUsagePersistentDiskFailureRetainsRowWithCappedRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	p := pendingUsage(path)
	attempts := make(chan time.Time, 32)
	p.appendFile = func(*os.File, []byte) error { attempts <- time.Now(); return errors.New("injected disk failure") }
	var logs usageLogBuffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	RecordUsage(path, UsageLine{ID: "failed-identity", Input: 1, USD: .0090004})
	writer := usageWriterFor(path)
	defer func() { p.mu.Lock(); p.appendFile = nil; p.mu.Unlock(); StopUsageWriter(path) }()
	var times []time.Time
	for len(times) < 12 {
		select {
		case at := <-attempts:
			times = append(times, at)
		case <-time.After(5 * time.Second):
			t.Fatal("retry stopped")
		}
	}
	if writer.admitted.Load()-writer.handled.Load() != 1 {
		t.Fatal("failed row not retained in memory")
	}
	// The final three intervals have reached the cap; tolerate scheduler delay.
	for i := len(times) - 3; i < len(times); i++ {
		d := times[i].Sub(times[i-1])
		if d < usageRetryCap || d > usageRetryCap+500*time.Millisecond {
			t.Fatalf("retry interval=%s, cap=%s", d, usageRetryCap)
		}
	}
	if n := strings.Count(logs.text(), "accounting pending identity"); n != 1 {
		t.Fatalf("identity logs=%d: %s", n, logs.text())
	}
	p.mu.Lock()
	p.appendFile = nil
	p.mu.Unlock()
	if !flushUsage(path) {
		t.Fatal("retry did not recover")
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 || rows[0].ID != "failed-identity" || rows[0].USD != .0090004 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestUsageJournalDeadlineExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUsageJournalDeadlineChild$", "--", "journal-deadline", path)
	if output, err := cmd.CombinedOutput(); err != nil || !bytes.Contains(output, []byte("deadline returned")) {
		t.Fatalf("child: %v %s", err, output)
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) != 0 {
		t.Fatalf("projection escaped the stall: %q %v", raw, err)
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 || rows[0].ID != "deadline-charge" || rows[0].USD != .0090004 {
		t.Fatalf("deadline lost charge: %+v %v", rows, err)
	}
	after, err := os.ReadFile(path + ".pending-v1.jsonl")
	if err != nil || len(after) != 0 {
		t.Fatalf("journal not compacted: %q %v", after, err)
	}
}

func TestUsageJournalDeadlineChild(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "journal-deadline" {
		t.Skip("subprocess helper")
	}
	path := os.Args[len(os.Args)-1]
	writer := usageWriterFor(path)
	writer.beforeWrite = func() { select {} }
	RecordUsage(path, UsageLine{ID: "deadline-charge", Input: 1, USD: .0090004})
	CloseUsage()
	fmt.Print("deadline returned\n")
}

func TestUsageJournalConcurrentReplayAndAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	var group sync.WaitGroup
	for worker := 0; worker < 3; worker++ {
		group.Go(func() {
			for i := 0; i < 20; i++ {
				RecordUsage(path, UsageLine{ID: fmt.Sprintf("shared-%d", i), At: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), Input: 1, USD: .0090004})
				RecoverUsage(path)
			}
		})
	}
	group.Wait()
	if !StopUsageWriter(path) {
		t.Fatal("did not drain")
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 20 {
		t.Fatalf("rows=%d %v", len(rows), err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.ID] {
			t.Fatal("double booking")
		}
		seen[row.ID] = true
	}
	raw, err := os.ReadFile(path + usagePendingSuffix)
	if err != nil || len(raw) != 0 {
		t.Fatalf("journal=%q %v", raw, err)
	}
}

func TestUsageJournalFailedLedgerReadPreservesAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	row := UsageLine{ID: "unreadable-ledger", At: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), Input: 1, USD: .0090004}
	raw, _ := json.Marshal(row)
	raw = append(raw, '\n')
	if err := os.WriteFile(path+usagePendingSuffix, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := ReadUsage(path, time.Time{})
	if err == nil {
		t.Fatal("directory was read as a ledger")
	}
	after, err := os.ReadFile(path + usagePendingSuffix)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("failed ledger read pruned journal: %q %v", after, err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		rows, err := ReadUsage(path, time.Time{})
		if err == nil && len(rows) == 1 {
			if rows[0] != row {
				t.Fatalf("row changed: %+v", rows[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retry did not recover: %+v %v", rows, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
