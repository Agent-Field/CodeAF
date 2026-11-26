package serve

import (
	"context"
	"log"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

const (
	// The first wait gives a briefly unavailable host time to return.
	hostedRemovalFirstWait = 5 * time.Second
	// The ceiling lets us notice a returned host within five minutes and limits
	// a down host to one failed delete per person every five minutes.
	hostedRemovalMaxWait = 5 * time.Minute
)

// hostedRemovalRetry finishes what a removal left undone. Removing a person
// writes a durable mark first (store.MarkHostedRemoval), so a host that is down
// at that moment, or a server that restarts before the host returns, does not
// lose the deletion: the mark is read again at start and after every wait, and
// it is cleared only when the host has confirmed the person's chats gone.
// There is at most one wait pending for all removed people.
type hostedRemovalRetry struct {
	mu         sync.Mutex
	stop       func() bool
	cancel     context.CancelFunc
	generation uint64
	closed     bool
	failed     int
	passes     sync.WaitGroup
}

// startHostedRemovals resumes after a restart: a mark left by an earlier
// process means a removal is still owed, and the first try comes soon, because
// the chats' host usually starts alongside this server.
func (h *Handler) startHostedRemovals() {
	if st := h.config.Store; st != nil {
		if marks, err := st.HostedRemovals(); err == nil && len(marks) > 0 {
			h.scheduleHostedRemovals()
		}
	}
}

// scheduleHostedRemovals starts the first wait. It does nothing while a wait is
// pending or a pass is running, because either one reads every mark when it
// ends, including the one that asked.
func (h *Handler) scheduleHostedRemovals() {
	r := &h.config.Hosted.state.removals
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.stop != nil || r.cancel != nil {
		return
	}
	h.armHostedRemovalsLocked(r, hostedRemovalFirstWait)
}

func (h *Handler) armHostedRemovalsLocked(r *hostedRemovalRetry, wait time.Duration) {
	r.generation++
	generation := r.generation
	run := func() {
		defer guard.Recover("hosted removal retry")
		h.runHostedRemovals(r, generation, wait)
	}
	if after := h.config.Hosted.removalAfter; after != nil {
		r.stop = after(wait, run)
	} else {
		r.stop = time.AfterFunc(wait, run).Stop
	}
}

// runHostedRemovals is one pass over every removal still owed. It holds no lock
// while it talks to the host, so Close can cancel it, and it clears a mark only
// when the host confirmed that person's chats gone.
func (h *Handler) runHostedRemovals(r *hostedRemovalRetry, generation uint64, wait time.Duration) {
	r.mu.Lock()
	if r.closed || generation != r.generation || r.stop == nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.stop = nil
	r.cancel = cancel
	r.passes.Add(1)
	r.mu.Unlock()
	defer r.passes.Done()
	finished := 0
	defer func() {
		cancel()
		h.finishHostedRemovals(r, wait, finished)
	}()

	st := h.config.Store
	marks, err := st.HostedRemovals()
	if err != nil {
		return
	}
	for _, person := range slices.Sorted(maps.Keys(marks)) {
		if ctx.Err() != nil {
			return
		}
		mark := marks[person]
		if h.cleanRemoved(ctx, person, mark.Since) && st.ClearHostedRemoval(person, mark.Since) == nil {
			finished++
		}
	}
}

// finishHostedRemovals ends a pass: nothing is scheduled when no mark is left,
// and otherwise the next wait is twice as long, up to the ceiling, so a host
// that is down for a day costs a few hundred cheap calls and a handful of log
// lines, and one that returns is noticed within minutes.
func (h *Handler) finishHostedRemovals(r *hostedRemovalRetry, wait time.Duration, finished int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancel = nil
	if r.closed {
		return
	}
	// Check while holding the same lock as scheduling so a removal marked
	// during this pass either appears here or can schedule its own retry.
	remaining, err := h.config.Store.HostedRemovals()
	logf := h.config.Hosted.removalLogf
	if logf == nil {
		logf = log.Printf
	}
	if err == nil && len(remaining) == 0 {
		r.failed = 0
		if finished > 0 {
			logf("hosted chats: finished removing the chats of %d removed people", finished)
		}
		return
	}
	r.failed++
	// Double the previous wait so the backoff never shifts past the ceiling.
	h.armHostedRemovalsLocked(r, min(wait*2, hostedRemovalMaxWait))
	if hostedRemovalLogDue(r.failed) {
		if err != nil {
			logf("hosted chats: could not read which removed people's chats still need to be removed; will retry")
		} else {
			logf("hosted chats: %d removed people are still waiting on the chats' host; will retry", len(remaining))
		}
	}
}

// hostedRemovalLogDue is true for the 1st failed pass and every power of ten
// after it, so the log grows with the logarithm of the outage, not its length.
func hostedRemovalLogDue(failed int) bool {
	for failed > 1 && failed%10 == 0 {
		failed /= 10
	}
	return failed == 1
}

// closeHostedRemovals ends the wait and the pass in flight, and returns only
// when no pass is still touching the store or the host.
func (h *Handler) closeHostedRemovals() {
	cfg := h.config.Hosted
	if cfg == nil || cfg.state == nil {
		return
	}
	r := &cfg.state.removals
	r.mu.Lock()
	r.closed = true
	r.generation++
	if r.stop != nil {
		r.stop()
		r.stop = nil
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	r.passes.Wait()
}
