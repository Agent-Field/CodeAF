package tui3

import tea "charm.land/bubbletea/v2"

// Browsing changes the window onto the tab list, never its presentation order or
// the selected conversation. A new selection brings its own tab back into view.
type tabViewport struct {
	from, to, total int
	active          string
	browsing        bool
	scrollable      bool
}

const (
	tabArrowCells = 3
	// A scrolling strip must afford a useful name in addition to its controls.
	tabReadableCells = 16
)

func (a *app) tabWindow(tabs []chatTab, widths []int, budget, active int) (int, int, bool) {
	identity := tabs[active].key
	if tabs[active].start {
		identity = "new-chat"
	}
	v := &a.tabView
	if v.active != identity {
		v.active, v.browsing = identity, false
	}
	used := 1
	for _, width := range widths {
		used += width + 1
	}
	scroll := used > budget && budget >= tabReadableCells+tabInsetCells+tabCloseCells+2+tabArrowsCells(1)
	available := budget
	if scroll {
		available -= tabArrowsCells(1)
	} else {
		v.browsing = false
	}
	end := func(from int) int {
		used, to := 1, from
		for to < len(widths) && used+widths[to]+1 <= available {
			used += widths[to] + 1
			to++
		}
		return max(from+1, to)
	}
	from := min(max(v.from, 0), len(tabs)-1)
	if !v.browsing {
		from = 0
		for end(from) <= active {
			from++
		}
	}
	to := end(from)
	v.from, v.to, v.total, v.scrollable = from, to, len(tabs), scroll
	return from, to, scroll
}

// tabArrowsCells is what a scrolling strip spends on its two arrows: their
// cells and the gap in front of the left one ([app.tabsFit]).
func tabArrowsCells(sepW int) int { return 2*tabArrowCells + sepW }

func (a *app) tabArrowPiece(right, enabled bool, at int) (tabPiece, *tabHit) {
	word, kind := a.linearMark("‹", "<"), tabScrollLeft
	if right {
		word, kind = a.linearMark("›", ">"), tabScrollRight
	}
	// A SCROLLING STRIP DRAWS BOTH ARROWS, the one with nowhere to go dim and
	// inert. Its cells are held either way so the window never shifts under a
	// press, and three blank cells there read as a gap nobody meant.
	if !enabled {
		return tabPiece{word: " " + word + " ", quiet: true}, nil
	}
	return tabPiece{word: " " + word + " ", kind: kind}, &tabHit{span: hudSpan{from: at, to: at + tabArrowCells}, kind: kind}
}

func (a *app) tabScroll(delta int) {
	v := &a.tabView
	if !v.scrollable || delta == 0 || delta > 0 && v.to >= v.total || delta < 0 && v.from == 0 {
		return
	}
	v.from = min(max(v.from+delta, 0), v.total-1)
	v.browsing = true
	a.chatTabBar = tabBar{}
	a.dropHover()
	a.touch()
}

func (a *app) tabReveal() {
	a.tabView.browsing = false
	a.chatTabBar = tabBar{}
}

func (a *app) tabWheel(msg tea.MouseWheelMsg) bool {
	width, _ := a.size()
	mouse := msg.Mouse()
	if a.showing() != nil || mouse.X < 0 || mouse.X >= width || mouse.Y < 0 || mouse.Y >= a.tabsHeight(width) {
		return false
	}
	switch mouse.Button {
	case tea.MouseWheelUp, tea.MouseWheelLeft:
		a.tabScroll(-1)
	case tea.MouseWheelDown, tea.MouseWheelRight:
		a.tabScroll(1)
	}
	return true
}
