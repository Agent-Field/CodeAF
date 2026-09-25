package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE POINTER, ASKED OF ALL SEVEN ─────────────────────────────────────────
//
// placemouse_test.go pins the three gestures on the places that had none;
// placeeveryone_test.go walks every room with the keyboard. Neither one walked
// every room with the POINTER, which is how the built binary came to have a tab
// bar that answered a press on four places and ignored it on the other three.
// This file asks the two pointer questions of all seven, over the same labs.

// A TAB WORD IS A DOOR FROM WHEREVER YOU ARE STANDING. The bar is the router's
// own row and it is drawn on every place identically, so a press on it that
// meant something different depending on which room you happened to be in would
// be the one row of the frame a person cannot trust.
func TestClickingATabWordWorksFromEveryPlace(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			// The frame is drawn first, because a press resolves against the bar
			// that was actually painted and never against a second guess at it.
			placeFrameText(a)
			target := pageSettings
			if place.id == pageSettings {
				target = pageSpend
			}
			x, ok := placeTabColumnOf(a, target)
			if !ok {
				t.Fatalf("the %s tab is not on the bar the %s place drew", target.word(), place.id.word())
			}
			if a.tabRow != placeTabRow {
				t.Fatalf("the %s place drew its tab bar on row %d", place.id.word(), a.tabRow)
			}
			drive(t, a, tea.MouseClickMsg{X: x, Y: a.tabRow, Button: tea.MouseLeft})
			if a.page != target {
				t.Fatalf("pressing the %s tab on the %s place left the router on %q",
					target.word(), place.id.word(), a.page.word())
			}
		})
	}
}

// THE WHEEL IS `↑↓` UNDER THE POINTER, on every place and by the same distance.
// A wheel that moved a different number of rows per room — or none at all in
// some of them — would be one gesture with seven meanings, which is what the
// owner met when they turned it over the built binary.
func TestTheWheelMovesEveryPlacesCursorLikeTheArrows(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			// ONE LAB WALKED WITH THE ARROWS AND ANOTHER WITH THE WHEEL, because
			// what is being compared is the two gestures' effect on the same
			// starting cursor over the same body.
			byKey, byWheel := place.open(t), place.open(t)
			// BOTH START AT THE TOP OF THE LIST. A lab that opens with its cursor
			// already on the last row proves nothing about a gesture that walks
			// down, and home's own lab opens exactly there — with its fold held
			// open, the cursor sits on the fold line it was opened from.
			placeWalkToTop(t, byKey)
			placeWalkToTop(t, byWheel)
			start := place.cursor(byKey)
			for i := 0; i < placeWheelRows; i++ {
				drive(t, byKey, key("down"))
			}
			want := place.cursor(byKey)
			if want == start {
				t.Skipf("the %s place's body is too short for `↓` to move at all", place.id.word())
			}
			drive(t, byWheel, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
			if got := place.cursor(byWheel); got != want {
				t.Fatalf("one turn of the wheel on the %s place moved the cursor to %d and %d presses of `↓` moved it to %d",
					place.id.word(), got, placeWheelRows, want)
			}
			// AND BACK THE OTHER WAY, because a wheel that only went down would
			// be a list a person can fall off the bottom of.
			drive(t, byWheel, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelUp})
			if got := place.cursor(byWheel); got != start {
				t.Fatalf("the wheel back up the %s place left the cursor on %d rather than %d",
					place.id.word(), got, start)
			}
		})
	}
}

// placeWalkToTop walks a place's cursor to the first row it will stop on, with
// the arrow a person would use. It is more presses than any of these labs has
// rows, because what is wanted is the clamp and not a count.
//
// AND THE LAST `↑` LEAVES THE BODY. There is a row above the first row now — the
// tab bar, which the cursor can stand on (pages.go's [barCursor]) — so the walk
// ends with `esc`, which is what puts the cursor back in the body on the row it
// walked up from. Without it every caller would be measuring its first `↓`
// against a cursor that was not in the list at all.
func placeWalkToTop(t *testing.T, a *app) {
	t.Helper()
	placeFrameText(a)
	for i := 0; i < 60; i++ {
		drive(t, a, key("up"))
	}
	if a.bar.on {
		drive(t, a, key("esc"))
	}
	placeFrameText(a)
}

// ── the pointer previews ────────────────────────────────────────────────────

// THE POINTER PREVIEWS AND THE CURSOR SELECTS, AND THE MOTION MESSAGE IS WHAT
// CARRIES IT. homehover_test.go proves home's card follows the pointer by
// calling [app.homeHover] itself, which is the right test of the law and no
// test at all of the road: the owner drove the built binary and reported that
// nothing previewed under the pointer, and a motion message swallowed on its
// way through [app.Update] would look exactly like that with every hover test
// in this package still green.
func TestAMotionMessageMovesHomesCardAndSelection(t *testing.T) {
	a, _, _ := hoverLab(t)
	if a.width < homeCardMin {
		t.Fatalf("the hover lab opens on %d columns and there is no card under %d", a.width, homeCardMin)
	}
	cursor := a.home.cursor
	before := homeCardTitle(t, a)
	at := homeLineOfKind(t, a, homeSession, "alpha")
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: homeLineY(t, a, at)})
	if title := homeCardTitle(t, a); title == before {
		t.Fatalf("a motion message over another row left the card on %q", title)
	}
	if a.home.cursor != at {
		t.Fatalf("the pointer moved the cursor from %d to %d", cursor, a.home.cursor)
	}
}

// AND THE TASKS PLACE LIGHTS THE ROW UNDER THE POINTER TOO. It is the one
// promoted place placemouse_test.go's preview law does not reach — its rows are
// its own hit kinds rather than body lines — and it is the longest list on the
// surface, which is exactly where a pointer is worth having.
//
// THE LIGHT IS A GROUND AND A WEIGHT AND NO LONGER A MARK, so it is read off the
// painted frame: the row under the pointer wears the cursor step, as the row
// under the cursor does (placeprose.go's [placeBand]).
func TestHoveringARowOfTheTasksPlaceLightsIt(t *testing.T) {
	a := historyApp(t, 200)
	a.width, a.height = 120, 30
	a.pal = newPalette(tokens.ANSI256, false)
	ground := a.pal.onPlaces().cursor("x", 1)
	ground = ground[:strings.Index(ground, "x")]
	_, hits, _, _ := a.taskSheetFrame(a.width, a.height)
	for y, hit := range hits {
		if hit.kind != taskSheetHitRow || hit.index == a.taskSheet.cursor {
			continue
		}
		cursor := a.taskSheet.cursor
		drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
		if a.taskSheet.cursor != hit.index {
			t.Fatalf("the pointer over row %d moved the cursor from %d to %d", y, cursor, a.taskSheet.cursor)
		}
		painted, _, _ := a.frame()
		if lines := splitLines(painted); y < len(lines) && strings.Contains(lines[y], ground) {
			return
		}
		t.Fatalf("row %d of the tasks place does not wear the cursor step under the pointer", y)
	}
	t.Fatal("the tasks place drew no row the pointer could stand on")
}

// ── the window moves, and not just the cursor ───────────────────────────────

// `↓` PAST THE BOTTOM OF THE WINDOW SCROLLS THE LIST. placeeveryone_test.go's
// walk proves the cursor is still drawn afterwards, which a place could satisfy
// by refusing to move it at all past the last visible row — a list that stops
// dead at the window's edge and a list that scrolls look identical to that
// question. This asks the sharper one: the FIRST body line on the frame has to
// have moved, so the rows under the window are genuinely reachable.
//
// The tasks place is the one this matters most on: its tail fades with depth
// (depthfade.go), which is a claim that there is more below — a claim a page
// that could not scroll would be making falsely.
//
// HOME OWES THIS LAW NO LONGER. Its grid has no window: a panel's rows are
// built for the room the frame has, a short frame squeezes and drops panels
// rather than scrolling them, and what does not fit is behind the panel's own
// `N more` fold (homegrid.go's [fitColumn], DESIGN §1 laws 5 and 9). So there
// is no row below the frame for the cursor to walk onto, and the half of this
// that home still owes — the cursor never walks off what was drawn — is
// [TestWalkingPastTheWindowKeepsTheCursorOnTheFrame]'s.
func TestWalkingPastTheWindowScrollsEveryPlacesList(t *testing.T) {
	for _, place := range everyPlaceTable() {
		if place.id == pageHome {
			continue
		}
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			// A SHORTER FRAME THAN THE LABS OPEN ON, because the law is only owed
			// by a place whose body does not fit — and three of these labs hold a
			// list their own window can draw whole. Cutting the room is how the
			// same seven labs are made to state it, rather than growing three
			// fixtures other tests read.
			a.height = 14
			placeWalkToTop(t, a)
			_, last, drawn := placeWindowOf(place.hits(a))
			if drawn == 0 {
				t.Fatalf("the %s place drew no body rows at all", place.id.word())
			}
			for i := 0; i < drawn+12; i++ {
				drive(t, a, key("down"))
			}
			_, lastAfter, _ := placeWindowOf(place.hits(a))
			cursor := place.cursor(a)
			onFrame := false
			for _, at := range place.hits(a) {
				if at == cursor {
					onFrame = true
					break
				}
			}
			if !onFrame {
				t.Fatalf("the cursor walked to body line %d and the %s place does not draw it",
					cursor, place.id.word())
			}
			// A BODY THAT FITS WHOLE HAS NO WINDOW TO MOVE, which is the common
			// case and the right one — the law is only owed by a place whose
			// cursor has walked past what its first frame could hold.
			if cursor <= last {
				t.Skipf("the %s place's whole body fits in its window, so there is nothing to scroll", place.id.word())
			}
			// THE FAR EDGE OF THE WINDOW IS WHAT HAS TO MOVE, and not its first
			// line: a place may pin a heading at the top of its region, and one
			// that did would have a window that scrolls perfectly while its first
			// drawn line never changes.
			if lastAfter <= last {
				t.Fatalf("the cursor walked to body line %d and the %s place still draws no further than line %d",
					cursor, place.id.word(), lastAfter)
			}
		})
	}
}

// placeWindowOf is the window a frame's hit map describes: the first and last
// body lines it put on screen, and how many rows it drew.
//
// THE THREE ARE NOT DERIVABLE FROM EACH OTHER. A place whose rows are two lines
// tall, or whose headings are lines no cursor stops on, draws a window with gaps
// in it — so the count is not `last - first` and the last line is not
// `first + count`.
func placeWindowOf(hits []int) (first, last, drawn int) {
	first, last = -1, -1
	for _, at := range hits {
		if at < 0 {
			continue
		}
		if first < 0 {
			first = at
		}
		if at > last {
			last = at
		}
		drawn++
	}
	return first, last, drawn
}

// A CLICK ON A ROW IS `enter` ON IT, on every place with rows to open. There
// were three click grammars — home and the tasks place opened the row, the other
// four moved the cursor and did nothing more — so the same gesture on the same
// shape of row meant two things depending on the room (PLACES-AUDIT.md finding
// 6). One lab is clicked and a second is walked to the same row with the arrows
// and entered; the two must land in the same place drawing the same frame.
//
// Home is one of them now: its first click used to select and its second open,
// which was the one screen of seven that wanted two. Settings keeps select-then-
// change, because its `enter` edits a value; memory's lines are the stated
// exception and have their own test below.
func TestAClickOnARowIsEnterOnEveryPlace(t *testing.T) {
	// AND THE HIT MAP IS THE ONE THE FRAME DREW, so the law is asked twice: on
	// the frame as the place opens, and on a squeezed frame whose window the
	// arrows have scrolled — where a map computed apart from the draw would
	// open the row above or below the one pressed.
	frames := []struct {
		name  string
		shape func(t *testing.T, a *app)
	}{
		// THE FRAME IS DRAWN ONCE BEFORE THE WALK on this shape too: home lays
		// its lines out for the room it is drawn in, and a list built before
		// any frame has a fold where the drawn list has a row.
		{"as opened", func(t *testing.T, a *app) { _ = placeFrameText(a) }},
		{"squeezed and scrolled", func(t *testing.T, a *app) {
			a.width, a.height = 60, 16
			// THE FRAME IS DRAWN ONCE AT THE NEW SIZE BEFORE THE WALK, as the
			// program draws after every message: home lays its lines out for the
			// room it is drawn in, so a walk between a resize and a draw is a
			// walk over the taller frame's lines, and "line 6" means two rows.
			_ = placeFrameText(a)
			for i := 0; i < 20; i++ {
				drive(t, a, key("down"))
			}
		}},
	}
	for _, place := range everyPlaceTable() {
		if place.id == pageSettings || place.id == pageMemory {
			continue
		}
		for _, frame := range frames {
			t.Run(place.id.word()+"/"+frame.name, func(t *testing.T) {
				clicked := place.open(t)
				frame.shape(t, clicked)
				y, target := placeClickTarget(t, clicked, place)
				keyed := place.open(t)
				// Both fixtures compare the same draft destination. Their temporary
				// project roots differ, and short Linux paths expose that difference
				// on the seam where longer macOS paths happened to truncate it.
				if place.id == pageHome {
					keyed.target.where = clicked.targetWhere()
				}
				frame.shape(t, keyed)
				for i := 0; i < 400 && place.cursor(keyed) != target; i++ {
					if place.cursor(keyed) < target {
						drive(t, keyed, key("down"))
					} else {
						drive(t, keyed, key("up"))
					}
				}
				if place.cursor(keyed) != target {
					t.Fatalf("the arrows never reached body line %d on the %s place", target, place.id.word())
				}
				drive(t, clicked, tea.MouseClickMsg{X: placeClickX(clicked), Y: y, Button: tea.MouseLeft})
				drive(t, keyed, key("enter"))
				if clicked.page != keyed.page {
					t.Fatalf("a click on the %s place's row landed on %q and enter on it on %q",
						place.id.word(), clicked.page.word(), keyed.page.word())
				}
				got, want := placeFrameText(clicked), placeFrameText(keyed)
				// A DOOR THAT KEPT THE PLACE UP — a note instead of a room — is
				// compared on the row it landed on and on the foot that answered,
				// because two roads to one row may leave the window scrolled two
				// ways: the arrows walked up to it, the click did not.
				if clicked.page == place.id {
					if place.cursor(clicked) != target {
						t.Fatalf("the click on the %s place landed on body line %d, not %d",
							place.id.word(), place.cursor(clicked), target)
					}
					got, want = placeFootText(got), placeFootText(want)
				}
				if got != want {
					t.Fatalf("a click and enter on the same %s row drew two frames:\nclick:\n%s\nenter:\n%s",
						place.id.word(), got, want)
				}
			})
		}
	}
}

// A CLICK NEVER SPENDS. Memory's `enter` on a line asks the model about it, so a
// press on a line opens the line's card instead and the place stays — and a
// press on its shelf folds it, which is that row's `enter`.
func TestAClickOnAMemoryLineOpensItsCardAndAsksNothing(t *testing.T) {
	var place everyPlace
	for _, p := range everyPlaceTable() {
		if p.id == pageMemory {
			place = p
		}
	}
	a := place.open(t)
	hits := place.hits(a)
	for y, at := range hits {
		stop, ok := a.mem.reading.at(at)
		if at < 0 || !ok || stop.line == nil {
			continue
		}
		drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
		if a.page != pageMemory {
			t.Fatalf("a click on a memory line left the place for %q", a.page.word())
		}
		if a.mem.expanded != stop.line.ID {
			t.Fatalf("a click on %q opened the card %q", stop.line.ID, a.mem.expanded)
		}
		// AND THE CARD IS NOT THE LIST: a second press over it lands on no row.
		cursor := a.mem.cursor
		drive(t, a, tea.MouseClickMsg{X: 4, Y: y + 1, Button: tea.MouseLeft})
		if a.mem.cursor != cursor || a.mem.expanded != stop.line.ID {
			t.Fatal("a press over the open card walked the list hidden under it")
		}
		return
	}
	t.Fatal("the memory lab drew no line to click")
}

// placeClickX is the column a click on a body row lands in: four cells in on
// every place, and inside the cursor's own column on home's grid, where one
// screen row holds a line of every column and the x says which
// ([homeMark.cells]). The table's hit map for home is the cursor's column, so
// this is the column the target was found in.
func placeClickX(a *app) int {
	if !a.at(pageHome) || !a.home.gridOn() {
		return 4
	}
	col := a.home.columnOf(a.home.cursor)
	if col < 0 || col >= len(a.home.gridX) {
		return 4
	}
	return a.home.gridX[col] + homeGridLead
}

// placeClickTarget is a row of the frame a place drew that opens something and
// that the cursor is not already on — the last such row, so the click has to
// move the cursor to be right.
func placeClickTarget(t *testing.T, a *app, place everyPlace) (y, target int) {
	t.Helper()
	stops := map[int]bool{}
	for _, at := range a.showing().stops(a) {
		stops[at] = true
	}
	y, target = -1, -1
	for row, at := range place.hits(a) {
		if at >= 0 && stops[at] && at != place.cursor(a) {
			y, target = row, at
		}
	}
	if y < 0 {
		t.Fatalf("the %s place drew no row to click that the cursor is not on", place.id.word())
	}
	return y, target
}

// A FOLD LINE IS A HIT TARGET, AND A DOOR BOTH WAYS. `▸ 11 more` on a place's
// own list opens the rest where they stand on one click, and the same line —
// now `▾ 11 fewer`, wherever it has moved to — puts them back on the next. It
// was drawn with the fold mark and answered nothing, which is a door painted on
// a wall.
func TestAClickOpensEveryPlacesFoldAndTheNextShutsIt(t *testing.T) {
	for _, place := range everyPlaceTable() {
		if place.id == pageHome || place.id == pageTasks || place.id == pageSettings {
			continue
		}
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			// A frame tall enough that a lab's fold is on it rather than below
			// the window.
			a.width, a.height = 120, 45
			y := placeFoldRow(a, tokens.GlyphCollapsed)
			if y < 0 {
				t.Skipf("the %s lab draws no fold", place.id.word())
			}
			drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
			if a.page != place.id {
				t.Fatalf("a click on the %s place's fold left the place for %q", place.id.word(), a.page.word())
			}
			back := placeFoldRow(a, tokens.GlyphExpanded)
			if back < 0 || !strings.Contains(placeFrameText(a), "fewer") {
				t.Fatalf("a click on the %s place's fold did not open it:\n%s", place.id.word(), placeFrameText(a))
			}
			drive(t, a, tea.MouseClickMsg{X: 4, Y: back, Button: tea.MouseLeft})
			if again := placeFoldRow(a, tokens.GlyphCollapsed); again != y || strings.Contains(placeFrameText(a), "fewer") {
				t.Fatalf("the second click on the %s place's fold did not put the page back:\n%s",
					place.id.word(), placeFrameText(a))
			}
		})
	}
}

// placeFoldRow is the first frame row under the head whose words start with a
// fold mark, and -1 for none.
func placeFoldRow(a *app, mark string) int {
	for y, line := range strings.Split(placeFrameText(a), "\n") {
		if y >= placeHeadRows && strings.HasPrefix(strings.TrimSpace(line), mark+" ") {
			return y
		}
	}
	return -1
}

// placeFootText is the last rows of a frame — the rule, the box and the hint,
// where a note a door left is drawn.
func placeFootText(frame string) string {
	lines := strings.Split(frame, "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, "\n")
}

// ── home's own doors, under the one grammar ─────────────────────────────────

// A PANEL'S HEADING IS A DOOR INTO THE PLACE IT NAMES, on the grid at two
// columns and at three: `needs you`, `tasks` and `since you left` open tasks,
// `spend` opens spend and `standing` opens standing — and `projects` and `threads`,
// which name nothing but their own panels, open nothing and leave home up
// (`threads` opened the typed search until 2026-09-17; the box under home is
// the search now). The heading is found by its WORDS on the painted frame and
// never by the map the press reads, so a map that drifted from the paint fails
// here rather than agreeing with itself.
func TestAClickOnAHomeHeadingOpensThePlaceItNames(t *testing.T) {
	for _, width := range []int{120, 180} {
		for _, slot := range homePanelOrder {
			if slot.word == "" {
				continue
			}
			t.Run(itoa(width)+"/"+slot.word, func(t *testing.T) {
				a := newSwitchLab(t).open(width, 45)
				x, y, ok := homeHeadingAt(a, slot.word)
				if !ok {
					t.Fatalf("the %q heading is not on the %d-column frame:\n%s", slot.word, width, homeText(a))
				}
				drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				want := slot.head
				if want == pageNone {
					want = pageHome
				}
				if a.page != want {
					t.Fatalf("a click on the %q heading landed on %q, want %q", slot.word, a.page.word(), want.word())
				}
			})
		}
	}
}

// A HEADING THAT IS A DOOR SAYS SO UNDER THE POINTER: its word underlines while
// the mouse is over it, and only the word — the explainer and the clause keep
// their ink. A heading that opens nothing (`projects`, `threads`) never
// underlines, because an affordance on a wall is a lie, and moving the pointer
// off a heading takes the underline with it. The underline is the pointer's
// only mark on a heading: the cursor's ground still follows the keyboard alone
// (docs/DESIGN-LANGUAGE.md, "the section holding the cursor marks its own
// heading").
func TestAHomeHeadingThatOpensAPlaceUnderlinesUnderThePointer(t *testing.T) {
	for _, width := range []int{120, 180} {
		for _, slot := range homePanelOrder {
			if slot.word == "" {
				continue
			}
			t.Run(itoa(width)+"/"+slot.word, func(t *testing.T) {
				a := newSwitchLab(t).open(width, 45)
				if a.pal.profile == tokens.NoColor {
					t.Skip("this lab draws no SGR, so it cannot show an underline")
				}
				placeFrameText(a)
				x, y, ok := homeHeadingAt(a, slot.word)
				if !ok {
					t.Fatalf("the %q heading is not on the %d-column frame:\n%s", slot.word, width, homeText(a))
				}
				drive(t, a, tea.MouseMotionMsg{X: x + 2, Y: y})
				row := homeFrameRow(a, y)
				under := a.pal.underline(slot.word)
				if placeFor(slot.head) != nil {
					if !strings.Contains(row, under) {
						t.Fatalf("the pointer is on the %q heading, which opens %s, and the word is not underlined:\n%q",
							slot.word, slot.head.word(), row)
					}
					if strings.Contains(ansi.Strip(row), slot.explainer) && slot.explainer != "" &&
						strings.Contains(row, a.pal.underline(slot.word+rowSep+slot.explainer)) {
						t.Fatalf("the explainer underlined with the word:\n%q", row)
					}
				} else if strings.Contains(row, "\x1b[4m") {
					t.Fatalf("the %q heading opens nothing and underlines under the pointer:\n%q", slot.word, row)
				}
				if line, ok := a.home.focusedLine(); ok && line.cell != nil && line.cell.kind == cellHead {
					t.Fatalf("the pointer on a heading moved the cursor onto it")
				}
				// AND OFF THE HEADING THE UNDERLINE GOES: one row down is a row or
				// a whisper, never a heading.
				drive(t, a, tea.MouseMotionMsg{X: x + 2, Y: y + 1})
				if row := homeFrameRow(a, y); strings.Contains(row, "\x1b[4m") {
					t.Fatalf("the pointer left the %q heading and it is still underlined:\n%q", slot.word, row)
				}
			})
		}
	}
}

// homeFrameRow is one screen row of home exactly as it is painted, colour and
// attributes and all.
func homeFrameRow(a *app, y int) string {
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	if y < 0 || y >= len(lines) {
		return ""
	}
	return lines[y]
}

// homeHeadingAt is where a panel's heading word is painted: the row whose text
// in one of the grid's columns starts with it. A row never starts at its
// column's edge — its mark or its blank lead is there — so only a heading can.
func homeHeadingAt(a *app, word string) (x, y int, ok bool) {
	for y, line := range strings.Split(homeText(a), "\n") {
		cells := []rune(line)
		for _, start := range a.home.gridX {
			if start < len(cells) && strings.HasPrefix(string(cells[start:]), word) {
				return start, y, true
			}
		}
	}
	return 0, 0, false
}

// A FOLD IS `enter` ON ONE CLICK. Ten questions are more than `needs you` holds,
// so its fold says `2 more` — and a press on it opens the panel exactly as
// walking onto it and pressing enter does, not a selection that waits for a
// second press.
func TestAClickOnAHomeFoldIsEnterOnIt(t *testing.T) {
	open := func() *app {
		lab := newSwitchLab(t)
		for i := 0; i < 9; i++ {
			lab.presence("-beta", "cccc00000000000"+string(rune('1'+i)), session.PresenceWaiting,
				"question "+itoa(i), lab.now)
		}
		return lab.open(120, 18)
	}
	clicked, keyed := open(), open()
	homeClickAt(t, clicked, homeFoldDoor(t, clicked, panelSessions))
	keyed.home.cursor = homeFoldDoor(t, keyed, panelSessions)
	drive(t, keyed, key("enter"))
	if !keyed.at(pageHome) || !keyed.home.openedOn || keyed.home.opened != panelSessions {
		t.Fatalf("enter on the fold of `needs you` did not open the panel (page %q, opened %v)", keyed.page.word(), keyed.home.openedOn)
	}
	if !clicked.at(pageHome) || clicked.home.openedOn != keyed.home.openedOn || clicked.home.opened != keyed.home.opened {
		t.Fatalf("a click on the fold of `needs you` landed on %q (opened %v) and enter on it on %q (opened %v)",
			clicked.page.word(), clicked.home.openedOn, keyed.page.word(), keyed.home.openedOn)
	}
}

// homeFoldDoor is the line of one panel's fold, where the fold is a door.
func homeFoldDoor(t *testing.T, a *app, panel homePanelID) int {
	t.Helper()
	for at, line := range a.home.lines {
		if line.cell != nil && line.cell.kind == cellFold && line.cell.panel == panel && line.stop() {
			return at
		}
	}
	t.Fatalf("the panel draws no fold that is a door:\n%s", homeText(a))
	return -1
}

// HOME AND THE PLACES PAINT A SECTION WORD IN ONE INK. Screen 2a paints section
// headings dim and the accent budget paints them muted, and home and the places
// had each spelled their choice; the ink is one line now (placeprose.go's
// [placeHeadingInk]), so every heading on home THAT IS A DOOR opens with
// exactly the ink [placeHeading] opens a place's section word with. A heading
// that opens nothing — `projects`, `threads` — is dim instead, word and
// explainer in one ink, so it cannot be mistaken for a door (owner,
// 2026-09-17). The panel holding the cursor wears the cursor's ground over its
// heading and is left out.
func TestHomesHeadingsWearThePlacesHeadingInk(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	a.pal = newTestPalette()
	lines, _, _, _ := a.homeFrame(a.width, a.height)
	frame := strings.Join(lines, "\n")
	inked := placeHeading("x", a.pal.onPlaces())
	open := inked[:strings.Index(inked, "x")]
	if open == "" {
		t.Fatal("the test palette paints a heading with no ink at all")
	}
	marked, _ := a.home.cursorPanel()
	for _, slot := range homePanelOrder {
		if slot.panel.id() == marked || slot.word == "" {
			continue
		}
		if placeFor(slot.head) == nil {
			want := a.pal.dim(slot.word)
			if slot.explainer != "" {
				want = a.pal.dim(slot.word + rowSep + slot.explainer)
			}
			if !strings.Contains(frame, want) {
				t.Fatalf("the %q heading opens nothing and is not painted dim", slot.word)
			}
			if strings.Contains(frame, open+slot.word) {
				t.Fatalf("the %q heading opens nothing and wears the door headings' ink", slot.word)
			}
			continue
		}
		if !strings.Contains(frame, open+slot.word) {
			t.Fatalf("the %q heading is not painted in the places' heading ink", slot.word)
		}
		// AND ITS EXPLAINER RIDES ONE SHADE UNDER IT — after the heading and the
		// separator, in the dim ink, for every panel the column holds it for.
		if slot.explainer == "" {
			continue
		}
		if !strings.Contains(frame, a.pal.dim(rowSep+slot.explainer)) {
			t.Fatalf("the %q explainer is not painted dim beside its heading", slot.word)
		}
	}
}
