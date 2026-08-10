package runner

// The per-SessionID runner registry, a port of src/session/run-state.ts:27-106,
// plus Session.BusyError (src/session/session.ts:416-420).
//
// Lock order: Runner.mu first, Registry.mu second, never the other way. In
// practice the two never overlap — the onIdle callback that deletes the map
// entry is invoked after the runner has released its own mutex (see the
// package comment) — but the order is stated so a future edit that moves
// onIdle inside the lock does not silently deadlock.

import (
	"context"
	"sync"
)

// BusyError is `class BusyError extends Error` with a sessionID field. It does
// not override `name`, so JS reports "Error".
type BusyError struct {
	SessionID string
}

func (e *BusyError) Error() string        { return "Session " + e.SessionID + " is busy" }
func (e *BusyError) ErrorName() string    { return "Error" }
func (e *BusyError) ErrorMessage() string { return e.Error() }

// Status is `status.set(sessionID, {type})` from SessionStatus.Service, the one
// external service run-state.ts talks to. `status` is "idle" or "busy".
type Status func(sessionID string, status string)

// Registry is the `Map<SessionID, Runner>` of run-state.ts:35 with its
// lifecycle hooks wired.
type Registry[A any] struct {
	mu       sync.Mutex
	m        map[string]*Runner[A]
	scopeCtx context.Context
	status   Status
}

// NewRegistry builds the registry over an instance scope. `status` may be nil.
func NewRegistry[A any](scopeCtx context.Context, status Status) *Registry[A] {
	if scopeCtx == nil {
		scopeCtx = context.Background()
	}
	return &Registry[A]{m: map[string]*Runner[A]{}, scopeCtx: scopeCtx, status: status}
}

func (rg *Registry[A]) setStatus(sessionID, s string) {
	if rg.status != nil {
		rg.status(sessionID, s)
	}
}

// Runner is run-state.ts:49-69: lazily create, and return the EXISTING runner
// untouched if there is one — including its already-bound onInterrupt, which is
// why a second caller's onInterrupt is ignored.
func (rg *Registry[A]) Runner(sessionID string, onInterrupt func() (A, error)) *Runner[A] {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	if existing, ok := rg.m[sessionID]; ok {
		return existing
	}
	next := New[A](rg.scopeCtx, Options[A]{
		OnIdle: func() {
			// The map entry is DELETED on idle, so the next turn of this
			// session gets a fresh runner with ids back at 0.
			rg.mu.Lock()
			delete(rg.m, sessionID)
			rg.mu.Unlock()
			rg.setStatus(sessionID, "idle")
		},
		OnBusy:      func() { rg.setStatus(sessionID, "busy") },
		OnInterrupt: onInterrupt,
		Busy:        func() { panic(&BusyError{SessionID: sessionID}) },
	})
	rg.m[sessionID] = next
	return next
}

// AssertNotBusy is run-state.ts:71-75.
//
// Divergence: the TS `throw`s inside an Effect.fn generator (so it becomes a
// fiber defect); this returns *BusyError. It is a predicate with no fiber to
// defect into and every caller is a Go caller. StartShell's busy path, which
// the design doc calls out explicitly, still panics.
func (rg *Registry[A]) AssertNotBusy(sessionID string) error {
	rg.mu.Lock()
	existing, ok := rg.m[sessionID]
	rg.mu.Unlock()
	if ok && existing.Busy() {
		return &BusyError{SessionID: sessionID}
	}
	return nil
}

// Cancel is run-state.ts:77-85: no runner, or an idle one, just forces the
// session status to idle.
func (rg *Registry[A]) Cancel(sessionID string) {
	rg.mu.Lock()
	existing, ok := rg.m[sessionID]
	rg.mu.Unlock()
	if !ok || !existing.Busy() {
		rg.setStatus(sessionID, "idle")
		return
	}
	existing.Cancel()
}

// EnsureRunning is run-state.ts:87-93.
func (rg *Registry[A]) EnsureRunning(
	ctx context.Context,
	sessionID string,
	onInterrupt func() (A, error),
	work Work[A],
) (A, error) {
	return rg.Runner(sessionID, onInterrupt).EnsureRunning(ctx, work)
}

// StartShell is run-state.ts:95-102. Panics with *BusyError when the session is
// already busy, exactly as the TS defects with one.
func (rg *Registry[A]) StartShell(
	ctx context.Context,
	sessionID string,
	onInterrupt func() (A, error),
	work Work[A],
	ready *Latch,
) (A, error) {
	return rg.Runner(sessionID, onInterrupt).StartShell(ctx, work, ready)
}

// Close is the scope finalizer at run-state.ts:36-44: cancel every runner
// concurrently, then clear the map. The snapshot-then-release is required
// because each Cancel drives onIdle, which takes this same mutex to delete its
// entry. ctx only governs the caller's wait; cancellation continues in the
// background, just as closing the Effect scope continues interrupting all
// children.
func (rg *Registry[A]) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rg.mu.Lock()
	runners := make([]*Runner[A], 0, len(rg.m))
	for _, r := range rg.m {
		runners = append(runners, r)
	}
	rg.mu.Unlock()

	var wg sync.WaitGroup
	for _, r := range runners {
		wg.Add(1)
		go func(r *Runner[A]) {
			defer wg.Done()
			r.Cancel()
		}(r)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		rg.mu.Lock()
		clear(rg.m)
		rg.mu.Unlock()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return context.Cause(ctx)
	}
	return nil
}
