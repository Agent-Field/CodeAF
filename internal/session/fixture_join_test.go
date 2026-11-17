package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// savingSeat stands for a seat that pipelines its saves, the way a cell seat
// does: Settle starts a save and returns at once, and only Drain waits for it.
type savingSeat struct {
	executor.Seat
	mu      sync.Mutex
	running int
}

func (s *savingSeat) Settle(context.Context) {
	s.mu.Lock()
	s.running++
	s.mu.Unlock()
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.mu.Lock()
		s.running--
		s.mu.Unlock()
	}()
}

func (s *savingSeat) Drain(ctx context.Context) error {
	for s.live() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	return nil
}

func (s *savingSeat) live() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// A FIXTURE AGENT LEAVES NO SAVE RUNNING WHEN ITS TEST ENDS. The fixture's
// cleanup asks the agent to wait for its receipts, which schedules one more save
// on a pipelining seat without waiting for it. Go removes a test's temporary
// folders right after the cleanups, so a save still running writes into a folder
// that is being removed and fails the test with "directory not empty" on a CI
// runner fast enough to land it in that window.
func TestAFixtureAgentLeavesNoSaveRunningWhenItsTestEnds(t *testing.T) {
	seat := &savingSeat{Seat: executor.Host}
	t.Run("agent", func(t *testing.T) {
		newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.Seat = seat })
	})
	if n := seat.live(); n != 0 {
		t.Fatalf("%d save(s) still running after the fixture's cleanup returned", n)
	}
}
