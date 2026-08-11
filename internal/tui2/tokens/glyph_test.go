package tokens

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/width"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The rulers are imported HERE and only here; the shipping package measures
// nothing, it only declares.
//
// Two of them, because the renderer is measured by two:
//
//   - ansi.StringWidth is GRAPHEME width, which is what internal/tui already
//     renders through (tui/view.go) and what the v2 block engine wraps and
//     truncates with (blocks/wrap.go).
//   - ansi.StringWidthWc is WCWIDTH, the ruler lipgloss and bubbletea reach for
//     through go-runewidth.
//
// A glyph that passes one and fails the other is a glyph that lays out
// correctly until someone changes a wrapping call, which is the worst kind of
// bug to own. Both must say one.
//
// East-Asian ambiguity is a third question and needs a third source:
// golang.org/x/text/width reads the Unicode East_Asian_Width property directly,
// so the [GlyphInfo.AmbiguousWidth] flags are checked against the standard
// rather than against a library's opinion of it.

func isAmbiguous(r rune) bool {
	return width.LookupRune(r).Kind() == width.EastAsianAmbiguous
}

// TestGlyphsAreSingleCell is 5.17's law enforced rather than asserted. A glyph
// that measures two cells shifts every column to its right on the row it
// appears in, which is how the current chat acquired its ghost frames.
func TestGlyphsAreSingleCell(t *testing.T) {
	for _, g := range Glyphs() {
		if utf8.RuneCountInString(g.Glyph) != 1 {
			t.Errorf("%s (%q) is not a single rune", g.Name, g.Glyph)
			continue
		}
		if r := []rune(g.Glyph)[0]; r != g.Rune {
			t.Errorf("%s: table says %U, constant is %U", g.Name, g.Rune, r)
		}
		if w := ansi.StringWidth(g.Glyph); w != 1 {
			t.Errorf("%s (%q, %U): grapheme width is %d cells, must be 1", g.Name, g.Glyph, g.Rune, w)
		}
		if w := ansi.StringWidthWc(g.Glyph); w != 1 {
			t.Errorf("%s (%q, %U): wcwidth is %d cells, must be 1", g.Name, g.Glyph, g.Rune, w)
		}
	}
}

// TestAmbiguousWidthFlags cross-checks the exported metadata against the
// Unicode property itself. The flags are how a shell decides whether to reserve
// a column under a CJK locale; metadata that drifted from reality would be
// worse than no metadata.
func TestAmbiguousWidthFlags(t *testing.T) {
	ambiguous := 0
	for _, g := range Glyphs() {
		got := isAmbiguous(g.Rune)
		if got != g.AmbiguousWidth {
			t.Errorf("%s (%q, %U): AmbiguousWidth=%v but East_Asian_Width is %v",
				g.Name, g.Glyph, g.Rune, g.AmbiguousWidth, width.LookupRune(g.Rune).Kind())
		}
		if got {
			ambiguous++
		}
	}
	t.Logf("%d of %d glyphs are East_Asian_Width=Ambiguous — one cell for us, two "+
		"under a CJK-locale terminal with ambiguous-wide enabled", ambiguous, len(Glyphs()))
}

// TestAnimatedSetsAgreeOnWidth is the reason the ◐◓◑◒ set of 5.21 is not
// shipped: a rotating glyph whose frames disagree about width makes a live row
// change width mid-spin. Whatever the spinner is, its frames must measure the
// same — and be ambiguous or not TOGETHER, since a set that is half-ambiguous
// changes width when the locale does. The same requirement binds the gauge
// ramp, which swaps cells as a value changes, and the sparkline.
func TestAnimatedSetsAgreeOnWidth(t *testing.T) {
	for name, set := range map[string][]string{
		"spinner":   SpinnerFrames[:],
		"gauge":     GaugeCells[:],
		"sparkline": SparklineCells[:],
	} {
		first := []rune(set[0])[0]
		wantAmbiguous := isAmbiguous(first)
		for i, c := range set {
			r := []rune(c)[0]
			if got := ansi.StringWidth(c); got != 1 {
				t.Errorf("%s cell %d (%q) is %d cells", name, i, c, got)
			}
			if got := isAmbiguous(r); got != wantAmbiguous {
				t.Errorf("%s cell %d (%q) ambiguous=%v, cell 0 (%q) is %v: the set "+
					"would change width under a CJK locale mid-animation",
					name, i, c, got, set[0], wantAmbiguous)
			}
		}
	}
}

// TestNoBannedGlyphs fails if a banned rune appears anywhere in the vocabulary,
// and also enforces the general rules the ban list instantiates: nothing in the
// emoji planes, no variation selectors.
func TestNoBannedGlyphs(t *testing.T) {
	banned := map[rune]string{}
	for _, b := range BannedGlyphs {
		banned[b.Rune] = b.Reason
	}
	for _, g := range Glyphs() {
		if reason, bad := banned[g.Rune]; bad {
			t.Errorf("%s uses banned glyph %q (%U): %s", g.Name, g.Glyph, g.Rune, reason)
		}
		switch {
		case g.Rune >= 0x1F000:
			t.Errorf("%s (%U) is in the emoji planes; chrome carries no emoji (5.17)", g.Name, g.Rune)
		case g.Rune == 0xFE0F || g.Rune == 0xFE0E:
			t.Errorf("%s carries a variation selector", g.Name)
		}
	}
	// The ban list itself must stay meaningful: every entry needs a reason.
	for _, b := range BannedGlyphs {
		if strings.TrimSpace(b.Reason) == "" {
			t.Errorf("banned glyph %U has no stated reason", b.Rune)
		}
	}
}

// TestBannedGlyphsAreActuallyBad checks the width claims made in the ban list,
// for the entries banned on width grounds. ⚡ is the interesting one: 5.17 lists
// it in its own vocabulary and it fails 5.17's own law, which is why this
// package ships ⇡ for "boosted" instead.
func TestBannedGlyphsAreActuallyBad(t *testing.T) {
	for _, r := range []rune{'⚡', '☰', '⌛', '⏳'} {
		w := ansi.StringWidth(string(r))
		if w == 1 {
			t.Errorf("%U now measures one cell under the grapheme ruler; the ban may be revisitable", r)
			continue
		}
		t.Logf("%U measures %d cells — banned on measurement, not on taste", r, w)
	}
}

// TestGaugeAndSparkline covers 5.17's ambient form: context as one eighth-block
// beside the model word. It must be total (a fraction that ran past its window
// still reads full rather than panicking a render) and it must be monotone, or
// the gauge would fall as the window filled.
func TestGaugeAndSparkline(t *testing.T) {
	cases := []struct {
		fraction float64
		want     string
	}{
		{-1, GaugeCells[0]},
		{0, GaugeCells[0]},
		{0.1, GaugeCells[0]},
		{0.25, GaugeCells[1]},
		{0.5, GaugeCells[2]},
		{0.62, GaugeCells[3]},
		{0.99, GaugeCells[4]},
		{1, GaugeCells[4]},
		{2, GaugeCells[4]},
	}
	for _, c := range cases {
		if got := Gauge(c.fraction); got != c.want {
			t.Errorf("Gauge(%v) = %q, want %q", c.fraction, got, c.want)
		}
	}
	// Monotone across the whole domain, including NaN, which must not panic.
	prev := 0
	for i := range 201 {
		f := float64(i-50) / 100
		idx := indexOf(GaugeCells[:], Gauge(f))
		if idx < prev {
			t.Fatalf("Gauge(%v) fell from cell %d to %d", f, prev, idx)
		}
		prev = idx
	}
	if got := Gauge(math.NaN()); got != GaugeCells[0] {
		t.Errorf("Gauge(NaN) = %q, want the empty cell — a reading that does not exist is not a full window", got)
	}
	if Sparkline(0) != SparklineCells[0] || Sparkline(1) != SparklineCells[len(SparklineCells)-1] {
		t.Error("sparkline endpoints are wrong")
	}
	if got := Sparkline(math.NaN()); got != SparklineCells[0] {
		t.Errorf("Sparkline(NaN) = %q, want the empty cell", got)
	}
}

// TestSpinnerIsTotal: the shared clock hands out a monotonically rising tick
// that will eventually wrap or arrive negative from a caller's arithmetic. A
// frame lookup must never panic a render (8.1.3).
func TestSpinnerIsTotal(t *testing.T) {
	for _, tick := range []int{-1 << 40, -7, -1, 0, 1, 9, 10, 1 << 30} {
		if Spinner(tick) == "" {
			t.Errorf("Spinner(%d) returned nothing", tick)
		}
	}
	// The clock is shared, so the cycle must be exactly the frame count: two
	// rows given the same tick must show the same frame, forever.
	for tick := range 40 {
		if Spinner(tick) != Spinner(tick+len(SpinnerFrames)) {
			t.Fatalf("spinner cycle is not %d frames at tick %d", len(SpinnerFrames), tick)
		}
	}
}

func TestGlyphInventoryIsComplete(t *testing.T) {
	// Every glyph constant declared in the package must appear in Glyphs(), or
	// the width gate has a hole in it.
	declared := []string{
		GlyphQueued, GlyphWorking, GlyphSettled, GlyphFailed, GlyphPaused,
		GlyphNeedsHuman, GlyphWaitsOn, GlyphCollapsed, GlyphExpanded,
		GlyphScopeUp, GlyphTruncated, GlyphCut, GlyphPromptChat, GlyphPromptSteer,
		GlyphBoosted, GlyphSeparator, GlyphMissing, GlyphEstimate,
		GlyphAccentRail, GlyphDragHandle, GlyphStepDone, GlyphStepRunning,
		GlyphStepPending, GlyphStepBlocked, GlyphQueuePill, GlyphDiffAdd,
		GlyphDiffDel, GlyphTreeBranch, GlyphTreeLast, GlyphTreeVert, GlyphTreeDash,
	}
	declared = append(declared, GaugeCells[:]...)
	declared = append(declared, SpinnerFrames[:]...)
	declared = append(declared, SparklineCells[:]...)

	inTable := map[string]bool{}
	names := map[string]bool{}
	for _, g := range Glyphs() {
		inTable[g.Glyph] = true
		if names[g.Name] {
			t.Errorf("duplicate glyph name %q", g.Name)
		}
		names[g.Name] = true
	}
	for _, d := range declared {
		if !inTable[d] {
			t.Errorf("glyph %q is declared but missing from Glyphs(); it escapes the width gate", d)
		}
	}
}

// TestCutIsNotEllipsis holds 12.5.2's distinction in place. An ellipsis says
// "there is more, ask for it"; a cut says "this stopped and should not have".
// One glyph for both would rebuild the lie of omission that law exists to end.
func TestCutIsNotEllipsis(t *testing.T) {
	if GlyphCut == GlyphTruncated {
		t.Fatal("the cut mark and the overflow mark must be different glyphs (12.5.2)")
	}
	if isAmbiguous([]rune(GlyphCut)[0]) {
		t.Error("the cut mark must not be ambiguous-width: it appears at the end of a live row")
	}
}

// TestCutMarkIsOneMark closes the seam the vocabulary law leaves open. 12.5.2's
// mark must be ONE thing, and it is spelled in two packages: this one is the
// vocabulary authority, and internal/tui2/blocks — which cannot import tokens,
// because the edge runs tokens → blocks to keep blocks a leaf — has to carry
// the byte itself to draw the cut rule.
//
// The test is the import edge doing the work the compiler cannot: tokens
// already depends on blocks, so tokens is the one place that can hold the two
// spellings side by side and fail when they part. blocks drew "⌁" for a while
// against this package's "╌", which is exactly the two-vocabularies-for-one-
// mark drift this pins shut.
func TestCutMarkIsOneMark(t *testing.T) {
	if GlyphCut != blocks.CutMark {
		t.Fatalf("two vocabularies for one cut mark (12.5.2): tokens %q, blocks %q",
			GlyphCut, blocks.CutMark)
	}
}

func indexOf(set []string, s string) int {
	for i, v := range set {
		if v == s {
			return i
		}
	}
	return -1
}

// TestSparklineCoversItsRamp: the braille burn-trend swaps cells as a value
// changes, so every cell must be reachable and the walk must be monotone —
// a trend that fell as the number rose would be a chart telling the opposite
// story.
func TestSparklineCoversItsRamp(t *testing.T) {
	reached := map[string]bool{}
	prev := 0
	for i := range 201 {
		f := float64(i-50) / 100
		cell := Sparkline(f)
		reached[cell] = true
		idx := indexOf(SparklineCells[:], cell)
		if idx < 0 {
			t.Fatalf("Sparkline(%v) returned %q, which is not on the ramp", f, cell)
		}
		if idx < prev {
			t.Fatalf("Sparkline(%v) fell from cell %d to %d", f, prev, idx)
		}
		prev = idx
	}
	for i, c := range SparklineCells {
		if !reached[c] {
			t.Errorf("sparkline cell %d (%q) is unreachable", i, c)
		}
	}
	for i, c := range GaugeCells {
		if Gauge(float64(i)/float64(len(GaugeCells))+0.01) != c {
			t.Errorf("gauge cell %d (%q) is unreachable", i, c)
		}
	}
}
