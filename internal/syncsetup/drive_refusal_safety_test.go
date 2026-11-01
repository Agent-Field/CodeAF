package syncsetup

import (
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestAStaleDriveRefusalImmediatelyMakesTheWindowAViewer(t *testing.T) {
	// Refusal retires a saved send hold, so even this minimal drive needs an isolated home.
	var notices []string
	d := &Drive{
		sync: &Sync{Home: t.TempDir()},
		opt:  DriveOptions{OnNotice: func(line string) { notices = append(notices, line) }},
	}
	d.refusal(directory.ErrFenceStale)
	d.refusal(directory.ErrFenceStale)
	if d.Gate() == nil {
		t.Fatal("a refused drive still allows new work")
	}
	want := chatlist.Superseded(unknownDevice)
	if line, viewer := d.Viewer(); !viewer || line != want {
		t.Fatalf("viewer = %q, %v; want %q, true", line, viewer, want)
	}
	if !slices.Equal(notices, []string{want}) {
		t.Fatalf("notices = %v, want [%q] once", notices, want)
	}
}

func TestStalePublishRefusalMakesViewerAndNotifiesOnce(t *testing.T) {
	h := newTwoHomes(t)
	ctx := t.Context()
	a := h.openA()
	a.mustSay("durable")
	h.durable(h.cell.ID, h.cell)
	h.block.set(true)
	a.mustSay("orphaned")
	h.lapse()
	mustNil(t, h.b.putDevice(ctx, nameB))
	if _, err := h.continuerB().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	h.block.set(false)
	want := chatlist.Superseded(nameB)
	waitFor(t, "A to become a viewer after stale publish", func() bool { _, viewer := a.drive.Viewer(); return viewer })
	if line, viewer := a.drive.Viewer(); !viewer || line != want {
		t.Fatalf("viewer = %q, %v; want %q, true", line, viewer, want)
	}
	kept := chatlist.KeptEdits(1, nameA)
	if got := h.wall.all(); !slices.Equal(got, []string{kept, want}) {
		t.Fatalf("notices = %v, want [%q %q]", got, kept, want)
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

// TestReleasedHeadIsContinuedWithThisComputersNewerTurns pins #2072: turns
// this computer sealed on top of a released head are its own work, so the
// drive takes the free lease and publishes them instead of opening the chat
// read-only. It replaces a law that locked the only computer out of its chat.
func TestReleasedHeadIsContinuedWithThisComputersNewerTurns(t *testing.T) {
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
	if err := drive.Gate(); err != nil {
		t.Errorf("this computer's newer turns opened read-only: %v", err)
	}
}
