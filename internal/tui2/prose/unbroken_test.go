package prose

// A LINK IS COPIED, NOT READ ALONG.
//
// The measure is the length a SENTENCE is read at, and a bare URL or a path is
// not a sentence. Breaking one at the measure did two wrong things at once: it
// broke a token that must not be broken, and it broke it early — at 160 columns
// the answer column is 130 cells and the measure is 88, so a link that would
// have fitted whole came out as `…&st` / `ream=true…` with forty columns of the
// frame standing empty beside it, and nothing on the row saying whether the
// break was the renderer's or the model's.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// theUnbreakables are the tokens this is about, at the lengths they really
// arrive at: a streaming API URL with a query on it, and an absolute transcript
// path. Both are longer than [DefaultMeasure] and shorter than a wide column.
var theUnbreakables = []struct {
	name string
	text string
}{
	{
		"a url with a query",
		"https://openrouter.ai/docs/api-reference/streaming?model=deepseek/deepseek-v4-flash-latest&stream=true",
	},
	{
		"an absolute path",
		"/home/dev/.aforge/v3/projects/-home-dev-work-aforge-v2-checkout/ffff0000longmsg1/transcript.jsonl",
	},
}

// rowHolding is the row a token came out whole on, or "" when no row has all of
// it. Asserting on the ROW rather than on the joined document is what makes the
// difference between "the characters are all somewhere" and "a person can select
// this in one go".
func rowHolding(rows []string, token string) string {
	for _, row := range rows {
		if strings.Contains(ansi.Strip(row), token) {
			return row
		}
	}
	return ""
}

// A TOKEN THAT FITS THE COLUMN IS LEFT WHOLE, even though it is longer than the
// reading measure the sentence around it is wrapped to.
func TestALinkTooLongForTheMeasureTakesTheWholeColumnBeforeItBreaks(t *testing.T) {
	for _, item := range theUnbreakables {
		src := "Look at " + item.text + " for the shape."
		for _, width := range []int{160, 130, 120, 110} {
			if width <= DefaultMeasure {
				t.Fatalf("%s: width %d is not wider than the measure, so this case proves nothing", item.name, width)
			}
			if ansi.StringWidth(item.text) <= DefaultMeasure {
				t.Fatalf("%s is %d cells, which the measure already fits — this case cannot see the defect",
					item.name, ansi.StringWidth(item.text))
			}
			rows := render(t, src, Options{Width: width, Measure: DefaultMeasure})
			row := rowHolding(rows, item.text)
			if row == "" {
				t.Fatalf("%s was broken at width %d, where it would have fitted whole in %d cells:\n%s",
					item.name, width, ansi.StringWidth(item.text), strings.Join(plainRows(rows), "\n"))
			}
			if cells := ansi.StringWidth(row); cells > width {
				t.Fatalf("%s: the row holding it is %d cells in a %d-cell column: %q", item.name, cells, width, row)
			}
		}
	}
}

// AND THE SENTENCES AROUND IT STILL WRAP TO THE MEASURE. The token is the
// exception and it is the only one: nothing else on the block grew a cell.
func TestOnlyTheUnbreakableTokenPassesTheReadingMeasure(t *testing.T) {
	prose := strings.TrimSpace(strings.Repeat("an ordinary sentence about the shape of the thing ", 8))
	rows := render(t, prose, Options{Width: 160, Measure: DefaultMeasure})
	for i, row := range rows {
		if cells := ansi.StringWidth(row); cells > DefaultMeasure {
			t.Fatalf("row %d of ordinary prose is %d cells and the measure is %d: %q",
				i, cells, DefaultMeasure, row)
		}
	}
}

// AND THE CEILING IS STILL A CEILING. A token longer than the COLUMN is broken
// at the column, and the pieces carry all of it.
func TestATokenLongerThanTheColumnIsStillBrokenAtTheColumn(t *testing.T) {
	token := strings.Repeat("supercalifragilistic", 12)
	width := 100
	if ansi.StringWidth(token) <= width {
		t.Fatalf("the token is %d cells, which fits a %d-cell column — this case proves nothing",
			ansi.StringWidth(token), width)
	}
	rows := render(t, token, Options{Width: width, Measure: DefaultMeasure})
	joined := strings.Join(plainRows(rows), "")
	if joined != token {
		t.Fatalf("breaking the token at the column lost or changed it:\nwant %q\ngot  %q", token, joined)
	}
	// It uses the whole column and not the measure, which is the half of the
	// row about breaking LATE rather than about not breaking at all.
	if cells := ansi.StringWidth(rows[0]); cells != width {
		t.Fatalf("the first piece is %d cells in a %d-cell column, want the whole column: %q",
			cells, width, rows[0])
	}
}

func plainRows(rows []string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ansi.Strip(row)
	}
	return out
}
