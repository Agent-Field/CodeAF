package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── the floor answers the pointer (owner's screenshot, 2026-10-08) ─────────

// factoryHoverLab is the floor in colour, wide enough for the peek, with the
// cursor on the first row and one frame drawn so the rows' window is known.
func factoryHoverLab(t *testing.T) *app {
	t.Helper()
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	a.pal = newPalette(tokens.TrueColor, false)
	a.fp.cursor = 0
	frame(a)
	return a
}

// factoryRowY is the screen row the floor drew walk position walk on.
func factoryRowY(t *testing.T, a *app, walk int) int {
	t.Helper()
	for y := 0; y < a.height; y++ {
		if a.factoryWalkAt(y) == walk {
			return y
		}
	}
	t.Fatalf("the floor drew no row for walk %d", walk)
	return -1
}

// factoryGround is the escape that opens the pointer's ground.
func factoryGround(a *app) string {
	return strings.SplitN(a.pal.cursor("§", 1), "§", 2)[0]
}

// THE POINTER RESTING ON A ROW SELECTS IT, as on every list place
// (placeselection.go): the third row takes the cursor and the pointer's
// ground, the first gives both up, and the peek shows the third row's item.
func TestFactoryPointerRestingSelectsTheRow(t *testing.T) {
	a := factoryHoverLab(t)
	y := factoryRowY(t, a, 2)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
	if a.fp.cursor != 2 || a.fp.hover != 2 {
		t.Fatalf("the pointer on the third row left cursor %d, hover %d", a.fp.cursor, a.fp.hover)
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		t.Fatal("the hovered row holds no item")
	}
	lines := strings.Split(frame(a), "\n")
	ground := factoryGround(a)
	if ground == "" {
		t.Fatal("the colour palette paints no ground")
	}
	if !strings.Contains(lines[y], ground) {
		t.Fatalf("the hovered row does not wear the pointer's ground: %q", plain(lines[y]))
	}
	if first := factoryRowY(t, a, 0); strings.Contains(lines[first], ground) {
		t.Fatalf("the first row kept the ground the pointer took: %q", plain(lines[first]))
	}
	ref := it.Ref()
	peek := false
	for _, line := range lines {
		if _, right, ok := strings.Cut(plain(line), "│"); ok && strings.Contains(right, ref) {
			peek = true
		}
	}
	if !peek {
		t.Fatalf("the peek does not show the hovered item %s", ref)
	}
}

// ONE ROW CARRIES THE GROUND: every body row that wears it is the hovered
// item's, so the cursor's ink and the pointer's are never on two rows at once.
func TestFactoryOneRowWearsThePointersGround(t *testing.T) {
	a := factoryHoverLab(t)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: factoryRowY(t, a, 3)})
	lines := strings.Split(frame(a), "\n")
	ground := factoryGround(a)
	body := placeHeadRows + a.fp.headRows
	worn := 0
	for y := body; y < body+a.fp.shown && y < len(lines); y++ {
		if !strings.Contains(lines[y], ground) {
			continue
		}
		worn++
		if w := a.factoryWalkAt(y); w != 3 {
			t.Fatalf("row %d (walk %d) wears the ground with the pointer on walk 3: %q", y, w, plain(lines[y]))
		}
	}
	if worn == 0 {
		t.Fatal("no row wears the pointer's ground")
	}
}

// OFF THE ROWS THE POINTER LETS GO: the peek and the handover are read, not
// rows, so resting there moves nothing and the hover is cleared; the cursor
// stays where the pointer left it, as it does on every list place.
func TestFactoryPointerOffTheRowsClearsTheHover(t *testing.T) {
	a := factoryHoverLab(t)
	y := factoryRowY(t, a, 2)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
	drive(t, a, tea.MouseMotionMsg{X: a.fp.rowsW + 10, Y: factoryRowY(t, a, 4)})
	if a.fp.hover != -1 || a.fp.cursor != 2 {
		t.Fatalf("the pointer in the peek left hover %d, cursor %d", a.fp.hover, a.fp.cursor)
	}
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: placeHeadRows})
	if a.fp.hover != -1 || a.fp.cursor != 2 {
		t.Fatalf("the pointer on the handover left hover %d, cursor %d", a.fp.hover, a.fp.cursor)
	}
	// AND A KEY TAKES THE SELECTION BACK, the hover with it.
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: factoryRowY(t, a, 1)})
	drive(t, a, key("down"))
	if a.fp.hover != -1 || a.fp.cursor != 2 {
		t.Fatalf("a key after the pointer left hover %d, cursor %d", a.fp.hover, a.fp.cursor)
	}
}

// THE ITEM PAGE'S LEFT COLUMN ANSWERS THE POINTER TOO: resting on a row
// highlights it and leaves the cursor where it was (owner's layout,
// 2026-10-09), and resting on the page beside it highlights nothing there.
func TestFactoryStageRailAnswersThePointer(t *testing.T) {
	a := factoryHoverLab(t)
	y := factoryRowY(t, a, 0)
	drive(t, a, clickAt(5, y), releaseAt(5, y), clickAt(5, y), releaseAt(5, y))
	if !a.fp.open {
		t.Fatal("two presses did not open the item page")
	}
	frame(a)
	if a.fp.railShown < 3 {
		t.Skipf("the item page drew %d rail rows", a.fp.railShown)
	}
	was := a.fp.stage
	ry := placeHeadRows + a.fp.railTop + 2
	drive(t, a, tea.MouseMotionMsg{X: factoryRailW + 20, Y: ry})
	if a.fp.stage != was {
		t.Fatalf("the pointer on the page beside the rail moved the stage to %d", a.fp.stage)
	}
	drive(t, a, tea.MouseMotionMsg{X: 3, Y: ry})
	if a.fp.stage != was || !a.fp.open {
		t.Fatalf("the pointer resting on the column's third row moved the cursor to %d", a.fp.stage)
	}
	if a.fp.hot.kind != factoryHotRow || a.fp.hot.row != a.fp.railFirst+2 {
		t.Fatalf("the pointer on the column's third row highlights %+v", a.fp.hot)
	}
}

// THE PICKER'S ROWS ANSWER THE POINTER: resting on a repository's row puts
// the picker's cursor on it, and resting on its heading moves nothing.
func TestFactoryPickerAnswersThePointer(t *testing.T) {
	a := factorySettingsLab(t, newSettingsFake(), 150)
	drive(t, a, key("R"))
	if a.fp.pick == nil {
		t.Fatal("R opened no picker")
	}
	frame(a)
	p := a.fp.pick
	target, y := -1, -1
	for row, walk := range p.hits {
		if walk >= 0 && walk != p.cursor {
			target, y = walk, placeHeadRows+row
			break
		}
	}
	if target < 0 {
		t.Fatal("the picker drew no second repository row")
	}
	drive(t, a, tea.MouseMotionMsg{X: 6, Y: placeHeadRows})
	if p.cursor == target {
		t.Fatal("the pointer on the picker's heading moved its cursor")
	}
	drive(t, a, tea.MouseMotionMsg{X: 6, Y: y})
	if p.cursor != target {
		t.Fatalf("the pointer on the picker's row %d left its cursor at %d", target, p.cursor)
	}
}
