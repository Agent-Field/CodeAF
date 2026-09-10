package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// readIn is this lab's reading with a FRAME UNDER IT: how many rows the column
// the list is drawn into actually has. Zero is the reading every other test in
// this file takes — no room handed in, and the list as it drew before it could
// ask (switcher.go's [switcherView.room]).
func (l switcherLab) readIn(room int) switcherReading {
	return readSwitcher(l.world, l.items, l.fired, switcherHere{session: l.here, project: l.bucket}, l.gone, l.seen, l.now,
		switcherView{room: room}, switcherLedgerInput{})
}

// switcherDrawnRows is how many of this reading's lines are things a person can
// stand on and open — conversations and watches, not headings, blanks or the
// fold.
func switcherDrawnRows(r switcherReading) int {
	n := 0
	for _, row := range switcherStops(r) {
		if row.kind == switcherConversation || row.kind == switcherStanding {
			n++
		}
	}
	return n
}

// TestTheListGrowsToTheFrameAndIsNeverShorterThanEight is [switcherShown]
// turned from a ceiling into a floor.
//
// A fifty-row terminal used to draw eight conversations, fold the rest behind
// `▸ 7 more, quiet since 5d`, and leave the bottom half of the screen
// blank — the frame had the rows and the reading had no way to be told about
// them.
func TestTheListGrowsToTheFrameAndIsNeverShorterThanEight(t *testing.T) {
	lab := newSwitcherLab()
	for _, want := range []struct {
		room  int
		rows  int
		folds bool
		why   string
	}{
		{room: 0, rows: 8, folds: true, why: "a reading told nothing about its frame draws what it always drew"},
		{room: 12, rows: 8, folds: true, why: "a short frame still owes a person the eight-row floor"},
		{room: 20, rows: 14, folds: true, why: "a taller frame draws the rows it has, and folds what is still under them"},
		{room: 40, rows: 15, folds: false, why: "a frame that holds the whole list has nothing left to fold"},
	} {
		r := lab.readIn(want.room)
		text := switcherText(r, 120)
		if got := switcherDrawnRows(r); got != want.rows {
			t.Fatalf("a column of %d rows drew %d conversations, want %d — %s:\n%s", want.room, got, want.rows, want.why, text)
		}
		if folds := strings.Contains(text, tokens.GlyphCollapsed); folds != want.folds {
			verb := "drew a fold"
			if want.folds {
				verb = "drew no fold"
			}
			t.Fatalf("a column of %d rows %s — %s:\n%s", want.room, verb, want.why, text)
		}
	}
}

// TestAGrownListStillPaysForItsOwnHeadings holds the same law on the grouped
// view (`alt+g`), where every project on the page costs a name and the blank
// over it out of the very rows the conversations wanted.
func TestAGrownListStillPaysForItsOwnHeadings(t *testing.T) {
	lab := newSwitcherLab()
	const room = 20
	r := readSwitcher(lab.world, lab.items, lab.fired, switcherHere{session: lab.here, project: lab.bucket}, lab.gone, lab.seen, lab.now,
		switcherView{grouped: true, room: room}, switcherLedgerInput{})
	drawn := len(r.lines)
	if drawn > room {
		t.Fatalf("a column of %d rows was handed %d lines, so the fold at the foot is off the bottom of the frame:\n%s",
			room, drawn, switcherText(r, 120))
	}
	// And it is not the floor pretending to be an answer: a twenty-row column
	// holds more than the eight rows a short one does.
	if got := switcherDrawnRows(r); got <= switcherShown {
		t.Fatalf("grouped in a column of %d rows drew %d conversations, want more than the floor of %d:\n%s",
			room, got, switcherShown, switcherText(r, 120))
	}
}

// trailingBlankRows is how many rows of nothing a frame ends the LIST with —
// counted from the last row that says anything up to the surface's own foot.
func trailingBlankRows(text string) int {
	lines := strings.Split(text, "\n")
	blank, seen := 0, false
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(ansi.Strip(lines[i])) == "" {
			if seen {
				blank++
			}
			continue
		}
		if seen {
			break
		}
		// The foot — the hint line and the composer — is not the list's blank.
		seen = true
		blank = 0
	}
	return blank
}
