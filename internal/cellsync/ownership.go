package cellsync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// Lease names the directory record and fence this batcher publishes under.
// After keeping orphaned turns, that record can be a branch rather than the
// local cell's ID; admission must compare the same lease as publishing does.
func (b *Batcher) Lease() (id string, fence uint64) {
	d, _, _ := b.view()
	return d.ID(), d.Fence
}

// RetireFence stops renewal beats under an observed old hold, leaving sealed turns
// to publish or branch through the directory's ordinary fencing checks.
// It is safe inside ownership callbacks that already hold the publisher lock.
// Requests already in flight remain fenced by the directory; their replies
// cannot restore permission or change the expectation of the next acquisition.
// The caller owns public notices after its admission and receipt retirement.
func (b *Batcher) RetireFence(id string, fence uint64) {
	changed := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.Driving.ID() != id || b.Driving.Fence != fence {
			return false
		}
		changed := !b.released || !b.renewalEnded
		b.released, b.renewalEnded = true, true
		return changed
	}()
	if changed {
		b.leaseChanged.fire()
	}
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
	// A completed idle release never revives its old fence even if an outside
	// heartbeat lifted its expiry; Acquire advances this device to a new fence.
	sameReleasedFence := idle && seen.Cell.Lease.Fence == d.Fence
	if b.isClosing() || pending != 0 || d.Head != seen.Cell.Head || seen.Cell.Lease.Expires > seen.Now && !sameReleasedFence {
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
	d.Fence, d.Frames, d.Size = v.Cell.Lease.Fence, v.Cell.Frames, v.Cell.Size
	b.mu.Lock()
	*b.Driving = d
	b.stale, b.released, b.renewalEnded = false, false, false
	// Acquiring a fresh fence clears the lease's pending, upload and working
	// state on the directory, so the next beat reports any change from zero.
	b.beatSent, b.beatUp, b.toldWork = 0, uploaded{}, false
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

// ReacquireWorking conditionally reacquires an in-flight turn's own lapsed
// lease in place (Fence+1) when no other computer has taken the chat and the
// published head on the relay still matches this batcher's base head.
func (b *Batcher) ReacquireWorking(ctx context.Context, seen directory.CellView) (bool, error) {
	if err := lockOwnership(ctx, &b.flushMu); err != nil {
		return false, err
	}
	defer b.flushMu.Unlock()
	if err := lockOwnership(ctx, &b.renewMu); err != nil {
		return false, err
	}
	defer b.renewMu.Unlock()
	d, pending, stale := b.view()
	if b.isClosing() || stale || d.Fence == 0 || seen.Cell.Lease.Fence != d.Fence || d.Head != seen.Cell.Head || seen.Cell.Lease.Expires > seen.Now {
		return false, nil
	}
	v, err := b.Publisher.Dir.Acquire(ctx, d.ID(), directory.AcquireOpts{
		RequireConditional: true,
		Expect:             &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence},
	})
	if err != nil {
		return false, err
	}
	if v.Cell.Head != d.Head {
		return false, b.Publisher.Dir.Release(ctx, d.ID(), v.Cell.Lease.Fence)
	}
	d.Fence, d.Frames, d.Size = v.Cell.Lease.Fence, v.Cell.Frames, v.Cell.Size
	b.mu.Lock()
	*b.Driving = d
	b.stale, b.released, b.renewalEnded = false, false, false
	b.acquireUncertain = false
	b.beatSent, b.beatUp, b.toldWork = 0, uploaded{}, false
	b.renewalSince = time.Time{}
	b.mu.Unlock()
	b.recovered(false)
	b.renewalChanged()
	b.leaseChanged.fire()
	b.turns.fire()
	if pending > 0 {
		b.work.fire()
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
	wasReleased := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		before := b.released
		b.released = true
		return before
	}()
	b.leaseChanged.fire()
	// Idle retries belong to the caller's backoff. Retrying inside close's
	// release loop could hide an unsent dial failure behind a later timeout.
	if err := b.Publisher.Dir.Release(ctx, d.ID(), d.Fence); err != nil && !errors.Is(err, directory.ErrFenceStale) {
		if !wasReleased && syncwords.RequestNotSent(err) {
			func() { b.mu.Lock(); defer b.mu.Unlock(); b.released = false }()
			b.leaseChanged.fire()
		}
		return false, syncwords.UploadFailure(err)
	}
	b.endRenewals()
	return true, nil
}

// ResumeHeld confirms a retained fence is live without an acquisition request.
// It cannot resume a possibly committed release, a changed head or a later writer.
func (b *Batcher) ResumeHeld(ctx context.Context, seen directory.CellView) bool {
	if lockOwnership(ctx, &b.flushMu) != nil {
		return false
	}
	defer b.flushMu.Unlock()
	resumed := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		d := b.Driving
		if b.closing || b.stale || b.released || b.renewalEnded || len(b.noted) != 0 || d.Fence != seen.Cell.Lease.Fence || d.Head != seen.Cell.Head || seen.Cell.Lease.Expires <= seen.Now {
			return false
		}
		b.released, b.renewalEnded = false, false
		b.renewalSince = time.Time{}
		return true
	}()
	if !resumed {
		return false
	}
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
