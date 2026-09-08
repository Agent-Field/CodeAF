package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// THE RAIL IS A STRAIGHT LINE, on every row, whatever is written beside it.
//
// It was not: a row carrying two `❤️` or two `🇯🇵` put the divider two cells left
// of where every other row put it, because the layout measured with grapheme
// widths while the renderer beneath it composed cells with the older reading.
// The sequences an earlier note blamed — ZWJ families — were never the ones that
// bent it; that note was read off a `capture-pane` dump, which is one entry per
// grid CELL and cannot be indexed by rune.
//
// The assertion is the one a person makes by looking: EVERY ROW PUTS THE RAIL IN
// THE SAME COLUMN. It is made twice, once in each of the two readings a terminal
// can give, because a fix that hardcoded either one would be right on half the
// terminals in the world and would pass a test that only checked its own half.
func TestTheRailStandsInOneColumnWhateverIsWrittenBesideIt(t *testing.T) {
	rows := []string{
		"a plain sentence with nothing unusual in it at all",
		"two hearts ❤️❤️ and then some words after them",
		"two flags \U0001F1EF\U0001F1F5\U0001F1EF\U0001F1F5 and then some words after them",
		"a family \U0001F468‍\U0001F469‍\U0001F467 and then some words after it",
		"日本語のテキスト that mixes wide cells with narrow ones",
		"a café with its accent hanging off the letter before",
	}
	for _, answer := range []struct {
		what string
		msg  tea.ModeReportMsg
	}{
		{"a terminal that never answered", tea.ModeReportMsg{}},
		{"a terminal that reported mode 2027", tea.ModeReportMsg{
			Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet,
		}},
	} {
		a := &app{}
		a.width, a.height = 120, 40
		if answer.msg.Mode != nil {
			a.ruler.noteModeReport(answer.msg)
		}
		const rail = "│"
		want := -1
		for _, row := range rows {
			joined := a.railJoin(row, rail)
			at := a.ruler.cells(strings.TrimSuffix(joined, rail))
			if want == -1 {
				want = at
				continue
			}
			if at != want {
				t.Fatalf("%s: the rail stands at column %d on\n\t%q\nbut at column %d on every row before it — "+
					"a divider that moves is a frame that looks broken", answer.what, at, row, want)
			}
		}
	}
}
