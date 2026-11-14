package cellsync

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

type lostIdleAcquire struct {
	directory.Client
	winner directory.Client
	later  bool
}

func (c lostIdleAcquire) Acquire(ctx context.Context, id string, opts directory.AcquireOpts) (directory.CellView, error) {
	if c.later {
		v, err := c.Client.Acquire(ctx, id, opts)
		if err != nil {
			return v, err
		}
		opts.Expect = &directory.LeaseExpectation{Device: v.Cell.Lease.Device, Fence: v.Cell.Lease.Fence}
	}
	if _, err := c.winner.Acquire(ctx, id, opts); err != nil {
		return directory.CellView{}, err
	}
	<-ctx.Done()
	return directory.CellView{}, ctx.Err()
}

func TestIdleRelease_R10_UncertainAcquireNeverAdoptsAnotherWindow(t *testing.T) {
	for _, scenario := range []string{"same device", "other device", "same device later fence"} {
		t.Run(scenario, func(t *testing.T) {
			r := newRig(t)
			r.publishFirst(map[string]string{"file": "saved"})
			b := r.batcher()
			released, err := b.ReleaseIdle(t.Context(), r.drv.Head, func() error { return nil })
			if err != nil || !released {
				t.Fatalf("release=%v error=%v", released, err)
			}
			seen := r.head(cellID)
			winner := r.dir.For(devA)
			if scenario == "other device" {
				winner = r.dir.For(devB)
			}
			b.Publisher.Dir = lostIdleAcquire{Client: r.dir.For(devA), winner: winner, later: scenario == "same device later fence"}
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			ok, err := b.Reacquire(ctx, directory.CellView{Now: seen.Lease.ReleasedAt, Cell: seen})
			if err == nil || ok {
				t.Fatalf("unknown acquire=%v error=%v", ok, err)
			}
			view, err := winner.Cell(t.Context(), cellID)
			if err != nil {
				t.Fatal(err)
			}
			ok, err = b.Reacquire(t.Context(), view)
			if err != nil || ok || b.Fence() == view.Cell.Lease.Fence {
				t.Fatalf("adopted another window: resumed=%v error=%v local=%d remote=%+v", ok, err, b.Fence(), view.Cell.Lease)
			}
			if _, _, holding := b.RenewalStatus(); holding {
				t.Fatal("uncertain writer claims renewal authority")
			}
			r.clock.Advance(directory.LeaseTTL + time.Millisecond)
			view, err = winner.Cell(t.Context(), cellID)
			if err != nil {
				t.Fatal(err)
			}
			b.Publisher.Dir = r.dir.For(devA)
			ok, err = b.Reacquire(t.Context(), view)
			if err != nil || !ok || b.Fence() != view.Cell.Lease.Fence+1 || b.AcquireUncertain() {
				t.Fatalf("lapsed retry stuck: resumed=%v error=%v local=%d remote=%+v", ok, err, b.Fence(), view.Cell.Lease)
			}
		})
	}
}
