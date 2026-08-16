package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// The resident loop was launched as `_ = Serve(ctx)`. One transient failure
// inside one pass ended the background half of the surface silently and
// permanently: no announcement, no restart, and a lease still claiming the role
// so every `aforge wake` stepped aside for a process that had stopped serving
// hours before. Standing watches, charters and practice simply never fired
// again, and nothing anywhere said so.
func TestTheResidentSupervisorRestartsAndThenGivesUpLoudly(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	var announced []string
	superviseResident(context.Background(), func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return errors.New("the provider refused the practice plan")
	}, time.Millisecond, time.Hour, func(body string) {
		mu.Lock()
		defer mu.Unlock()
		announced = append(announced, body)
	})

	mu.Lock()
	defer mu.Unlock()
	if calls != residentRestartLimit {
		t.Fatalf("serve ran %d times, want %d restarts before giving up", calls, residentRestartLimit)
	}
	if len(announced) != 1 {
		t.Fatalf("announcements = %v, want exactly one and never silence", announced)
	}
	if !containsAll(announced[0], "background", "paused", "the provider refused the practice plan") {
		t.Fatalf("the user was told %q, which names neither what stopped nor why", announced[0])
	}
}

// A run long enough to count as healthy resets the ledger: a resident that
// works for hours between hiccups must never be abandoned for the sum of them.
func TestTheResidentSupervisorForgivesAWellSpacedFailure(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	superviseResident(ctx, func(context.Context) error {
		calls++
		if calls > residentRestartLimit+2 {
			cancel()
			return ctx.Err()
		}
		time.Sleep(2 * time.Millisecond)
		return errors.New("a transient hiccup")
	}, time.Millisecond, time.Millisecond, func(string) {
		t.Error("the supervisor gave up on a resident that was still recovering")
	})
	if calls <= residentRestartLimit {
		t.Fatalf("serve ran %d times; the healthy-run reset never applied", calls)
	}
}

// The ordinary shutdown: the surface is closing, Serve returns the cancellation
// it was given, and there is nothing to restart and nothing to say.
func TestTheResidentSupervisorExitsQuietlyOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		superviseResident(ctx, func(inner context.Context) error {
			calls++
			<-inner.Done()
			return inner.Err()
		}, time.Millisecond, time.Hour, func(string) {
			t.Error("a clean shutdown was announced as a failure")
		})
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the supervisor did not exit when the surface closed")
	}
	if calls != 1 {
		t.Fatalf("serve ran %d times; a cancelled context must not be restarted", calls)
	}
}

func containsAll(body string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(body, part) {
			return false
		}
	}
	return true
}
