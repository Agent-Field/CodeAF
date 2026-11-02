package syncsetup

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestRelayOutageO1O2OwnExpiredLeaseWithUnpublishedHead(t *testing.T) {
	r := newDriveRig(t)
	r.spokenFixture()
	first, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	created, err := r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{Head: first.Turn.ID})
	mustNil(t, err)
	mustNil(t, cellstore.NotePublished(r.cell, first.Turn.ID))
	// The relay missed a locally saved turn and all renewal traffic while asleep.
	appendTo(t, transcriptOf(r.cell), "{\"type\":\"message\",\"role\":\"user\",\"content\":\"during outage\"}\n")
	latest, err := r.engine.Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	r.clock.Advance(directory.LeaseTTL + time.Second)
	var notices noticeLog
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{DeviceName: "this computer", OnNotice: notices.add})
	mustNil(t, err)
	defer drive.Close(t.Context())
	if err := drive.Gate(); err != nil {
		t.Fatalf("O1/O2 own expired lease refused local work: %v; notices %v", err, notices.all())
	}
	waitFor(t, "O2 unpublished descendant to reach the relay", func() bool { return r.directoryHead(r.cell.ID) == latest.Turn.ID })
	v, err := r.a.Dir.Cell(t.Context(), r.cell.ID)
	mustNil(t, err)
	if v.Cell.Lease.Device != r.a.Device.ID() || v.Cell.Lease.Fence != created.Cell.Lease.Fence || v.Cell.Lease.Expires <= v.Now {
		t.Fatalf("O2 recovery did not renew the unchanged own hold: %+v", v)
	}
}
