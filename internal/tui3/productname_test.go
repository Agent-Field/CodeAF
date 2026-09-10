package tui3

import (
	"strings"
	"testing"
)

// THE PRODUCT HAS ONE NAME AND EVERY SCREEN SAYS IT.
//
// The wordmark on the first screen of a fresh install used to spell `openaf`
// while the prose three rows under it — on the same frame — said `aforge`, and
// the top line of every place after it said `aforge` again. Two constants held
// the one fact (styles.go's [product] and a `pulseName` that no longer exists),
// which is the ONE SOURCE OF TRUTH law's own worked example: a name written down
// twice is a name that gets renamed once.

// TestTheProductIsNamedOnceAndItIsTheNameYouType holds the constant to the
// command a person actually typed to get here.
func TestTheProductIsNamedOnceAndItIsTheNameYouType(t *testing.T) {
	if product != "aforge" {
		t.Fatalf("this surface calls itself %q, and a person reaches it by typing %q — the wordmark and the binary have to be one word", product, "aforge")
	}
}

// TestTheWordmarkCanSpellTheProductsWholeName is the gate the letterforms need:
// [wordmarkRows] SKIPS a letter it has no glyph for, so a missing form is not a
// build error — it is the product's name with a hole in it, three rows tall, on
// the first screen anybody sees.
func TestTheWordmarkCanSpellTheProductsWholeName(t *testing.T) {
	for _, letter := range product {
		if _, ok := wordmarkGlyphs[letter]; !ok {
			t.Fatalf("the wordmark has no letterform for %q, so it draws %q where it should draw %q", string(letter), drawnWordmark(t), product)
		}
	}
	rows := wordmarkRows(false)
	if len(rows) != 3 {
		t.Fatalf("the wordmark is %d rows, want 3", len(rows))
	}
	// Three cells a letter and one between them, which is what says every letter
	// of the name is on the row rather than most of them.
	want := 4*len([]rune(product)) - 1
	for i, row := range rows {
		if got := len([]rune(row)); got != want {
			t.Fatalf("wordmark row %d is %d cells and %q is %d letters:\n%s\nwant a row of %d cells", i, got, product, len([]rune(product)), strings.Join(rows, "\n"), want)
		}
	}
	if got := wordmarkRows(true); len(got) != 1 || got[0] != product {
		t.Fatalf("the terminal that cannot draw boxes was given %q, want the one word %q", got, product)
	}
}

// TestTheFirstScreensWordmarkAndItsProseNameOneProduct is the clash itself: the
// letterforms and the sentence under them, on one frame, read off one constant.
func TestTheFirstScreensWordmarkAndItsProseNameOneProduct(t *testing.T) {
	prose := []struct {
		what string
		said string
	}{
		{"the connect step's prose", setupKeyWord},
		{"the sign-in step's prose", setupConnectWord},
		{"the day's limit explanation", controlLimitWord},
		{"the day's limit detail", controlLimitDetail},
		{"the door onto what esc walked past", setupStepLater(setupControls)},
		{"the OAuth consent line", connectPurpose("Google")},
		{"the desktop notification's title", notifyTitle},
	}
	for _, row := range prose {
		if !strings.Contains(row.said, product) {
			t.Fatalf("%s reads %q — the wordmark over it draws %q, and one screen may not name two products", row.what, row.said, product)
		}
		if strings.Contains(row.said, "openaf") {
			t.Fatalf("%s still spells the retired name:\n\tdrawn: %s\n\twant:  the same sentence with %q in it", row.what, row.said, product)
		}
	}
}

// drawnWordmark is the wordmark as the screen would hold it, for a failure
// message that shows the hole rather than describing it.
func drawnWordmark(t *testing.T) string {
	t.Helper()
	return strings.Join(wordmarkRows(false), "\n")
}

// TestTheWordmarksRightEdgeIsNeverAHoleBetweenTwoStrokes is the letterform's own
// gate, and it is about the RIGHT EDGE OF THE WHOLE WORD.
//
// The last glyph of [product] is drawn at the right edge of the first block
// anybody sees, and `e` used to be `┌─┐ / ├─  / └─┘`: a blank cell with the
// bowl's `┐` directly above it and its `┘` directly below. A hole punched
// through the edge of a block of box-drawing, with ink on both sides of it, does
// not read as an open letterform — it reads as a word the terminal cut off, and
// the wave that found this filed it as a truncation and went looking for a
// layout bug that was not there.
//
// An edge cell that is blank because the letter simply STOPS there is fine and
// is what an `r` or an `f` looks like; the defect is only the hole BETWEEN two
// strokes, which is what this asserts and nothing more.
func TestTheWordmarksRightEdgeIsNeverAHoleBetweenTwoStrokes(t *testing.T) {
	rows := wordmarkRows(false)
	if len(rows) < 3 {
		t.Fatalf("the wordmark is %d rows, want at least 3", len(rows))
	}
	edge := make([]rune, len(rows))
	for i, row := range rows {
		runes := []rune(row)
		if len(runes) == 0 {
			t.Fatalf("wordmark row %d is empty:\n%s", i, strings.Join(rows, "\n"))
		}
		edge[i] = runes[len(runes)-1]
	}
	for r := 1; r < len(edge)-1; r++ {
		if edge[r] == ' ' && edge[r-1] != ' ' && edge[r+1] != ' ' {
			t.Fatalf("the wordmark's last letter has a hole in its right edge on row %d — %q above, a blank, %q below — "+
				"so the word reads as one the terminal cut off:\n%s\nclose that cell with a stroke",
				r, string(edge[r-1]), string(edge[r+1]), strings.Join(rows, "\n"))
		}
	}
	// AND THE EDGE IS STILL A LETTER AND NOT A BOX. `a` closes its crossbar with
	// `┤` and `e` must not, or the two letters of this name that differ in one
	// cell stop differing at all.
	if wordmarkGlyphs['e'] == wordmarkGlyphs['a'] {
		t.Fatalf("`e` and `a` are now the same letterform %v — the name would read `aforga`", wordmarkGlyphs['e'])
	}
}
