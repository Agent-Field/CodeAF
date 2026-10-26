package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestHomeOmitsRemoteChatsUntilTheirFirstSavedTurn(t *testing.T) {
	listing := directory.Listing{Now: 1000, Devices: map[string]directory.Device{"other": {Name: "tower"}}, Cells: map[string]directory.Cell{
		"empty": {Lease: directory.Lease{Device: "other", Expires: 2000}},
		"saved": {Head: "saved-head", Title: "Saved chat", DurableAt: 900, Lease: directory.Lease{Device: "other", Expires: 2000}},
	}}
	rows := func() chatlist.Static {
		return chatlist.Rows(listing, "here", func(s string) (string, error) { return s, nil })
	}
	a := machinesHome(t, rows(), 200)
	if strings.Contains(homeText(a), "untitled") {
		t.Fatalf("empty remote chat appears on Home:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "Saved chat") {
		t.Fatal("saved remote chat was hidden")
	}
	saved := listing.Cells["empty"]
	saved.Head, saved.Title, saved.DurableAt = "first-head", "First saved turn", 950
	listing.Cells["empty"] = saved
	a.machines = rows()
	drain(t, a, a.askMachines())
	if !strings.Contains(homeText(a), "First saved turn") {
		t.Fatal("first saved turn did not reveal the chat")
	}
}
