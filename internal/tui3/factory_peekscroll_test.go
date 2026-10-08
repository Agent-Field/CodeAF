package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// factoryLongLab is the fixture floor at 160 columns with item 8 carrying a
// body of eighty paragraphs, `para 01` to `para 80`, a blank row between each
// (Markdown folds single breaks), so the peek beside the rows overflows.
func factoryLongLab(t *testing.T) *app {
	t.Helper()
	a := factoryPlaceLab(t)
	a.width, a.height = 160, 44
	var paras []string
	for i := 1; i <= 80; i++ {
		n := itoa(i)
		if i < 10 {
			n = "0" + n
		}
		paras = append(paras, "para "+n+" of the proposal")
	}
	body := strings.Join(paras, "\n\n")
	factoryPaneItem(t, a, 8).Body = body
	// A RE-READ CARRIES THE SAME BODY, so the beat reads what the floor read.
	load := a.factory.Load
	a.factory.Load = func() (factory.Snapshot, error) {
		snap, err := load()
		for i := range snap.Items {
			if snap.Items[i].ID == 8 {
				snap.Items[i].Body = body
			}
		}
		return snap, err
	}
	factoryOn(t, a, 8)
	factoryFrameLines(a)
	return a
}

// factoryMoreRowY is the screen row whose peek column ends with `▾ more`, and
// -1 when none does.
func factoryMoreRowY(a *app) int {
	more := a.icon(tokens.GExpanded) + " more"
	for y, line := range factoryFrameLines(a) {
		// The pane's last cell is before the verbs' column when one stands.
		if _, pane, div := factorySplitAt(line); div >= 0 && strings.HasSuffix(strings.TrimRight(pane, " "), more) {
			return y
		}
		if strings.HasSuffix(strings.TrimRight(line, " "), more) {
			return y
		}
	}
	return -1
}

func factoryFrameHas(a *app, words string) bool {
	return strings.Contains(strings.Join(factoryFrameLines(a), "\n"), words)
}

// THE REPRODUCTION (owner's screenshot, 2026-10-08 15:38): a long body is cut
// with `▾ more`, and `J` in every spelling a terminal sends moves the window
// off the first paragraph; `K` brings it back.
func TestFactoryPeekLongBodyScrollsByKey(t *testing.T) {
	for _, j := range []struct {
		name string
		down tea.KeyPressMsg
		up   tea.KeyPressMsg
	}{
		{"shifted text", tea.KeyPressMsg{Code: 'J', Text: "J", Mod: tea.ModShift}, tea.KeyPressMsg{Code: 'K', Text: "K", Mod: tea.ModShift}},
		{"plain", key("J"), key("K")},
		{"no text", tea.KeyPressMsg{Code: 'j', ShiftedCode: 'J', Mod: tea.ModShift}, tea.KeyPressMsg{Code: 'k', ShiftedCode: 'K', Mod: tea.ModShift}},
	} {
		t.Run(j.name, func(t *testing.T) {
			a := factoryLongLab(t)
			if factoryMoreRowY(a) < 0 || !factoryFrameHas(a, "para 01") {
				t.Fatalf("the long body is not cut with more:\n%s", strings.Join(factoryFrameLines(a), "\n"))
			}
			drive(t, a, j.down)
			if factoryFrameHas(a, "para 01") || !factoryFrameHas(a, "para 02") {
				t.Fatalf("J did not move the window off the first paragraph:\n%s", strings.Join(factoryFrameLines(a), "\n"))
			}
			drive(t, a, j.up)
			if !factoryFrameHas(a, "para 01") {
				t.Fatalf("K did not bring the first paragraph back:\n%s", strings.Join(factoryFrameLines(a), "\n"))
			}
		})
	}
}

// THE WHEEL OVER THE PEEK SCROLLS IT, [placeWheelRows] rows a tick, and
// leaves the cursor where it stood; over the rows it walks the cursor.
func TestFactoryPeekWheelScrollsThePeekAndWalksTheRows(t *testing.T) {
	a := factoryLongLab(t)
	y := factoryMoreRowY(a)
	from := a.fp.cursor
	drive(t, a, tea.MouseWheelMsg{X: a.fp.rowsW + 10, Y: y, Button: tea.MouseWheelDown})
	factoryFrameLines(a)
	if a.fp.cursor != from || a.fp.scroll != placeWheelRows {
		t.Fatalf("the wheel over the peek: cursor %d (was %d), scroll %d", a.fp.cursor, from, a.fp.scroll)
	}
	if factoryFrameHas(a, "para 01") {
		t.Fatal("the wheel over the peek left the first paragraph on screen")
	}
	drive(t, a, tea.MouseWheelMsg{X: a.fp.rowsW + 10, Y: y, Button: tea.MouseWheelUp})
	factoryFrameLines(a)
	if a.fp.scroll != 0 || !factoryFrameHas(a, "para 01") {
		t.Fatalf("the wheel back up left the scroll at %d", a.fp.scroll)
	}
	drive(t, a, tea.MouseWheelMsg{X: 3, Y: y, Button: tea.MouseWheelDown})
	if a.fp.cursor == from {
		t.Fatal("the wheel over the rows did not walk the cursor")
	}
}

// A PRESS ON `▾ more` SCROLLS A PAGE; a press elsewhere in the peek moves
// nothing.
func TestFactoryPeekClickOnMoreScrollsAPage(t *testing.T) {
	a := factoryLongLab(t)
	y := factoryMoreRowY(a)
	page := a.fp.scrollPage
	drive(t, a, clickAt(a.width-4, y-1), releaseAt(a.width-4, y-1))
	if a.fp.scroll != 0 {
		t.Fatalf("a press above more scrolled to %d", a.fp.scroll)
	}
	drive(t, a, clickAt(a.width-4, y), releaseAt(a.width-4, y))
	factoryFrameLines(a)
	if a.fp.scroll != page || page < 2 || factoryFrameHas(a, "para 01") {
		t.Fatalf("a press on more scrolled to %d, want a page of %d", a.fp.scroll, page)
	}
}

// THE SCROLL OUTLIVES THE BEAT: a re-read of the floor keeps it; a cursor
// that moved to another item starts that item at its top.
func TestFactoryPeekScrollSurvivesTheBeat(t *testing.T) {
	a := factoryLongLab(t)
	drive(t, a, key("J"), key("J"), key("J"))
	for i := 0; i < 3; i++ {
		_, cmd := (placeFactory{}).tick(a, a.now())
		drive(t, a, runCmd(cmd)...)
		factoryFrameLines(a)
	}
	if a.fp.scroll != 3 || factoryFrameHas(a, "para 01") {
		t.Fatalf("three beats left the scroll at %d", a.fp.scroll)
	}
	drive(t, a, key("down"))
	factoryFrameLines(a)
	if a.fp.scroll != 0 {
		t.Fatalf("a cursor move kept the scroll at %d", a.fp.scroll)
	}
	drive(t, a, key("up"))
	if !factoryFrameHas(a, "para 01") {
		t.Fatal("coming back to the item kept its scroll")
	}
}

// THE ITEM PAGE'S ISSUE TAKES THE SAME FOUR ROADS: `J`, the wheel over the
// pane (over the rail it walks the rail), and a press on `▾ more`.
func TestFactoryItemPageIssueScrollsByWheelAndMore(t *testing.T) {
	a := factoryLongLab(t)
	drive(t, a, key("enter"))
	factoryFrameLines(a)
	if !a.fp.open || !factoryFrameHas(a, "para 01") {
		t.Fatalf("the item page does not show the issue with J K scroll:\n%s", strings.Join(factoryFrameLines(a), "\n"))
	}
	y := factoryMoreRowY(a)
	if y < 0 {
		t.Fatalf("the issue is not cut with more:\n%s", strings.Join(factoryFrameLines(a), "\n"))
	}
	stage := a.fp.stage
	// THE PANE'S RIGHT EDGE is the verbs' column's rule while the column
	// stands, and the frame's otherwise.
	edge := a.width
	if a.factoryVerbsDrawn() {
		edge = a.fp.verbX
	}
	drive(t, a, tea.MouseWheelMsg{X: edge - 10, Y: y, Button: tea.MouseWheelDown})
	factoryFrameLines(a)
	if a.fp.stage != stage || a.fp.scroll != placeWheelRows {
		t.Fatalf("the wheel over the issue: stage %d (was %d), scroll %d", a.fp.stage, stage, a.fp.scroll)
	}
	drive(t, a, tea.MouseWheelMsg{X: edge - 10, Y: y, Button: tea.MouseWheelUp})
	factoryFrameLines(a)
	page := a.fp.scrollPage
	drive(t, a, clickAt(edge-4, y), releaseAt(edge-4, y))
	factoryFrameLines(a)
	if a.fp.scroll != page || a.fp.stage != stage {
		t.Fatalf("a press on more: scroll %d want %d, stage %d", a.fp.scroll, page, a.fp.stage)
	}
	drive(t, a, tea.MouseWheelMsg{X: 2, Y: placeHeadRows + a.fp.railTop, Button: tea.MouseWheelDown})
	if a.fp.stage == stage {
		t.Fatal("the wheel over the rail did not walk it")
	}
}
