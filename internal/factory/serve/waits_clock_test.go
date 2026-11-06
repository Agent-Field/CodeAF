package serve

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// synctest injects the scheduler's time.Now, NewTimer and NewTicker together.
// Waiting for a timer's channel advances that clock without a wall-clock sleep.
func waitsAdvance(d time.Duration) {
	timer := time.NewTimer(d)
	<-timer.C
	synctest.Wait()
}

type waitsLoopFixture struct {
	h       *Handler
	reads   atomic.Int64
	block   atomic.Bool
	entered chan struct{}
	resume  chan struct{}
}

func waitsLoopSetup(t *testing.T) *waitsLoopFixture {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// This one streamed document makes each list observable through the store's
	// existing retention callback. No other reader or filesystem watcher runs.
	if _, err := st.Create(factory.Item{State: factory.StateNew, Stream: &factory.Stream{}}); err != nil {
		t.Fatal(err)
	}
	f := &waitsLoopFixture{entered: make(chan struct{}, 1), resume: make(chan struct{})}
	st.SetOutputPolicy(func() (int, int) {
		f.reads.Add(1)
		if f.block.Load() {
			f.entered <- struct{}{}
			<-f.resume
		}
		return 0, 0
	})
	ctx, cancel := context.WithCancel(context.Background())
	f.h = &Handler{config: Config{Store: st}, watch: &rootWatch{ctx: ctx, cancel: cancel, floor: newHub()}}
	f.h.startWaits()
	synctest.Wait()
	if f.reads.Load() != 1 {
		t.Fatalf("startup scans = %d", f.reads.Load())
	}
	return f
}

// W1, W7: the existing floor hub delivers a change to the running scheduler.
func TestWaitsFloorChangeSchedulesScan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := waitsLoopSetup(t)
		defer f.h.Close()
		waitsAdvance(2 * time.Second)
		f.h.watch.floor.send("1")
		synctest.Wait()
		if f.reads.Load() != 2 {
			t.Fatalf("floor change scans = %d", f.reads.Load())
		}
	})
}

// W1, W7: changes inside two seconds share one scan at the original deadline.
func TestWaitsScanCoalescesForTwoSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := waitsLoopSetup(t)
		defer f.h.Close()
		f.h.watch.floor.send("1")
		synctest.Wait()
		waitsAdvance(time.Second)
		f.h.watch.floor.send("2")
		synctest.Wait()
		waitsAdvance(time.Second - time.Nanosecond)
		if f.reads.Load() != 1 {
			t.Fatalf("scanned before two seconds: %d", f.reads.Load())
		}
		waitsAdvance(time.Nanosecond)
		if f.reads.Load() != 2 {
			t.Fatalf("coalesced scans = %d", f.reads.Load())
		}
		waitsAdvance(2 * time.Second)
		if f.reads.Load() != 2 {
			t.Fatalf("a duplicate scan remained pending: %d", f.reads.Load())
		}
	})
}

// W1, W7: the safety tick scans even when the floor reports no changes.
func TestWaitsSafetyTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := waitsLoopSetup(t)
		defer f.h.Close()
		waitsAdvance(time.Minute - time.Nanosecond)
		if f.reads.Load() != 1 {
			t.Fatalf("early safety scan: %d", f.reads.Load())
		}
		waitsAdvance(time.Nanosecond)
		if f.reads.Load() != 2 {
			t.Fatalf("safety scans = %d", f.reads.Load())
		}
		waitsAdvance(time.Minute)
		if f.reads.Load() != 3 {
			t.Fatalf("second safety scan = %d", f.reads.Load())
		}
	})
}

// W7: the real Close door cancels pending work and joins an active store scan.
func TestWaitsCloseCancelsAndJoins(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := waitsLoopSetup(t)
			defer f.h.Close()
			f.h.watch.floor.send("1")
			synctest.Wait()
			if f.reads.Load() != 1 {
				t.Fatal("scan did not wait for its deadline")
			}
			f.h.Close()
			f.h.watch.floor.send("2")
			waitsAdvance(time.Minute)
			if f.reads.Load() != 1 {
				t.Fatalf("Close left a scan pending: %d", f.reads.Load())
			}
			f.h.watch.floor.mu.Lock()
			subscribers := len(f.h.watch.floor.subs)
			f.h.watch.floor.mu.Unlock()
			if subscribers != 0 {
				t.Fatalf("Close left %d floor subscriptions", subscribers)
			}
		})
	})
	t.Run("active", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := waitsLoopSetup(t)
			defer f.h.Close()
			defer close(f.resume)
			waitsAdvance(2 * time.Second)
			f.block.Store(true)
			f.h.watch.floor.send("1")
			<-f.entered
			closed := make(chan struct{})
			go func() { f.h.Close(); close(closed) }()
			synctest.Wait()
			select {
			case <-closed:
				t.Fatal("Close returned while the scan was still active")
			default:
			}
			f.resume <- struct{}{}
			<-closed
			waitsAdvance(time.Minute)
			if f.reads.Load() != 2 {
				t.Fatalf("scan after Close: %d", f.reads.Load())
			}
		})
	})
}
