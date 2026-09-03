package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// switcherAskRow is the row the audit caught the surface clipping: a question
// waiting on somebody, with a long note, a project tag and an age.
func switcherAskRow() switcherRow {
	return switcherRow{
		kind:    switcherConversation,
		title:   "tell me when CI goes red on master",
		project: "aforge-v2",
		note:    "asks: May I re-run the typecheck job to see whether it is flaky?",
		age:     "4m",
		needs:   true,
	}
}

func switcherRowText(row switcherRow, width int) string {
	return ansi.Strip(switcherPaintRow(row, width, newPalette(tokens.NoColor, false), false, switcherPaint{}))
}

// TestTheNameIsWholeBeforeAnyFactGetsACell is rowfit.go's law 1 on the list this
// surface exists to choose from.
//
// The give-way loop's guard used to be an EIGHT-CELL FLOOR ON THE NAME, so it
// never fired while the title had eight cells left: at a hundred columns the row
// drew `? tell me when CI goe… aforge-v2 asks: May I re-run the typecheck job to
// see whether it is flaky? 4m` — eighteen cells on the one fact that tells this
// conversation from another, and seventy-eight on a sentence the card exists to
// carry properly.
func TestTheNameIsWholeBeforeAnyFactGetsACell(t *testing.T) {
	row := switcherAskRow()
	const width = 100
	drawn := switcherRowText(row, width)
	if !strings.Contains(drawn, row.title) {
		t.Fatalf("at %d columns the list drew\n\t%q\nand the name it is a list of is %q — the facts give way first, and the name is cut only when the frame will not hold it alone",
			width, drawn, row.title)
	}
	if strings.Contains(drawn, row.note) {
		t.Fatalf("at %d columns the list drew\n\t%q\nwith the whole note on it — the note is the first fact to give way, and it did not give way soon enough to leave the name whole",
			width, drawn)
	}
	// AND THE FACTS THAT STILL FIT ARE STILL THERE. Dropping more than the row
	// had to would be the same defect with the halves swapped.
	for _, want := range []string{row.project, row.age} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("at %d columns the list drew\n\t%q\nand lost %q, which fits beside the whole name", width, drawn, want)
		}
	}
}

// TestAWideFrameKeepsTheNoteBesideTheWholeName is the other end of the same
// ladder: a fact gives way for the name and for nothing else.
func TestAWideFrameKeepsTheNoteBesideTheWholeName(t *testing.T) {
	row := switcherAskRow()
	const width = 160
	drawn := switcherRowText(row, width)
	for _, want := range []string{row.title, row.note, row.project, row.age} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("at %d columns the list drew\n\t%q\nand lost %q, which the frame had room for", width, drawn, want)
		}
	}
}

// TestANameTooLongForTheFrameTakesTheRowAlone is the one case law 1 allows a cut
// in — and it is the case the eight-cell floor is still there for.
func TestANameTooLongForTheFrameTakesTheRowAlone(t *testing.T) {
	row := switcherAskRow()
	row.title = strings.Repeat("a very long conversation name ", 4)
	const width = 60
	drawn := switcherRowText(row, width)
	if ansi.StringWidth(drawn) > width {
		t.Fatalf("the row overran its frame at %d columns:\n\t%q", width, drawn)
	}
	for _, gone := range []string{row.note, row.project, row.age} {
		if strings.Contains(drawn, gone) {
			t.Fatalf("at %d columns a name that had to be cut still drew the fact %q:\n\t%q\nwant the name alone, which is what [rowPlan.fit] does with a cut name",
				width, gone, drawn)
		}
	}
	if !strings.Contains(drawn, glyphMore) {
		t.Fatalf("a name too long for the frame was not marked as cut:\n\t%q", drawn)
	}
}

// TestAPhoneInboxDrawsAStandingItemOnlyOnce is homephone.go's second law — A ROW
// APPEARS ONCE — held for the third kind of row on that screen.
//
// A watch that needs somebody was lifted into `waiting on you` AND drawn again
// under its own project four rows later: two lines each, four of the twenty-six
// a pocket terminal has, on the one tier with none to spare.
func TestAPhoneInboxDrawsAStandingItemOnlyOnce(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	a := phoneHome(t, lab, mine)
	const words = "tell me when CI goes red on master"
	a.home.items = map[string][]StandingItemView{lab.project("-tmp-alpha"): {{Item: standing.Item{
		ID: "watch", Words: words, NeedsPerson: "may I re-run the typecheck job?", Updated: now,
	}}}}
	a.home.rebuild()
	text := phoneText(a)
	if n := strings.Count(text, words); n != 1 {
		t.Fatalf("the watch was drawn %d times, want once — lifted into `waiting on you` and not again under its project:\n%s", n, text)
	}
	if !strings.Contains(text, homePhoneWaitingWord) {
		t.Fatalf("the watch was not lifted into %q at all:\n%s", homePhoneWaitingWord, text)
	}
}
