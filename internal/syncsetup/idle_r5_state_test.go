package syncsetup

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIdleRelease_R5_5_OrdinaryStateRoundWaitsForCompetingFlush(t *testing.T) {
	h := newTwoHomes(t)
	mustNil(t, h.a.putDevice(t.Context(), nameA))
	entered, release, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { stateMu.Lock(); defer stateMu.Unlock(); defer close(ended); close(entered); <-release }()
	defer func() { close(release); <-ended }()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- h.a.SyncState(ctx) }()
	select {
	case err := <-result:
		t.Fatalf("ordinary state round did not serialize behind the flush: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("state round cancellation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("state round ignored its caller's cancellation")
	}
}
