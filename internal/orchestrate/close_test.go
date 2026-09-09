package orchestrate

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Cancellation may return the snapshot promptly, but the owner's join must
// still cover the worker that is finishing its last write after cancellation.
func TestWaitOwnsWorkersAfterRunReturns(t *testing.T) {
	started, cut, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseCall := func() { once.Do(func() { close(release) }) }
	defer releaseCall()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := New("read notes", &script{steps: []Amendment{{Add: []Node{{ID: "read-notes", Goal: "read notes"}}}}},
		execFunc(func(ctx context.Context, _ Node, _ []NodeStatus) (string, float64, error) {
			close(started)
			<-ctx.Done()
			close(cut)
			<-release
			return "", 0, ctx.Err()
		}), Options{})
	returned := make(chan struct{})
	go func() {
		_, _ = run.Run(ctx)
		close(returned)
	}()
	waitFor(t, started, "worker admission")
	cancel()
	waitFor(t, returned, "Run to return without waiting for a worker")
	waitFor(t, cut, "worker cancellation")
	joined := make(chan struct{})
	go func() { run.Wait(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("join returned while an accepted worker could still write")
	case <-time.After(25 * time.Millisecond):
	}
	releaseCall()
	waitFor(t, joined, "all accepted calls to return")
}

// Naming deliberately outlives a finished run. It must still have an owner
// that can wait for its last callback before closing the session's stores.
func TestWaitOwnsNamesAfterRunReturns(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseCall := func() { once.Do(func() { close(release) }) }
	defer releaseCall()
	run := New("read notes", &script{steps: []Amendment{{Add: []Node{{ID: "n1", Goal: "read notes"}}}}},
		execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
			return "read", 0, nil
		}), Options{Name: func(context.Context, Node) string {
			close(started)
			<-release
			return "release notes"
		}})
	_, _ = run.Run(testContext(t))
	waitFor(t, started, "the name call")
	joined := make(chan struct{})
	go func() { run.Wait(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("join forgot the name still publishing a row")
	case <-time.After(25 * time.Millisecond):
	}
	releaseCall()
	waitFor(t, joined, "all accepted calls to return")
}
