package cellsync

import (
	"context"
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
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	d, pending, _ := b.view()
	if b.isClosing() || pending != 0 || d.Head != seen.Cell.Head || seen.Cell.Lease.Expires > seen.Now {
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
	d.Fence, d.Frames = v.Cell.Lease.Fence, v.Cell.Frames
	b.mu.Lock()
	*b.Driving = d
	b.stale, b.renewalEnded = false, false
	b.renewalSince = time.Time{}
	b.mu.Unlock()
	b.renewalChanged()
	return true, nil
}
