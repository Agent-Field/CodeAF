package session

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// awaitTestCompletion waits for a real acknowledgement. Its failure guard
// follows the suite's budget, so repository I/O on a loaded box cannot turn an
// otherwise correct landing into a race against an unrelated fixed window.
func awaitTestCompletion(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	timer := time.NewTimer(awaitPatience(t))
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("no acknowledgement of %s arrived within the suite's patience", what)
	}
}

// controlledWatch drives the real command, delta, notification and stopping
// loop. Only the wait between ticks is replaced, so a completed tick is a fact
// the test can read before changing the command's input.
type controlledWatch struct {
	completed chan struct{}
	next      chan struct{}
}

func controlWatch(agent *Agent) *controlledWatch {
	clock := &controlledWatch{completed: make(chan struct{}, 1), next: make(chan struct{})}
	agent.jobs.watchTickWait = func(ctx context.Context) {
		select {
		case clock.completed <- struct{}{}:
		case <-ctx.Done():
			return
		}
		select {
		case <-clock.next:
		case <-ctx.Done():
		}
	}
	return clock
}

func (clock *controlledWatch) completedTick(t *testing.T, watched *job) {
	t.Helper()
	select {
	case <-clock.completed:
	case <-watched.done:
	case <-time.After(awaitPatience(t)):
		t.Fatal("the watch did not complete its commanded tick")
	}
}

func (clock *controlledWatch) tick(t *testing.T, watched *job) {
	t.Helper()
	select {
	case clock.next <- struct{}{}:
	case <-watched.done:
		t.Fatal("the watch stopped before its next commanded tick")
	case <-time.After(awaitPatience(t)):
		t.Fatal("the watch did not accept its next commanded tick")
	}
	clock.completedTick(t, watched)
}

// heldSteerBash keeps a real command alive until the test releases it. Its
// lifetime follows the steer acknowledgement rather than a guessed sleep that
// could end before a loaded machine reaches the assertion.
func heldSteerBash(t *testing.T, ending string) (string, func()) {
	t.Helper()
	release := filepath.Join(t.TempDir(), "release-bash")
	command := "while [ ! -f " + shellQuoted(release) + " ]; do sleep 0.01; done; echo " + shellQuoted(ending)
	return command, func() {
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatalf("release the held bash: %v", err)
		}
	}
}

// controlledSteer holds both clocks: a command stays young until advanced,
// and a scheduled callback runs only when fired. The real scheduling delay is
// retained so the test can still assert that the grace was armed at its bound.
type controlledSteer struct {
	delay time.Duration
	fire  func()
}

func controlSteer(agent *Agent) *controlledSteer {
	clock := &controlledSteer{}
	agent.mu.Lock()
	agent.steerAge = func(*bare.BashCall) time.Duration { return 0 }
	agent.steerAfter = func(delay time.Duration, fire func()) *time.Timer {
		clock.delay, clock.fire = delay, fire
		// The timer is only a stop handle; no wall-clock callback can race the
		// command-age advance or the deliberate firing made by this test.
		return time.NewTimer(24 * time.Hour)
	}
	agent.mu.Unlock()
	return clock
}

// controlledCheckCall exposes the share the product assigned and the expiry
// that a test fires only once its stalled provider call has actually started.
type controlledCheckCall struct {
	bound  time.Duration
	expire func()
}

// controlledCheckContext retains a told deadline without arming a real timer.
// Its explicit expiry reports DeadlineExceeded, while parent cancellation and
// cleanup keep their ordinary cancellation meaning.
type controlledCheckContext struct {
	context.Context
	until   time.Time
	expired atomic.Bool
}

func (ctx *controlledCheckContext) Deadline() (time.Time, bool) { return ctx.until, true }

func (ctx *controlledCheckContext) Err() error {
	err := ctx.Context.Err()
	if err != nil && ctx.expired.Load() {
		return context.DeadlineExceeded
	}
	return err
}

func controlCheck(calls chan<- controlledCheckCall) func(context.Context, time.Duration) (context.Context, context.CancelFunc) {
	return func(parent context.Context, bound time.Duration) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		opened := &controlledCheckContext{Context: ctx, until: time.Now().Add(bound)}
		calls <- controlledCheckCall{bound: bound, expire: func() {
			opened.expired.Store(true)
			cancel()
		}}
		return opened, cancel
	}
}
