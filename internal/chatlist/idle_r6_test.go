package chatlist

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestIdleOpen_R6_LapsedWhileWorkingKeepsTheCard(t *testing.T) {
	for _, working := range []bool{true, false} {
		listing := directory.Listing{Now: 1000, Devices: map[string]directory.Device{"A": {}, "B": {}}, Cells: map[string]directory.Cell{
			"chat": {Head: "saved", Lease: directory.Lease{Device: "A", Fence: 7, Expires: 500, Working: working}},
		}}
		row := Rows(listing, "B", nil)[0]
		if row.AutoOpen || row.Quiet().AutoOpen || OfferFor(row).Kind != ContinueHere {
			t.Fatalf("lapsed hold must keep consent (last working=%v): %+v", working, row)
		}
	}
}

func TestIdleOpen_R6_ReleasedStillOpensDirectly(t *testing.T) {
	listing := directory.Listing{Now: 1000, Devices: map[string]directory.Device{"A": {}, "B": {}}, Cells: map[string]directory.Cell{
		"chat": {Head: "saved", Lease: directory.Lease{Device: "A", Fence: 7, ReleasedAt: 500}},
	}}
	row := Rows(listing, "B", nil)[0]
	if !row.AutoOpen || !row.Released || OfferFor(row).Kind != Open {
		t.Fatalf("released chat lost direct opening: %+v", row)
	}
}
