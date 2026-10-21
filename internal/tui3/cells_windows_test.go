package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/atlas"
)

// TestC11WindowsMenuHasNoMoveRows proves C11 at the actual completion registry
// while keeping typed aliases able to reach the Windows explanation.
func TestC11WindowsMenuHasNoMoveRows(t *testing.T) {
	m := menu{}
	m.rank("")
	for _, hit := range m.hits {
		switch commands[hit].name {
		case "pair", "devices", "atlas":
			t.Fatalf("Windows offered /%s", commands[hit].name)
		}
	}
	if len(atlas.Maps) != 0 {
		t.Fatal("Windows offered a pairing atlas")
	}
	if canonicalCommand("sync") != "pair" {
		t.Fatal("a typed alias lost its explanation")
	}
}
