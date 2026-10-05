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
// waits for its disk appends, and is safe to call more than once.
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
// accounting boundary, while this function turns a receipt into a local
// event. The append finishes before this boundary returns; shutdown waits
// for any append already in progress.
func recordUsageDelta(input, output int, dimensions UsageDimensions) {
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
	event := UsageReceipt(session.mode, input, output, session.id, time.Now(), dimensions)
	// The receipt is appended before accounting returns so a hard stop cannot
	// strand completed usage in an unscheduled goroutine. Network work stays
	// on the periodic sender.
	defer session.wg.Done()
	if err := SpoolSync(event); err != nil {
		oneWarning("codeaf: usage counts could not be saved locally")
	}
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

// CaptureUsageRecorder binds a later provider receipt to the original session.
// A receipt worker may finish after that session closes or a new one opens;
// looking up the active session then would attribute its tokens to the wrong
// work. This callback records at most once and still honors current opt-out.
func CaptureUsageRecorder(dimensions UsageDimensions) func(int, int) {
	session := activeUsageSession.Load()
	if session == nil || !enabledFor() {
		return nil
	}
	mode, id := session.mode, session.id
	var once sync.Once
	return func(input, output int) {
		once.Do(func() {
			if !enabledFor() {
				return
			}
			event := UsageReceipt(mode, input, output, id, time.Now(), dimensions)
			if err := SpoolSync(event); err != nil {
				oneWarning("codeaf: usage counts could not be saved locally")
			}
		})
	}
}
