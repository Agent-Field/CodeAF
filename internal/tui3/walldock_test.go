package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// dockRowY is the frame row the keys were drawn on, found the way the
// pointer finds it (view.go's [app.chromeAt]).
func dockRowY(t *testing.T, a *app) int {
	t.Helper()
	for y := a.height - 1; y >= 0; y-- {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeStatus {
			return y
		}
	}
	t.Fatal("no keys row on the frame")
	return -1
}

// dockKeysRow lays the keys row out at width and returns it plain.
func dockKeysRow(a *app, width int) string {
	a.width = width
	return plain(strings.Join(a.statusRows(width), "\n"))
}

// ONE CONVERSATION IS NOT A MAP: the dock waits for a second.
func TestDockIsAbsentWithOneConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace, a.title = "/tmp/lab/this-one.jsonl", "/tmp/lab", "Shipping the parser"
	emptyMachine(a)
	a.width, a.height = 120, 30
	row := dockKeysRow(a, 120)
	if strings.Contains(row, "▦") || a.dock.wall.pressable() || a.dock.label.pressable() || len(a.dock.cells) != 0 {
		t.Fatalf("a dock with one conversation: %q %+v", row, a.dock)
	}
}

// THREE CONVERSATIONS ARE THREE CELLS, in the strip's order, each painted by
// what it is doing, with the one in front as ▣.
func TestDockDrawsEveryConversationInStripOrder(t *testing.T) {
	a, _, _ := tabApp(t)
	tabs := append([]chatTab(nil), a.dockTabs()...)
	if len(tabs) != 3 {
		t.Fatalf("%d conversations", len(tabs))
	}
	// One of the two behind is working and the other waits on a person.
	var working, asking string
	for _, tab := range tabs {
		if tab.here {
			continue
		}
		held := a.behind[tab.key]
		if held == nil || held.watch == nil {
			t.Fatalf("tab %q is not held", tab.word)
		}
		// AN INERT WATCHER, as the strip's own signal tests use (tabsignal_test.go's
		// [signalLab]). The live one's goroutine stores its first reading of the
		// agent when it starts, and a flag set here before that could be put
		// back to idle under the second render: red 4 times in 30 on Spark.
		held.watch = &behindWatch{key: tab.key, agent: held.conv.Agent}
		if working == "" {
			held.watch.turning.Store(true)
			working = tab.key
		} else {
			held.watch.waits.Store(true)
			asking = tab.key
		}
	}
	a.width = 120
	painted := strings.Join(a.statusRows(120), "")
	row := plain(painted)
	if !strings.Contains(row, "▦ ") {
		t.Fatalf("no dock: %q", row)
	}
	if len(a.dock.cells) != 3 {
		t.Fatalf("%d cells: %+v", len(a.dock.cells), a.dock.cells)
	}
	for i, cell := range a.dock.cells {
		if cell.tab.key != tabs[i].key {
			t.Fatalf("cell %d is %q, the strip has %q there", i, cell.tab.word, tabs[i].word)
		}
		got := ansi.Cut(row, cell.span.from, cell.span.to)
		want := "■"
		if cell.tab.here {
			want = "▣"
		}
		if got != want {
			t.Fatalf("cell %d at %v reads %q, want %q\n%q", i, cell.span, got, want, row)
		}
		// The hue is the strip's own for that state.
		var ink string
		switch {
		case cell.tab.here:
			ink = a.pal.ink(want)
		case cell.tab.key == working:
			ink = a.pal.accent(want)
		case cell.tab.key == asking:
			ink = a.pal.warn(want)
		}
		if !strings.Contains(painted, ink) {
			t.Fatalf("cell %d is not painted %q", i, ink)
		}
	}
	if got := ansi.Cut(row, a.dock.wall.from, a.dock.wall.to); got != "▦" {
		t.Fatalf("the wall's mark is not where it was recorded: %q", got)
	}
	// THE DOOR IS `▦ All`, SPELLED AS THE STRIP SPELLS IT, AND ONE BUTTON: the
	// recorded span holds the glyph and the word, and the glyph is its first cell.
	if !a.dock.label.pressable() || ansi.Cut(row, a.dock.label.from, a.dock.label.to) != "▦ "+tabWallWord {
		t.Fatalf("the door is not where it was recorded: %q %+v", row, a.dock.label)
	}
	if !strings.Contains(painted, a.pal.dim(" "+dockLabel)) {
		t.Fatalf("the word is not dim: %q", painted)
	}
	if a.dock.label.from != a.dock.wall.from {
		t.Fatalf("the glyph does not lead the door: label %+v wall %+v", a.dock.label, a.dock.wall)
	}
	// The ASCII floor spells the state in the character.
	a.linear = true
	row = dockKeysRow(a, 120)
	var marks string
	for _, cell := range a.dock.cells {
		marks += ansi.Cut(row, cell.span.from, cell.span.to)
	}
	if ansi.Cut(row, a.dock.wall.from, a.dock.wall.to) != "#" || strings.Count(marks, "@") != 1 || !strings.Contains(marks, "o") || !strings.Contains(marks, "!") {
		t.Fatalf("the linear dock: %q marks %q", row, marks)
	}
}

// A HAND ON THE DOCK: the hint slot names what is under it, a press on a
// cell brings that conversation to the front, and a press on ▦ opens the wall.
func TestDockHoverNamesAndPressGoes(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = frame(a)
	y := dockRowY(t, a)
	if len(a.dock.cells) != 3 {
		t.Fatalf("the frame drew %d cells", len(a.dock.cells))
	}
	var target dockCell
	for _, cell := range a.dock.cells {
		if !cell.tab.here {
			target = cell
			break
		}
	}
	a.setHover(target.span.from, y)
	if a.hot.kind != hoverDockCell || a.hot.key != target.tab.key {
		t.Fatalf("the hover on a cell is %+v", a.hot)
	}
	before := append([]dockCell(nil), a.dock.cells...)
	lines := screenLines(a)
	row := lines[y]
	if !strings.HasPrefix(row, " "+dockCellHint(target.tab)) {
		t.Fatalf("the hovered cell does not say %q: %q", dockCellHint(target.tab), row)
	}
	for i, cell := range a.dock.cells {
		if cell.span != before[i].span {
			t.Fatal("the dock moved under the pointer")
		}
	}
	a.setHover(a.dock.wall.from, y)
	if row := screenLines(a)[y]; !strings.HasPrefix(row, " "+dockChatsWord) {
		t.Fatalf("the hovered wall mark: %q", row)
	}
	a.setHover(a.dock.label.from, y)
	if a.hot.kind != hoverDockLabel {
		t.Fatalf("the hover on the word is %+v", a.hot)
	}
	if row := screenLines(a)[y]; !strings.HasPrefix(row, " "+dockChatsWord) {
		t.Fatalf("the hovered word: %q", row)
	}
	for _, cell := range a.dock.cells {
		if !cell.tab.here {
			continue
		}
		a.setHover(cell.span.from, y)
		want := cell.tab.full + hintSegment + "you are here"
		if row := screenLines(a)[y]; !strings.HasPrefix(row, " "+want) {
			t.Fatalf("the square in front says %q, want %q", row, want)
		}
		break
	}

	if _, took := a.dockPress(target.span.from, y); !took {
		t.Fatal("the dock did not take a press on a cell")
	}
	if a.frontTabKey() != target.tab.key {
		t.Fatalf("the press brought %q forward, want %q", a.frontTabKey(), target.tab.key)
	}
	_ = frame(a)
	y = dockRowY(t, a)
	if _, took := a.dockPress(a.dock.label.from+2, y); !took || !a.wall.on {
		t.Fatal("the word did not open the wall")
	}
	a.closeWall()
	_ = frame(a)
	y = dockRowY(t, a)
	if _, took := a.dockPress(a.dock.wall.from, y); !took || !a.wall.on {
		t.Fatal("the wall's mark did not open the wall")
	}
	// A press on the blank between the keys and the dock is nothing.
	a.closeWall()
	_ = frame(a)
	y = dockRowY(t, a)
	start := a.dock.wall.from
	if a.dock.label.pressable() {
		start = a.dock.label.from
	}
	if _, took := a.dockPress(start-2, y); took {
		t.Fatal("the blank before the dock took a press")
	}
}

// THE ROW NEVER OVERFLOWS, and the phone's deck carries no dock.
func TestDockKeysRowFitsEveryWidth(t *testing.T) {
	a, _, _ := tabApp(t)
	for _, width := range []int{60, 80, 120, 180} {
		row := dockKeysRow(a, width)
		if w := ansi.StringWidth(row); w != width {
			t.Fatalf("at %d the keys row is %d wide: %q", width, w, row)
		}
		for _, cell := range a.dock.cells {
			if cell.span.to > width-1 {
				t.Fatalf("at %d a cell lies past the row: %+v", width, cell)
			}
		}
		if width >= 80 && !a.dock.wall.pressable() {
			t.Fatalf("at %d no dock: %q", width, row)
		}
	}
	row := dockKeysRow(a, 44)
	if strings.Contains(row, "▦") || a.dock.wall.pressable() || len(a.dock.cells) != 0 {
		t.Fatalf("the phone has a dock: %q", row)
	}
}

// THE CAP COUNTS WHAT IT CANNOT SPELL, and the one in front stays inside it.
func TestDockLayoutCapsAndKeepsTheFront(t *testing.T) {
	tabs := make([]chatTab, 20)
	tabs[17].here = true
	from, count, hidden, label, ok := dockLayout(tabs, 80)
	if !ok || !label || count != dockCap || hidden != 20-dockCap || from > 17 || from+count <= 17 {
		t.Fatalf("from %d count %d hidden %d label %v ok %v", from, count, hidden, label, ok)
	}
	if _, _, _, _, ok := dockLayout(tabs, 3); ok {
		t.Fatal("a dock was fitted into three cells")
	}
	if _, _, _, _, ok := dockLayout(tabs[:1], 80); ok {
		t.Fatal("one conversation made a dock")
	}
}

// THE WORD GOES BEFORE ANY CELL DOES. A room that holds the squares and not
// the word draws the squares. A room that holds both draws the word. A room
// that cannot hold two squares draws nothing.
func TestDockLabelDropsBeforeACell(t *testing.T) {
	tabs := make([]chatTab, 3)
	tabs[1].here = true
	cells := dockWidth(3, 0, false)
	from, count, hidden, label, ok := dockLayout(tabs, cells)
	if !ok || label || count != 3 || from != 0 || hidden != 0 {
		t.Fatalf("the word stayed or a cell went: from %d count %d hidden %d label %v ok %v", from, count, hidden, label, ok)
	}
	from, count, hidden, label, ok = dockLayout(tabs, dockWidth(3, 0, true))
	if !ok || !label || count != 3 || hidden != 0 {
		t.Fatalf("the wide row dropped the word: from %d count %d hidden %d label %v ok %v", from, count, hidden, label, ok)
	}
	if _, _, _, _, ok := dockLayout(tabs, cells-1); ok {
		t.Fatal("a dock was drawn below two cells of room")
	}
}

// EACH KIND OF CELL SAYS WHAT A PRESS DOES, in the words the hint line uses.
func TestDockHoverHintPerCellKind(t *testing.T) {
	idle := chatTab{full: "Shipping the parser", signal: tabIdle}
	if got := dockCellHint(idle); got != "Go to Shipping the parser"+hintSegment+"idle"+hintSegment+"click" {
		t.Fatalf("idle: %q", got)
	}
	running := chatTab{word: "price scrape", signal: tabWorking}
	if got := dockCellHint(running); got != "Go to price scrape"+hintSegment+"running"+hintSegment+"click" {
		t.Fatalf("running: %q", got)
	}
	waiting := chatTab{full: "Refactor the rail", signal: tabNeedsPerson}
	if got := dockCellHint(waiting); got != "Go to Refactor the rail"+hintSegment+"waiting on you"+hintSegment+"click" {
		t.Fatalf("waiting: %q", got)
	}
	here := chatTab{full: "Shipping the parser", here: true, signal: tabWorking}
	if got := dockCellHint(here); got != "Shipping the parser"+hintSegment+"you are here" {
		t.Fatalf("here: %q", got)
	}
	if dockChatsWord != dockWallWord {
		t.Fatalf("the word and the glyph say %q", dockChatsWord)
	}
}

// A PRESS ON THE CONVERSATION ALREADY IN FRONT DOES NOTHING: no switch, no
// wall, nothing to redraw.
func TestDockPressOnTheOneInFrontDoesNothing(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = frame(a)
	y := dockRowY(t, a)
	front := a.frontTabKey()
	for _, cell := range a.dock.cells {
		if !cell.tab.here {
			continue
		}
		cmd, took := a.dockPress(cell.span.from, y)
		if !took || cmd != nil || a.frontTabKey() != front || a.wall.on {
			t.Fatalf("a press on the front: took %v cmd %v front %q wall %v", took, cmd != nil, a.frontTabKey(), a.wall.on)
		}
		return
	}
	t.Fatal("no cell for the conversation in front")
}

// THE DOCK'S HOVER IS THE WALL'S HOVER: the cell under the pointer sits on
// the same cursor ground a tile and a wall button wear under it.
func TestDockHoverWearsTheWallsHoverGround(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = frame(a)
	y := dockRowY(t, a)
	cell := a.dock.cells[0]
	a.setHover(cell.span.from, y)
	lines := strings.Split(frame(a), "\n")
	ground := a.pal.cursor(a.pal.ink(a.dockGlyph(cell.tab)), 0)
	if !strings.Contains(lines[y], ground) {
		t.Fatalf("the hovered cell is not on the cursor ground: %q", lines[y])
	}
	// THE DOOR IS ONE GROUND FROM ANY CELL OF IT: the glyph, the space and the
	// word light together, because a press on any of them opens the wall.
	door := a.dock.label
	word := a.pal.cursor(a.pal.ink(a.dockWallGlyph()+" "+dockLabel), 0)
	for x := door.from; x < door.to; x++ {
		a.setHover(x, y)
		lines = strings.Split(frame(a), "\n")
		if !strings.Contains(lines[y], word) {
			t.Fatalf("the pointer at %d of the door %+v does not light all of it: %q", x, door, lines[y])
		}
	}
	if wall := wallButtonPaint(a.pal, wallButton{label: "x"}, true); !strings.Contains(wall, a.pal.cursor(" ", 0)[:strings.Index(a.pal.cursor(" ", 0), " ")]) {
		t.Fatalf("the wall's hover is not the cursor ground: %q", wall)
	}
}

// ONE WORD, ONE DOOR. The dock's door to the wall is spelled as the strip's
// door to it is, `▦ All`, and both open the wall; the nav's `chats` over
// them is the way back to the conversations and never the wall. The dock
// used to say `chats` too, so one word on one frame led two places.
func TestTheDocksDoorIsSpelledAsTheStripsDoor(t *testing.T) {
	a, _, _ := tabApp(t)
	a.width, a.height = 120, 30
	rows := screenLines(a)
	y := dockRowY(t, a)
	strip := strings.TrimSpace(ansi.Cut(rows[tabStripRow], a.wall.door.from, a.wall.door.to))
	dock := ansi.Cut(rows[y], a.dock.label.from, a.dock.label.to)
	if strip != "▦ "+tabWallWord || dock != strip {
		t.Fatalf("the strip's door says %q and the dock's says %q", strip, dock)
	}
	if strings.Contains(ansi.Cut(rows[y], a.dock.label.from-8, a.width), "chats") {
		t.Fatalf("the dock still carries the nav's word: %q", rows[y])
	}
	for _, press := range []func() (tea.Cmd, bool){
		func() (tea.Cmd, bool) { return a.dockPress(a.dock.label.to-1, y) },
		func() (tea.Cmd, bool) { return a.tabPress(a.wall.door.from+1, tabStripRow) },
	} {
		if _, took := press(); !took || !a.wall.on {
			t.Fatal("a press on `▦ All` did not open the wall")
		}
		a.closeWall()
		rows = screenLines(a)
		y = dockRowY(t, a)
	}
	for _, span := range a.tabs {
		if span.id != pageChats {
			continue
		}
		drive(t, a, tea.MouseClickMsg{X: span.from + 1, Y: navRow, Button: tea.MouseLeft})
		if a.wall.on {
			t.Fatal("the nav's `chats` opened the wall")
		}
	}
}
