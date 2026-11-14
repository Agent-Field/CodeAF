package chatlist

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestIdleOpen_C6_C7_C10_C11_OnlyPairedFreeRowsAutoOpen(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		peers   int
		expires int64
		auto    bool
	}{
		{"paired released", 2, 0, true},
		{"paired lapsed", 2, 9, false},
		{"paired live old build", 2, 11, false},
		{"single released", 1, 0, false},
		{"single live", 1, 11, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			listing := directory.Listing{Now: 10, Devices: map[string]directory.Device{"A": {}}, Cells: map[string]directory.Cell{
				"chat": {Head: "saved", Lease: directory.Lease{Device: "A", Fence: 1, Expires: scenario.expires}},
			}}
			if scenario.peers == 2 {
				listing.Devices["B"] = directory.Device{}
			}
			row := Rows(listing, "B", nil)[0]
			if row.AutoOpen != scenario.auto || row.Quiet().AutoOpen != scenario.auto {
				t.Fatalf("auto open=%v, want %v; offline decoration changed authority=%v", row.AutoOpen, scenario.auto, row.Quiet().AutoOpen)
			}
		})
	}
}
