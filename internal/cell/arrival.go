package cell

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultArrivalStallBound is the fallback stall bound for waiting on arriving
// workspace files when a caller supplies none.
var DefaultArrivalStallBound = 2 * time.Minute

// WorkspaceArrival coordinates tools and captures with a background workspace
// fetch after a chat has opened from its conversation records (C1, C3, C4).
type WorkspaceArrival struct {
	mu           sync.Mutex
	notify       chan struct{}
	roots        []string
	ready        bool
	running      bool
	err          error
	lastProgress time.Time
	failedAt     time.Time
	stallBound   func() time.Duration
	retry        func()
}

var arrivals = struct {
	sync.Mutex
	byPath map[string]*WorkspaceArrival
}{byPath: map[string]*WorkspaceArrival{}}

func cleanArrivalKey(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// BeginWorkspaceArrival registers an in-flight workspace fetch for cellRoot and
// workspace. Tools and full-tree captures wait on it until Complete is called.
func BeginWorkspaceArrival(cellRoot, workspace string, stallBound func() time.Duration, retry func()) *WorkspaceArrival {
	now := time.Now()
	a := &WorkspaceArrival{
		notify:       make(chan struct{}),
		running:      true,
		lastProgress: now,
		stallBound:   stallBound,
		retry:        retry,
	}
	arrivals.Lock()
	defer arrivals.Unlock()
	for _, p := range []string{cellRoot, workspace} {
		if k := cleanArrivalKey(p); k != "" {
			a.roots = append(a.roots, k)
			arrivals.byPath[k] = a
		}
	}
	return a
}

// LookupWorkspaceArrival returns the active arrival gate for any of paths, or nil.
func LookupWorkspaceArrival(paths ...string) *WorkspaceArrival {
	arrivals.Lock()
	defer arrivals.Unlock()
	for _, p := range paths {
		k := cleanArrivalKey(p)
		if k == "" {
			continue
		}
		if a := arrivals.byPath[k]; a != nil && !a.isReady() {
			return a
		}
		if parent := filepath.Dir(k); parent != k {
			if a := arrivals.byPath[parent]; a != nil && !a.isReady() {
				return a
			}
		}
	}
	return nil
}

// WorkspaceArriving reports whether workspace files are still arriving (or
// waiting on a failed arrival's retry) for any of paths.
func WorkspaceArriving(paths ...string) bool {
	return LookupWorkspaceArrival(paths...) != nil
}

// WaitWorkspace waits for any arriving workspace covering paths to be put in
// place. It returns nil immediately when no background arrival is pending.
func WaitWorkspace(ctx context.Context, paths ...string) error {
	a := LookupWorkspaceArrival(paths...)
	if a == nil {
		return nil
	}
	return a.Wait(ctx)
}

func (a *WorkspaceArrival) isReady() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ready
}

func (a *WorkspaceArrival) broadcastLocked() {
	close(a.notify)
	a.notify = make(chan struct{})
}

// BindPaths associates additional root or workspace paths with this arrival.
func (a *WorkspaceArrival) BindPaths(paths ...string) {
	if a == nil {
		return
	}
	arrivals.Lock()
	defer arrivals.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ready {
		return
	}
	for _, p := range paths {
		if k := cleanArrivalKey(p); k != "" {
			a.roots = append(a.roots, k)
			arrivals.byPath[k] = a
		}
	}
}

// SetRetry sets or updates the retry callback for this arrival.
func (a *WorkspaceArrival) SetRetry(retry func()) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retry = retry
}

// Touch notes that bytes or progress arrived, resetting the stall clock.
func (a *WorkspaceArrival) Touch() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.lastProgress = time.Now()
	a.broadcastLocked()
	a.mu.Unlock()
}

// BeginRetry marks a failed arrival as running again.
func (a *WorkspaceArrival) BeginRetry() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.ready {
		a.running = true
		a.err = nil
		a.lastProgress = time.Now()
		a.broadcastLocked()
	}
	a.mu.Unlock()
}

// Retry invokes the arrival's retry callback when failed.
func (a *WorkspaceArrival) Retry() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	fn := a.retry
	ready := a.ready
	a.mu.Unlock()
	if ready || fn == nil {
		return false
	}
	fn()
	return true
}

// Fail records a background fetch failure. The gate remains closed so tools
// never run against a partial workspace tree (C3, C4).
func (a *WorkspaceArrival) Fail(err error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.ready {
		a.running = false
		a.err = err
		a.failedAt = time.Now()
		a.broadcastLocked()
	}
	a.mu.Unlock()
}

// Complete marks the workspace as fully arrived and wakes waiting callers.
func (a *WorkspaceArrival) Complete() {
	if a == nil {
		return
	}
	arrivals.Lock()
	a.mu.Lock()
	a.ready = true
	a.running = false
	a.err = nil
	roots := a.roots
	a.roots = nil
	a.broadcastLocked()
	a.mu.Unlock()
	for _, k := range roots {
		if arrivals.byPath[k] == a {
			delete(arrivals.byPath, k)
		}
	}
	arrivals.Unlock()
}

// Ready reports whether the workspace has arrived.
func (a *WorkspaceArrival) Ready() bool {
	if a == nil {
		return true
	}
	return a.isReady()
}

// Err returns the current background failure error, if any.
func (a *WorkspaceArrival) Err() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

func (a *WorkspaceArrival) bound() time.Duration {
	if a.stallBound != nil {
		if d := a.stallBound(); d > 0 {
			return d
		}
	}
	if DefaultArrivalStallBound > 0 {
		return DefaultArrivalStallBound
	}
	return 2 * time.Minute
}

// Wait blocks until the workspace has arrived and been put in place, bounded by
// the move stall guard. When the background fetch fails, Wait keeps waiting up
// to the bound so a retry can still complete the tree, and then reports why in
// plain words rather than letting a tool run on a partial tree (C3, C4).
func (a *WorkspaceArrival) Wait(ctx context.Context) error {
	if a == nil {
		return nil
	}
	for {
		a.mu.Lock()
		if a.ready {
			a.mu.Unlock()
			return nil
		}
		ch := a.notify
		running := a.running
		err := a.err
		last := a.lastProgress
		failedAt := a.failedAt
		a.mu.Unlock()

		bound := a.bound()
		var wait time.Duration
		if running {
			wait = bound - time.Since(last)
			if wait <= 0 {
				return fmt.Errorf("workspace files stalled: nothing came through for %s", bound)
			}
		} else if err != nil {
			wait = bound - time.Since(failedAt)
			if wait <= 0 {
				return fmt.Errorf("workspace files could not arrive: %w", err)
			}
		} else {
			wait = bound
		}

		timer := time.NewTimer(wait)
		select {
		case <-ch:
			timer.Stop()
		case <-timer.C:
			a.mu.Lock()
			ready := a.ready
			curRunning := a.running
			curErr := a.err
			curLast := a.lastProgress
			a.mu.Unlock()
			if ready {
				return nil
			}
			if !curRunning && curErr != nil {
				return fmt.Errorf("workspace files could not arrive: %w", curErr)
			}
			if curRunning && time.Since(curLast) >= bound {
				return fmt.Errorf("workspace files stalled: nothing came through for %s", bound)
			}
		case <-ctx.Done():
			timer.Stop()
			a.mu.Lock()
			curErr := a.err
			a.mu.Unlock()
			if curErr != nil {
				return fmt.Errorf("workspace files could not arrive: %w", curErr)
			}
			return ctx.Err()
		}
	}
}
