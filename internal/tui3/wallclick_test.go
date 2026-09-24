package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

// A HAND CAN DO WHAT THE KEYS DO: pick two tiles with their boxes, make a team
// of them from the tray, keep the name the wall offered, and land in it.
func TestWallClickPicksTilesAndMakesATeam(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	if n := len(a.wallShown(a.now())); n < 2 {
		t.Fatalf("%d tiles", n)
	}

	// The box is revealed by the hover, then pressed.
	body := wallHitFor(t, a, wallHitTile, 1)
	a.wallMotion(body.x0+4, body.y0+2)
	_ = a.wallFrame(a.width, a.height)
	wallClick(t, a, wallHitFor(t, a, wallHitSelect, 1))
	if len(a.wall.marked) != 1 {
		t.Fatalf("the box marked %d tiles", len(a.wall.marked))
	}
	// Selection mode: a press on another tile's body picks it too.
	wallClick(t, a, wallHitFor(t, a, wallHitTile, 0))
	if len(a.wall.marked) != 2 {
		t.Fatalf("a press in selection mode marked %d tiles", len(a.wall.marked))
	}
	if !strings.Contains(wallPlainFrame(a.wallFrame(a.width, a.height)), "2 selected") {
		t.Fatal("no tray")
	}

	wallClick(t, a, wallHitFor(t, a, wallHitAction, int(wallActMakeTeam)))
	if !a.wall.naming || a.wall.name == "" || !a.wall.nameFresh {
		t.Fatalf("make team opened no card with a name in it: naming=%v name=%q", a.wall.naming, a.wall.name)
	}
	offered := a.wall.name
	// While the card is up nothing under it answers.
	if cmd := a.wallDo(wallHit{kind: wallHitAction, arg: int(wallActClear)}); cmd != nil || len(a.wall.marked) != 2 {
		t.Fatal("the card let a press through")
	}
	wallClick(t, a, wallHitFor(t, a, wallHitAction, int(wallActSave)))
	if a.wall.naming || a.wall.activeID != "" || len(a.wall.teams) == 0 || a.wall.teams[len(a.wall.teams)-1].Name != offered {
		t.Fatalf("create did not make %q and stay in the view: %+v active=%q", offered, a.wall.teams, a.wall.activeID)
	}
	if len(a.wall.marked) != 0 || a.wall.made != offered {
		t.Fatalf("after create: marked=%v made=%q", a.wall.marked, a.wall.made)
	}
	if !strings.Contains(ansi.Strip(a.wallFrame(a.width, a.height)[a.wall.headRows+1]), "Made "+offered) {
		t.Fatal("the chips row does not say the team was made")
	}

	// The chip for all widens the wall again, and the typed name replaces the
	// offered one.
	wallClick(t, a, wallHitForTeam(t, a, wallHitChip, ""))
	if a.wall.activeID != "" {
		t.Fatalf("the all chip left team %q active", a.wall.activeID)
	}
	a.wallKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	a.wallKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if a.wall.name != "q" {
		t.Fatalf("the first key did not replace the offered name: %q", a.wall.name)
	}
	a.wallKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	// esc with tiles picked clears the pick before it closes anything.
	a.wallKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !a.wall.on || len(a.wall.marked) != 0 {
		t.Fatalf("esc: on=%v marked=%v", a.wall.on, a.wall.marked)
	}
}

// A CONVERSATION'S TEAMS ARE A CLICK AWAY: its ●+ opens the popover, a box
// puts it in a team and takes it out again, and a team's dot opens its
// settings, where it is renamed, recoloured and deleted, the last only once
// the question is answered.
func TestWallClickTeamsPopoverAndSettings(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	harbor, err := a.teamMake("harbor", []chatTab{tiles[0].tab})
	if err != nil {
		t.Fatal(err)
	}
	orbit, err := a.teamMake("orbit", []chatTab{tiles[0].tab})
	if err != nil {
		t.Fatal(err)
	}
	_ = a.wallFrame(a.width, a.height)
	key := tiles[1].tab.key

	// Hover the tile so its controls are drawn, then press its ●+.
	body := wallHitFor(t, a, wallHitTile, 1)
	a.wallMotion(body.x0+4, body.y0+3)
	_ = a.wallFrame(a.width, a.height)
	wallClick(t, a, wallHitFor(t, a, wallHitTeams, 1))
	if a.wall.pop.kind != wallPopMembers || len(a.wall.pop.targets) != 1 || a.wall.pop.targets[0] != key {
		t.Fatalf("the popover: %+v", a.wall.pop)
	}
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	if !strings.Contains(frame, "☐ ● harbor") || !strings.Contains(frame, "+ New team") {
		t.Fatalf("the popover is not drawn:\n%s", frame)
	}
	wallClick(t, a, wallHitForTeam(t, a, wallHitPopRow, orbit))
	if got := a.teamsOf(key); len(got) != 1 || got[0] != orbit {
		t.Fatalf("after one box the conversation is in %v", got)
	}
	a.wallKey(tea.KeyPressMsg{Code: tea.KeyUp})
	a.wallKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := a.teamsOf(key); len(got) != 2 {
		t.Fatalf("after the keyboard's box the conversation is in %v", got)
	}
	a.wallKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.wall.pop.kind != wallPopNone || !a.wall.on {
		t.Fatal("esc did not put the popover away, or took the whole view with it")
	}

	// The dot on a segment opens that team's settings.
	_ = a.wallFrame(a.width, a.height)
	wallClick(t, a, wallHitForTeam(t, a, wallHitChipMenu, harbor))
	// The team's card (teamsheet.go), over the wall.
	if !a.tsheet.on || a.tsheet.team != harbor || a.tsheet.name.String() != "harbor" {
		t.Fatalf("settings: %+v", a.tsheet)
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
	wallClick(t, a, wallHitForTeam(t, a, wallHitChipMenu, harbor))
	a.teamSheetDo(tsCloseTeam)
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
	if _, took := a.tabPress(a.wall.chip.from+1, placeTabRow); !took || !a.teamMenu.on || a.wall.on {
		t.Fatal("the chip did not open the team switcher")
	}
	t.Logf("%q", row)
}
