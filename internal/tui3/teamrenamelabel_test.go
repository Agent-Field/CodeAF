package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// A RENAME REACHES THE LABELS THAT SAY THE NAME. The rail caches the rows it
// drew, and the composer's `to ◆ name manager` is read when the box is drawn.
// Both follow the name the teams hold now, with no read of the disk.
func TestARenamedTeamUpdatesTheRunLabelAndTheComposer(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	parent, ok := a.teamByID(harbor)
	if !ok {
		t.Fatal("no harbor")
	}
	start := teamstore.Entry{
		ID: "000000000008", Kind: teamstore.KindStart, From: teamstore.FromManager,
		To: "api", Team: orbit, Text: "run it",
	}
	if a.traffic.rows == nil {
		a.traffic.rows = map[string][]teamstore.Entry{}
	}
	a.traffic.rows[harbor] = []teamstore.Entry{start}
	draw := func() string {
		rows, _, _, _ := a.trafficBody(parent, 8, 70)
		return ansi.Strip(strings.Join(rows, "\n"))
	}
	if got := draw(); !strings.Contains(got, "to run orbit") {
		t.Fatalf("the start does not name the team it made:\n%s", got)
	}
	if err := a.teamRename(orbit, "backend"); err != nil {
		t.Fatal(err)
	}
	got := draw()
	if strings.Contains(got, "to run orbit") || !strings.Contains(got, "to run backend") {
		t.Fatalf("after the rename the rail still says the old name:\n%s", got)
	}

	a.tp.sel = harbor
	a.tp.host = a.frontTabKey()
	before := a.teamsComposerWord()
	if !strings.Contains(before, "harbor") {
		t.Fatalf("the composer does not name the team: %q", before)
	}
	if err := a.teamRename(harbor, "dock"); err != nil {
		t.Fatal(err)
	}
	after := a.teamsComposerWord()
	if strings.Contains(after, "harbor") || !strings.Contains(after, "dock") {
		t.Fatalf("after the rename the composer says %q", after)
	}
	a.input.insert("hi")
	if pieces := a.seamPieces(120); !strings.Contains(pieces.name, "dock") {
		t.Fatalf("the seam kept the old team: %q", pieces.name)
	}
}
