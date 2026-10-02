package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// menuApp is the strip's three conversations with two teams, harbor holding
// the conversation in front and orbit holding one behind it, and harbor shown.
func menuApp(t *testing.T) (a *app, harbor, orbit string) {
	t.Helper()
	a, _, _ = tabApp(t)
	tabs := a.tabList()
	front := a.frontTabKey()
	var here, behind []chatTab
	for _, tab := range tabs {
		if tab.key == front {
			here = append(here, tab)
		} else {
			behind = append(behind, tab)
		}
	}
	if len(here) != 1 || len(behind) < 2 {
		t.Fatalf("the fixture's strip: %+v", tabs)
	}
	var err error
	if harbor, err = a.teamMake("harbor", []chatTab{here[0], behind[0]}); err != nil {
		t.Fatal(err)
	}
	if orbit, err = a.teamMake("orbit", behind[1:2]); err != nil {
		t.Fatal(err)
	}
	a.teamActivate(harbor)
	a.touch()
	_ = a.tabsRow(a.width)
	return a, harbor, orbit
}

// menuFrame is the whole frame, plain, and its rows.
func menuFrame(t *testing.T, a *app) (string, []string) {
	t.Helper()
	frame, _, _ := a.frame()
	rows := strings.Split(frame, "\n")
	if len(rows) != a.height {
		t.Fatalf("the frame has %d rows, want %d", len(rows), a.height)
	}
	for i, r := range rows {
		if w := ansi.StringWidth(r); w > a.width {
			t.Fatalf("row %d is %d cells, wider than %d: %q", i, w, a.width, ansi.Strip(r))
		}
	}
	return ansi.Strip(frame), rows
}

// menuHit is the switcher's row with code (and id, for a team) on the last
// frame.
func menuHit(t *testing.T, a *app, code int, id string) wallHit {
	t.Helper()
	for _, hit := range a.teamMenu.hits {
		if hit.arg == code && hit.id == id {
			return hit
		}
	}
	t.Fatalf("no switcher row %d/%q in %+v", code, id, a.teamMenu.hits)
	return wallHit{}
}

// THE CHIP IS THE SWITCHER, ON THE CHAT AS ON THE WALL. A press opens a menu
// under it with None and every active team; its rows lie on their words, never overlap, and the frame keeps its
// size; the keyboard walks it and a choice narrows the strip without leaving
// the conversation in front when it is a member.
func TestTeamMenuIsTheStripsSwitcher(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	if _, took := a.tabPress(a.wall.chip.from+1, tabStripRow); !took || !a.teamMenu.on {
		t.Fatal("the chip did not open the switcher")
	}
	frame, rows := menuFrame(t, a)
	for _, want := range []string{"╭─ Teams ─", "◉ ● harbor", "○ ● orbit", "○   None"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the switcher lacks %q\n%s", want, frame)
		}
	}
	card := a.teamMenu.card
	if card.y0 != tabStripRow+1 || card.x0 != a.wall.chip.from {
		t.Fatalf("the switcher hangs at %+v, the chip is at %+v", card, a.wall.chip)
	}
	for i, h := range a.teamMenu.hits {
		label := strings.TrimSpace(ansi.Strip(ansi.Cut(rows[h.y0], h.x0, h.x1)))
		if label == "" || h.x0 < card.x0 || h.x1 > card.x1 {
			t.Fatalf("row %d's target is on %q at %+v, outside %+v", i, label, h, card)
		}
		for _, o := range a.teamMenu.hits[i+1:] {
			if h.y0 == o.y0 && h.x0 < o.x1 && o.x0 < h.x1 {
				t.Fatalf("two targets overlap: %+v %+v", h, o)
			}
		}
	}
	t.Logf("the switcher open over the chat, 160x40:\n%s", strings.Join(strings.Split(frame, "\n")[:12], "\n"))

	// The keyboard is on harbor; down is orbit, enter takes it. orbit does
	// not hold the conversation in front, so the switch is to its member.
	a.teamMenuKey(tea.KeyPressMsg{Code: tea.KeyDown})
	a.teamMenuKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.teamMenu.on || a.wall.activeID != orbit {
		t.Fatalf("enter on orbit: menu %v, active %q", a.teamMenu.on, a.wall.activeID)
	}
	if o, _ := a.teamByID(orbit); !teamHolds(o, a.frontTabKey()) {
		t.Fatal("orbit does not hold the conversation in front, and nothing switched to its member")
	}
	// Back to harbor by the pointer: harbor holds this one too, so nothing
	// switches.
	remembered := a.teamViews.views[harbor].key
	if err := a.teamAdd(harbor, []chatTab{a.teamMenuFront()}); err != nil {
		t.Fatal(err)
	}
	a.openTeamMenu()
	_, _ = menuFrame(t, a)
	hit := menuHit(t, a, wallPopTeam, harbor)
	if _ = a.teamMenuPress(hit.x0+2, hit.y0); a.wall.activeID != harbor || a.frontTabKey() != remembered {
		t.Fatalf("choosing harbor switched the conversation in front: active %q", a.wall.activeID)
	}
	// All widens the strip.
	a.openTeamMenu()
	_, _ = menuFrame(t, a)
	hit = menuHit(t, a, teamMenuNone, "")
	a.teamMenuPress(hit.x0+2, hit.y0)
	if a.wall.activeID != "" {
		t.Fatalf("All left %q shown", a.wall.activeID)
	}
}

// ADD FLIPS TO REMOVE. The row puts the conversation in front into the team
// that is shown and takes it out again, saved, with the menu still up.
func TestTeamMenuOffersOnlyOverlayChoices(t *testing.T) {
	a, _, _ := menuApp(t)
	a.openTeamMenu()
	frame, _ := menuFrame(t, a)
	for _, word := range []string{"Add this conversation", "Remove this conversation", "New team", "Team settings", "Make manager", "Closed"} {
		if strings.Contains(frame, word) {
			t.Fatalf("management action %q remains in overlay picker", word)
		}
	}
	if len(a.teamMenuRows()) != len(a.teamsOpenTree())+1 {
		t.Fatal("picker has extra rows")
	}
}

// A PRESS OFF THE MENU PUTS IT AWAY AND DOES NOTHING ELSE; esc does the same,
// and while it is up no key reaches the box under it.
func TestTeamMenuClosesOnAPressOffItAndOnEsc(t *testing.T) {
	a, harbor, _ := menuApp(t)
	a.openTeamMenu()
	_, _ = menuFrame(t, a)
	box := a.input.String()
	_, _ = a.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if a.input.String() != box || !a.teamMenu.on {
		t.Fatal("a key typed under the switcher")
	}
	_, _ = a.Update(tea.MouseClickMsg{X: a.width - 2, Y: a.height - 3, Button: tea.MouseLeft})
	if a.teamMenu.on || a.wall.activeID != harbor || a.wall.on {
		t.Fatalf("a press off the switcher: menu %v, active %q", a.teamMenu.on, a.wall.activeID)
	}
	a.openTeamMenu()
	_, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.teamMenu.on {
		t.Fatal("esc did not close the switcher")
	}
	// The chip that opened it closes it.
	a.openTeamMenu()
	_, _ = menuFrame(t, a)
	_, _ = a.Update(tea.MouseClickMsg{X: a.wall.chip.from + 1, Y: tabStripRow, Button: tea.MouseLeft})
	if a.teamMenu.on {
		t.Fatal("the chip did not close its own switcher")
	}
}

// A team selected from the grid leaves the grid and enables its overlay.
func TestTeamMenuChoosesOverlayFromGridWithoutFilteringIt(t *testing.T) {
	a, harbor, _ := menuApp(t)
	front := a.frontTabKey()
	a.input.insert("keep this draft")
	_ = a.openWall()
	_, _ = menuFrame(t, a)
	_, _ = a.wallPress(a.wall.chip.from+1, tabStripRow)
	if !a.teamMenu.on {
		t.Fatal("grid chip did not open overlay picker")
	}
	_, _ = menuFrame(t, a)
	hit := menuHit(t, a, wallPopTeam, harbor)
	drive(t, a, runCmd(a.teamMenuPress(hit.x0+1, hit.y0))...)
	if a.wall.on || a.teamViews.id != harbor || a.frontTabKey() != front || a.input.String() != "keep this draft" {
		t.Fatal("overlay choice did not return to unchanged chat")
	}
}

// WITH NO TEAM SHOWN THE CHIP IS A QUIET `Teams ▾` while there are teams, and
// is not there at all while there are none.
func TestTeamMenuQuietChipWithNoTeamShown(t *testing.T) {
	a, _, _ := tabApp(t)
	a.teamsEnsure()
	if row := plain(a.tabsRow(a.width)); strings.Contains(row, "▾") {
		t.Fatalf("a chip with no teams: %q", row)
	}
	a, _, _ = menuApp(t)
	a.teamActivate("")
	a.touch()
	row := plain(a.tabsRow(a.width))
	if !strings.Contains(row, " Teams ▾ ") || !a.wall.chip.pressable() {
		t.Fatalf("no quiet chip: %q", row)
	}
	if _, took := a.tabPress(a.wall.chip.from+1, tabStripRow); !took || !a.teamMenu.on {
		t.Fatal("the quiet chip did not open the switcher")
	}
	frame, _ := menuFrame(t, a)
	if !strings.Contains(frame, "◉   None") || strings.Contains(frame, "Add this conversation") || strings.Contains(frame, "Team settings") {
		t.Fatalf("the switcher with no team shown:\n%s", frame)
	}
	a.pal.ascii = true
	a.touch()
	frame, _ = menuFrame(t, a)
	if !strings.Contains(frame, "*   None") || strings.ContainsAny(frame, "◉○") {
		t.Fatalf("the ASCII switcher:\n%s", frame)
	}
}

// TestTeamMenuPrintsFrame prints the strip with the switcher open, on a
// 120-column chat, for a person to look at.
func TestTeamMenuPrintsFrame(t *testing.T) {
	a, _, _ := menuApp(t)
	a.width, a.height = 120, 30
	a.touch()
	_ = a.tabsRow(a.width)
	a.openTeamMenu()
	frame, _ := menuFrame(t, a)
	t.Logf("120x30, the chat with the team switcher open:\n%s", strings.Join(strings.Split(frame, "\n")[:14], "\n"))
}
