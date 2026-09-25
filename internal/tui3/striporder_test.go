package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// THE STRIP READS HOME, THE TEAM CHIP, THE MANAGER, THEN THE TABS. Home is a
// fixed door and stands first; the chip filters the tabs, so it sits right
// before them with the manager's place after it. The hits follow the words,
// and as the row narrows Home goes first, then the chip, never the tab in
// front.
func TestTheStripReadsHomeThenTheTeamThenItsTabs(t *testing.T) {
	a, _, _, _ := trafficApp(t)
	a.open = func(workspace, transcript string) (Conversation, error) { return Conversation{}, nil }
	a.width, a.height = 160, 40
	if !a.homeDoorOpen() {
		t.Fatal("the fixture has no Home door")
	}
	row := ansi.Strip(a.tabsRow(a.width))
	home, chip, manager := strings.Index(row, "Home"), strings.Index(row, "harbor ▾"), strings.Index(row, teamManagerGlyph+" Manager")
	if home < 0 || chip < 0 || manager < 0 || !(home < chip && chip < manager) {
		t.Fatalf("the strip reads %q", row)
	}
	var homeHit tabHit
	for _, hit := range a.chatTabHits {
		if hit.kind == tabHome {
			homeHit = hit
		}
		if hit.kind == tabManager || hit.kind == tabHere || hit.kind == tabOther {
			if hit.span.from < a.wall.chip.to {
				t.Fatalf("a tab's hit %+v is before the chip's end %d", hit, a.wall.chip.to)
			}
		}
	}
	if homeHit.span.from != headLabelAt || a.wall.chip.from != headLabelAt+len(" Home ")+2 {
		t.Fatalf("Home's hit %+v and the chip's %+v are not where they are drawn", homeHit.span, a.wall.chip)
	}
	if got := plainCells(row, a.wall.chip.from, a.wall.chip.to); !strings.Contains(got, "harbor") {
		t.Fatalf("the chip's hit covers %q", got)
	}
	// Narrowing: Home goes before the chip, and the tab in front stays.
	sawHomeless := false
	for w := 159; w >= roomHeadFloor; w-- {
		a.chatTabBar = tabBar{}
		row := ansi.Strip(a.tabsRow(w))
		hasHome, hasChip := strings.Contains(row, "Home"), strings.Contains(row, "harbor")
		if hasHome && !hasChip {
			t.Fatalf("at %d the chip went before Home: %q", w, row)
		}
		if !hasHome && hasChip {
			sawHomeless = true
		}
		front := false
		for _, hit := range a.chatTabHits {
			if hit.kind == tabHere || (hit.tab.here && hit.kind == tabManager) || hit.tab.key == a.frontTabKey() {
				front = true
			}
		}
		if !front {
			t.Fatalf("at %d the tab in front is gone: %q", w, row)
		}
	}
	if !sawHomeless {
		t.Fatal("no width dropped Home and kept the chip")
	}
}
