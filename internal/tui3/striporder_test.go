package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// THE STRIP READS THE TEAM CHIP, THE MANAGER, THEN THE TABS. The chip filters
// the tabs, so it stands first, right before them, with the manager's place
// after it. There is no `home` piece in front of it any more: home is the
// nav's first word, on the row over the strip on every page (topnav.go). The
// hits follow the words, and as the row narrows the chip goes before the tab
// in front ever does.
func TestTheStripReadsTheTeamThenItsTabs(t *testing.T) {
	a, _, _, _ := trafficApp(t)
	a.open = func(workspace, transcript string) (Conversation, error) { return Conversation{}, nil }
	a.width, a.height = 160, 40
	row := ansi.Strip(a.tabsRow(a.width))
	chip, manager := strings.Index(row, "harbor ▾"), strings.Index(row, teamManagerGlyph+" Manager")
	if chip < 0 || manager < 0 || chip > manager {
		t.Fatalf("the strip reads %q", row)
	}
	if strings.Contains(row, " home ") || strings.Contains(row, "Home") {
		t.Fatalf("the strip still carries a way home, which is the nav's first word now: %q", row)
	}
	for _, hit := range a.chatTabHits {
		if hit.kind == tabManager || hit.kind == tabHere || hit.kind == tabOther {
			if hit.span.from < a.wall.chip.to {
				t.Fatalf("a tab's hit %+v is before the chip's end %d", hit, a.wall.chip.to)
			}
		}
	}
	if a.wall.chip.from != headLabelAt {
		t.Fatalf("the chip's hit %+v is not the strip's first piece at %d", a.wall.chip, headLabelAt)
	}
	if got := plainCells(row, a.wall.chip.from, a.wall.chip.to); !strings.Contains(got, "harbor") {
		t.Fatalf("the chip's hit covers %q", got)
	}
	// Narrowing: the chip goes, and the tab in front stays.
	sawChipless := false
	for w := 159; w >= roomHeadFloor; w-- {
		a.chatTabBar = tabBar{}
		row := ansi.Strip(a.tabsRow(w))
		if !strings.Contains(row, "harbor") {
			sawChipless = true
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
	if !sawChipless {
		t.Fatal("no width gave the chip up to keep the tab in front")
	}
}
