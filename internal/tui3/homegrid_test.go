package tui3

import (
	"strings"
	"testing"
	"time"
)

// ── THE GRID'S LAWS (docs/design/home-mission-control/DESIGN.md §1) ──────────

// homeRowOf is the BODY row a word first appears on — the frame's four head
// rows are the router's, and the tab bar spells three of the panels' words —
// and the cell it starts at, or -1 for a word the body does not hold.
func homeRowOf(frame, word string) (row, col int) {
	for y, line := range strings.Split(frame, "\n") {
		if y < placeHeadRows {
			continue
		}
		if at := strings.Index(line, word); at >= 0 {
			return y, len([]rune(line[:at]))
		}
	}
	return -1, -1
}

// ACT LEFT, WATCH RIGHT: one column under a hundred and ten cells, two under a
// hundred and seventy, three past it (law 2).
func TestTheGridIsOneColumnThenTwoThenThree(t *testing.T) {
	for width, want := range map[int]int{60: 1, 109: 1, 110: 2, 169: 2, 170: 3, 240: 3} {
		if got := homeGridCols(width); got != want {
			t.Fatalf("at %d cells the grid has %d columns, want %d", width, got, want)
		}
	}
}

// AT EIGHTY CELLS EVERY PANEL STANDS IN ONE COLUMN, in the order a person reads
// the two-column page: their own panels, then the machine's.
func TestAtEightyTheGridIsOneColumnInReadingOrder(t *testing.T) {
	a := newSwitchLab(t).open(80, 24)
	frame := homeText(a)
	last := -1
	for _, word := range []string{"needs you", "where you were", "projects", "running"} {
		row, col := homeRowOf(frame, word)
		if row < 0 {
			t.Fatalf("%q is not on an eighty-cell home:\n%s", word, frame)
		}
		if row <= last || col != homeGridMargin {
			t.Fatalf("%q is at row %d cell %d, after row %d in one column:\n%s", word, row, col, last, frame)
		}
		last = row
	}
}

// AT A HUNDRED AND TWENTY THE PERSON'S PANELS ARE ON THE LEFT AND THE MACHINE'S
// ON THE RIGHT, and the two columns start on the same row.
func TestAtOneTwentyThePersonActsLeftAndTheMachineIsWatchedRight(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	frame := homeText(a)
	needs, needsCol := homeRowOf(frame, "needs you")
	running, runningCol := homeRowOf(frame, "running")
	if needs < 0 || needs != running || runningCol <= needsCol {
		t.Fatalf("needs you (row %d) and running (row %d) are not the heads of two columns:\n%s", needs, running, frame)
	}
	projects, projectsCol := homeRowOf(frame, "projects")
	if projects <= needs || projectsCol != needsCol {
		t.Fatalf("projects is not under needs you in the left column:\n%s", frame)
	}
	spend, spendCol := homeRowOf(frame, "spend")
	if spend <= running || spendCol != runningCol {
		t.Fatalf("spend is not under running in the right column:\n%s", frame)
	}
}

// AT A HUNDRED AND EIGHTY THERE ARE THREE COLUMNS, and projects heads the third.
func TestAtOneEightyProjectsAndSpendHaveAColumnOfTheirOwn(t *testing.T) {
	a := newSwitchLab(t).open(180, 45)
	frame := homeText(a)
	needs, needsCol := homeRowOf(frame, "needs you")
	running, runningCol := homeRowOf(frame, "running")
	projects, projectsCol := homeRowOf(frame, "projects")
	if needs != running || running != projects || !(needsCol < runningCol && runningCol < projectsCol) {
		t.Fatalf("needs you, running and projects do not head three columns:\n%s", frame)
	}
}

// A SHORT TERMINAL SQUEEZES IN PRIORITY ORDER (law 5): next up gives way first,
// then spend, and a squeezed panel keeps its heading and its fold.
func TestAShortColumnShrinksTheLowestPriorityPanelFirst(t *testing.T) {
	rows := func(n int) homePanelRows {
		lines := make([]homeLine, n)
		for i := range lines {
			lines[i] = homeLine{kind: homeLedger, cell: &homeCell{title: "row"}}
		}
		return homePanelRows{lines: lines}
	}
	column := []*homeGridPanel{
		{slot: homeSlotOf(panelRunning), read: rows(4), shown: 4},
		{slot: homeSlotOf(panelSpend), read: rows(4), shown: 4},
		{slot: homeSlotOf(panelNext), read: rows(4), shown: 4},
	}
	// Three panels of a heading and four rows, and two blanks between: 17.
	// Next up at its floor of three frees two.
	squeezeColumn(column, 15)
	if column[2].shown == 4 || column[1].shown != 4 || column[0].shown != 4 {
		t.Fatalf("next up should give way alone: shown %d %d %d", column[0].shown, column[1].shown, column[2].shown)
	}
	if got := column[2].height(); got != homeSlotOf(panelNext).least {
		t.Fatalf("next up squeezed to %d rows, want its floor of %d", got, homeSlotOf(panelNext).least)
	}
	squeezeColumn(column, 8)
	if !column[2].dropped || column[0].dropped {
		t.Fatalf("at eight rows next up should be gone and running kept: %+v %+v", column[0], column[2])
	}
	if homeColumnHeight(column) > 8 {
		t.Fatalf("the column is %d rows in a room of 8", homeColumnHeight(column))
	}
}

// AND A REAL FRAME THAT SHORT STILL DRAWS EVERY HEADING IT KEPT, with the
// person's two panels first to stay.
func TestTheSqueezeAtOneTwentyByTwentyFourKeepsNeedsAndRecent(t *testing.T) {
	a := newSwitchLab(t).open(120, 24)
	frame := homeText(a)
	for _, word := range []string{"needs you", "where you were"} {
		if row, _ := homeRowOf(frame, word); row < 0 {
			t.Fatalf("%q was squeezed off a 120×24 home:\n%s", word, frame)
		}
	}
	if len(strings.Split(frame, "\n")) != 24 {
		t.Fatalf("the frame is not 24 rows")
	}
}

// AN EMPTY PANEL WHISPERS (law 4): its heading and one dim line naming what
// arrives there — and never a sentence saying it is empty.
func TestAnEmptyPanelWhispersWhatArrivesAndNeverThatItIsEmpty(t *testing.T) {
	for id, words := range homeWhisper {
		for _, banned := range []string{"nothing", "no ", "empty", "none"} {
			if strings.HasPrefix(words, banned) || strings.Contains(words, " "+banned) {
				t.Fatalf("panel %d whispers %q, which announces an absence", id, words)
			}
		}
	}
	lab := newHomeLab(t)
	mine := lab.session("-alpha", "aaaa000000000001", "the only chat", lab.workspace("alpha"), time.Now())
	a := lab.app(mine)
	a.width, a.height = 120, 45
	a.openHome()
	frame := homeText(a)
	if row, _ := homeRowOf(frame, homeWhisper[panelRunning]); row < 0 {
		t.Fatalf("an empty running panel does not whisper:\n%s", frame)
	}
}
