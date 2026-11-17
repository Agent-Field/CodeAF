package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdleRelease_R8_LateReleasedObservationCannotRefuseRetakenWork(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	previous := c.drive.sendLeaseSnapshot()
	a, _ := idleContractSession(t, h, c)
	d := c.drive
	before := idleContractReleased(t, h, d)
	held := d.sendLeaseSnapshot()
	if held.Fence != 0 {
		t.Fatal("idle release retained its send receipt")
	}
	done := make(chan error, 1)
	func() {
		d.receiptMu.Lock()
		defer d.receiptMu.Unlock()
		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			confirmed, err := d.ConfirmWork(ctx)
			if err == nil && !confirmed {
				err = context.Canceled
			}
			done <- err
		}()
		waitFor(t, "reacquisition before its send receipt", func() bool {
			return d.batcher.Fence() > before.Cell.Lease.Fence && d.Gate() == nil
		})
		// Deliver the old free reading after the new acquisition has cleared
		// the viewer, but before it can write its receipt. No ownership or
		// readiness result is substituted.
		d.availableAfter(held, before.Cell.Lease)
	}()
	mustNil(t, <-done)
	if err := d.Gate(); err != nil {
		t.Fatalf("retake acquired fence %d but still refuses work: %v", d.batcher.Fence(), err)
	}
	if d.IdleReleased() {
		t.Fatal("retaken window still reports released")
	}
	_, fence, err := h.a.SendLease(h.cell.ID)
	mustNil(t, err)
	if fence != d.batcher.Fence() {
		t.Fatalf("retake receipt=%d; acquired=%d", fence, d.batcher.Fence())
	}
	// A second observation after receipt publication must be harmless too.
	d.availableAfter(held, before.Cell.Lease)
	d.availableAfter(previous, before.Cell.Lease)
	if err := d.Gate(); err != nil {
		t.Fatal(err)
	}
	events, err := a.SubmitBash(t.Context(), "! printf ran > r8-f1-ran.txt")
	mustNil(t, err)
	for range events {
	}
	raw, err := os.ReadFile(filepath.Join(h.work, "r8-f1-ran.txt"))
	mustNil(t, err)
	if string(raw) != "ran" {
		t.Fatalf("retaken ! send did not run: %q", raw)
	}
}
