package syncsetup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestAReturnAfterTheWriterReopensIsNotALostRace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	a := h.openA()
	a.mustSay("before moving")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(ctx))
	moved, err := h.continuerB().Take(ctx, h.cell.ID)
	mustNil(t, err)
	stale := &directory.LeaseExpectation{Device: h.b.Device.ID(), Fence: moved.Taken.Driving.Fence}
	h.openOn(h.b, h.engB, moved.Taken.Cell, workspaceOf(moved.Taken.Cell.Root), nameB)
	current, err := h.a.Dir.Cell(ctx, h.cell.ID)
	mustNil(t, err)
	if current.Cell.Lease.Device != stale.Device || current.Cell.Lease.Fence <= stale.Fence {
		t.Fatalf("writer did not reopen: %+v", current.Cell.Lease)
	}
	_, err = h.continuerA().TakeSeen(ctx, h.cell.ID, stale)
	t.Logf("card holder=%s ownership=%d; reopened holder=%s ownership=%d; stale confirmation=%v", stale.Device, stale.Fence, current.Cell.Lease.Device, current.Cell.Lease.Fence, err)
	if !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("stale consent unexpectedly succeeded: %v", err)
	}
	// The same holder remains; the surface must ask again for this new ownership.
	fresh, err := h.a.Dir.Cell(ctx, h.cell.ID)
	mustNil(t, err)
	if fresh.Cell.Lease != current.Cell.Lease {
		t.Fatal("stale consent displaced the writer")
	}
	consent := &directory.LeaseExpectation{Device: fresh.Cell.Lease.Device, Fence: fresh.Cell.Lease.Fence}
	got, err := h.continuerA().TakeSeen(ctx, h.cell.ID, consent)
	mustNil(t, err)
	if got.Taken.Driving.Fence <= consent.Fence {
		t.Fatal("fresh consent did not continue")
	}
}
