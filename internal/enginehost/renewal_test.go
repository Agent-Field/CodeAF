package enginehost

import (
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// renewingAgent supplies the health changes that arrive while a chat is idle.
type renewingAgent struct {
	stubAgent
	mu      sync.Mutex
	since   time.Time
	held    bool
	changes chan struct{}
}

func (a *renewingAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.since, 10 * time.Second, a.held
}

func (a *renewingAgent) RenewalChanges() <-chan struct{} { return a.changes }

func (a *renewingAgent) change(since time.Time, held bool) {
	a.mu.Lock()
	a.since, a.held = since, held
	a.mu.Unlock()
	a.changes <- struct{}{}
}

// Contract D1 carries welcome, idle failure, recovery and displacement across the host wire.
func TestHostCarriesRenewalHealthWithoutSurfaceCalls(t *testing.T) {
	shortHome(t)
	workspace := t.TempDir()
	far := &renewingAgent{held: true, changes: make(chan struct{}, 1)}
	stopped := make(chan error, 1)
	go func() {
		stopped <- Run(workspace, Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: far, Workspace: workspace}, nil
		}})
	}()
	waitForHostQuietly(t, workspace)
	t.Cleanup(func() { _ = Retire(workspace, true); <-stopped })
	client := dialHost(t, workspace)
	defer client.Close()
	reader, ok := any(client.Agent()).(interface {
		RenewalStatus() (time.Time, time.Duration, bool)
	})
	if !ok {
		t.Fatal("the surface's hosted agent cannot read renewal health")
	}
	if since, period, held := reader.RenewalStatus(); !since.IsZero() || period != 10*time.Second || !held {
		t.Fatalf("welcome renewal state = %v, %v, %v", since, period, held)
	}
	before := client.CallsMade()
	firstFailure := time.Now().Add(-20 * time.Second).UTC()
	for _, state := range []struct {
		since time.Time
		held  bool
	}{{firstFailure, true}, {time.Time{}, true}, {time.Time{}, false}} {
		far.change(state.since, state.held)
		waitUntil(t, "idle renewal state", func() bool {
			since, period, held := reader.RenewalStatus()
			return since.Equal(state.since) && period == 10*time.Second && held == state.held
		})
	}
	if calls := client.CallsMade() - before; calls != 0 {
		t.Fatalf("reading cached renewal health made %d calls", calls)
	}
}
