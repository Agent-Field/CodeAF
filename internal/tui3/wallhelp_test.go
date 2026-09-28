package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// ? IS HELP AND n IS THE NEXT ONE WAITING: the needs-you count still goes to
// the next waiting conversation, and says n, and ? puts up the sheet.
func TestWallQuestionMarkIsHelpAndNIsNext(t *testing.T) {
	v := wallUnmarked(wallFixture(6))
	v.hover = wallHitRef{kind: wallHitAction, arg: int(wallActNext)}
	if got := wallHint(v, false); got != "Go to the next conversation waiting on you · n" {
		t.Fatalf("the needs-you count says %q", got)
	}
	// The count is still a button on its own words.
	pal := newPalette(tokens.TrueColor, false)
	rows, hits := renderWall(pal, wallUnmarked(wallFixture(6)), 120, 40)
	found := false
	for _, hit := range hits {
		if hit.kind == wallHitAction && hit.arg == int(wallActNext) {
			found = strings.Contains(ansi.Strip(ansi.Cut(rows[hit.y0], hit.x0, hit.x1)), "1 needs you")
		}
	}
	if !found {
		t.Fatal("the needs-you count is not a button on its words")
	}

	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	focus := a.wall.focus
	wallKeyPress(a, "n")
	if a.wall.help {
		t.Fatal("n put up the help sheet")
	}
	wallKeyPress(a, "?")
	if !a.wall.help || a.wall.focus != focus {
		t.Fatalf("?: help %v, focus %d from %d", a.wall.help, a.wall.focus, focus)
	}
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	for _, want := range []string{"─ Conversations ─", "Navigate", "Organize", "Next needing you", "Automatic columns"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the sheet lacks %q:\n%s", want, frame)
		}
	}
	wallKeyPress(a, "?")
	if a.wall.help || !a.wall.on {
		t.Fatalf("? again: help %v on %v", a.wall.help, a.wall.on)
	}
}

// THE SHEET IS THE ESC LADDER'S INNERMOST RUNG, and a press off it puts it
// away and does nothing else.
func TestWallHelpSheetClosesLikeACard(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	a.wallSettle()
	_ = a.wallFrame(a.width, a.height)
	wallKeyPress(a, " ")
	wallKeyPress(a, "?")
	if !a.wall.help || len(a.wall.marked) != 1 {
		t.Fatalf("the setup: help %v marked %v", a.wall.help, a.wall.marked)
	}
	wallKeyPress(a, "esc")
	if a.wall.help || len(a.wall.marked) != 1 || !a.wall.on {
		t.Fatalf("esc on the sheet: help %v marked %v on %v", a.wall.help, a.wall.marked, a.wall.on)
	}

	a.wall.marked = map[string]bool{}
	wallKeyPress(a, "?")
	_ = a.wallFrame(a.width, a.height)
	card := a.wall.card
	if card.w() == 0 {
		t.Fatal("the sheet's place was not recorded")
	}
	if _, took := a.wallPress(card.x0, card.y0); !took || !a.wall.help {
		t.Fatal("a press on the sheet's border put it away")
	}
	x, y := a.width-3, a.height-3
	if card.holds(x, y) {
		t.Fatal("the test's far corner is on the sheet")
	}
	focus := a.wall.focus
	if _, took := a.wallPress(x, y); !took || a.wall.help || !a.wall.on || a.wall.focus != focus {
		t.Fatalf("a press off the sheet: help %v on %v focus %d", a.wall.help, a.wall.on, a.wall.focus)
	}

	// The toolbar's Help puts it up.
	_ = a.wallFrame(a.width, a.height)
	wallClick(t, a, wallHitFor(t, a, wallHitAction, int(wallActHelp)))
	if !a.wall.help {
		t.Fatal("the toolbar's Help did not put up the sheet")
	}
}

// EVERY ROW OF THE SHEET IS A PRESS, and does what its key does, on the
// focused conversation, with the sheet put away first.
func TestWallHelpRowsDoWhatTheirKeysDo(t *testing.T) {
	rows := wallHelpRows(false)
	index := func(label string) int {
		for i, r := range rows {
			if r.label == label {
				return i
			}
		}
		t.Fatalf("no row %q", label)
		return -1
	}
	open := func() *app {
		a, _, _ := tabApp(t)
		_ = a.openWall()
		a.wallSettle()
		a.wallMove(0, len(a.wallShown(a.now())))
		_ = a.wallFrame(a.width, a.height)
		wallKeyPress(a, "?")
		_ = a.wallFrame(a.width, a.height)
		return a
	}

	a := open()
	wallClick(t, a, wallHitFor(t, a, wallHitHelp, index("Move")))
	if a.wall.help || a.wall.focus != 1 {
		t.Fatalf("Move: help %v focus %d", a.wall.help, a.wall.focus)
	}
	a = open()
	wallClick(t, a, wallHitFor(t, a, wallHitHelp, index("Select")))
	if a.wall.help || len(a.wall.marked) != 1 {
		t.Fatalf("Select: help %v marked %v", a.wall.help, a.wall.marked)
	}
	a = open()
	wallClick(t, a, wallHitFor(t, a, wallHitHelp, index("Filter")))
	if a.wall.help || !a.wall.filterOn {
		t.Fatalf("Filter: help %v filtering %v", a.wall.help, a.wall.filterOn)
	}
	a = open()
	wallClick(t, a, wallHitFor(t, a, wallHitHelp, index("Add to teams")))
	if a.wall.help || a.wall.pop.kind != wallPopMembers {
		t.Fatalf("Add to teams: help %v pop %+v", a.wall.help, a.wall.pop)
	}
	a = open()
	wallClick(t, a, wallHitFor(t, a, wallHitHelp, index("Back")))
	if a.wall.help || a.wall.on {
		t.Fatalf("Back: help %v on %v", a.wall.help, a.wall.on)
	}
	a = open()
	key := a.wallShown(a.now())[0].tab.key
	wallClick(t, a, wallHitFor(t, a, wallHitHelp, index("Open")))
	if a.wall.on || a.frontTabKey() != key {
		t.Fatalf("Open: on %v front %q, want %q", a.wall.on, a.frontTabKey(), key)
	}

	// And every row answers, on its own words, and closes the sheet.
	for i, r := range rows {
		a := open()
		hit := wallHitFor(t, a, wallHitHelp, i)
		if got := ansi.Strip(ansi.Cut(a.wallFrame(a.width, a.height)[hit.y0], hit.x0, hit.x1)); !strings.Contains(got, r.label) {
			t.Fatalf("row %q is drawn as %q", r.label, got)
		}
		wallClick(t, a, hit)
		if a.wall.help {
			t.Fatalf("row %q left the sheet up", r.label)
		}
	}
}

// THE SHEET SCROLLS WHEN IT DOES NOT FIT, by key and by wheel, and its foot
// says which way the rest lies; with room it drops nothing.
func TestWallHelpSheetScrolls(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		for _, sz := range [][2]int{{80, 21}, {120, 40}, {60, 30}, {44, 20}, {80, 14}} {
			v := wallUnmarked(wallFixture(6))
			v.help = true
			name := fmt.Sprintf("%s %dx%d", pname, sz[0], sz[1])
			card := wallHelpCard(pal, v, sz[0], sz[1])
			if len(card.rows) == 0 {
				t.Fatalf("%s: no sheet", name)
			}
			seen := map[int]bool{}
			for top := 0; top <= card.over; top++ {
				v.helpTop = top
				rows, hits := renderWall(pal, v, sz[0], sz[1])
				wallCheckRows(t, name, rows, sz[0], sz[1])
				wallCheckHits(t, name, hits, sz[0], sz[1])
				for _, hit := range hits {
					if hit.kind == wallHitHelp {
						seen[hit.arg] = true
					}
				}
			}
			if len(seen) != len(wallHelpRows(pal.ascii)) {
				t.Fatalf("%s: %d of %d rows reachable", name, len(seen), len(wallHelpRows(pal.ascii)))
			}
			if card.over > 0 && !strings.Contains(ansi.Strip(card.rows[len(card.rows)-1]), "more") {
				t.Fatalf("%s: a scrolled sheet does not say so: %q", name, ansi.Strip(card.rows[len(card.rows)-1]))
			}
		}
	}

	a, _, _ := tabApp(t)
	a.height = 18
	_ = a.openWall()
	a.wallSettle()
	wallKeyPress(a, "?")
	_ = a.wallFrame(a.width, a.height)
	if a.wall.helpMax == 0 {
		t.Fatalf("an 18-row window's sheet does not scroll:\n%s", wallPlainFrame(a.wallFrame(a.width, a.height)))
	}
	wallKeyPress(a, "down")
	if a.wall.helpTop != 1 || !a.wall.help {
		t.Fatalf("down: top %d help %v", a.wall.helpTop, a.wall.help)
	}
	wallKeyPress(a, "end")
	if a.wall.helpTop != a.wall.helpMax {
		t.Fatalf("end: top %d of %d", a.wall.helpTop, a.wall.helpMax)
	}
	wallKeyPress(a, "home")
	scroll, focus := a.wall.scroll, a.wall.focus
	a.wallWheel(10, a.wall.headRows+6, true)
	if a.wall.helpTop != 1 || a.wall.scroll != scroll || a.wall.focus != focus {
		t.Fatalf("the wheel: sheet at %d, grid at %d from %d, focus %d from %d", a.wall.helpTop, a.wall.scroll, scroll, a.wall.focus, focus)
	}
	t.Logf("80x18 window, the sheet scrolled a line:\n%s", wallPlainFrame(a.wallFrame(a.width, a.height)))
}
