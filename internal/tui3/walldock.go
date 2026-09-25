package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ── THE DOCK: EVERY OPEN CONVERSATION, UNDER THE BOX ────────────────────────
//
// The wall's door used to be a `⊞` at the right end of the tab strip, which is
// the far end of the frame from where a person's hands are. The owner asked for
// it to come down to where people type, so the row under the box ends in a
// small map of every conversation this window has open:
//
//	› say what you want done
//	  alt+k chats · / commands                                   ▦ ▪▪▣▪
//
// `▦` is the wall, and a press on it opens the wall. Each cell after it is one
// conversation, in the strip's own order ([app.tabList]), painted by what it
// is doing: the live hue while it works, the warning hue while it waits on a
// person, dim at rest. The one in front is `▣` in ink. A press on a cell goes
// to that conversation by the strip's own door ([app.tabGo]), so a cell can
// never do something its tab would not.
//
// IT READS MEMORY AND NOTHING ELSE. The cells are the strip's list and the
// strip's signals ([app.tabSignalFor]), both of which are the keeper's cached
// facts, because this row is drawn on every frame and the frame may not touch
// the disk (framedisk_law_test.go).
//
// IT IS DRAWN ONLY WHERE IT HAS SOMETHING TO SAY. One conversation is not a
// map, so the dock waits for a second (the emptiness law). It is drawn only on
// the row the keys are drawn on, so never on the wall itself, never on the
// phone's deck, and never on the new-chat frame, which has no keys row.
//
// AND IT TAKES ONLY WHAT THE KEYS LEFT. The keys are how a person drives this
// frame from the keyboard, and the aliveness on a frame with no seam is the
// one fact a frame may never lose (footswap.go), so both are laid out first
// and the dock is fitted into what remains: fewer cells and a `+N` count
// first, and then no dock at all. The telemetry is on the seam, a row the dock
// never draws on, so no number is ever given up for it.

// dockCap is how many conversations the dock spells before it counts the rest.
const dockCap = 12

// dockFloor is the fewest cells a narrowed dock will draw. Below it the dock is
// dropped whole: one cell and a count is not a map of anything.
const dockFloor = 2

// dockWallWord is what the hint slot says while the pointer rests on `▦`.
// It names teams because the view it opens is where a first team is made:
// with no team yet, `▦ All` is the only door on the strip that leads to one,
// and `Conversations` alone gave a person no reason to look there.
const dockWallWord = "Every open conversation, and your teams" + hintSegment + wallOpenKey

// dockCell is one conversation's cell as it was drawn: its column and its tab.
type dockCell struct {
	span hudSpan
	tab  chatTab
}

// dockMap is where the dock landed on the last frame, written by the draw and
// read by the pointer (render.go's [hudSpan] bargain): a press resolves against
// what was painted, never against a second computation of it.
type dockMap struct {
	wall  hudSpan
	cells []dockCell
}

// dockClear forgets where the dock was drawn, on every frame before the keys
// row is laid out, so a row that draws no dock answers for none.
func (a *app) dockClear() {
	a.dock.wall = hudSpan{}
	a.dock.cells = a.dock.cells[:0]
}

// dockTabs is the conversations the dock draws: the strip's list, less the
// new-chat placeholder and the work tab, which are not conversations. The
// slice is the dock's own, refilled in place so a frame allocates nothing
// once the dock has been drawn once.
func (a *app) dockTabs() []chatTab {
	// A WINDOW WITH ONE CONVERSATION HAS NO DOCK, and it is told so before the
	// strip's list is built: a tab is the conversation in front, one this
	// process holds, or one still on the recency stack ([app.tabList]), and the
	// one in front is always on that stack ([app.rememberOpen] is every road to
	// the front), so with nothing held and at most one on it there cannot be two. That is
	// most frames, scrolling included, and the list is allocations the scroll's
	// own law counts (inputsmooth_test.go).
	if len(a.behind) == 0 && len(a.prev) < 2 {
		a.dockList = a.dockList[:0]
		return a.dockList
	}
	out := a.dockList[:0]
	for _, tab := range a.tabList() {
		if tab.start || tab.work {
			continue
		}
		out = append(out, tab)
	}
	a.dockList = out
	return out
}

// dockLayout is which cells a dock of at most room cells draws: the first
// tab shown, how many, and how many it could not spell. The one in front is
// always inside the window, and the order is never changed. ok is false when
// no dock fits, or when there is nothing to map.
func dockLayout(tabs []chatTab, room int) (from, count, hidden int, ok bool) {
	n := len(tabs)
	if n < 2 {
		return 0, 0, 0, false
	}
	front := 0
	for i, tab := range tabs {
		if tab.here {
			front = i
		}
	}
	for count = min(n, dockCap); count >= dockFloor; count-- {
		hidden = n - count
		if dockWidth(count, hidden) > room {
			continue
		}
		from = 0
		if front >= count {
			from = front - count + 1
		}
		return from, count, hidden, true
	}
	return 0, 0, 0, false
}

// dockWidth is the cells a dock of count cells and hidden more takes.
func dockWidth(count, hidden int) int {
	// Each cell is a full square and a space: `■ ■ ▣`, big enough to aim
	// at, one cell of air so neighbours do not run into a bar.
	w := 2 + 2*count - 1
	if hidden > 0 {
		w += 2 + len(strconv.Itoa(hidden))
	}
	return w
}

// dockGlyph is one cell's mark. The wide glyphs carry state by colour alone,
// so the ASCII floor spells it in the character instead: `o` working, `!`
// waiting on a person, `.` at rest, `@` in front.
func (a *app) dockGlyph(tab chatTab) string {
	ascii := a.pal.ascii || a.linear
	switch {
	case tab.here && ascii:
		return "@"
	case tab.here:
		return "▣"
	case !ascii:
		return "■"
	case tab.signal == tabNeedsPerson:
		return "!"
	case tab.signal == tabWorking:
		return "o"
	}
	return "."
}

// dockWallGlyph is the wall's mark at the dock's left.
func (a *app) dockWallGlyph() string {
	if a.pal.ascii || a.linear {
		return "#"
	}
	return "▦"
}

// dockPaint draws the dock with its first cell at column at, and records where
// every piece of it landed. The wall's mark wears the active team's colour,
// so the dock also says which team this window is in.
func (a *app) dockPaint(tabs []chatTab, from, count, hidden, at int) string {
	a.dock.wall = hudSpan{from: at, to: at + 1}
	var b strings.Builder
	wall := a.dockWallGlyph()
	switch {
	case a.hot.kind == hoverDockWall:
		b.WriteString(a.pal.cursor(a.pal.ink(wall), 0))
	default:
		ink := a.pal.dim
		if sp, ok := a.teamActive(); ok {
			if pen := a.pal.teamInk(sp.HueSpec()); pen != nil && !a.linear {
				ink = pen
			}
		}
		b.WriteString(ink(wall))
	}
	b.WriteString(" ")
	cells := a.dock.cells[:0]
	for i, tab := range tabs[from : from+count] {
		col := at + 2 + 2*i
		if i > 0 {
			b.WriteString(" ")
		}
		cells = append(cells, dockCell{span: hudSpan{from: col, to: col + 1}, tab: tab})
		glyph := a.dockGlyph(tab)
		switch {
		case a.hot.kind == hoverDockCell && a.hot.key == tab.key:
			b.WriteString(a.pal.cursor(a.pal.ink(glyph), 0))
		case tab.here:
			b.WriteString(a.pal.ink(glyph))
		default:
			b.WriteString(a.pal.tabSignalInk(tab.signal, glyph))
		}
	}
	a.dock.cells = cells
	if hidden > 0 {
		b.WriteString(a.pal.dim(" +" + strconv.Itoa(hidden)))
	}
	return b.String()
}

// dockHoverWords is what the hint slot says while the pointer rests on the
// dock, and "" when it rests anywhere else: the wall's name and key over `▦`,
// and a conversation's name and what it is doing over its cell. The strip's
// own door to the wall (chattabs.go) is explained here too, in the same words.
func (a *app) dockHoverWords() string {
	// THE TEAM'S OWN DOORS EXPLAIN THEMSELVES HERE TOO: the manager's place on
	// the strip, the team chip, and every row and word of the Traffic
	// (teammanager.go, teamrailpointer.go).
	if words := a.teamHoverWords(); words != "" {
		return words
	}
	switch a.hot.kind {
	case hoverTab:
		if a.wall.door.pressable() && a.hot.index == a.wall.door.from {
			return dockWallWord
		}
	case hoverDockWall:
		if a.dock.wall.pressable() {
			return dockWallWord
		}
	case hoverDockCell:
		for _, cell := range a.dock.cells {
			if cell.tab.key != a.hot.key {
				continue
			}
			name := cell.tab.full
			if name == "" {
				name = cell.tab.word
			}
			switch cell.tab.signal {
			case tabWorking:
				name += hintSegment + "running"
			case tabNeedsPerson:
				name += hintSegment + "needs you"
			}
			if cell.tab.here {
				return name + hintSegment + "here"
			}
			// What a press does, as every other hint says it.
			return name + hintSegment + "click opens it"
		}
	}
	// THE MANAGER'S TASKS' HEADER IS THE WAY BACK TO THE TRAFFIC (teamrail.go).
	if a.hot.kind == hoverRailDoor && a.trafficOn() {
		return "Back to the traffic" + hintSegment + railStowKey
	}
	// AND A JUMP THAT FOUND NOTHING SAYS SO, for a moment (teamjump.go).
	return a.trafficJumpWords()
}

// dockAt is the dock's piece under column x on the keys row, as a hover.
func (a *app) dockAt(x int) (hoverAt, bool) {
	if a.dock.wall.holds(x) {
		return hoverAt{kind: hoverDockWall}, true
	}
	for _, cell := range a.dock.cells {
		if cell.span.holds(x) {
			return hoverAt{kind: hoverDockCell, key: cell.tab.key}, true
		}
	}
	return hoverAt{}, false
}

// dockPress is a press on the keys row, and it takes only the dock's own
// cells: `▦` opens the wall, a cell goes to its conversation, and the one in
// front is already where the press would go.
func (a *app) dockPress(x, y int) (tea.Cmd, bool) {
	if a.wall.on || a.copy.on || a.rew.on {
		return nil, false
	}
	// Laying the chrome out again is what records the dock for this frame, so
	// the row is resolved first and the columns read after it.
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeStatus {
		return nil, false
	}
	if width, _ := a.size(); layoutTier(width) == tierPhone {
		return nil, false
	}
	if a.dock.wall.holds(x) {
		return a.openWall(), true
	}
	for _, cell := range a.dock.cells {
		if !cell.span.holds(x) {
			continue
		}
		if cell.tab.here {
			return nil, true
		}
		return a.tabGo(cell.tab), true
	}
	return nil, false
}

// dockRow lays the dock into the keys row once the keys are fitted: end is
// the column the dock must finish before, and used the cells the keys took
// from the left. It returns the dock painted and its width, both empty when
// no dock fits or none is due.
func (a *app) dockRow(width, end, used int) (string, int) {
	if a.wall.on || layoutTier(width) == tierPhone {
		return "", 0
	}
	free := end - 1
	if used > 0 {
		free -= used + hudGap
	}
	tabs := a.dockTabs()
	from, count, hidden, ok := dockLayout(tabs, free)
	if !ok {
		return "", 0
	}
	w := dockWidth(count, hidden)
	return a.dockPaint(tabs, from, count, hidden, end-w), w
}
