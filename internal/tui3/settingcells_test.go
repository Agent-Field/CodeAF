package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// ── THE TAB STRIP IS LAID OUT IN CELLS, NOT IN BYTES ────────────────────────
//
// [tabChipCols] measured `len(title)`, a count of bytes, while everything that
// reads it — [tabWindow]'s fit, [tabSpans]'s arithmetic, the band [sheetTabBar]
// paints under the open tab — is laid out in the columns a terminal draws. The
// two agree only while every tab title is ASCII, which they all are today; the
// moment one is not, the strip hides the wrong number of chips and a click
// lands on the neighbour of the tab it was aimed at.
//
// The titles below are the two ways the readings part company: a double-width
// rune, which weighs three bytes and draws two cells, and a combining accent,
// which weighs two bytes and draws none at all.

// cellTabs is the strip with one wide title and one combining one in it. The
// ASCII names stay so that the geometry under test is the real strip's — nine
// chips that do not fit eighty columns — and not a special case.
var cellTabs = []string{
	"Session",
	"模型設定",  // four runes, twelve bytes, EIGHT cells
	"café", // five runes, six bytes, FOUR cells
	"Workspace",
	"Display",
	"Providers",
	"Spending",
	"Connections",
	"Context",
}

// withTabs swaps the strip's titles for the length of one test. The three
// functions under test read this one package variable and nothing else, so
// nothing outside them can see the change.
func withTabs(t *testing.T, titles []string) {
	t.Helper()
	previous := settingTabs
	settingTabs = titles
	t.Cleanup(func() { settingTabs = previous })
}

// A CHIP IS AS WIDE AS ITS WORD DRAWS.
func TestATabChipIsMeasuredInTheCellsItDrawsAndNotInBytes(t *testing.T) {
	for _, title := range []struct {
		name  string
		title string
		cells int
	}{
		{"an ASCII word, where bytes and cells agree", "Session", 7},
		{"a double-width word, three bytes to two cells", "模型設定", 8},
		{"a combining accent, which draws no cell of its own", "café", 4},
	} {
		t.Run(title.name, func(t *testing.T) {
			want := title.cells + tabPadCols
			if got := tabChipCols(title.title); got != want {
				t.Fatalf("the chip for %q is sized at %d cells\n  sized: %d\n  draws: %d (%d for the word, %d for its air)\n"+
					"  a strip sized in bytes hides the wrong chips and mis-aims every click after this one",
					title.title, got, got, want, title.cells, tabPadCols)
			}
		})
	}
}

// AND THE STRIP PUTS EVERY CHIP WHERE IT SAYS IT DID, READ BY CELL.
//
// The span a click is resolved against is a pair of COLUMN numbers, so the only
// honest way to check it is to cut the drawn line at those columns. The strip's
// existing tests slice the line as `[]rune`, which is right for ASCII, wrong by
// one cell per wide rune, and cannot settle a combining mark at all — the two
// runes of `e´` are one cell and a rune index walks straight past it.
func TestTheSettingsTabStripPlacesEveryChipWhereItSaysInCells(t *testing.T) {
	withTabs(t, cellTabs)
	pal := newPalette(tokens.ANSI256, false)
	for _, width := range []int{160, 120, 80, 60, 40} {
		for active, title := range settingTabs {
			flat := plain(sheetTabBar(width, active, pal))
			if drawn := ansi.StringWidth(flat); drawn > width {
				t.Fatalf("at %d columns, standing on %q, the strip draws %d cells:\n  %q",
					width, title, drawn, flat)
			}
			span := tabSpans(width, active)[active]
			if span.to <= span.from {
				t.Fatalf("at %d columns the open tab %q has no span at all", width, title)
			}
			// The cells the span claims, cut out of the line at those very
			// columns. Anything but the tab's own word means the strip and the
			// pointer disagree about where that chip is.
			if got := strings.TrimSpace(ansi.Cut(flat, span.from, span.to)); got != title {
				t.Fatalf("at %d columns the span for %q covers columns %d-%d, which draw %q\n"+
					"  covers: %q\n  want:   %q\n  line:   %q",
					width, title, span.from, span.to, got, got, title, flat)
			}
			for _, x := range []int{span.from, span.to - 1} {
				if at, ok := tabAtColumn(x, width, active); !ok || at != active {
					t.Fatalf("at %d columns, a press on column %d of %q landed on %s (found=%v)\n  line: %q",
						width, x, title, settingTabs[at], ok, flat)
				}
			}
		}
	}
}
