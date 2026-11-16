package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// A chat list row carries its crew: from this computer's teams store for a
// chat it knows, from the holder's sealed record for one held elsewhere.
func TestChatRowsCarryTheirCrew(t *testing.T) {
	root := filepath.Join(t.TempDir(), "manager")
	transcript := session.Place{Dir: root}.Transcript()
	crews := []teamstore.Team{
		{ID: "local1", Name: "Here crew", Members: []teamstore.Member{{Key: transcript, File: transcript}}, Manager: transcript},
		{ID: "fact1", Name: "Item 7", FactoryManaged: true, Members: []teamstore.Member{{Key: teamstore.RemoteKey("moved")}}},
	}
	remote := chatlist.Row{Crew: chatlist.Crew{V: 1, ID: "da48d479ea82", Name: "Todo crew", Manager: true}}

	cases := []struct {
		name string
		row  appwire.ChatRow
		fact chatlist.Row
		root string
		want appwire.ChatRow
	}{
		{"a chat here, from the store", appwire.ChatRow{Cell: "m", Here: true}, chatlist.Row{}, root,
			appwire.ChatRow{Cell: "m", Here: true, Crew: "local1", CrewName: "Here crew", CrewManager: true}},
		{"a chat held elsewhere, from its record", appwire.ChatRow{Cell: "r"}, remote, "",
			appwire.ChatRow{Cell: "r", Crew: "da48d479ea82", CrewName: "Todo crew", CrewManager: true}},
		{"the store wins over an older record", appwire.ChatRow{Cell: "moved"}, remote, "",
			appwire.ChatRow{Cell: "moved", Crew: "fact1", CrewName: "Item 7", CrewKind: "factory"}},
		{"a chat here the store does not list is in none", appwire.ChatRow{Cell: "h", Here: true}, remote, filepath.Join(t.TempDir(), "h"),
			appwire.ChatRow{Cell: "h", Here: true}},
		{"an older holder sends a loose row", appwire.ChatRow{Cell: "o"}, chatlist.Row{}, "",
			appwire.ChatRow{Cell: "o"}},
	}
	for _, c := range cases {
		row := c.row
		appChatCrew(&row, c.fact, crews, c.root)
		if row.Crew != c.want.Crew || row.CrewName != c.want.CrewName || row.CrewManager != c.want.CrewManager || row.CrewKind != c.want.CrewKind {
			t.Errorf("%s: row = %+v, want crew %q %q manager=%v kind=%q", c.name, row, c.want.Crew, c.want.CrewName, c.want.CrewManager, c.want.CrewKind)
		}
	}
}

func TestHelloSaysChatRowsCarryCrews(t *testing.T) {
	var b appBackend
	if !b.ChatRowsCarryCrews() {
		t.Fatal("the app engine's rows do not carry crews")
	}
	var _ appwire.ChatsCrewer = &b
}
