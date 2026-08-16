package prose

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

const goFence = "```go\n" +
	"// count returns how many times the sentinel appears.\n" +
	"func count(xs []string, want string) int {\n" +
	"\tn := 0\n" +
	"\tfor _, x := range xs {\n" +
	"\t\tif x == want {\n" +
	"\t\t\tn += 1\n" +
	"\t\t}\n" +
	"\t}\n" +
	"\treturn n\n" +
	"}\n" +
	"```\n"

// TestCodeHighlightsAtTruecolor is the positive half of the degradation ladder:
// where the ramp exists, a keyword, a string, a number and a comment are drawn
// in four different colours, and all four are colours THIS tree named.
func TestCodeHighlightsAtTruecolor(t *testing.T) {
	rows := render(t, goFence, Options{Width: 72, Styler: styler(tokens.TrueColor)})
	all := strings.Join(rows, "\n")
	for _, slot := range []tokens.CodeSlot{
		tokens.CodeKeyword, tokens.CodeNumber, tokens.CodeComment, tokens.CodeFunction,
	} {
		if !strings.Contains(all, slot.Fg(tokens.TrueColor, tokens.FocusNormal)) {
			t.Errorf("no %s in the highlighted block", slot)
		}
	}
}

// TestCodeHighlightsAt256 pins the middle rung. The cube is coarse, but the
// slots still resolve to different entries, so the block is a highlighted block
// and not a block wearing one colour.
func TestCodeHighlightsAt256(t *testing.T) {
	rows := render(t, goFence, Options{Width: 72, Styler: styler(tokens.ANSI256)})
	all := strings.Join(rows, "\n")
	seen := map[string]bool{}
	for _, slot := range tokens.CodeSlots() {
		if seq := slot.Fg(tokens.ANSI256, tokens.FocusNormal); strings.Contains(all, seq) {
			seen[seq] = true
		}
	}
	if len(seen) < 3 {
		t.Errorf("only %d distinct code colours survived 256, want at least 3", len(seen))
	}
}

// TestCodeDoesNotHighlightAt16 is the ruling itself: below 256 colours the ramp
// does not exist, so the block is drawn at the ordinary text tier and NOT in six
// of the classic sixteen. A lying colour is worse than no colour (5.20).
func TestCodeDoesNotHighlightAt16(t *testing.T) {
	for _, p := range []tokens.Profile{tokens.NoColor, tokens.ANSI16} {
		rows := render(t, goFence, Options{Width: 72, Styler: styler(p)})
		all := strings.Join(rows, "\n")
		for _, slot := range tokens.CodeSlots() {
			if seq := slot.Fg(p, tokens.FocusNormal); seq != "" && strings.Contains(all, seq) {
				t.Errorf("%s: %s was painted where the ramp does not exist", p, slot)
			}
		}
		if p == tokens.ANSI16 {
			if !strings.Contains(all, tokens.TextPrimary.Fg(p, tokens.FocusNormal)) {
				t.Errorf("%s: unhighlighted code is not at the primary tier", p)
			}
		}
		// Whatever the profile, the source itself must survive intact.
		if !strings.Contains(ansi.Strip(all), "func count(xs []string, want string) int {") {
			t.Errorf("%s: the source line did not survive", p)
		}
	}
}

// TestCodeKeepsOneRowPerSourceLine: indentation is how source is read, and a
// wrapped line puts a continuation at column zero where the eye is counting
// levels. One source line is one screen row, at every width.
func TestCodeKeepsOneRowPerSourceLine(t *testing.T) {
	want := 10 // the fence body, not counting the fences themselves
	for width := 20; width <= 140; width += 7 {
		rows := render(t, goFence, Options{Width: width, Styler: styler(tokens.TrueColor)})
		if len(rows) != want {
			t.Errorf("width %d: %d rows, want %d", width, len(rows), want)
		}
	}
}

// TestCodeBlockHasGutterAndGround checks the block's frame: a hairline gutter at
// the chrome tier, one cell of padding, and a raised ground where the profile
// has one — the padding is what draws the plane out to the right edge, so the
// rows are width-stable and the block is a rectangle.
func TestCodeBlockHasGutterAndGround(t *testing.T) {
	for _, p := range profiles {
		rows := render(t, goFence, Options{Width: 60, Styler: styler(p)})
		for i, row := range rows {
			flat := ansi.Strip(row)
			if !strings.HasPrefix(flat, tokens.GlyphCodeGutter+" ") {
				t.Fatalf("%s row %d has no gutter: %q", p, i, flat)
			}
			if p.SheetGround() {
				if got := ansi.StringWidth(row); got != 60 {
					t.Errorf("%s row %d is %d cells; a grounded block must fill its width", p, i, got)
				}
				if !strings.Contains(row, tokens.Sheet.Bg(p, tokens.FocusNormal)) {
					t.Errorf("%s row %d has no raised ground", p, i)
				}
			}
		}
	}
}

// TestCodeTabsBecomeIndentLevels: a tab inside a fence is an indent level, not
// a cursor movement, so it becomes four cells rather than one — the opposite of
// what [scrub] does to a tab in prose, and for the opposite reason.
func TestCodeTabsBecomeIndentLevels(t *testing.T) {
	rows := plain(render(t, "```\n\tone\n\t\ttwo\n```\n", Options{Width: 40}))
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d: %q", len(rows), rows)
	}
	if !strings.Contains(rows[0], "     one") { // gutter + pad + 4
		t.Errorf("one tab did not become four cells: %q", rows[0])
	}
	if !strings.Contains(rows[1], "         two") {
		t.Errorf("two tabs did not become eight cells: %q", rows[1])
	}
}

// TestUnknownLanguageStillRenders: a model writes ```mermaid and ```text and
// ```. A lexer we do not have must cost the reader nothing.
func TestUnknownLanguageStillRenders(t *testing.T) {
	for _, lang := range []string{"", "text", "mermaid", "not-a-language-at-all"} {
		src := "```" + lang + "\nplain body line\n```\n"
		rows := plain(render(t, src, Options{Width: 40, Styler: styler(tokens.TrueColor)}))
		if len(rows) != 1 || !strings.Contains(rows[0], "plain body line") {
			t.Errorf("lang %q lost its body: %q", lang, rows)
		}
	}
}

// TestIndentedCodeBlockRenders: four spaces is still a code block, and a model
// that has never heard of a fence still gets one.
func TestIndentedCodeBlockRenders(t *testing.T) {
	rows := plain(render(t, "text\n\n    indented := 1\n", Options{Width: 40}))
	last := rows[len(rows)-1]
	if !strings.HasPrefix(last, tokens.GlyphCodeGutter) || !strings.Contains(last, "indented := 1") {
		t.Errorf("indented code did not render as a block: %q", last)
	}
}

// TestCodeStyleCoversItsOwnRamp is a pin on the chroma seam: every colour
// [codeStyle] can produce must be one this tree named, or [slotFor] would
// silently answer "body tier" for a token that was meant to be coloured.
func TestCodeStyleCoversItsOwnRamp(t *testing.T) {
	for _, slot := range tokens.CodeSlots() {
		found := false
		for _, mapped := range slotIndex {
			if mapped == slot {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no chroma token type resolves to %s; the slot is unreachable", slot)
		}
	}
}
