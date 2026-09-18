package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func selectedRowY(t *testing.T, a *app, at int) int {
	t.Helper()
	var hits []int
	switch a.page {
	case pageHome:
		return homeLineY(t, a, at)
	case pageTasks:
		_, rows, _, _ := a.taskSheetFrame(a.width, a.height)
		for y, hit := range rows {
			if hit.kind == taskSheetHitRow && hit.index == at {
				return y
			}
		}
	case pageMemory:
		_, hits, _, _ = a.memoryFrame(a.width, a.height)
	case pageStanding:
		_, hits, _, _ = a.standingPlaceFrame(a.width, a.height)
	case pageSearch:
		_, hits, _, _ = a.searchFrame(a.width, a.height)
	case pageSpend:
		_, hits, _, _ = a.spendFrame(a.width, a.height)
	}
	return placeBodyRowOf(t, hits, at)
}

func TestListSelectionFollowsTheLatestNavigationMethod(t *testing.T) {
	labs := map[string]func(*testing.T) *app{
		"threads":  placeAppOneColumn,
		"tasks":    func(t *testing.T) *app { a, _ := stripWalkLab(t); return a },
		"standing": standPlaceLab, "memory": memoryPlaceLab,
		"search": searchPlaceWithHits, "spend": spendPlaceLab,
	}
	for name, lab := range labs {
		t.Run(name, func(t *testing.T) {
			a := lab(t)
			placeFrameText(a)
			pl := a.showing()
			stops := pl.stops(a)
			if len(stops) < 2 {
				t.Fatal("the fixture needs two selectable rows")
			}
			y := selectedRowY(t, a, stops[0])
			drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
			if got := pl.cursorAt(a); got != stops[0] {
				t.Fatalf("mouse selected %d, want %d", got, stops[0])
			}
			drive(t, a, key("down"))
			if got := pl.cursorAt(a); got != stops[1] {
				t.Fatalf("keyboard selected %d, want %d", got, stops[1])
			}
			if a.home.hover >= 0 || a.mem.hover >= 0 || a.orders.hover >= 0 || a.search.hover >= 0 || a.spend.hover >= 0 || a.hot.kind == hoverTaskSheet {
				t.Fatal("keyboard navigation left a second highlighted row")
			}
			// Repeated reports from a parked pointer cannot undo a newer key.
			drive(t, a, tea.MouseMotionMsg{X: 4, Y: y}, pointerMsg{})
			if got := pl.cursorAt(a); got != stops[1] {
				t.Fatal("stationary mouse took back the keyboard selection")
			}
			// Real motion, even within the same row, takes ownership again.
			y = selectedRowY(t, a, stops[0])
			drive(t, a, tea.MouseMotionMsg{X: 5, Y: y})
			if got := pl.cursorAt(a); got != stops[0] {
				t.Fatal("new mouse motion did not take over")
			}
		})
	}
}

func TestHomePutAwayActsOnTheLastKeyboardSelection(t *testing.T) {
	a := placeAppOneColumn(t)
	placeFrameText(a)
	first := a.home.cursor
	y := selectedRowY(t, a, first)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y}, key("down"))
	selected, ok := a.home.focusedLine()
	if !ok || selected.kind != homeSession || a.home.cursor == first {
		t.Fatal("keyboard did not reach another thread")
	}
	var archived string
	a.archive = func(dir string, away bool) error {
		if !away {
			t.Fatal("put-away brought a thread back")
		}
		archived = dir
		return nil
	}
	drive(t, a, key("right"))
	if !a.strip.open || a.strip.row != a.rowIdentity() {
		t.Fatal("options did not belong to the selected row")
	}
	drive(t, a, key("a"))
	if archived != selected.row.Dir {
		t.Fatalf("put away acted on %q, want %q", archived, selected.row.Dir)
	}
}

func TestQueuedMouseMotionCannotUndoKeyboardSelection(t *testing.T) {
	a := placeAppOneColumn(t)
	placeFrameText(a)
	first := a.home.cursor
	y := selectedRowY(t, a, first)
	a.Update(tea.MouseMotionMsg{X: 4, Y: y})
	a.Update(tea.MouseMotionMsg{X: 5, Y: y})
	if !a.ptr.have {
		t.Fatal("fixture did not queue a mouse motion")
	}
	drive(t, a, key("down"))
	selected := a.home.cursor
	drive(t, a, pointerMsg{})
	if selected == first || a.home.cursor != selected || a.home.hover >= 0 {
		t.Fatal("a pending mouse tick restored the old selection")
	}
}

func TestMouseNavigationRetiresTheOldRowsOptions(t *testing.T) {
	a, agent := stripWalkLab(t)
	stops := a.taskSheet.stops(a)
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatal("task options did not open")
	}
	other := stops[0]
	y := selectedRowY(t, a, other)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
	if a.taskSheet.cursor != other || a.strip.open {
		t.Fatal("new mouse selection kept the old task options")
	}
	drive(t, a, key("s"))
	if len(agent.asked) != 0 {
		t.Fatal("a stale option stopped the old task")
	}
}

func TestChatSwitcherSharesMouseAndKeyboardSelection(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drive(t, a, key(hopOpenKey))
	a.hop.originY = 4
	a.hopOver(make([]string, 20), 80, a.pal)
	target := a.hop.spots[1]
	x, y := a.hop.left+3, a.hop.top+target.row
	drive(t, a, tea.MouseMotionMsg{X: x, Y: y})
	if a.hop.at != target.at {
		t.Fatal("mouse did not select the switcher row")
	}
	drive(t, a, key("down"))
	selected := a.hop.at
	if selected == target.at || a.hot.kind == hoverHop {
		t.Fatal("keyboard left the old switcher selection highlighted")
	}
	drive(t, a, tea.MouseMotionMsg{X: x, Y: y}, pointerMsg{})
	if a.hop.at != selected {
		t.Fatal("parked mouse undid switcher keyboard navigation")
	}
}
