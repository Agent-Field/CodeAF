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
		{"the crew step's prose", setupCrewWord},
		{"the spending step's title", setupRailsTitle},
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
