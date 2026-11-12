package syncsetup

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// Contract F1 forbids a late publication callback from reviving a viewer's receipt.
func TestDisplacedWriterCannotRecordAnotherSendHold(t *testing.T) {
	s := &Sync{Home: t.TempDir(), Ledger: "fixture"}
	d := &Drive{sync: s, cell: cell.Cell{ID: "chat"}, directoryID: "chat", viewer: Viewer{Line: "continued elsewhere"}, batcher: &cellsync.Batcher{Driving: &cellsync.Driving{Fence: 7}}}
	mustNil(t, d.recordSendLease())
	if _, _, err := s.SendLease("chat"); !os.IsNotExist(err) {
		t.Fatalf("viewer recreated a send hold: %v", err)
	}
}

// Contract F2 retires stale receipts while retaining the relay's matching live hold.
func TestRelayReadingReconcilesOnlyTheStaleSendHold(t *testing.T) {
	for _, kind := range []string{"older fence", "other holder", "expired", "current"} {
		t.Run(kind, func(t *testing.T) {
			s := &Sync{Home: t.TempDir(), Ledger: "fixture"}
			mustNil(t, s.recordSendLease("chat", sendLease{Cell: "chat", Fence: 3}))
			view := directory.CellView{Now: 100, Cell: directory.Cell{Lease: directory.Lease{Device: s.Device.ID(), Fence: 3, Expires: 200}}}
			switch kind {
			case "older fence":
				view.Cell.Lease.Fence = 4
			case "other holder":
				view.Cell.Lease.Device = "other"
			case "expired":
				view.Cell.Lease.Expires = 100
			}
			mustNil(t, s.reconcileSendLease("chat", "chat", view))
			_, fence, err := s.SendLease("chat")
			if kind == "current" {
				if err != nil || fence != 3 {
					t.Fatalf("current receipt lost: %d %v", fence, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("stale receipt survived: %d %v", fence, err)
			}
		})
	}
}

// App admission learns a move before the next publisher check. It must retire
// the send hold and wake cached sync readers through that door too.
func TestConfirmWorkRetiresSendHoldAfterAnotherComputerTakesChat(t *testing.T) {
	r := newDriveRig(t)
	ctx := context.Background()
	created, err := r.a.Dir.Create(ctx, r.cell.ID, directory.CellInit{Head: head})
	mustNil(t, err)
	fence := created.Cell.Lease.Fence
	mustNil(t, r.a.recordSendLease(r.cell.ID, sendLease{Cell: r.cell.ID, Fence: fence}))
	r.clock.advance(directory.LeaseTTL)
	_, err = r.b.Dir.Acquire(ctx, r.cell.ID, directory.AcquireOpts{})
	mustNil(t, err)
	renewed := make(chan struct{}, 1)
	d := &Drive{sync: r.a, cell: r.cell, directoryID: r.cell.ID, recordedFence: fence,
		batcher: &cellsync.Batcher{Driving: &cellsync.Driving{Cell: r.cell, Fence: fence}},
		opt:     DriveOptions{OnRenewalChange: func() { renewed <- struct{}{} }}}
	confirmed, err := d.ConfirmWork(ctx)
	if err != nil || confirmed {
		t.Fatalf("displaced admission = %v, %v", confirmed, err)
	}
	var refusal *executor.WorkRefusal
	if !errors.As(d.Gate(), &refusal) || refusal.HolderID != r.b.Device.ID() {
		t.Fatalf("admission lost the new holder: %v", d.Gate())
	}
	select {
	case <-renewed:
	default:
		t.Error("admission did not wake sync readers after displacement")
	}
	until := time.After(5 * time.Second)
	for {
		_, _, err := r.a.SendLease(r.cell.ID)
		if os.IsNotExist(err) {
			break
		}
		select {
		case <-until:
			t.Fatalf("displaced send hold survived admission: %v", err)
		case <-time.After(time.Millisecond):
		}
	}
}
