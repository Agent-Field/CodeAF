package session

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestUsageJournalSyncBatches(t *testing.T) {
	for _, n := range []int{1, 64} {
		name := "interval"
		if n == 64 {
			name = "row-count"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), UsageLedgerName)
				synced := make(chan time.Time, 8)
				pendingUsage(path).syncFile = func(f *os.File) error { synced <- time.Now(); return f.Sync() }
				blocked := make(chan struct{})
				usageWriterFor(path).beforeWrite = func() { <-blocked }
				defer func() { close(blocked); StopUsageWriter(path) }()
				start := time.Now()
				for range n {
					RecordUsage(path, UsageLine{Input: 1})
				}
				if n == 1 {
					select {
					case <-synced:
						t.Fatal("journal sync ran on the caller before the batch interval")
					default:
					}
				}
				select {
				case at := <-synced:
					if n == 64 && at.Sub(start) != 0 {
						t.Fatalf("row threshold waited for timer: %s", at.Sub(start))
					}
					if n == 1 && at.Sub(start) < usageSyncInterval {
						t.Fatalf("early sync: %s", at.Sub(start))
					}
				case <-time.After(time.Second):
					t.Fatal("background sync did not run")
				}
			})
		})
	}
}

func TestUsageJournalCleanCloseSyncsBeforeCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	synced := make(chan int64, 8)
	pendingUsage(path).syncFile = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		synced <- info.Size()
		return f.Sync()
	}
	RecordUsage(path, UsageLine{ID: "close-sync", Input: 1})
	if !StopUsageWriter(path) {
		t.Fatal("close did not drain")
	}
	select {
	case size := <-synced:
		if size == 0 {
			t.Fatal("journal compacted before sync")
		}
	default:
		t.Fatal("clean close did not sync journal")
	}
	raw, err := os.ReadFile(path + usagePendingSuffix)
	if err != nil || len(raw) != 0 {
		t.Fatalf("pending=%d err=%v", len(raw), err)
	}
}

func TestUsageJournalSyncDoesNotBlockAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	pendingUsage(path).syncFile = func(f *os.File) error {
		once.Do(func() { close(started); <-release })
		return f.Sync()
	}
	defer func() { close(release); StopUsageWriter(path) }()
	for range 64 {
		RecordUsage(path, UsageLine{Input: 1})
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sync did not start")
	}
	returned := make(chan struct{})
	go func() { RecordUsage(path, UsageLine{Input: 1}); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("slow background sync blocked RecordUsage")
	}
}
