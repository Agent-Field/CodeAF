package cellsync

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

type r8ReleaseDirectory struct {
	directory.Client
	failure error
	commit  bool
	calls   int
}

func (d *r8ReleaseDirectory) Release(ctx context.Context, id string, fence uint64) error {
	d.calls++
	if d.failure != nil && !d.commit {
		return d.failure
	}
	if err := d.Client.Release(ctx, id, fence); err != nil {
		return err
	}
	return d.failure
}

func TestIdleRelease_R8_RefusedReleaseKeepsHoldingSameFence(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"journal": "saved"})
	b := r.batcher()
	cause := &url.Error{Op: "Post", URL: "http://fixture.invalid/release", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
	dir := &r8ReleaseDirectory{Client: r.dirA, failure: fmt.Errorf("%w: %w", directory.ErrUnreachable, cause)}
	b.Publisher.Dir = dir
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	// An idle release must preserve the dial error rather than hide it behind
	// close's retry deadline, and must not sleep inside this ownership boundary.
	b.Sleep = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	released, err := b.ReleaseIdle(ctx, head, func() error { return nil })
	if released || !errors.Is(err, syscall.ECONNREFUSED) || dir.calls != 1 {
		t.Fatalf("refused release=%v error=%v requests=%d", released, err, dir.calls)
	}
	if _, _, holding := b.RenewalStatus(); !holding || b.Fence() != 1 {
		t.Fatalf("refused release retired fence %d, holding=%v", b.Fence(), holding)
	}
	dir.failure = nil
	r.clock.Advance(20 * time.Second)
	b.beat(t.Context())
	if got := r.head(cellID); r.dirA.beats.Load() != 1 || got.Lease.Fence != 1 || got.Lease.Expires != r.clock.Now().Add(directory.LeaseTTL).UnixMilli() {
		t.Fatalf("next renewal did not renew fence 1: %+v beats=%d", got.Lease, r.dirA.beats.Load())
	}
}

func TestIdleRelease_R8_UncertainReleaseNeverRenewsOrResumesOldFence(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			r := newRig(t)
			head := r.publishFirst(map[string]string{"journal": "saved"})
			b := r.batcher()
			dir := &r8ReleaseDirectory{Client: r.dirA, failure: context.DeadlineExceeded, commit: committed}
			b.Publisher.Dir = dir
			if released, err := b.ReleaseIdle(t.Context(), head, func() error { return nil }); released || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("uncertain release=%v error=%v", released, err)
			}
			// Even an outside renewal at the same fence cannot undo uncertainty.
			_, err := r.dir.For(devA).Heartbeat(t.Context(), cellID, directory.Beat{Fence: 1})
			if err != nil {
				t.Fatal(err)
			}
			seen, err := dir.Cell(t.Context(), cellID)
			if err != nil {
				t.Fatal(err)
			}
			if b.ResumeHeld(t.Context(), seen) {
				t.Fatal("ResumeHeld revived an uncertain release")
			}
			b.beat(t.Context())
			if _, _, holding := b.RenewalStatus(); holding || r.dirA.beats.Load() != 0 {
				t.Fatal("uncertain release renewed its old fence")
			}
			dir.failure = &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
			_, _ = b.ReleaseIdle(t.Context(), head, func() error { return nil })
			b.beat(t.Context())
			if _, _, holding := b.RenewalStatus(); holding || r.dirA.beats.Load() != 0 {
				t.Fatal("a refused retry revived the earlier uncertain release")
			}
			dir.failure = nil
			ok, err := b.Reacquire(t.Context(), seen)
			if err != nil || !ok || b.Fence() != 2 {
				t.Fatalf("uncertain release did not acquire fence 2: ok=%v error=%v fence=%d", ok, err, b.Fence())
			}
		})
	}
}
