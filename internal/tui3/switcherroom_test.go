package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

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
