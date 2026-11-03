package cellsync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// Lease names the directory record and fence this batcher publishes under.
// After keeping orphaned turns, that record can be a branch rather than the
// local cell's ID; admission must compare the same lease as publishing does.
func (b *Batcher) Lease() (id string, fence uint64) {
	d, _, _ := b.view()
	return d.ID(), d.Fence
}

// Reacquire resumes an unchanged saved chat after its lease became free. The
// conditional acquisition cannot displace a winner that arrived after the read.
// Serializing with publishes keeps an old flush from replacing the new fence.
func (b *Batcher) Reacquire(ctx context.Context, seen directory.CellView) (bool, error) {
	idle := func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.released }()
	if idle {
		if err := lockOwnership(ctx, &b.flushMu); err != nil {
			return false, err
		}
		defer b.flushMu.Unlock()
		if err := lockOwnership(ctx, &b.renewMu); err != nil {
			return false, err
		}
		defer b.renewMu.Unlock()
	} else {
		b.flushMu.Lock()
		defer b.flushMu.Unlock()
	}
	d, pending, _ := b.view()
	if b.isClosing() || pending != 0 || d.Head != seen.Cell.Head || seen.Cell.Lease.Expires > seen.Now {
		return false, nil
	}
	wasUncertain := false
	if idle {
		wasUncertain = func() bool {
			b.mu.Lock()
			defer b.mu.Unlock()
			before := b.acquireUncertain
			b.acquireUncertain = true
			return before
		}()
	}
	v, err := b.Publisher.Dir.Acquire(ctx, d.ID(), directory.AcquireOpts{
		RequireConditional: true,
		Expect:             &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence},
	})
	if idle && (err == nil && v.Cell.Head != d.Head || errors.Is(err, directory.ErrConditionalTake) || !wasUncertain && (errors.Is(err, directory.ErrLeaseHeld) || errors.Is(err, directory.ErrFenceStale))) {
		func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.acquireUncertain = false
		}()
	}
	if err != nil {
		return false, err
	}
	if v.Cell.Head != d.Head {
		return false, b.Publisher.Dir.Release(ctx, d.ID(), v.Cell.Lease.Fence)
	}
	d.Fence, d.Frames = v.Cell.Lease.Fence, v.Cell.Frames
	b.mu.Lock()
	*b.Driving = d
	b.stale, b.released, b.renewalEnded = false, false, false
	if idle {
		// Ownership readers must see the confirmed fence before uncertainty
		// clears, so an in-flight reading cannot revive an older fence.
		b.acquireUncertain = false
	}
	b.renewalSince = time.Time{}
	b.mu.Unlock()
	b.renewalChanged()
	if idle {
		b.leaseChanged.fire()
	}
	return true, nil
}

// ReleaseIdle releases only a fully published head. The caller excludes new
// work and seals; all uploads have already finished. Contended publisher or
// heartbeat locks retain the hold for a later attempt instead of admission.
func (b *Batcher) ReleaseIdle(ctx context.Context, head string, beforeRelease func() error) (bool, error) {
	if !b.flushMu.TryLock() {
		return false, nil
	}
	defer b.flushMu.Unlock()
	d, pending, stale := b.view()
	if b.isClosing() || stale || pending != 0 || d.Fence == 0 || d.Head != head {
		return false, nil
	}
	if !b.renewMu.TryLock() {
		return false, nil
	}
	defer b.renewMu.Unlock()
	if err := beforeRelease(); err != nil {
		return false, err
	}
	b.mu.Lock()
	b.released = true
	b.mu.Unlock()
	b.leaseChanged.fire()
	if err := b.release(ctx); err != nil {
		return false, err
	}
	b.endRenewals()
	return true, nil
}

// ResumeHeld confirms that a failed release left this fence live. It cannot
// resume a changed head or a later writer, and sends no acquisition request.
func (b *Batcher) ResumeHeld(ctx context.Context, seen directory.CellView) bool {
	if lockOwnership(ctx, &b.flushMu) != nil {
		return false
	}
	defer b.flushMu.Unlock()
	b.mu.Lock()
	d := b.Driving
	if b.closing || b.stale || len(b.noted) != 0 || d.Fence != seen.Cell.Lease.Fence || d.Head != seen.Cell.Head || seen.Cell.Lease.Expires <= seen.Now {
		b.mu.Unlock()
		return false
	}
	b.released, b.renewalEnded = false, false
	b.renewalSince = time.Time{}
	b.mu.Unlock()
	b.leaseChanged.fire()
	b.renewalChanged()
	return true
}

func lockOwnership(ctx context.Context, mu *sync.Mutex) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if mu.TryLock() {
			return nil
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// AcquireUncertain says an idle take-back may have committed without a reply.
// A later own-device fence is not evidence that this window acquired it.
func (b *Batcher) AcquireUncertain() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.acquireUncertain
}
