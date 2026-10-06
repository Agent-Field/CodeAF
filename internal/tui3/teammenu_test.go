package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/charmbracelet/x/ansi"
)

func TestTeamMenuOffersGlobalOverlayOnlyWithManager(t *testing.T) {
	a, _, _ := menuApp(t)
	var rootID string
	if err := a.teamEdit(func(f *teamstore.File) error {
		rootID = f.MakeRoot(a.now())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertRoot := func(want bool) {
		t.Helper()
		found := false
		for _, row := range a.teamMenuRows() {
			if row.id == rootID {
				found = true
			}
		}
		if found != want {
			t.Fatalf("global choice = %v, want %v", found, want)
		}
	}
	assertRoot(false)
	if err := a.teamEdit(func(f *teamstore.File) error {
		return f.SetManager(rootID, "global")
	}); err != nil {
		t.Fatal(err)
	}
	assertRoot(true)
	a.openTeamMenu()
	menuFrame(t, a)
	hit := menuHit(t, a, wallPopTeam, rootID)
	drive(t, a, runCmd(a.teamMenuPress(hit.x0+1, hit.y0))...)
	if a.wall.activeID != rootID {
		t.Fatal("global choice did not activate global overlay")
	}
	a.openTeamMenu()
	if a.teamMenu.cursor != 1 {
		t.Fatalf("global choice not selected: %d", a.teamMenu.cursor)
	}
	a.tp.previews = map[string]teamsPreview{"global": {missing: true}}
	assertRoot(false)
	if err := a.teamEdit(func(f *teamstore.File) error { return f.ClearManager(rootID) }); err != nil {
		t.Fatal(err)
	}
	assertRoot(false)
}

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

func TestTeamOverlayClearChipHasIndependentTargets(t *testing.T) {
	a, harbor, _ := menuApp(t)
	if err := a.teamRename(harbor, "a very long team name that must be abbreviated"); err != nil {
		t.Fatal(err)
	}
	for _, ascii := range []bool{false, true} {
		a.pal.ascii = ascii
		a.touch()
		row := a.tabsRow(a.width)
		name, clear, grid := a.wall.chip, a.wall.chipClear, a.wall.door
		if !name.pressable() || !clear.pressable() || name.to != clear.from {
			t.Fatalf("name and clear targets: %+v %+v", name, clear)
		}
		if label := plain(ansi.Cut(row, clear.from, clear.to)); label != a.tabCloseWord()+" " {
			t.Fatalf("clear target covers %q", label)
		}
		if strings.Contains(plain(ansi.Cut(row, name.from, name.to)), "▾") {
			t.Fatal("selected chip still carries a dropdown caret")
		}
		for _, target := range []struct {
			span hudSpan
			kind tabKind
		}{{name, tabTeam}, {clear, tabTeamClear}} {
			for x := target.span.from; x < target.span.to; x++ {
				hit, ok := a.tabAt(x, tabStripRow)
				if !ok || hit.kind != target.kind {
					t.Fatalf("column %d targets %+v", x, hit)
				}
			}
			a.hot, _ = a.tabHoverAt(target.span.from, tabStripRow)
			_ = a.tabsRow(a.width)
			// A second frame exercises the cached row's independent targets.
			_ = a.tabsRow(a.width)
			if a.wall.chip != name || a.wall.chipClear != clear || a.wall.door != grid {
				t.Fatal("hover or cached frame moved targets or the All button")
			}
			hit, ok := a.hotTab()
			if !ok || hit.kind != target.kind {
				t.Fatal("hover resolved to a different action")
			}
		}
	}
	a.width = 40
	a.touch()
	_ = a.tabsRow(a.width)
	if a.wall.chipClear.pressable() {
		t.Fatal("hidden chip retained a clear target after resizing")
	}
}

func TestTeamOverlayClearChipUsesNoneTransition(t *testing.T) {
	for _, mode := range []string{"chat", "menu", "global"} {
		t.Run(mode, func(t *testing.T) {
			a, harbor, _ := menuApp(t)
			if mode == "global" {
				var rootID string
				if err := a.teamEdit(func(f *teamstore.File) error {
					rootID = f.MakeRoot(a.now())
					return f.SetManager(rootID, a.frontTabKey())
				}); err != nil {
					t.Fatal(err)
				}
				a.teamActivate(rootID)
			}
			front := a.frontTabKey()
			a.input.insert("keep this draft")
			before := mustTeam(t, a, harbor)
			if mode == "menu" {
				a.openTeamMenu()
			}
			menuFrame(t, a)
			clear := a.wall.chipClear
			if !clear.pressable() {
				t.Fatal("no clear button")
			}
			_, _ = a.Update(tea.MouseClickMsg{X: clear.from, Y: tabStripRow, Button: tea.MouseLeft})
			if a.wall.activeID != "" || a.teamViews.id != "" || a.tp.sel != teamsAllRow || a.teamMenu.on || a.wall.on {
				t.Fatal("clear did not return to None and synchronize All teams")
			}
			if a.frontTabKey() != front || a.input.String() != "keep this draft" {
				t.Fatal("clear changed the conversation or its draft")
			}
			after := mustTeam(t, a, harbor)
			if after.Manager != before.Manager || len(after.Members) != len(before.Members) || after.Closed() {
				t.Fatal("clear changed the team")
			}
			frame, _ := menuFrame(t, a)
			if !strings.Contains(frame, " Teams ▾ ") || a.wall.chipClear.pressable() {
				t.Fatal("None did not restore the ordinary dropdown")
			}
			_, _ = a.Update(tea.MouseClickMsg{X: a.wall.chip.from + 1, Y: tabStripRow, Button: tea.MouseLeft})
			if !a.teamMenu.on || a.teamMenu.cursor != 0 {
				t.Fatal("ordinary chip did not reopen dropdown on None")
			}
		})
	}
}

func TestTeamOverlayClearChipTracksHoverAboveOpenMenu(t *testing.T) {
	a, _, _ := menuApp(t)
	a.openTeamMenu()
	menuFrame(t, a)
	for _, span := range []hudSpan{a.wall.chip, a.wall.chipClear} {
		_, _ = a.Update(tea.MouseMotionMsg{X: span.from, Y: tabStripRow})
		_, _ = a.Update(pointerMsg{})
		menuFrame(t, a)
		if a.hot != (hoverAt{kind: hoverTab, index: span.from}) {
			t.Fatalf("open menu suppressed chip hover: got %+v, want column %d; menu %v", a.hot, span.from, a.teamMenu.on)
		}
	}
	hit := menuHit(t, a, teamMenuNone, "")
	_, _ = a.Update(tea.MouseMotionMsg{X: hit.x0 + 1, Y: hit.y0})
	_, _ = a.Update(pointerMsg{})
	if a.hot != (hoverAt{}) || a.teamMenu.hover != hit.ref() {
		t.Fatal("menu row retained stale clear-button hover")
	}
}

func TestTeamOverlayClearChipRestoresOrdinaryConversationAndDrafts(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	ordinary := a.frontTabKey()
	a.input.insert("ordinary draft")
	// Re-entering an overlay saves the actual ordinary viewport and selection.
	a.teamActivate("")
	wantView := tabViewport{from: 2, browsing: true}
	a.tabView = wantView
	a.teamActivate(orbit)
	member := a.frontTabKey()
	if member == ordinary {
		t.Fatal("fixture did not switch to a different team conversation")
	}
	a.input.insert("member draft")
	_ = a.tabsRow(a.width)
	_, cmd := a.Update(tea.MouseClickMsg{X: a.wall.chipClear.from, Y: tabStripRow, Button: tea.MouseLeft})
	drive(t, a, runCmd(cmd)...)
	if a.frontTabKey() != ordinary || a.input.String() != "ordinary draft" || a.tabView != wantView || a.tp.sel != teamsAllRow {
		t.Fatal("clear did not restore the saved ordinary conversation, draft and viewport")
	}
	drive(t, a, runCmd(a.teamActivate(orbit))...)
	if a.frontTabKey() != member || a.input.String() != "member draft" {
		t.Fatal("clearing discarded the team conversation's draft")
	}
	if len(mustTeam(t, a, harbor).Members) != 2 {
		t.Fatal("clear changed unrelated membership")
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

func TestGlobalPickerCountExcludesRetainedCandidates(t *testing.T) {
	a, _, _ := menuApp(t)
	tabs := a.tabList()
	var rootID string
	if err := a.teamEdit(func(f *teamstore.File) error {
		rootID = f.MakeRoot(a.now())
		if err := f.SetManager(rootID, a.frontTabKey()); err != nil {
			return err
		}
		for _, tab := range tabs {
			if tab.key != a.frontTabKey() {
				return f.AddMember(rootID, teamstore.Member{Key: tab.key})
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.openTeamMenu()
	frame, _ := menuFrame(t, a)
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "All teams") && strings.Contains(line, "│") {
			body := strings.TrimSpace(strings.Split(line, "│")[1])
			if !strings.HasSuffix(body, "1") {
				t.Fatalf("root count included retained candidate: %s", line)
			}
			return
		}
	}
	t.Fatal("global picker row absent")
}
