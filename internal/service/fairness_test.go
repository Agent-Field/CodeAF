package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type slowChecks struct {
	*fakeRuntime
	clock  *fakeClock
	seenMu sync.Mutex
	seen   map[string]int
	calls  int
}

func (r *slowChecks) Probe(ctx context.Context, spec Spec) error {
	done := make(chan struct{})
	timer := r.clock.AfterFunc(CheckTimeout, func() { close(done) })
	defer timer.Stop()
	r.seenMu.Lock()
	r.seen[spec.Name]++
	r.calls++
	r.seenMu.Unlock()
	select {
	case <-done:
		return errors.New("slow health check failed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitForChecks(t *testing.T, r *slowChecks, total int) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		r.seenMu.Lock()
		n := r.calls
		r.seenMu.Unlock()
		if n >= total {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("health workers never began %d checks", total)
}

func TestOldSlowServicesCannotPermanentlyStarveNewerServices(t *testing.T) {
	h := newHarness(t)
	r := &slowChecks{fakeRuntime: h.rt, clock: h.clock, seen: map[string]int{}}
	h.s.rt = r
	for n := 0; n < 12; n++ {
		h.keep(fmt.Sprintf("service-%d", n), PolicyNever)
	}
	h.clock.fireDue(CheckEvery)
	waitForChecks(t, r, h.s.workers)
	for wave := 1; wave <= 12; wave++ {
		h.clock.fireDue(CheckTimeout)
		waitForChecks(t, r, (wave+1)*h.s.workers)
	}
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	if len(r.seen) != 12 {
		t.Fatalf("only %d of 12 approved services were ever checked after 60 seconds under bounded load: %v", len(r.seen), r.seen)
	}
}
