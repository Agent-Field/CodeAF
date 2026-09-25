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
//	  alt+k chats · / commands                              ▦ All ▪▪▣▪
//
// `▦ All` is the wall's door, spelled exactly as the strip's own door to it is
// spelled (chattabs.go's [tabWallWord]), and it is ONE BUTTON: the glyph and the
// word are one target, and the pointer's ground covers both. Each cell after it
// is one conversation, in the strip's own order ([app.tabList]), painted by what
// it is doing: the live hue while it works, the warning hue while it waits on a
// person, dim at rest. The one in front is `▣` in ink. A press on a cell goes
// to that conversation by the strip's own door ([app.tabGo]), so a cell can
// never do something its tab would not.
//
// ONE WORD, ONE DOOR, EVERYWHERE. The word here used to be `chats`, and the
// nav's `chats` sits over the same frame and goes somewhere else: back to the
// conversation in front (place_chats.go). Two presses on one word that land in
// two places is a word a person cannot learn, so this door says what it opens,
// in the words the strip already uses for it.
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
// and the dock is fitted into what remains: the word `All` first, then
// fewer cells and a `+N` count, and then no dock at all. The telemetry is on
// the seam, a row the dock never draws on, so no number is ever given up for it.

// dockCap is how many conversations the dock spells before it counts the rest.
const dockCap = 12

// dockFloor is the fewest cells a narrowed dock will draw. Below it the dock is
// dropped whole: one cell and a count is not a map of anything.
const dockFloor = 2

// dockLabel is the word after the wall's glyph, the strip's own word for the
// same door ([tabWallWord]). It is ASCII, so its length is its width.
const dockLabel = tabWallWord

// dockWallWord is what the hint slot says while the pointer rests on the
// strip's own door to the wall (`▦ All`), and the dock's `▦ All` says
// [dockChatsWord], which is the same sentence: they are the same door.
//
// IT SAYS GRID AND TABS, NOT "EVERY CONVERSATION", because the nav's `chats`
// sits right over it on every page and is the place for every conversation
// (topnav.go). Two doors a row apart that both said "every conversation"
// would be one door drawn twice; this one is the tabs open here, side by side.
const dockWallWord = "The grid of your open tabs, and your teams" + hintSegment + wallOpenKey

// dockChatsWord is what the hint slot says while the pointer rests on the
// dock's `▦ All`.
const dockChatsWord = dockWallWord

// dockCell is one conversation's cell as it was drawn: its column and its tab.
type dockCell struct {
	span hudSpan
	tab  chatTab
}

// dockMap is where the dock landed on the last frame, written by the draw and
// read by the pointer (render.go's [hudSpan] bargain): a press resolves against
// what was painted, never against a second computation of it. label is the
// whole door, glyph and word, empty on a row that dropped the word; wall is
// the glyph's own cell, which is the whole door on a row with no word.
type dockMap struct {
	label hudSpan
	wall  hudSpan
	cells []dockCell
}

// dockClear forgets where the dock was drawn, on every frame before the keys
// row is laid out, so a row that draws no dock answers for none.
func (a *app) dockClear() {
	a.dock.label = hudSpan{}
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
// tab shown, how many, how many it could not spell, and whether the word
// `All` fits after the glyph. The one in front is always inside the window, and
// the order is never changed. ok is false when no dock fits, or when there
// is nothing to map.
//
// THE WORD DROPS FIRST. A dock that still names every cell it can is worth
// more than a word in front of a shorter one, so the word is tried only
// beside the widest window of cells that fits, and given up before any cell.
func dockLayout(tabs []chatTab, room int) (from, count, hidden int, label, ok bool) {
	n := len(tabs)
	if n < 2 {
		return 0, 0, 0, false, false
	}
	front := 0
	for i, tab := range tabs {
		if tab.here {
			front = i
		}
	}
	place := func(count, hidden int) (int, int) {
		from := 0
		if front >= count {
			from = front - count + 1
		}
		return from, hidden
	}
	widest := min(n, dockCap)
	hidden = n - widest
	if dockWidth(widest, hidden, true) <= room {
		from, hidden = place(widest, hidden)
		return from, widest, hidden, true, true
	}
	for count = widest; count >= dockFloor; count-- {
		hidden = n - count
		if dockWidth(count, hidden, false) > room {
			continue
		}
		from, hidden = place(count, hidden)
		return from, count, hidden, false, true
	}
	return 0, 0, 0, false, false
}

// dockWidth is the cells a dock of count cells and hidden more takes. label
// is the word `All` and the space between it and the wall's glyph.
func dockWidth(count, hidden int, label bool) int {
	// Each cell is a full square and a space: `■ ■ ▣`, big enough to aim
	// at, one cell of air so neighbours do not run into a bar.
	w := 2 + 2*count - 1
	if hidden > 0 {
		w += 2 + len(strconv.Itoa(hidden))
	}
	if label {
		w += 1 + len(dockLabel)
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

// dockPaint draws the dock with its first piece at column at, and records
// where every piece of it landed. The wall's mark wears the active team's
// colour, so the dock also says which team this window is in. The word after
// it stays dim: it names the door, and the squares carry the state.
//
// THE DOOR IS ONE BUTTON: under the pointer the ground covers the glyph, the
// space and the word, which is exactly the span a press on it takes.
func (a *app) dockPaint(tabs []chatTab, from, count, hidden int, label bool, at int) string {
	var b strings.Builder
	cursor := at
	a.dock.wall = hudSpan{from: cursor, to: cursor + 1}
	a.dock.label = hudSpan{}
	door := 1
	if label {
		door += 1 + len(dockLabel)
		a.dock.label = hudSpan{from: cursor, to: cursor + door}
	}
	wall := a.dockWallGlyph()
	switch {
	case a.hot.kind == hoverDockWall || a.hot.kind == hoverDockLabel:
		word := wall
		if label {
			word += " " + dockLabel
		}
		b.WriteString(a.pal.cursor(a.pal.ink(word), 0))
	default:
		ink := a.pal.dim
		if sp, ok := a.teamActive(); ok {
			if pen := a.pal.teamInk(sp.HueSpec()); pen != nil && !a.linear {
				ink = pen
			}
		}
		b.WriteString(ink(wall))
		if label {
			b.WriteString(a.pal.dim(" " + dockLabel))
		}
	}
	cursor += door - 1
	b.WriteString(" ")
	cells := a.dock.cells[:0]
	for i, tab := range tabs[from : from+count] {
		col := cursor + 2 + 2*i
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

// dockCellHint is the hint line for one cell. The one in front says where
// you already are. Every other cell says where a press goes, what that
// conversation is doing, and that a click does it.
func dockCellHint(tab chatTab) string {
	name := tab.full
	if name == "" {
		name = tab.word
	}
	if tab.here {
		return name + hintSegment + "you are here"
	}
	state := "idle"
	switch tab.signal {
	case tabWorking:
		state = "running"
	case tabNeedsPerson:
		state = "waiting on you"
	}
	return "Go to " + name + hintSegment + state + hintSegment + "click"
}

// dockHoverWords is what the hint slot says while the pointer rests on the
// dock, and "" when it rests anywhere else: `The grid of your open tabs, and
// your teams · alt+v` over `▦ All`, and [dockCellHint] over a cell. The strip's own door
// to the wall (chattabs.go) is explained here too, in its own sentence.
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
		if words := a.stripHoverWords(); words != "" {
			return words
		}
	case hoverDockLabel, hoverDockWall:
		if (a.hot.kind == hoverDockLabel && a.dock.label.pressable()) || (a.hot.kind == hoverDockWall && a.dock.wall.pressable()) {
			return dockChatsWord
		}
	case hoverDockCell:
		for _, cell := range a.dock.cells {
			if cell.tab.key != a.hot.key {
				continue
			}
			return dockCellHint(cell.tab)
		}
	}
	// THE MANAGER'S TASKS' HEADER IS THE WAY BACK TO THE TRAFFIC (teamrail.go).
	if a.hot.kind == hoverRailDoor && a.trafficOn() {
		return "Back to the traffic" + hintSegment + railStowKey
	}
	// AND A JUMP THAT FOUND NOTHING SAYS SO, for a moment (teamjump.go).
	return a.trafficJumpWords()
}

// stripHoverWords is what the hint slot says while the pointer rests on a
// piece of the strip that has no sentence of its own elsewhere: a tab, its
// `×`, the new-chat `+` and the scroll arrows. EVERY DOOR ON THE ROW SAYS WHAT
// IT DOES, so none of them is a mark a person has to press to learn about.
// A tab says what its square in the dock says ([dockCellHint]), because they
// are one conversation and one door.
func (a *app) stripHoverWords() string {
	hit, ok := a.hotTab()
	if !ok {
		return ""
	}
	name := hit.tab.full
	if name == "" {
		name = hit.tab.word
	}
	switch hit.kind {
	case tabHere, tabOther:
		if hit.kind == tabHere && (a.roomOpen() || a.startingChat()) {
			return "Back to " + name + hintSegment + "click"
		}
		return dockCellHint(hit.tab)
	case tabClose:
		words := "Close this tab" + hintSegment + "the work keeps running"
		if hit.tab.here {
			return words + hintSegment + a.chords.say(closeTabChord)
		}
		return words + hintSegment + "click"
	case tabNew:
		return "New chat" + hintSegment + a.chords.say(newChatChord)
	case tabScrollLeft:
		return "More tabs to the left" + hintSegment + "click"
	case tabScrollRight:
		return "More tabs to the right" + hintSegment + "click"
	}
	return ""
}

// dockAt is the dock's piece under column x on the keys row, as a hover.
func (a *app) dockAt(x int) (hoverAt, bool) {
	if a.dock.label.holds(x) {
		return hoverAt{kind: hoverDockLabel}, true
	}
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
// cells: `▦ All` opens the wall, a cell goes to its conversation, and
// the one in front is already where the press would go.
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
	if a.dock.label.holds(x) || a.dock.wall.holds(x) {
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
	from, count, hidden, label, ok := dockLayout(tabs, free)
	if !ok {
		return "", 0
	}
	w := dockWidth(count, hidden, label)
	return a.dockPaint(tabs, from, count, hidden, label, end-w), w
}
