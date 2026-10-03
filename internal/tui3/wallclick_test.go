package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// wallHitFor is the first target of kind and arg on the wall as it was last
// drawn. The arg is matched exactly: a negative one is a real target (the All
// chip is -1, the popover's delete rows are below it), never a wildcard.
func wallHitFor(t *testing.T, a *app, kind wallHitKind, arg int) wallHit {
	t.Helper()
	for _, hit := range a.wall.hits {
		if hit.kind == kind && hit.arg == arg {
			return hit
		}
	}
	t.Fatalf("no target %d/%d on the wall:\n%s", kind, arg, wallPlainFrame(a.wallFrame(a.width, a.height)))
	return wallHit{}
}

// wallHitForTeam is the target of kind on team id ("" for All) on the last
// frame.
func wallHitForTeam(t *testing.T, a *app, kind wallHitKind, id string) wallHit {
	t.Helper()
	for _, hit := range a.wall.hits {
		if hit.kind == kind && hit.id == id {
			return hit
		}
	}
	t.Fatalf("no target %d/%q on the wall:\n%s", kind, id, wallPlainFrame(a.wallFrame(a.width, a.height)))
	return wallHit{}
}

// wallClick moves the pointer onto a target, repaints, and presses it, the
// order a hand does it in.
func wallClick(t *testing.T, a *app, hit wallHit) tea.Cmd {
	t.Helper()
	a.wallMotion(hit.x0, hit.y0)
	_ = a.wallFrame(a.width, a.height)
	cmd, took := a.wallPress(hit.x0, hit.y0)
	if !took {
		t.Fatalf("the wall did not take a press on %+v", hit)
	}
	_ = a.wallFrame(a.width, a.height)
	return cmd
}

// Selection and bulk dismissal use the same view-only door as the keys.
func TestWallClickPicksTilesAndClosesViews(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	body := wallHitFor(t, a, wallHitTile, 1)
	a.wallMotion(body.x0+4, body.y0+2)
	_ = a.wallFrame(a.width, a.height)
	wallClick(t, a, wallHitFor(t, a, wallHitSelect, 1))
	wallClick(t, a, wallHitFor(t, a, wallHitTile, 0))
	if len(a.wall.marked) != 2 {
		t.Fatalf("selected %d", len(a.wall.marked))
	}
	wallClick(t, a, wallHitFor(t, a, wallHitAction, int(wallActCloseViews)))
	if len(a.wallShown(a.now())) != len(tiles) || len(a.wall.marked) != 0 {
		t.Fatal("Close views discarded conversations or left selection")
	}
	for _, tile := range tiles[:2] {
		if a.behind[tile.tab.key] == nil {
			t.Fatal("closing a view discarded its conversation")
		}
	}
}

// A WALL TILE LEAVES THE PLACE IT WAS OPENED FROM. The teams page can raise
// the wall just as chats can, and choosing a conversation must put that
// conversation in front rather than leaving the page over the switch.
func TestWallTileFromTeamsPageLandsOnConversation(t *testing.T) {
	a, _, _ := tabApp(t)
	runCmd(a.showPage(pageTeams))
	tiles := a.wallShown(a.now())
	if len(tiles) < 2 {
		t.Fatalf("the teams lab has %d wall tiles, want a tile behind the front", len(tiles))
	}
	want := tiles[1].tab.key
	runCmd(a.wallOpen(tiles, 1))
	if a.at(pageTeams) {
		t.Fatal("opening a wall tile left the teams page in front")
	}
	if a.wall.on {
		t.Fatal("opening a wall tile left the wall open")
	}
	if got := a.frontTabKey(); got != want {
		t.Fatalf("opening a wall tile put %q in front, want %q", got, want)
	}
}

// Membership context stays in the grid while settings remain on Teams.
func TestWallKeepsTeamControlsOnTeamsPage(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	tiles := a.wallShown(a.now())
	harbor, err := a.teamMake("harbor", []chatTab{tiles[0].tab})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.teamMake("orbit", []chatTab{tiles[0].tab}); err != nil {
		t.Fatal(err)
	}
	a.wallMotion(10, 10)
	_ = a.wallFrame(a.width, a.height)
	for _, hit := range a.wall.hits {
		switch hit.kind {
		case wallHitTeams, wallHitChip, wallHitChipMenu, wallHitAddTeam:
			t.Fatalf("grid offers team control %+v", hit)
		}
	}
	a.closeWall()
	runCmd(a.showPage(pageTeams))
	runCmd(a.teamsSelect(harbor))
	target := teamsTargetOf(t, a, teamsActSettings, harbor)
	drive(t, a, tea.MouseClickMsg{X: target.x0, Y: target.y, Button: tea.MouseLeft})
	if !a.tsheet.on || a.tsheet.team != harbor {
		t.Fatal("Teams settings did not open")
	}

	before := a.wall.teams[0].HueSpec()
	a.teamSheetDo(tsSwatch + 2)
	if a.wall.teams[0].HueSpec() == before {
		t.Fatal("a swatch did not recolour the team")
	}
	a.tsheet.cursor = tsName
	for range "harbor" {
		a.teamSheetKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	for _, r := range "dock" {
		a.teamSheetKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	a.teamSheetKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.wall.teams[0].Name != "dock" || a.tsheet.on {
		t.Fatalf("rename: %q, card %+v", a.wall.teams[0].Name, a.tsheet)
	}
	_ = a.wallFrame(a.width, a.height)
	// The card's `Close team…` closes it (ruling c-9: a team is deleted only
	// once closed, from the teams page), and the wall's Teams row drops it.
	runCmd(a.teamsDo(teamsTarget{act: teamsActSettings, id: harbor}))
	a.teamSheetDo(tsCloseTeam)
	a.teamSheetDo(tsCloseNow)
	if got, ok := a.teamByID(harbor); !ok || !got.Closed() {
		t.Fatalf("Close team did not close it: %+v", a.teamNames())
	}
	if shown := a.wallTeams(); len(shown) != 1 || shown[0].Name != "orbit" {
		t.Fatalf("after the close the wall lists %d teams", len(shown))
	}
	if len(a.wallShown(a.now())) != len(tiles) {
		t.Fatal("closing a team closed a conversation on the wall")
	}
}

// WHILE A TEAM NARROWS THE STRIP, THE STRIP SAYS WHICH: a chip at its left
// end, which opens the team switcher (teammenu.go).
func TestTabTeamChipNamesTheShownTeam(t *testing.T) {
	a, _, _ := tabApp(t)
	plainRow := plain(a.tabsRow(a.width))
	if strings.Contains(plainRow, "▾") {
		t.Fatalf("a chip with no team shown: %q", plainRow)
	}
	tabs := a.tabList()
	i, err := a.teamMake("harbor", tabs[:1])
	if err != nil {
		t.Fatal(err)
	}
	a.wall.activeID = i
	a.touch()
	row := plain(a.tabsRow(a.width))
	if !strings.Contains(row, "● harbor ▾") || !a.wall.chip.pressable() {
		t.Fatalf("no chip: %q %+v", row, a.wall.chip)
	}
	for _, hit := range a.chatTabHits {
		if hit.span.from < a.wall.chip.to {
			t.Fatalf("a tab was drawn under the chip: %+v", hit)
		}
	}
	if _, took := a.tabPress(a.wall.chip.from+1, tabStripRow); !took || !a.teamMenu.on || a.wall.on {
		t.Fatal("the chip did not open the team switcher")
	}
	t.Logf("%q", row)
}
