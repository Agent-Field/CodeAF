package tui3

// THE PERSON'S OWN WORDS AND THE DIVIDER BESIDE THEM.
//
// [app.railJoin] pads a conversation row out to [app.bodyWidth] and writes the
// rail's seam in the very next column, so a body row that measures the column
// exactly ends with its last letter against the rule. A person's message is the
// block this happens to, because it is wrapped to the COLUMN while the model's
// prose is wrapped to a reading measure several cells short of it — one row of a
// six-line question read `…and tell me│` with the words touching the divider
// while the rows above and below it stood clear.
//
// EVERY ASSERTION HERE IS IN DISPLAY CELLS. A rune count is what let this whole
// class of defect live on this surface, so the sequences below are the ones a
// rune count gets wrong — a ZWJ family, a VS16 heart, a regional-indicator flag,
// full-width CJK and a combining acute — and not one of them is measured by
// slicing []rune.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// hostileWidths is one line per way a rune count and a cell count disagree.
// Every one of them is a real sequence, spelled with its code points visible.
var hostileWidths = []struct {
	name string
	text string
}{
	{"a zwj family", "\U0001F469‍\U0001F469‍\U0001F467‍\U0001F466"},
	{"a vs16 heart", "❤️"},
	{"a regional-indicator flag", "\U0001F1EF\U0001F1F5"},
	{"full-width cjk", "日本語のテキスト"},
	{"a combining acute", "café naïve"},
	{"plain ascii", "the quick brown fox"},
}

// userRowsAt is every row of the person's own message as the conversation draws
// it, unpainted and with the trailing pad left off — what is asserted is where
// the LAST CELL of the text falls, which is what the rail sits beside.
func userRowsAt(a *app, width int) []string {
	var out []string
	for _, r := range a.visible(width) {
		if text := plain(r.text); strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimRight(text, " "))
		}
	}
	return out
}

// A PERSON'S OWN WORDS STOP A CELL SHORT OF THE RAIL, at every width and with
// every grapheme this surface has been wrong about.
func TestAPersonsWrappedWordsNeverTouchTheRail(t *testing.T) {
	for _, hostile := range hostileWidths {
		// A paragraph long enough to wrap several times at every width below, so
		// at least one row is forced hard against the column's edge.
		text := strings.TrimSpace(strings.Repeat(hostile.text+" and then some ordinary words to fill the row out ", 8))
		for _, width := range []int{160, 120, 100, 80, 60} {
			a := newTestApp(&fakeAgent{model: "m"})
			a.width, a.height = width, 40
			a.entries = []entry{{kind: entryUser, text: text}}
			body := a.bodyWidth()
			for at, row := range userRowsAt(a, body) {
				cells := ansi.StringWidth(row)
				if cells > body-userRailGutter {
					t.Fatalf("%s at %d columns: row %d of the message is %d cells wide in a %d-cell column, "+
						"so it ends against the rail with no gutter — want at most %d\n%s",
						hostile.name, width, at, cells, body, body-userRailGutter, row)
				}
			}
		}
	}
}

// AND THE FOLD THAT COUNTS THOSE ROWS COUNTS THE ROWS THE PAINT MAKES. The two
// are the two ends of one measurement ([userLead] says so): the paint wraps the
// message, and the door under a node's instruction says how many rows the fold
// is holding back. A fold that kept the OLD measure while the paint took the new
// one reports one row fewer than the paint hid, and the door then offers to open
// something it has already mis-counted.
//
// THE WIDTH IS CHOSEN SO THE ONE CELL MATTERS, and the test says so out loud
// before it asserts anything: a case where both measures wrap to the same number
// of rows would pass against the drift it is named for.
func TestTheBriefFoldCountsTheLinesThePaintActuallyMakes(t *testing.T) {
	text := strings.TrimSpace(strings.Repeat("word ", 60))
	discriminating := 0
	for width := 100; width <= 170; width++ {
		a := newTestApp(&fakeAgent{model: "m"})
		a.width, a.height = width, 80
		body := a.bodyWidth()
		// The one cell has to change the ROW COUNT here, or this width cannot
		// tell a fold on the old measure from a fold on the new one.
		if len(wrap(text, body-userLeadCols)) == len(wrap(text, userBodyCols(body))) {
			continue
		}
		discriminating++
		// The paint, asked of the renderer itself and not of an arithmetic said
		// twice here: an ordinary message, drawn whole.
		a.entries = []entry{{kind: entryUser, text: text}}
		painted := len(userRowsAt(a, body))
		// The same message as a node's instruction, whose door says what it hid.
		a.entries = []entry{{kind: entryUser, text: text, brief: true}}
		hidden := briefFoldHidden(&a.entries[0], body)
		if want := painted - briefFoldLines; hidden != want {
			t.Fatalf("at %d columns the paint makes %d rows of the message and shows %d of them, "+
				"so the door hides %d — and it says %d",
				width, painted, briefFoldLines, want, hidden)
		}
	}
	if discriminating == 0 {
		t.Fatal("no width between 100 and 170 wraps this message to a different number of rows " +
			"with and without the gutter's cell, so this test cannot see the drift it is named for")
	}
}
