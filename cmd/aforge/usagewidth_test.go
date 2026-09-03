package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A FLAG'S SENTENCE IS FOLDED BY WHAT IT DRAWS, NOT BY WHAT IT WEIGHS.
//
// [wrapAt]'s whole job is fitting an eighty-column terminal, and it measured
// `len` — bytes. The two agree only for ASCII. A sentence carrying a wide rune
// counted three bytes for two cells and wrapped short and ragged; a combining
// accent counted extra bytes for a mark that takes no cell at all, and did the
// same. Neither is visible today because every flag sentence in this binary is
// ASCII, which is exactly why it is worth pinning: the first model id or
// provider name with a non-ASCII character in it would have moved the fold and
// nothing would have said so.
//
// EVERY LINE IS READ BY CELL. A test that measured the folded lines the way the
// code used to measure them could not tell the two readings apart.
func TestAFlagSentenceIsFoldedByDisplayCellsAndNotByBytes(t *testing.T) {
	for _, sentence := range []struct {
		name string
		text string
		// widest is the widest word in the text, in cells: no fold can produce
		// a line narrower than that, so it is the floor a real fold sits above.
		widest int
	}{
		{
			name:   "plain ASCII, the case that already worked",
			text:   strings.TrimSpace(strings.Repeat("keep the whole record of this run ", 8)),
			widest: 6,
		},
		{
			name: "a double-width name, where a byte count reads three cells for two",
			// Each of these words draws eight cells and weighs twelve bytes.
			text:   strings.TrimSpace(strings.Repeat("模型名前 the model this run sat in ", 8)),
			widest: 8,
		},
		{
			name: "a combining sequence, where a byte count reads three cells for one",
			// "e" and a combining acute: two runes, three bytes, ONE cell.
			text:   strings.TrimSpace(strings.Repeat("caché the answer for this run ", 8)),
			widest: 6,
		},
	} {
		t.Run(sentence.name, func(t *testing.T) {
			const width = 74
			lines := wrapAt(sentence.text, width)
			if len(lines) == 0 {
				t.Fatal("the sentence folded to nothing at all")
			}
			for at, line := range lines {
				if drawn := ansi.StringWidth(line); drawn > width {
					t.Fatalf("line %d draws %d cells in a %d-column fold:\n  %q",
						at+1, drawn, width, line)
				}
			}
			// AND IT USED THE ROOM IT HAD. A fold that measures bytes wraps
			// early on this text and leaves a column of white space nobody
			// asked for; every line but the last must be within one word of
			// full.
			for at, line := range lines[:len(lines)-1] {
				drawn := ansi.StringWidth(line)
				next := strings.Fields(lines[at+1])[0]
				if drawn+1+ansi.StringWidth(next) <= width {
					t.Fatalf("line %d draws %d of %d cells and the next word %q would still have fit — "+
						"the fold is measuring something other than what the terminal draws:\n  %q",
						at+1, drawn, width, next, line)
				}
				if drawn < sentence.widest {
					t.Fatalf("line %d draws %d cells, narrower than the widest word in the sentence (%d):\n  %q",
						at+1, drawn, sentence.widest, line)
				}
			}
			// Nothing was dropped or duplicated on the way through.
			if joined, want := strings.Join(strings.Fields(strings.Join(lines, " ")), " "),
				strings.Join(strings.Fields(sentence.text), " "); joined != want {
				t.Fatalf("the fold changed the words:\n  got:  %q\n  want: %q", joined, want)
			}
		})
	}
}
