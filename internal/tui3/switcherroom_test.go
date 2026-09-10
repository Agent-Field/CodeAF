package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

// TestHomeDrawsAsManyConversationsAsTheFrameHolds is the same fix at the door a
// person uses: the whole screen, at two heights, counted off the drawn frame.
func TestHomeDrawsAsManyConversationsAsTheFrameHolds(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := ""
	for i := 0; i < 24; i++ {
		file := lab.session("-tmp-alpha", fmt.Sprintf("aaaa0000000%05d", i), fmt.Sprintf("chat number %02d", i), "/tmp/alpha", now.Add(-time.Duration(i+1)*time.Hour))
		if i == 0 {
			mine = file
		}
	}
	a := lab.app(mine)
	a.openHome()

	count := func(width, height int) (int, string) {
		a.width, a.height = width, height
		text := homeText(a)
		return strings.Count(text, "Chat Number"), text
	}
	short, shortText := count(120, 24)
	tall, tallText := count(120, 50)
	if tall <= short {
		t.Fatalf("a 50-row terminal drew %d conversations and a 24-row one drew %d — the taller frame is meant to spend its rows on the list:\n%s", tall, short, tallText)
	}
	if tall <= switcherShown {
		t.Fatalf("a 50-row terminal drew %d conversations, want more than the floor of %d:\n%s", tall, switcherShown, tallText)
	}
	if short < switcherShown {
		t.Fatalf("a 24-row terminal drew %d conversations, want at least the floor of %d:\n%s", short, switcherShown, shortText)
	}
	// AND THE BLANK ROWS ARE THE POINT. A frame that grew its list and still
	// ended in a column of nothing would have moved the defect rather than
	// fixed it.
	if blank := trailingBlankRows(tallText); blank > 2 {
		t.Fatalf("a 50-row terminal left %d rows of nothing under a folded list:\n%s", blank, tallText)
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
