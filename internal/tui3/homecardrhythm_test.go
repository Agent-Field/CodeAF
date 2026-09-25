package tui3

import (
	"strings"
	"testing"
)

// rhythmBands is a card of one band per group, each band one row, so that what
// the assertions read is the GAPS and nothing else.
func rhythmBands() []cardBand {
	bands := cardBandsOf(cardGroupIdentity, []string{"name", "place"})
	bands = append(bands, cardBandsOf(cardGroupActivity, []string{"work"}, []string{"made for you"})...)
	bands = append(bands, cardBandsOf(cardGroupEconomics, []string{"spent $1.63"})...)
	bands = append(bands, cardBandsOf(cardGroupVerbs, []string{"→ verbs: close"})...)
	return bands
}

// gapsBefore is how many blank rows stand immediately above a row's text.
func gapsBefore(rows []string, text string) int {
	at := -1
	for i, row := range rows {
		if strings.TrimSpace(row) == text {
			at = i
			break
		}
	}
	if at < 0 {
		return -1
	}
	blanks := 0
	for at--; at >= 0 && strings.TrimSpace(rows[at]) == ""; at-- {
		blanks++
	}
	return blanks
}

// ONE GAP LAW, AND IT IS THE GROUP THAT DECIDES WHICH.
//
// The card is four readings and the size of the gap is what says where one ends
// and the next begins (homecardrhythm.go). This asserts the RULE — a group
// boundary is [homeCardGroupGap] and everything inside a group is
// [homeCardGap] — rather than the row numbers a particular card happens to
// produce, so a band added between two others cannot quietly change the rhythm.
func TestTheCardsGapIsOneInsideAGroupAndTwoBetweenThem(t *testing.T) {
	rows := homeCardStack(rhythmBands(), 40)

	// The address is the title's second ROW and not a band of its own, so there
	// is no gap at all between them.
	if got := gapsBefore(rows, "place"); got != 0 {
		t.Fatalf("the place line has %d blank rows over it, want none:\n%s", got, strings.Join(rows, "\n"))
	}
	if at := gapsBefore(rows, "made for you"); at != homeCardGap {
		t.Fatalf("inside the activity group the gap is %d, want %d:\n%s", at, homeCardGap, strings.Join(rows, "\n"))
	}
	for _, first := range []string{"work", "spent $1.63", "→ verbs: close"} {
		if at := gapsBefore(rows, first); at != homeCardGroupGap {
			t.Fatalf("the group beginning %q has a gap of %d, want %d:\n%s",
				first, at, homeCardGroupGap, strings.Join(rows, "\n"))
		}
	}
}

// AIR IS THE FIRST THING GIVEN UP, AND A FACT IS THE LAST.
//
// A frame too short for the group rhythm is redrawn at the flat one before a
// single band is dropped; only a frame too short for THAT loses bands, and it
// loses them from the bottom. Whitespace is the cheapest thing on the card.
func TestTheCardGivesUpItsAirBeforeItGivesUpABand(t *testing.T) {
	bands := rhythmBands()
	wide := homeCardStack(bands, 40)
	flat := homeCardStack(bands, len(wide)-1)

	// Every fact the wide card drew is still on the short one — only blanks went.
	for _, row := range wide {
		if strings.TrimSpace(row) == "" {
			continue
		}
		if gapsBefore(flat, strings.TrimSpace(row)) < 0 {
			t.Fatalf("%q went when only the air should have:\n%s", row, strings.Join(flat, "\n"))
		}
	}
	if at := gapsBefore(flat, "spent $1.63"); at != homeCardGap {
		t.Fatalf("the short card's gap is %d, want the flat rhythm's %d:\n%s",
			at, homeCardGap, strings.Join(flat, "\n"))
	}

	// And a frame too short for even that drops from the BOTTOM, never the top.
	cut := homeCardStack(bands, len(flat)-1)
	if gapsBefore(cut, "→ verbs: close") >= 0 {
		t.Fatalf("the last band survived a frame with no room for it:\n%s", strings.Join(cut, "\n"))
	}
	if cut[0] != "name" || cut[homeCardPlaceRow] != "place" {
		t.Fatalf("the identity band is not the card's first band:\n%s", strings.Join(cut, "\n"))
	}
}

// AND THE TITLE NEVER GOES AT ALL. A card given one row is its own first band,
// which is what makes this a preview rather than a blank column.
func TestTheCardKeepsItsIdentityInAnyRoomAtAll(t *testing.T) {
	rows := homeCardStack(rhythmBands(), 1)
	if len(rows) != 1 || rows[0] != "name" {
		t.Fatalf("a one-row card is %q, want the title alone", strings.Join(rows, "\n"))
	}
}

// A LIST THAT WRAPS KEEPS THE COMMA ON THE ROW IT ENDS.
//
// The verbs line is a sentence and not a run of independent facts, so its
// punctuation belongs to the clause before it — a fold that dropped the comma
// would read as two lists. The room for it is reserved while the row is packed,
// which is what makes the rule hold at the width where the clause reaches the
// last cell and not only at the comfortable ones ([bandClausesWithSeparator]).
func TestAWrappedCommaListKeepsItsCommaAtEveryWidth(t *testing.T) {
	plain := func(s string) string { return s }
	// From 30 up: below the card's own floor ([homeCardCol] is 36) there is
	// nothing to promise, and at a width where one clause fills a row to the last
	// cell the clause keeps the cell and the comma goes — a word matters more
	// than the punctuation after it.
	words := []string{"→ verbs: close", "copy name", "new in project", "open folder"}
	for width := 30; width <= 60; width++ {
		rows := bandClausesWithSeparator(width, 9, ", ", plain, words...)
		for at, row := range rows[:max(0, len(rows)-1)] {
			if !strings.HasSuffix(strings.TrimRight(row, " "), ",") {
				t.Fatalf("at width %d row %d ends %q, want a comma:\n%s",
					width, at, row, strings.Join(rows, "\n"))
			}
		}
		if last := rows[len(rows)-1]; strings.HasSuffix(strings.TrimSpace(last), ",") {
			t.Fatalf("at width %d the last row ends in a comma: %q", width, last)
		}
	}
}

// AND THE DOT BETWEEN INDEPENDENT FACTS DOES THE OPPOSITE. It stands BETWEEN
// two clauses, so the row break stands in for it and it never survives a fold.
func TestAWrappedFactsLineDropsItsDotAtTheFold(t *testing.T) {
	plain := func(s string) string { return s }
	rows := bandClauses(24, 0, plain, "touched 6 files", "spent $1.88", "186.9k tokens")
	if len(rows) < 2 {
		t.Fatalf("the facts did not wrap at 24 cells:\n%s", strings.Join(rows, "\n"))
	}
	for at, row := range rows {
		if strings.HasSuffix(strings.TrimSpace(row), "·") {
			t.Fatalf("row %d ends in a separator: %q", at, row)
		}
	}
}
