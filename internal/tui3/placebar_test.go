package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE TAB BAR AS A ROW, AS A PERSON MEETS IT.
//
// The bar was seven targets and a map: a pointer could open a room from it and a
// hand on the keyboard had `tab`, which is a walk with no cursor in it — every
// step opens the room it lands on. So reading the bar cost seven openings. Every
// test below is about the cursor that fixed that, and about the pointer's own
// half of the same fix (pages.go's [barCursor], placemouse.go).

// barTop walks whatever place is standing to the first row of its body and then
// one further, which is the journey `↑` off the top makes.
//
// THE FRAME IS DRAWN FIRST, and it has to be: the bar is reachable only on a
// frame that PAINTED one (SCREEN 3a's clause — no key does anything that is not
// on screen), and the row it landed on is recorded by the draw.
func barTop(t *testing.T, a *app) {
	t.Helper()
	a.frame()
	pl := a.showing()
	if pl == nil {
		t.Fatal("no place is standing")
	}
	for i := 0; i < len(pl.stops(a))+3 && !a.bar.on; i++ {
		drive(t, a, key("up"))
	}
	// HOME HAS NO WAY UP ONTO THE BAR (owner, 2026-09-17; pages.go's
	// [app.barReach]): `↑` stays in the field there, and the bar is reached by
	// a press, `tab` or a chord. A test that wants the cursor on home's bar
	// raises it the way a press would, after proving the walk did not.
	if !a.bar.on && a.at(pageHome) {
		a.barRaise()
		a.frame()
	}
}

// barWordSpan is where one place's chip landed on the bar the last frame drew.
func barWordSpan(t *testing.T, a *app, id page) placeTabSpan {
	t.Helper()
	for _, span := range a.tabs {
		if span.id == id {
			return span
		}
	}
	t.Fatalf("%q is not on the bar this frame drew", id.word())
	return placeTabSpan{}
}

// ── the law, over the registry ──────────────────────────────────────────────

// THE BAR IS A ROW THE CURSOR CAN STAND ON, ON EVERY PLACE AND AT EVERY WIDTH.
//
// It is ONE LOOP OVER THE REGISTRY and not seven copies, for
// [TestEveryPlaceTakesExactlyTheWholeFrameAtEveryWidth]'s reason: a place added
// later is covered by having been registered, and the whole argument for the bar
// being frame state is that no place has a word to say about it.
//
// The five clauses, each asserted below:
//
//	↑ from the first row of the body   lands the cursor on the bar
//	←/→                                walk the words and open nothing
//	esc                                puts the cursor back in the body
//	↓                                  goes into the place under the cursor
//	enter                              the same, on the key a hand reaches for
func TestTheBarIsARowOnEveryPlace(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			for _, width := range []int{80, 120, 200} {
				a := place.open(t)
				a.width, a.height = width, 30
				pl := placeFor(place.id)

				barTop(t, a)
				if !a.bar.on {
					t.Fatalf("at %d columns ↑ off the top of the %s place never reached the bar",
						width, place.id.word())
				}
				if a.bar.at != place.id {
					t.Fatalf("at %d columns the cursor landed on %q rather than on the word of the room it left",
						width, a.bar.at.word())
				}
				// AND THE PLACE'S OWN CURSOR DID NOT MOVE OFF ITS FIRST STOP. The
				// router claims `↑` there before the place ever sees it, which is
				// what makes the first `↓` back down free.
				if stops := pl.stops(a); len(stops) > 0 && pl.cursorAt(a) != stops[0] {
					t.Fatalf("at %d columns walking onto the bar left the %s place's cursor on %d, want its first stop %d",
						width, place.id.word(), pl.cursorAt(a), stops[0])
				}

				// ←/→ WALK THE WORDS AND OPEN NOTHING.
				drive(t, a, key("right"))
				if a.page != place.id {
					t.Fatalf("at %d columns → on the bar opened %q", width, a.page.word())
				}
				if want := nextOn(barPages(place.id, false), place.id, false); a.bar.at != want {
					t.Fatalf("at %d columns → landed the cursor on %q, want %q", width, a.bar.at.word(), want.word())
				}
				// AND `→` DOES NOT OPEN A ROW'S VERBS UP HERE. The strip is a
				// second reading of a ROW, and the bar is not a row of any place's
				// reading (verbstrip.go).
				if a.strip.open {
					t.Fatalf("at %d columns → on the bar opened a verb strip", width)
				}
				drive(t, a, key("left"))
				if a.bar.at != place.id {
					t.Fatalf("at %d columns ← did not walk back to %q", width, place.id.word())
				}

				// esc PUTS THE CURSOR BACK IN THE BODY AND LEAVES THE ROOM STANDING.
				drive(t, a, key("esc"))
				if a.bar.on {
					t.Fatalf("at %d columns esc left the cursor on the bar", width)
				}
				if a.page != place.id {
					t.Fatalf("at %d columns esc from the bar closed the place to %q", width, a.page.word())
				}

				// ↓ GOES INTO THE PLACE UNDER THE CURSOR, and on the word you are
				// already standing in that is the body you came from — landed on
				// its first stop, because that is where the walk up left it.
				barTop(t, a)
				drive(t, a, key("down"))
				if a.bar.on {
					t.Fatalf("at %d columns ↓ left the cursor on the bar", width)
				}
				if stops := pl.stops(a); len(stops) > 0 && pl.cursorAt(a) != stops[0] {
					t.Fatalf("at %d columns the first ↓ off the bar landed on %d, want the first stop %d",
						width, pl.cursorAt(a), stops[0])
				}

				// AND enter IS THE SAME DOOR, on the word one along — which opens
				// the room rather than merely coming back down.
				barTop(t, a)
				drive(t, a, key("right"), key("enter"))
				want := nextOn(barPages(place.id, false), place.id, false)
				if want == pageChats {
					// The way back to the chats opens no room: it leaves them.
					want = pageNone
				}
				if a.page != want {
					t.Fatalf("at %d columns enter on the bar landed on %q, want %q", width, a.page.word(), want.word())
				}
				if a.bar.on {
					t.Fatalf("at %d columns enter left the cursor on the bar of the room it opened", width)
				}
			}
		})
	}
}

// ── the drawing ─────────────────────────────────────────────────────────────

// THE CURSOR'S WORD WEARS THE CURSOR'S BAND, and it replaces the selected mark
// rather than stacking on it: two grounds on one word would be the screen saying
// "you are here" and "your cursor is here" at once and neither clearly.
//
// WHAT CHANGES IS THE INK AND NOTHING ELSE. The bar is the same seven words in
// the same cells before and after, which is what makes walking onto it a change
// of attention rather than a change of screen.
func TestTheBarCursorRepaintsTheWordAndMovesNothing(t *testing.T) {
	a := placeApp(t)
	before := a.frame
	beforeFrame, _, _ := before()
	beforeRow := strings.Split(beforeFrame, "\n")[placeTabRow]

	barTop(t, a)
	afterFrame, _, _ := a.frame()
	afterRow := strings.Split(afterFrame, "\n")[placeTabRow]

	if beforeRow == afterRow {
		t.Fatalf("the bar is painted exactly as it was with the cursor on it: %q", plain(afterRow))
	}
	if plain(beforeRow) != plain(afterRow) {
		t.Fatalf("the cursor moved a cell on the bar:\n%q\n%q", plain(beforeRow), plain(afterRow))
	}
	// AND WALKING ALONG IT REPAINTS AGAIN, so the cursor is visibly somewhere
	// rather than merely somewhere in the program's own memory.
	drive(t, a, key("right"))
	walked, _, _ := a.frame()
	if strings.Split(walked, "\n")[placeTabRow] == afterRow {
		t.Fatal("→ along the bar changed nothing on the frame")
	}
}

// AND THE WIDTH LADDER MAY NOT DROP THE WORD THE CURSOR IS ON. The bar gives up
// words as the frame narrows (pages.go's [app.placeTabBar]), and a cursor on a
// word that was given up would be a cursor nobody can see — which is SCREEN 3a's
// clause said about a row rather than about a key.
func TestTheNarrowBarKeepsTheWordTheCursorIsOn(t *testing.T) {
	a := placeApp(t)
	barTop(t, a)
	// Walk to a place with nothing new in it, which is exactly what the narrow
	// bar would otherwise drop.
	drive(t, a, key("right"), key("right"), key("right"))
	on := a.bar.at
	if on == a.page {
		t.Fatal("the walk came back to the room it started in, so this proves nothing")
	}
	for _, width := range []int{40, 30, 20} {
		a.width = width
		a.frame()
		if _, ok := placeTabColumnOf(a, on); !ok {
			t.Fatalf("at %d columns the bar dropped the word the cursor is on (%q)", width, on.word())
		}
	}
}

// ── the bar is not a mode ───────────────────────────────────────────────────

// `tab`, `shift+tab` AND `alt+1…9` KEEP WORKING FROM THE BAR, and a printable
// character goes to the composer with the cursor following it back down.
//
// A ROW THAT CAPTURED THE KEYBOARD WOULD BE A MODE, and the six classes have no
// modal split in them (SCREEN 3a): "any letter goes to the composer, on every
// page, always" has no asterisk, and a person who starts typing has stopped
// looking at the bar.
func TestTheBarIsNotAMode(t *testing.T) {
	t.Run("tab still walks", func(t *testing.T) {
		a := placeApp(t)
		barTop(t, a)
		drive(t, a, key("tab"))
		if want := nextPage(pageHome, false); a.page != want {
			t.Fatalf("tab from the bar landed on %q, want %q", a.page.word(), want.word())
		}
		if a.bar.on {
			t.Fatal("tab left the cursor on the bar of the room it opened")
		}
	})

	t.Run("the numbers still jump", func(t *testing.T) {
		a := placeApp(t)
		barTop(t, a)
		drive(t, a, key(placeChord(pageSpend)))
		if a.page != pageSpend {
			t.Fatalf("%s from the bar landed on %q", placeChord(pageSpend), a.page.word())
		}
		if a.bar.on {
			t.Fatalf("%s left the cursor on the bar", placeChord(pageSpend))
		}
	})

	t.Run("a letter types and brings the cursor down", func(t *testing.T) {
		a := placeApp(t)
		barTop(t, a)
		drive(t, a, key("p"))
		if a.bar.on {
			t.Fatal("typing left the cursor on the bar")
		}
		if got := a.home.box.String(); !strings.Contains(got, "p") {
			t.Fatalf("the letter never reached the composer: the box says %q", got)
		}
	})
}

// AND THE BAR WRAPS RATHER THAN CLAMPING, which is the one cursor on this
// surface that does. Every list clamps because it has a top and a bottom a
// person is reading towards; the bar is the same ring `tab` walks, and two keys
// over one row of words may not disagree about where its ends are.
func TestTheBarCursorWrapsAtBothEnds(t *testing.T) {
	a := placeApp(t)
	barTop(t, a)
	ring := barPages(a.page, false)
	drive(t, a, key("left"))
	if last := ring[len(ring)-1]; a.bar.at != last {
		t.Fatalf("← off the first word landed on %q, want %q", a.bar.at.word(), last.word())
	}
	drive(t, a, key("right"))
	if a.bar.at != ring[0] {
		t.Fatalf("→ off the last word landed on %q, want %q", a.bar.at.word(), ring[0].word())
	}
}

// ── the pointer on the bar ──────────────────────────────────────────────────

// MOTION OVER A TAB WORD CHANGES THAT WORD'S INK AND NOTHING ELSE ON THE FRAME,
// and motion off it puts the ink back.
//
// A TAB WORD IS A DOOR AND A DOOR SHOULD LOOK BACK. The bar answered a press and
// said nothing at all while a pointer crossed it, so seven words that open seven
// rooms read as a label strip until somebody gambled a click.
func TestHoveringATabWordLiftsItsInkAndNothingElse(t *testing.T) {
	a := placeApp(t)
	before, _, _ := a.frame()
	span := barWordSpan(t, a, pageSettings)

	drive(t, a, tea.MouseMotionMsg{X: span.from, Y: placeTabRow})
	after, _, _ := a.frame()
	beforeRows, afterRows := strings.Split(before, "\n"), strings.Split(after, "\n")
	if len(beforeRows) != len(afterRows) {
		t.Fatalf("the frame changed height under a pointer: %d rows became %d", len(beforeRows), len(afterRows))
	}
	for at := range beforeRows {
		if at == placeTabRow {
			continue
		}
		if beforeRows[at] != afterRows[at] {
			t.Fatalf("hovering a tab word repainted row %d:\n%q\n%q",
				at, plain(beforeRows[at]), plain(afterRows[at]))
		}
	}
	if beforeRows[placeTabRow] == afterRows[placeTabRow] {
		t.Fatalf("the hovered word is painted exactly as it was: %q", plain(afterRows[placeTabRow]))
	}
	// AND THE CELLS ARE THE SAME CELLS: the ink lifted, the bar did not move.
	if plain(beforeRows[placeTabRow]) != plain(afterRows[placeTabRow]) {
		t.Fatalf("the hover moved a cell on the bar:\n%q\n%q",
			plain(beforeRows[placeTabRow]), plain(afterRows[placeTabRow]))
	}
	// AND IT MOVED NOTHING A KEY WOULD HAVE MOVED.
	if a.page != pageHome {
		t.Fatalf("resting a pointer on a tab word opened %q", a.page.word())
	}

	// MOTION OFF IT RESTORES. A word left lifted after the hand moved away is a
	// door claiming to be under a pointer that is somewhere else.
	drive(t, a, tea.MouseMotionMsg{X: span.from, Y: placeHeadRows + 1})
	restored, _, _ := a.frame()
	if strings.Split(restored, "\n")[placeTabRow] != beforeRows[placeTabRow] {
		t.Fatalf("the pointer left the bar and the ink stayed lifted:\n%q",
			plain(strings.Split(restored, "\n")[placeTabRow]))
	}
}

// AND THE GAP BETWEEN TWO CHIPS LIFTS NOTHING. It belongs to no room, exactly as
// it does for a press ([app.placeTabPress]).
func TestHoveringTheGapBetweenTabsLiftsNothing(t *testing.T) {
	a := placeApp(t)
	before, _, _ := a.frame()
	gap, ok := placeTabGapColumn(a)
	if !ok {
		t.Skip("this bar has no gap between two chips")
	}
	drive(t, a, tea.MouseMotionMsg{X: gap, Y: placeTabRow})
	after, _, _ := a.frame()
	if strings.Split(before, "\n")[placeTabRow] != strings.Split(after, "\n")[placeTabRow] {
		t.Fatalf("a pointer in the gap lifted a word:\n%q", plain(strings.Split(after, "\n")[placeTabRow]))
	}
}

// THE HOVER AND THE CURSOR COMPOSE: the pointer's ink on one word, the cursor's
// band on another, on the same bar and at the same time. They are two different
// things and the screen says so — which is home's own law about the list, owed
// to the one row above it.
func TestTheHoverAndTheBarCursorCompose(t *testing.T) {
	a := placeApp(t)
	barTop(t, a)
	drive(t, a, key("right"))
	a.frame()
	if a.bar.at == pageSettings {
		t.Fatal("the cursor walked onto the word this test means to hover")
	}
	span := barWordSpan(t, a, pageSettings)
	drive(t, a, tea.MouseMotionMsg{X: span.from, Y: placeTabRow})

	if a.tabHover != pageSettings {
		t.Fatalf("the pointer is recorded over %q", a.tabHover.word())
	}
	if !a.bar.on || a.bar.at == pageSettings {
		t.Fatalf("the hover moved the cursor: it is on %q (up=%v)", a.bar.at.word(), a.bar.on)
	}
	// Both marks are on the one row, and the row still says the same four words.
	row := strings.Split(mustFrame(a), "\n")[placeTabRow]
	for _, id := range barPages(a.page, false) {
		if !strings.Contains(plain(row), id.word()) {
			t.Fatalf("the bar lost %q while wearing two marks: %q", id.word(), plain(row))
		}
	}
}

// THE WHEEL OVER THE BAR WALKS THE PLACES, one room a tick. The wheel means "the
// next one of these" everywhere else on this surface; over a row whose contents
// are the seven rooms, the next one of these is the next room.
func TestTheWheelOverTheBarWalksThePlaces(t *testing.T) {
	a := placeApp(t)
	a.frame()
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeTabRow, Button: tea.MouseWheelDown})
	if want := nextPage(pageHome, false); a.page != want {
		t.Fatalf("a wheel notch over the bar landed on %q, want %q", a.page.word(), want.word())
	}
	a.frame()
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeTabRow, Button: tea.MouseWheelUp})
	if a.page != pageHome {
		t.Fatalf("the wheel back up landed on %q, want home again", a.page.word())
	}
	// AND ONE ROOM A TICK AND NOT THREE. Three would open two rooms nobody asked
	// to see on the way to the third, and each opening throws away a filter.
	a.frame()
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeTabRow, Button: tea.MouseWheelDown})
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeTabRow, Button: tea.MouseWheelDown})
	if want := pages()[2]; a.page != want {
		t.Fatalf("two notches walked to %q, want %q", a.page.word(), want.word())
	}
}
