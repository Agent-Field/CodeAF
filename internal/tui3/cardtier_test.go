package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE CARD ARRIVES ON AN ORDINARY WINDOW.
//
// It needed a hundred and sixty columns, so a laptop never saw one and could not
// discover it existed. The reason was the number the list is measured at:
// [homeSwitchFull] reserved room for the row's NOTE — the `asks: …` clause — and
// the note is the very fact the card was built to carry properly, so the layout
// held cells back from the card in order to draw the card's own sentence badly
// in the list.
func TestTheCardArrivesOnAnEverydayWindowAndNotOnlyOnAHugeOne(t *testing.T) {
	// A 140-column window is an ordinary large laptop, and it is the width the
	// audit's frames were captured at with no card on them.
	for _, width := range []int{136, 140, 160} {
		if got := homeTierAt(width); got != homeTierCard {
			t.Errorf("a %d-column frame is tier %v, want a card beside the list", width, got)
		}
	}
	if homeCardMin >= 160 {
		t.Fatalf("the card tier begins at %d columns, which is a window almost nobody has", homeCardMin)
	}
	// AND IT IS STILL AN ADDITION AND NOT A NUMBER SOMEBODY TYPED.
	if homeCardMin != homeSwitchFull+homeGutter+homeCardCol {
		t.Fatalf("homeCardMin is %d, want the sum of the columns and the gutter (%d)",
			homeCardMin, homeSwitchFull+homeGutter+homeCardCol)
	}
}

// AND WHAT THE LIST GIVES UP FOR IT IS THE NOTE, WHICH IS RANKED FIRST OUT.
//
// The name, the project tag and the age are what [homeSwitchFull] undertakes to
// draw whole, and they are all three on the row of a window that now has a card.
// The note is not, and that is the trade: it goes to the card.
func TestBesideACardARowStillKeepsItsNameItsProjectAndItsAge(t *testing.T) {
	row := switcherRow{
		kind:    switcherConversation,
		title:   strings.Repeat("a", homeSwitchName),
		project: strings.Repeat("p", 18),
		age:     "12h",
		note:    "asks: May I re-run the typecheck job to see whether it is flaky?",
	}
	pal := newPalette(tokens.NoColor, false)
	// A HUNDRED AND FORTY COLUMNS — an ordinary large laptop, and the width the
	// audit captured a frame at with no card on it at all.
	const width = 140
	list, card := homeColumns(width)
	if card < homeCardCol {
		t.Fatalf("a %d-column frame drew a card of %d cells, so there is no trade to check", width, card)
	}
	drawn := ansi.Strip(switcherPaintRow(row, list, pal, false, switcherPaint{}))

	for _, want := range []struct{ what, text string }{
		{"the name", row.title},
		{"the project tag", row.project},
		{"the age", row.age},
	} {
		if !strings.Contains(drawn, want.text) {
			t.Fatalf("beside a card the list is %d cells and %s gave way:\n%q", list, want.what, drawn)
		}
	}
	// AND THE NOTE HAS GONE, which is the trade this row is about: it is ranked
	// first out of the row and the card beside it is where it is drawn properly.
	if strings.Contains(drawn, "asks:") {
		t.Fatalf("the list at %d cells still draws the note the card exists to carry:\n%q", list, drawn)
	}
}

// AND THE LIST NEVER PAYS FOR THE CARD OUT OF THE THREE IT UNDERTOOK TO KEEP.
// Every width at which a card is drawn leaves the list [homeSwitchFull] cells or
// more, which is what the measurement above is a measurement OF.
func TestTheListNeverDropsBelowWhatItUndertookToDraw(t *testing.T) {
	for _, width := range []int{homeCardMin, 140, 160, 200, 400} {
		if list, _ := homeColumns(width); list < homeSwitchFull {
			t.Fatalf("a %d-column frame left the list %d cells, under the %d it draws every fact at",
				width, list, homeSwitchFull)
		}
	}
	// AND THE MEASUREMENT IS THE PARTS AND NOT A NUMBER: a name, a mark, a
	// project tag and an age, each fact carrying the space switcherTailWidth
	// puts in front of it.
	want := homeSwitchName + 1 + 2 + (1 + 18) + (1 + 3)
	if homeSwitchFull != want {
		t.Fatalf("homeSwitchFull is %d, want the row it is measured from (%d)", homeSwitchFull, want)
	}
}
