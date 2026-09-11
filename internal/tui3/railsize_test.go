package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE SEAM IS A HANDLE EVEN WHEN IT RUNS THROUGH A NODE ROW. The handle takes
// the press before the door behind it, and a second press gives the tier back.
func TestRailSeamClickTogglesTheWideTier(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	y := a.bodyTop()

	drive(t, a, tea.MouseClickMsg{X: a.railLeft(), Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.railLeft(), Y: y, Button: tea.MouseLeft})
	if !a.railWide || a.railWidth() != railWideCols {
		t.Fatalf("seam press did not widen the rail: wide=%v width=%d", a.railWide, a.railWidth())
	}
	drive(t, a, tea.MouseClickMsg{X: a.railLeft(), Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.railLeft(), Y: y, Button: tea.MouseLeft})
	if a.railWide || a.railWidth() != railCols {
		t.Fatalf("second seam press did not narrow the rail: wide=%v width=%d", a.railWide, a.railWidth())
	}
}

// THE FOOTER NAMES THE HANDLE WHILE THE ROSTER HAS THE KEYBOARD, whether or
// not a title happened to be cut. Its button and its sentence give both trips.
func TestRailFooterHintFollowsFocusAndTogglesBothWays(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	drive(t, a, altT())

	rail := strings.Join(railText(a, a.viewHeight()), "\n")
	if !strings.Contains(rail, railWideHint) {
		t.Fatalf("focused rail did not offer its handle:\n%s", rail)
	}
	pressHint := func() {
		t.Helper()
		for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
			if line, ok := a.railLineAt(y); ok && line.hint {
				drive(t, a, tea.MouseClickMsg{X: a.railLeft() + 3, Y: y, Button: tea.MouseLeft})
				drive(t, a, tea.MouseReleaseMsg{X: a.railLeft() + 3, Y: y, Button: tea.MouseLeft})
				return
			}
		}
		t.Fatal("rail footer drew no pressable hint")
	}

	pressHint()
	if !a.railWide {
		t.Fatal("widen footer offer did not widen the rail")
	}
	rail = strings.Join(railText(a, a.viewHeight()), "\n")
	if !strings.Contains(rail, railNarrowHint) {
		t.Fatalf("wide rail did not offer the way back:\n%s", rail)
	}
	pressHint()
	if a.railWide {
		t.Fatal("narrow footer offer did not narrow the rail")
	}
}

// THE HANDLE LIGHTS BY ITSELF. A pointer on the seam does not borrow the node
// row's whole-width hover band; the existing accent/hover treatment paints the
// two cells a person can grab.
func TestHoveringRailSeamLightsTheHandle(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	y := a.bodyTop()
	a.setHover(a.railLeft(), y)

	if a.hot.kind != hoverRailSeam {
		t.Fatalf("seam hover resolved to %+v", a.hot)
	}
	want := a.pal.cursor(a.pal.accent(railSeam), len([]rune(railSeam)))
	row := a.railRows(a.viewHeight())[y-a.bodyTop()]
	if !strings.HasPrefix(row, want) {
		t.Fatalf("seam did not take the hover style:\n got %q\nwant prefix %q", row, want)
	}
}

// A NODE ROW REMAINS A DOOR outside the two handle cells. The seam has gained
// a target; the title beside it has not lost its own.
func TestRailNodeContentStillOpensItsRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	y := -1
	for at := a.bodyTop(); at < a.bodyTop()+a.viewHeight(); at++ {
		if node := a.railNodeAt(at); node != nil && node.id == 7 {
			y = at
			break
		}
	}
	if y < 0 {
		t.Fatal("node 7 is not visible in the rail")
	}
	drive(t, a, tea.MouseClickMsg{
		X: a.railLeft() + len([]rune(railSeam)) + 1,
		Y: y, Button: tea.MouseLeft,
	})
	drive(t, a, tea.MouseReleaseMsg{
		X: a.railLeft() + len([]rune(railSeam)) + 1,
		Y: y, Button: tea.MouseLeft,
	})
	if a.room == nil || a.room.id != 7 {
		t.Fatalf("node content opened room %+v, want node 7", a.room)
	}
}
