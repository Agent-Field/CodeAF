package syncsetup

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// Contract F1 forbids a late publication callback from reviving a viewer's receipt.
func TestDisplacedWriterCannotRecordAnotherSendHold(t *testing.T) {
	s := &Sync{Home: t.TempDir(), Ledger: "fixture"}
	d := &Drive{sync: s, cell: cell.Cell{ID: "chat"}, directoryID: "chat", line: "continued elsewhere", batcher: &cellsync.Batcher{Driving: &cellsync.Driving{Fence: 7}}}
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
