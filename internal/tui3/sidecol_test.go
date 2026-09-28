package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// sideDoorOf is the screen cell of the first door on the column's own rows
// that does act kind, failing the test when the column draws none.
func sideDoorOf(t *testing.T, a *app, kind int) (int, int) {
	t.Helper()
	view, _ := a.railDrawnView(a.viewHeight())
	for i, line := range view {
		if line.side == nil {
			continue
		}
		for _, d := range line.side.doors {
			if d.act.kind == kind {
				return a.railLeft() + ansi.StringWidth(railSeam) + d.span.from, a.topHeight() + i
			}
		}
	}
	t.Fatalf("the column draws no door that does act %d", kind)
	return 0, 0
}

// sideRowOn is the screen cell of row key on the column, at a column of it no
// door covers, failing the test when the column does not draw it.
func sideRowOn(t *testing.T, a *app, key string) (int, int) {
	t.Helper()
	view, _ := a.railDrawnView(a.viewHeight())
	for i, line := range view {
		if line.side == nil || line.side.key != key {
			continue
		}
		for x := 0; x < a.railRoom(); x++ {
			if line.side.doorAt(x) < 0 {
				return a.railLeft() + ansi.StringWidth(railSeam) + x, a.topHeight() + i
			}
		}
	}
	t.Fatalf("the column does not draw row %q", key)
	return 0, 0
}

// sideBandFixture is a plain chat with two tasks waiting on the person, three
// failures nobody has opened, one running task and one landed one.
func sideBandFixture(t *testing.T) *app {
	t.Helper()
	a, _, _ := roomApp(t)
	for _, id := range []uint64{1, 2} {
		a.taskUpdate(update(id, "Port the parser "+itoa(int(id)), session.TaskUnverified,
			session.TaskNotice{Merge: mergeWordAborted, Branch: "task/p" + itoa(int(id))}))
	}
	// Each failure is seen running first, as live work is: a node whose first
	// news is a failure is one a reopened conversation replayed.
	for _, id := range []uint64{3, 4, 5} {
		a.taskUpdate(update(id, "Cut the goldens "+itoa(int(id)), session.TaskRunning, session.TaskNotice{}))
		a.taskUpdate(update(id, "Cut the goldens "+itoa(int(id)), session.TaskFailed,
			session.TaskNotice{Ending: session.TaskEndingSteps}))
	}
	a.taskUpdate(update(6, "Fix the loader", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(7, "Read the law", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	a.height = 30
	return a
}

// sideDrawn is the column's own rows as the frame lays them: the key of each
// line that is one of them, "" for every other line, and the raw text.
func sideDrawn(a *app) ([]string, []string) {
	view, _ := a.railDrawnView(a.viewHeight())
	raw := a.railRows(a.viewHeight())
	keys := make([]string, len(view))
	for i, line := range view {
		if line.side != nil {
			keys[i] = line.side.key
		}
	}
	return keys, raw
}

// THE BAND IS WHAT NEEDS THE PERSON, AMBER ONLY FOR WHAT IS BLOCKED ON THEM.
// Two tasks whose next step is the person's lead in amber, newest first; the
// failures follow with a ✗ in ordinary ink; three rows and then `+N more`;
// and nothing in the band is drawn again in the list under it.
func TestTheBandCarriesAsksInAmberAndFailuresInInk(t *testing.T) {
	a := sideBandFixture(t)
	keys, raw := sideDrawn(a)
	var band []int
	for i, k := range keys {
		if k != "" && k != sideHeadKey {
			band = append(band, i)
		}
	}
	want := []string{"task/2", "task/1", "fail/5", "band/more"}
	if len(band) != len(want) {
		t.Fatalf("the band drew %v, want %v:\n%s", keys, want, strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	for i, at := range band {
		if keys[at] != want[i] {
			t.Fatalf("band row %d is %q, want %q (all %v)", i, keys[at], want[i], keys)
		}
	}
	if more := plain(raw[band[3]]); !strings.Contains(more, "+2 more") {
		t.Fatalf("the band's last row is %q, want +2 more", more)
	}
	// THE PAINT. The palette's warn ink is on the asks and on nothing else.
	warn := a.pal.ask("Q")
	warn = warn[:strings.Index(warn, "Q")]
	if warn == "" {
		t.Fatal("the test palette paints no warn ink, so this test would prove nothing")
	}
	for i, at := range band[:3] {
		amber := strings.Contains(raw[at], warn)
		if ask := i < 2; amber != ask {
			t.Fatalf("band row %q amber=%v, want %v:\n%q", keys[at], amber, ask, raw[at])
		}
	}
	if !strings.Contains(plain(raw[band[2]]), a.taskStateMark(a.tasks[5])+" Cut the goldens 5") {
		t.Fatalf("the failure row is %q", plain(raw[band[2]]))
	}
	// A RULE CLOSES THE BAND, and the list starts under it.
	if rule := plain(raw[band[3]+1]); !strings.Contains(rule, "────") {
		t.Fatalf("no rule under the band: %q", rule)
	}
	// NOT REPEATED BELOW.
	railOpenAll(a)
	for _, e := range a.railEntries() {
		if e.node != nil && e.node.id <= 5 {
			t.Fatalf("band item %d is drawn again in the list", e.node.id)
		}
	}
	// `+2 more` SHOWS THEM ALL, and `fewer` folds them back.
	x, y := sideRowOn(t, a, "band/more")
	railClick(t, a, x, y)
	if n := len(a.sideBand()); n != 5 {
		t.Fatalf("the band holds %d items", n)
	}
	sideRowOn(t, a, "fail/3")
	if more := plain(strings.Join(railText(a, a.viewHeight()), "\n")); !strings.Contains(more, "fewer") {
		t.Fatalf("the opened band offers no way back:\n%s", more)
	}
	x, y = sideRowOn(t, a, "band/more")
	railClick(t, a, x, y)
	if a.side.bandAll {
		t.Fatal("`fewer` did not fold the band back")
	}
	// OPENING A FAILURE IS LOOKING AT IT: it leaves the band and joins the
	// finished work.
	x, y = sideRowOn(t, a, "fail/5")
	railClick(t, a, x, y)
	if !a.roomOpen() || a.room.id != 5 {
		t.Fatalf("a press on the failure opened %d", roomID(a))
	}
	for _, item := range a.sideBand() {
		if item.key == "fail/5" {
			t.Fatal("the failure the person opened is still in the band")
		}
	}
	listed := false
	for _, e := range a.railEntries() {
		listed = listed || (e.node != nil && e.node.id == 5)
	}
	if !listed {
		t.Fatal("the failure the person opened is not in the list")
	}
}

// FOUR ITEMS ARE FOUR ROWS. `+1 more` would spend the row it saves, so the
// band only counts what it cannot draw from the fifth item on.
func TestTheBandDrawsFourItemsRatherThanCountOne(t *testing.T) {
	a := sideBandFixture(t)
	a.side.acked = map[uint64]bool{3: true}
	keys, _ := sideDrawn(a)
	got := strings.Join(keys, " ")
	for _, want := range []string{"task/2", "task/1", "fail/5", "fail/4"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the band lost %q: %v", want, keys)
		}
	}
	if strings.Contains(got, "band/more") {
		t.Fatalf("four items were cut to three and a count: %v", keys)
	}
}

// NOTHING NEEDING THE PERSON DRAWS NOTHING: no band, no rule, no `none`. And
// a plain terminal gets the linear marks.
func TestAnEmptyBandIsGoneAndAPlainTerminalReadsItsMarks(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "Fix the loader", session.TaskRunning, session.TaskNotice{}))
	rows := railText(a, 10)
	if strings.TrimRight(rows[1], " ") != "│ Running 1" {
		t.Fatalf("an empty band left something between the header and the list:\n%s", strings.Join(rows, "\n"))
	}
	for _, row := range rows {
		for _, never := range []string{"none", "Needs you", "────"} {
			if strings.Contains(row, never) {
				t.Fatalf("an idle column drew %q:\n%s", never, strings.Join(rows, "\n"))
			}
		}
	}
	b := sideBandFixture(t)
	b.linear = true
	text := strings.Join(railText(b, b.viewHeight()), "\n")
	for _, want := range []string{b.taskStateMark(b.tasks[2]) + " Port the parser 2", b.taskStateMark(b.tasks[5]) + " Cut the goldens 5", "----"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the plain terminal's band is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "✗") || strings.Contains(text, "─") {
		t.Fatalf("the plain terminal drew a glyph it cannot be trusted with:\n%s", text)
	}
}

// NOTHING IN THE COLUMN MOVES THE CONVERSATION OR THE HEADER. A band item
// arriving, a group folding, a row arriving, and in a team chat the other
// word coming to the front: the body keeps its width and the header its row.
func TestNothingInTheColumnMovesTheBodyOrTheHeader(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	body := a.bodyWidth()
	check := func(what string) {
		t.Helper()
		if a.bodyWidth() != body {
			t.Fatalf("%s moved the conversation from %d to %d columns", what, body, a.bodyWidth())
		}
		view, _ := a.railDrawnView(a.viewHeight())
		if len(view) == 0 || view[0].side == nil || view[0].side.key != sideHeadKey {
			t.Fatalf("%s moved the header off the column's first row", what)
		}
	}
	a.taskUpdate(update(8, "Cut the trailer", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(8, "Cut the trailer", session.TaskFailed, session.TaskNotice{Ending: session.TaskEndingSteps}))
	check("a failure arriving in the band")
	a.sideToggleGroup(railDone)
	check("opening a group")
	a.sideToggleGroup(railDone)
	check("folding a group")
	a.taskUpdate(update(9, "Port the loader", session.TaskRunning, session.TaskNotice{}))
	check("a new row")

	m, _, _, _ := trafficApp(t)
	m.width, m.height = 160, 40
	if m.sideKind() != sideKindManager {
		t.Fatalf("the fixture's front chat is kind %d", m.sideKind())
	}
	width := m.bodyWidth()
	if m.railWidth() != sideColsFor(m.width) {
		t.Fatalf("the manager's column is %d wide, want %d", m.railWidth(), sideColsFor(m.width))
	}
	for _, view := range []int{sideTasks, sideTraffic, sideTasks} {
		m.sideSetView(view)
		if m.bodyWidth() != width || m.sideView() != view {
			t.Fatalf("switching to view %d moved the conversation from %d to %d", view, width, m.bodyWidth())
		}
		if v, _ := m.railDrawnView(m.viewHeight()); len(v) == 0 || v[0].side == nil || v[0].side.key != sideHeadKey {
			t.Fatalf("switching to view %d moved the header", view)
		}
	}
}

// THE HOVER GROUND IS THE CLICK TARGET. Every cell of a band row lights that
// row and a press on any of them opens its task; on the header, each word and
// the key light alone, and the air between them lights nothing and does
// nothing.
func TestTheHoverGroundIsTheClickTarget(t *testing.T) {
	a := sideBandFixture(t)
	_, y := sideRowOn(t, a, "task/2")
	left := a.railLeft() + ansi.StringWidth(railSeam)
	for col := 0; col < a.railRoom(); col++ {
		a.setHover(left+col, y)
		if a.hot.kind != hoverSide || a.hot.key != "task/2" || a.hot.index != -1 {
			t.Fatalf("cell %d of the band row lit %+v", col, a.hot)
		}
	}
	lit := a.railRows(a.viewHeight())[y-a.topHeight()]
	if w := ansi.StringWidth(lit); w != ansi.StringWidth(railSeam)+a.railRoom() {
		t.Fatalf("the hover ground is %d cells, want the whole row", w)
	}
	for _, col := range []int{0, a.railRoom() / 2, a.railRoom() - 1} {
		a.closeRoom()
		railClick(t, a, left+col, y)
		if !a.roomOpen() || a.room.id != 2 {
			t.Fatalf("a press on cell %d of the band row opened %d", col, roomID(a))
		}
	}
	a.closeRoom()

	// THE HEADER, cell by cell, against its own doors.
	_, row := a.sideHeadRow(a.railRoom())
	hy := a.topHeight()
	for col := 0; col < a.railRoom(); col++ {
		a.setHover(left+col, hy)
		door := row.doorAt(col)
		switch {
		case door >= 0 && (a.hot.kind != hoverSide || a.hot.key != sideHeadKey || a.hot.index != door):
			t.Fatalf("header cell %d is door %d and lit %+v", col, door, a.hot)
		case door < 0 && a.hot.kind == hoverSide:
			t.Fatalf("header cell %d is no door and lit %+v", col, a.hot)
		}
	}
	// A plain chat's one word is not a door: there is nothing to switch to.
	if len(row.doors) != 1 || row.doors[0].act.kind != sideActHide {
		t.Fatalf("a plain chat's header has doors %+v", row.doors)
	}
}
