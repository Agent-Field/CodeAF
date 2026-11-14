package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestIdleOpen_R6_LapsedWhileWorkingKeepsTheCard(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	listing := directory.Listing{Now: 1000, Devices: map[string]directory.Device{"A": {}, "B": {}}, Cells: map[string]directory.Cell{
		heldCell: {Head: "saved", Lease: directory.Lease{Device: "A", Fence: 7, Expires: 500, Working: true}},
	}}
	row := chatlist.Rows(listing, "B", nil)[0]
	taker := &idleOpenTaker{}
	a, _ := heldLab(t, &flipping{status: chatlist.Off}, taker)
	command := a.homeMachineEnter(homeLine{remote: &row})
	drain(t, a, command)
	if row.AutoOpen || taker.opens != 0 || len(taker.cells) != 0 || !strings.Contains(homeText(a), continueAsk) {
		t.Fatalf("lapsed working hold opened without consent: row=%+v free opens=%d takes=%v\n%s", row, taker.opens, taker.cells, homeText(a))
	}
}

func TestIdleOpen_R6_ReleasedStillOpensDirectly(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	listing := directory.Listing{Now: 1000, Devices: map[string]directory.Device{"A": {}, "B": {}}, Cells: map[string]directory.Cell{
		heldCell: {Head: "saved", Lease: directory.Lease{Device: "A", Fence: 7, ReleasedAt: 500}},
	}}
	row := chatlist.Rows(listing, "B", nil)[0]
	taker := &idleOpenTaker{}
	a, _ := heldLab(t, &flipping{status: chatlist.Idle}, taker)
	command := a.homeMachineEnter(homeLine{remote: &row})
	if !row.AutoOpen || command == nil || strings.Contains(homeText(a), continueAsk) {
		t.Fatal("released hold did not enter direct opening")
	}
	_ = command()
	if taker.opens != 1 || len(taker.cells) != 0 || taker.seen == nil || taker.seen.Device != "A" || taker.seen.Fence != 7 {
		t.Fatalf("released hold did not use conditional free opening: %+v", taker)
	}
}
