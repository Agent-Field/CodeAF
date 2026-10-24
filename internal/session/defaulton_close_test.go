package session

import (
	"context"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/guard"
)

type closingCapture struct {
	start, release chan struct{}
	once           sync.Once
}

func (s *closingCapture) Seal(ctx context.Context, _ cell.Cell, _ cellstore.TurnInfo) (cellstore.Sealed, error) {
	s.once.Do(func() { close(s.start) })
	select {
	case <-s.release:
		return cellstore.Sealed{}, nil
	case <-ctx.Done():
		return cellstore.Sealed{}, ctx.Err()
	}
}

type closingSeat struct {
	executor.Seat
	entered chan struct{}
	once    sync.Once
}

func (s *closingSeat) Settle(ctx context.Context) { executor.Settle(ctx, s.Seat) }

func (s *closingSeat) Drain(ctx context.Context) error {
	s.once.Do(func() { close(s.entered) })
	return executor.Drain(ctx, s.Seat)
}

// D3 requires process exit to wait for the capture whose command result was
// already returned. The real agent close must reach the same pending recorder.
func TestD3AgentCloseWaitsForTheReturnedCommandsCapture(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), executor.DrainWithin)
	defer cancel()
	celltest.Available(t)
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	must(t, err)
	store := &closingCapture{start: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(store.release) }) })
	seat, err := cellstore.SeatOver(executor.HostBound, c, t.TempDir(), nil, nil,
		func(cellstore.Engine) cellstore.Store { return store }, cellstore.WithPipeline())
	must(t, err)
	closing := &closingSeat{Seat: seat, entered: make(chan struct{})}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.Place = Place{Dir: c.Root, Workspace: cfg.Workspace}
		cfg.SessionFile = cfg.Place.Transcript()
		cfg.Seat = closing
	})
	events, err := agent.SubmitBash(t.Context(), "!printf saved")
	must(t, err)
	collect(t, events)
	select {
	case <-store.start:
	case <-ctx.Done():
		t.Fatal("capture never started")
	}
	done := make(chan error, 1)
	guard.Go("test closing chat", func() { done <- agent.Close() })
	select {
	case <-closing.entered:
	case err := <-done:
		t.Fatalf("close skipped pending capture: %v", err)
	case <-ctx.Done():
		t.Fatal("close never reached capture")
	}
	select {
	case err := <-done:
		t.Fatalf("close answered before capture finished: %v", err)
	default:
	}
	release.Do(func() { close(store.release) })
	must(t, <-done)
}
