package telemetry

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// PeriodicFlushInterval is how long a running session may leave a completed
// usage delta waiting locally. It mirrors the ordinary server-side analytics
// queue cadence while keeping network work entirely off the model-call path.
const PeriodicFlushInterval = 30 * time.Second

type usageSession struct {
	mode Mode
	id   string
	mu   sync.Mutex
	done bool
	wg   sync.WaitGroup
}

var activeUsageSession atomic.Pointer[usageSession]

// BeginUsageSession gives the provider accounting door the session identity
// needed for usage_delta rows. Its returned function closes only this session,
// waits for its asynchronous disk appends, and is safe to call more than once.
func BeginUsageSession(mode Mode, sessionID string) func() {
	session := &usageSession{mode: mode, id: sessionID}
	activeUsageSession.Store(session)
	var once sync.Once
	return func() {
		once.Do(func() {
			activeUsageSession.CompareAndSwap(session, nil)
			session.mu.Lock()
			session.done = true
			session.mu.Unlock()
			session.wg.Wait()
		})
	}
}

// recordUsageDelta keeps counting and sending separate: CountTokens owns the
// exact provider boundary, while this function turns a positive delta into a
// local event. The append is asynchronous, and session shutdown waits for it.
func recordUsageDelta(input, output int) {
	if input <= 0 && output <= 0 {
		return
	}
	session := activeUsageSession.Load()
	if session == nil || !enabledFor() {
		return
	}
	session.mu.Lock()
	if session.done {
		session.mu.Unlock()
		return
	}
	session.wg.Add(1)
	session.mu.Unlock()
	event := UsageDelta(session.mode, input, output, session.id, time.Now())
	guard.Go("telemetry.usage-delta", func() {
		defer session.wg.Done()
		_ = SpoolSync(event)
	})
}

// StartPeriodicFlush sends queued events while a session stays open. It never
// runs network work on a model goroutine, and the returned stop waits until the
// loop has exited so the caller can perform one final bounded flush safely.
func StartPeriodicFlush(interval time.Duration) func() {
	if !enabledFor() || interval <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	guard.Go("telemetry.periodic-flush", func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				_ = Flush(ctx)
				cancel()
			case <-stop:
				return
			}
		}
	})
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}
