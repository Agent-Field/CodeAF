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
	seal := rule(a.width)
	rows[tabStripRow+1] = seal
	frame = strings.Join(rows, "\n")
	shown := strings.Split(a.tabTitlePreview(frame), "\n")
	if !strings.Contains(plain(shown[chatHeadRows-1]), a.title) {
		t.Fatal("hover did not put the full title on the head's blank row")
	}
	if shown[tabStripRow+1] != seal {
		t.Fatal("preview covered the rule under the strip")
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

// THE NAME ENDS WHERE ITS TAB ENDS: a name wider than the tab sticks out to the
// left, and only a name too wide for the room left of that edge takes cells to
// its right, and then only as many as it needs.
func TestTabTitlePreviewEndsUnderItsTab(t *testing.T) {
	a, _, _ := tabApp(t)
	// Every title below opens with the same words, so the tab's cut label, and
	// with it the tab's place on the strip, is the same for all of them.
	base := "Shipping the parser with complete unicode support"
	a.title = base
	a.tabsRow(a.width)
	span := tabSpanFor(t, a, a.title)
	a.hot, _ = a.tabHoverAt(span.from, tabStripRow)
	hit, ok := a.hotTab()
	if !ok {
		t.Fatal("the pointer is not over the tab")
	}
	end := a.tabBoxEnd(hit)
	if end != span.to+tabCloseCells {
		t.Fatalf("the tab's box ends at %d, want its close cells' edge %d", end, span.to+tabCloseCells)
	}
	preview := func(title string) []string {
		t.Helper()
		a.title = title
		row := a.tabsRow(a.width)
		if got := tabSpanFor(t, a, title); got != span {
			t.Fatalf("a longer title moved the tab from %+v to %+v", span, got)
		}
		rows := strings.Split(strings.Repeat(strings.Repeat(" ", a.width)+"\n", a.height-1), "\n")
		rows[tabStripRow] = row
		shown := strings.Split(a.tabTitlePreview(strings.Join(rows, "\n")), "\n")
		for i := range shown {
			shown[i] = plain(shown[i])
		}
		return shown
	}
	grown := func(width int) string {
		title := base
		for len(title) < width {
			title += " and more"
		}
		return strings.TrimSpace(title[:width])
	}

	if end-headLabelAt <= len(base) || end > a.width-headLabelAt {
		t.Fatalf("the fixture's tab ends at %d, which cannot show both a short and a long name", end)
	}
	shown := preview(base)
	if at := strings.Index(shown[chatHeadRows-1], base); at+len(base) != end {
		t.Fatalf("a name that fits ends at %d, want the tab's edge %d:\n%q", at+len(base), end, shown[chatHeadRows-1])
	}

	long := grown(end - headLabelAt + 10)
	shown = preview(long)
	if at := strings.Index(shown[chatHeadRows-1], long); at != headLabelAt {
		t.Fatalf("a name wider than the room left of its tab starts at %d, want the margin %d:\n%q", at, headLabelAt, shown[chatHeadRows-1])
	}

	wide := grown(a.width + 20)
	shown = preview(wide)
	first, second := shown[chatHeadRows-1], shown[chatHeadRows]
	if !strings.HasPrefix(first, strings.Repeat(" ", headLabelAt)+"Shipping") || strings.TrimSpace(second) == "" || !strings.HasPrefix(second, strings.Repeat(" ", headLabelAt)) || second[headLabelAt] == ' ' {
		t.Fatalf("a name wider than the row does not wrap from the margin:\n%q\n%q", first, second)
	}
}
