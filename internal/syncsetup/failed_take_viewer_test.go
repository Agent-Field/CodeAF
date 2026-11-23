package syncsetup

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestReleasedFailedTakeKeepsSourceClosedAndAvailable(t *testing.T) {
	d, memory := sameComputerDriveWithDirectory(t)
	other := memory.For("failed-taker")
	taken, err := other.Acquire(t.Context(), d.cell.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: d.sync.Device.ID(), Fence: d.batcher.Fence()}})
	mustNil(t, err)
	mustNil(t, d.CheckLease(t.Context()))
	if d.Gate() == nil {
		t.Fatal("source admitted work under the taker's hold")
	}
	mustNil(t, other.Release(t.Context(), d.cell.ID, taken.Cell.Lease.Fence))
	mustNil(t, d.CheckLease(t.Context()))
	if line, viewer := d.Viewer(); !viewer || line != chatlist.Available {
		t.Fatalf("released take viewer=%q,%v; want %q,true", line, viewer, chatlist.Available)
	}
	if d.Gate() == nil {
		t.Fatal("release restored stale work permission")
	}
	if _, _, err := d.sync.SendLease(d.cell.ID); err == nil {
		t.Fatal("release restored the source send receipt")
	}
}
