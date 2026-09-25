package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A WRAPPED /help ROW KEEPS THE COLUMN ITS SENTENCE STARTED IN. At a narrow
// width the old wrap put the rest of the sentence at column 0, which read as
// another key. The column is the leading spaces when the row already hangs,
// and otherwise the cell after the gap that pads the key.
func TestHelpWrappedLinesKeepTheirColumn(t *testing.T) {
	help := helpText("", chordSpelling{meta: chordAltWord})
	for _, width := range []int{60, 80, 110} {
		room := width - noteLead - workIndentCols(width)
		wrapped := false
		for _, line := range strings.Split(help, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			col := sheetColumn(line)
			pieces := wrapSheetLine(line, room)
			if len(pieces) < 2 {
				continue
			}
			wrapped = true
			for i, piece := range pieces {
				if i == 0 || col == 0 {
					continue
				}
				got := 0
				for _, r := range piece {
					if r != ' ' {
						break
					}
					got++
				}
				if got != col {
					t.Fatalf("at %d columns a wrapped row starts at %d, want the first line's column %d:\n  %q\n  %q",
						width, got, col, line, piece)
				}
			}
		}
		if !wrapped {
			t.Fatalf("at %d columns no /help row wrapped, so the column was never checked", width)
		}

		a := newTestApp(&fakeAgent{model: "m"})
		a.width, a.height = width, 80
		a.railAway = true
		a.slash("/help")
		var note *entry
		for i := range a.entries {
			if a.entries[i].kind == entryNote && strings.Contains(a.entries[i].text, "/help") {
				note = &a.entries[i]
			}
		}
		if note == nil || !note.sheet {
			t.Fatal("/help did not land as a column sheet")
		}
		body := a.bodyWidth()
		for _, row := range a.renderEntry(0, note, body) {
			plainRow := plain(row)
			if ansi.StringWidth(plainRow) > body {
				t.Fatalf("at %d columns a /help row is wider than the column:\n%q", width, plainRow)
			}
		}
	}
}
