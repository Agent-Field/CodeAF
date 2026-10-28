package syncsetup

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestAStaleDriveRefusalImmediatelyMakesTheWindowAViewer(t *testing.T) {
	d := &Drive{sync: &Sync{Home: t.TempDir()}}
	d.refusal(directory.ErrFenceStale)
	if d.Gate() == nil {
		t.Fatal("a refused drive still allows new work")
	}
}

func TestTheSendProofNamesTheDrivesActualFence(t *testing.T) {
	r := newDriveRig(t)
	sealed, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer drive.Close(t.Context())
	id, fence, err := r.a.SendLease(r.cell.ID)
	mustNil(t, err)
	if id != r.cell.ID || fence != drive.batcher.Fence() {
		t.Fatalf("send proof = %s/%d, drive = %d", id, fence, drive.batcher.Fence())
	}
}

func TestFenceF1ReleasedHoldCannotAuthorizeTools(t *testing.T) {
	r := newDriveRig(t)
	sealed, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer drive.Close(t.Context())
	r.clock.Advance(directory.LeaseTTL + time.Second)
	mustNil(t, r.a.Dir.Release(t.Context(), r.cell.ID, drive.batcher.Fence()))
	mustNil(t, drive.CheckLease(t.Context()))
	if drive.Gate() == nil {
		t.Fatal("released ownership still authorizes tools")
	}
}

func TestReleasedOlderHeadIsNotAcquiredOverLocalWork(t *testing.T) {
	r := newDriveRig(t)
	first, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	created, err := r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: first.Turn.ID})
	mustNil(t, err)
	mustNil(t, r.a.Dir.Release(t.Context(), r.cell.ID, created.Cell.Lease.Fence))
	_, err = r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer drive.Close(t.Context())
	if drive.Gate() == nil {
		t.Error("a released older head was silently acquired over local work")
	}
}
