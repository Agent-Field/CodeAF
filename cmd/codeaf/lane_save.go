package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// Two hundred local compactions of 512 lane pairs, 256 workload classes and
// 512 journal records (442,493 snapshot bytes, 114,688 journal bytes) had a
// p99 of 16.738302 ms. Three times that gives slow writes room without an
// unbounded quit; this is below the 500 ms maximum for lane saving.
const laneSaveWithin = 3 * 16_738_302 * time.Nanosecond

const laneSaveTimedOut = "codeaf: lane saving timed out; quitting without waiting further"

// startLaneSaving gives the process exit a handle on its writer's lifetime.
func startLaneSaving(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	guard.Go("lanes/persist", func() {
		defer close(done)
		defer lanes.Flush()
		lanes.Persist(ctx)
	})
	return done
}

func finishLaneSaving(cancel context.CancelFunc, done <-chan struct{}) {
	cancel()
	timer := time.NewTimer(laneSaveWithin)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		fmt.Fprintln(os.Stderr, laneSaveTimedOut)
	}
}
