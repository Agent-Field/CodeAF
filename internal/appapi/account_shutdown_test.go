package appapi

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// A blocked tick makes the shutdown race deterministic without a real sleep.
func TestServerCloseCancelsAndJoinsAnInFlightStandingTick(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	s := New(testToken, Options{Agents: NewAgentRegistry(t.TempDir(), nil), StandingTick: func(ctx context.Context, _ *standing.Store) error {
		close(started)
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	}})
	t.Cleanup(s.Close)
	s.wakeOrders()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("standing tick did not start")
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel its tick")
	}
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before the tick ended")
	}
}
