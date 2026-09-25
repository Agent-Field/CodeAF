package prose

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The tests here are FRAME-SHAPED: they render at a width and a profile and
// assert on the rows a terminal would actually receive. That is deliberate.
// Every bug 13.3 found — questions rendered raw, tables wrapped to soup — was
// invisible to a test that checked an intermediate structure and visible in the
// first row of the frame, so the frame is what is asserted.

// profiles is the ladder every degradation claim is walked over. Naming it once
// means a test cannot quietly cover three of the four.
var profiles = []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor}

func styler(p tokens.Profile) *tokens.Styler {
	return tokens.NewStyler(p, tokens.FocusNormal)
}

// render is the test-side door. It returns the rows and, for the assertions
// that are about layout rather than colour, their plain-text forms.
func render(t *testing.T, src string, opts Options) []string {
	t.Helper()
	rows := Render(src, opts)
	for i, row := range rows {
		if strings.ContainsAny(row, "\n\r") {
			t.Fatalf("row %d contains a line break: %q", i, row)
		}
		if w := ansi.StringWidth(row); w > opts.Width {
			t.Fatalf("row %d is %d cells at width %d: %q", i, w, opts.Width, row)
		}
	}
	return rows
}

func plain(rows []string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ansi.Strip(row)
	}
	return out
}

func joined(rows []string) string { return strings.Join(plain(rows), "\n") }

// TestParagraphWrapsAtTheMeasure is the first half of "a 200-column window is a
// wide window, not a wide sentence". The terminal is 120 cells and the measure
// is 40; every row must obey the 40, and no row may end mid-word.
func TestParagraphWrapsAtTheMeasure(t *testing.T) {
	src := "The measure is the reading length and the width is the ceiling, " +
		"and a renderer that confused the two would hand the reader a sentence " +
		"a hundred and twenty cells long."
	rows := render(t, src, Options{Width: 120, Measure: 40, Styler: styler(tokens.TrueColor)})
	if len(rows) < 3 {
		t.Fatalf("expected the paragraph to wrap, got %d rows", len(rows))
	}
	for i, row := range plain(rows) {
		if w := ansi.StringWidth(row); w > 40 {
			t.Errorf("row %d is %d cells, measure is 40: %q", i, w, row)
		}
		if strings.HasPrefix(row, " ") || strings.HasSuffix(row, " ") {
			t.Errorf("row %d carries edge whitespace: %q", i, row)
		}
	}
	// Nothing may be lost or invented on the way through the wrapper.
	if got, want := strings.Join(strings.Fields(joined(rows)), " "), src; got != want {
		t.Errorf("wrapping changed the text:\n got %q\nwant %q", got, want)
	}
}

// TestMeasureDefaultsAndClamps pins the two rules a caller has to be able to
// predict: an unset measure is [DefaultMeasure], and a measure wider than the
// terminal is the terminal.
func TestMeasureDefaultsAndClamps(t *testing.T) {
	long := strings.Repeat("word ", 200)
	wide := render(t, long, Options{Width: 200})
	for _, row := range plain(wide) {
		if w := ansi.StringWidth(row); w > DefaultMeasure {
			t.Fatalf("unset measure wrapped at %d, want <= %d", w, DefaultMeasure)
		}
	}
	narrow := render(t, long, Options{Width: 30, Measure: 500})
	for _, row := range plain(narrow) {
		if w := ansi.StringWidth(row); w > 30 {
			t.Fatalf("an over-wide measure escaped the ceiling: %d cells", w)
		}
	}
}

// TestHeadingsPromoteByTier holds 5.13's ruling that a heading is louder by
// TIER and not by size. h1 and h2 stand at the primary tier, h3 steps down to
// secondary and h4 to tertiary — and only h1 takes bold, because bold is the
// one weight a terminal has and spending it on four levels spends it on none.
func TestHeadingsPromoteByTier(t *testing.T) {
	st := styler(tokens.TrueColor)
	want := []struct {
		src  string
		tok  tokens.Token
		bold bool
	}{
		{"# one", tokens.TextPrimary, true},
		{"## two", tokens.TextPrimary, false},
		{"### three", tokens.TextSecondary, false},
		{"#### four", tokens.TextTertiary, false},
		{"##### five", tokens.TextTertiary, false},
	}
	for _, w := range want {
		rows := render(t, w.src, Options{Width: 40, Styler: st})
		if len(rows) != 1 {
			t.Fatalf("%q rendered %d rows, want 1", w.src, len(rows))
		}
		seq := w.tok.Fg(tokens.TrueColor, tokens.FocusNormal)
		if !strings.Contains(rows[0], seq) {
			t.Errorf("%q is not drawn at %s: %q", w.src, w.tok, rows[0])
		}
		if got := strings.Contains(rows[0], "\x1b[1m"); got != w.bold {
			t.Errorf("%q bold = %v, want %v", w.src, got, w.bold)
		}
	}
}

// TestListsHang is the whole point of a list gutter: a wrapped item aligns
// under its own first word, never under its marker.
func TestListsHang(t *testing.T) {
	src := "- a bullet item long enough that it has to wrap onto a second row\n" +
		"- short\n"
	rows := plain(render(t, src, Options{Width: 34, Styler: styler(tokens.TrueColor)}))
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d: %q", len(rows), rows)
	}
	if !strings.HasPrefix(rows[0], tokens.GlyphProseBullet+" ") {
		t.Errorf("first row has no marker: %q", rows[0])
	}
	if !strings.HasPrefix(rows[1], "  ") || strings.HasPrefix(rows[1], "   ") {
		t.Errorf("continuation row does not hang at the marker column: %q", rows[1])
	}
	if !strings.HasPrefix(rows[2], tokens.GlyphProseBullet+" short") {
		t.Errorf("second item lost its marker: %q", rows[2])
	}
}

// TestOrderedListsShareARightEdge pins the numeric gutter: 9. and 10. line up
// on the period, so the numbers read as a column of numbers.
func TestOrderedListsShareARightEdge(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		b.WriteString("1. item\n")
	}
	rows := plain(render(t, b.String(), Options{Width: 40}))
	if len(rows) != 10 {
		t.Fatalf("want 10 rows, got %d", len(rows))
	}
	if got, want := rows[8], " 9. item"; got != want {
		t.Errorf("row 9 is %q, want %q", got, want)
	}
	if got, want := rows[9], "10. item"; got != want {
		t.Errorf("row 10 is %q, want %q", got, want)
	}
}

// TestABareNumberedReplyStillDraws holds the reply boundary where Markdown
// parses a number and full stop as an ordered list with an empty item.
func TestABareNumberedReplyStillDraws(t *testing.T) {
	for _, src := range []string{"32.", "1024."} {
		rows := plain(render(t, src, Options{Width: 80}))
		if got, want := strings.Join(rows, "\n"), src; got != want {
			t.Errorf("Render(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestBlockquoteCarriesItsGutter checks that the bar runs down EVERY row of the
// quote including the blank one between its paragraphs — a dashed bar reads as
// two quotes.
func TestBlockquoteCarriesItsGutter(t *testing.T) {
	src := "> first paragraph\n>\n> second paragraph\n"
	rows := plain(render(t, src, Options{Width: 40}))
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d: %q", len(rows), rows)
	}
	for i, row := range rows {
		if !strings.HasPrefix(row, tokens.GlyphProseQuote) {
			t.Errorf("row %d has no gutter bar: %q", i, row)
		}
	}
	if strings.TrimSpace(rows[1]) != tokens.GlyphProseQuote {
		t.Errorf("the gap row should be bar-only, got %q", rows[1])
	}
}

// TestBlockquoteRecedes pins the dim: quoted text is one tier below the
// surrounding prose, and its bar is the chrome tier.
func TestBlockquoteRecedes(t *testing.T) {
	rows := render(t, "> quoted", Options{Width: 40, Styler: styler(tokens.TrueColor)})
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if !strings.Contains(rows[0], tokens.TextSecondary.Fg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Errorf("quoted text is not at the secondary tier: %q", rows[0])
	}
	if !strings.Contains(rows[0], tokens.TextTertiary.Fg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Errorf("the gutter bar is not at the chrome tier: %q", rows[0])
	}
}

// TestRuleIsAHairlineAtTheMeasure: a thematic break ends where the sentences
// above it end, not where the terminal does.
func TestRuleIsAHairlineAtTheMeasure(t *testing.T) {
	rows := plain(render(t, "text\n\n---\n\nmore", Options{Width: 100, Measure: 30}))
	found := false
	for _, row := range rows {
		if strings.HasPrefix(row, tokens.GlyphTreeDash) {
			found = true
			if got := ansi.StringWidth(row); got != 30 {
				t.Errorf("rule is %d cells, want the measure (30)", got)
			}
		}
	}
	if !found {
		t.Fatal("no rule was drawn")
	}
}

// TestInlineCodeDegradesToBackticks is honest degradation on the inline path.
// Above 256 colours inline code is marked by a raised ground and the backticks
// are gone; below there is no raised ground to draw, so the mark comes back as
// the character it was — a span with neither ground nor mark is a span the
// reader cannot tell from the sentence around it.
func TestInlineCodeDegradesToBackticks(t *testing.T) {
	for _, p := range profiles {
		rows := render(t, "call `len(x)` first", Options{Width: 40, Styler: styler(p)})
		got := plain(rows)[0]
		hasTicks := strings.Contains(got, "`len(x)`")
		if want := !p.SheetGround(); hasTicks != want {
			t.Errorf("%s: backticks = %v, want %v (%q)", p, hasTicks, want, got)
		}
		if p.SheetGround() && !strings.Contains(rows[0], tokens.Sheet.Bg(p, tokens.FocusNormal)) {
			t.Errorf("%s: inline code has no raised ground", p)
		}
	}
}

// TestLinksKeepTheirAddress: a terminal has no hover, so the URL is drawn — but
// only when it says something the label did not.
func TestLinksKeepTheirAddress(t *testing.T) {
	rows := plain(render(t, "see [the audit](notes/audit.md) for more", Options{Width: 60}))
	if got := rows[0]; !strings.Contains(got, "the audit") || !strings.Contains(got, "(notes/audit.md)") {
		t.Errorf("link lost half of itself: %q", got)
	}
	auto := plain(render(t, "<https://example.com/x>", Options{Width: 60}))
	if n := strings.Count(auto[0], "https://example.com/x"); n != 1 {
		t.Errorf("an autolink drew its address %d times, want 1: %q", n, auto[0])
	}
}

// TestLinkTextIsUnderlined pins the second half of the link idiom: the label
// carries a mark of its own so it is still a link where the address was
// suppressed for being a repeat.
func TestLinkTextIsUnderlined(t *testing.T) {
	rows := render(t, "<https://example.com/x>", Options{Width: 60, Styler: styler(tokens.TrueColor)})
	if !strings.Contains(rows[0], "\x1b[4m") {
		t.Errorf("link text is not underlined: %q", rows[0])
	}
}

// TestEmphasisComposes: nesting must not leave an attribute switched on, and
// three delimiters are both weights.
func TestEmphasisComposes(t *testing.T) {
	rows := render(t, "***both*** and plain", Options{Width: 60, Styler: styler(tokens.TrueColor)})
	if !strings.Contains(rows[0], "\x1b[1m") || !strings.Contains(rows[0], "\x1b[3m") {
		t.Errorf("*** did not produce both weights: %q", rows[0])
	}
	if !strings.HasSuffix(ansi.Strip(rows[0]), "and plain") {
		t.Errorf("emphasis leaked past its span: %q", ansi.Strip(rows[0]))
	}
}

// TestEmptySourceRendersNothing: a reply that said nothing must not push the
// transcript down by a row.
func TestEmptySourceRendersNothing(t *testing.T) {
	for _, src := range []string{"", "   ", "\n\n\n", "\t"} {
		if rows := Render(src, Options{Width: 40}); len(rows) != 0 {
			t.Errorf("%q rendered %d rows, want none: %q", src, len(rows), rows)
		}
	}
}

// TestRenderIsPure is the caching contract: same source, same width, same
// Styler, same bytes. A surface that caches rows per (width, version) — which
// the block engine already does — is only correct if this holds.
func TestRenderIsPure(t *testing.T) {
	opts := Options{Width: 64, Styler: styler(tokens.TrueColor)}
	first := Render(DemoSource, opts)
	second := Render(DemoSource, opts)
	if len(first) != len(second) {
		t.Fatalf("row counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("row %d differs between renders:\n %q\n %q", i, first[i], second[i])
		}
	}
}

// TestRenderToAppends pins the buffer-reuse door: RenderTo appends and never
// touches what the caller already had.
func TestRenderToAppends(t *testing.T) {
	dst := []string{"keep me"}
	got := RenderTo(dst, "hello", Options{Width: 20})
	if len(got) != 2 || got[0] != "keep me" {
		t.Fatalf("RenderTo did not append onto the caller's slice: %q", got)
	}
}

// TestNilStylerRendersPlain: a caller with no Styler gets legible text and no
// escape bytes, which is also exactly what NoColor draws.
func TestNilStylerRendersPlain(t *testing.T) {
	rows := render(t, "# head\n\nbody **bold**\n", Options{Width: 40})
	for _, row := range rows {
		if strings.ContainsRune(row, 0x1b) {
			t.Errorf("a nil Styler emitted escapes: %q", row)
		}
	}
	if strings.Contains(joined(rows), "**") {
		t.Error("markdown punctuation survived into the frame")
	}
}
