package tui3

import (
	"strings"
	"testing"
)

func TestTabHoverRevealsFullTitleWithoutMovingTargets(t *testing.T) {
	a, _, _ := tabApp(t)
	a.title = "Shipping the parser with complete unicode support"
	a.tabsRow(a.width)
	span := tabSpanFor(t, a, a.title)
	a.hot, _ = a.tabHoverAt(span.from, tabStripRow)
	before := a.tabsRow(a.width)
	frame := strings.Repeat(strings.Repeat(" ", a.width)+"\n", a.height-1)
	rows := strings.Split(frame, "\n")
	rows[tabStripRow] = before
	frame = strings.Join(rows, "\n")
	shown := a.tabTitlePreview(frame)
	if !strings.Contains(plain(shown), a.title) {
		t.Fatal("hover lost the full title")
	}
	if a.tabsRow(a.width) != before || tabSpanFor(t, a, a.title) != span {
		t.Fatal("preview moved the tab")
	}
	hidden := strings.Repeat(" ", a.width)
	if a.tabTitlePreview(hidden) != hidden {
		t.Fatal("preview covered a frame without tabs")
	}
	a.hot = hoverAt{}
	if a.tabTitlePreview(frame) != frame {
		t.Fatal("preview remained after pointer left")
	}
}
