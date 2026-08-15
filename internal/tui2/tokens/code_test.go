package tokens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The code ramp signs the same two contracts the palette does — a contrast gate
// and a degradation ladder — so it is tested the same way: by walking every
// slot rather than by spot-checking the ones a reviewer happened to look at.

// TestCodeRampContrast is the shipping gate for the ramp. A code block is drawn
// on the [Sheet], and a selected row of one could be drawn on the [Band], so all
// three grounds are walked — but only through [Legal], because the composition
// law that forbids a dimmed foreground on a raised background is a law about
// this ramp too. Asserting a pairing the surface may not draw would be gating a
// screen nobody can reach.
func TestCodeRampContrast(t *testing.T) {
	grounds := []Token{Ground, Sheet, Band}
	for _, s := range CodeSlots() {
		for f := Focus(0); f < focusCount; f++ {
			min := s.Class().MinContrast(f)
			for _, g := range grounds {
				// TextPrimary stands in for the ramp on the legality question:
				// every slot is an ordinary non-surface foreground, so the rule
				// that governs one governs all seven.
				if !Legal(TextPrimary, f, g, FocusNormal) {
					continue
				}
				got := Contrast(s.Color(f), g.Color(FocusNormal))
				if got < min {
					t.Errorf("%s on %s at %s: contrast %.2f < %.2f",
						s, g, f, got, min)
				}
			}
		}
	}
}

// TestCodeRampDistinct is the ramp's own version of the hue-separation law: six
// pastels that resolve to the same cell are one pastel wearing six names, and a
// reader would learn nothing from the colour. The gate is stated in the same
// [distance] units the 256-cube separation uses.
func TestCodeRampDistinct(t *testing.T) {
	slots := CodeSlots()
	for i, a := range slots {
		for _, b := range slots[i+1:] {
			if d := distance(a.Color(FocusNormal), b.Color(FocusNormal)); d < minSeparation256 {
				t.Errorf("%s and %s are only %d apart; the ramp needs %d",
					a, b, d, minSeparation256)
			}
		}
	}
}

// TestCodeRampDegrades pins the honest-degradation ruling: the ramp exists at
// 256 and truecolor and does not exist below, and [CodeHighlighting] answers
// exactly where the SGR strings do. A caller that trusted the predicate and a
// caller that trusted the bytes must reach the same screen.
func TestCodeRampDegrades(t *testing.T) {
	for p := Profile(0); p < profileCount; p++ {
		want := p >= ANSI256
		if got := CodeHighlighting(p); got != want {
			t.Errorf("CodeHighlighting(%s) = %v, want %v", p, got, want)
		}
		for _, s := range CodeSlots() {
			for f := Focus(0); f < focusCount; f++ {
				seq := s.Fg(p, f)
				if want && seq == "" {
					t.Errorf("%s at %s/%s: highlighting is on but the SGR is empty", s, p, f)
				}
				if !want && seq != "" {
					t.Errorf("%s at %s/%s: highlighting is off but the SGR is %q", s, p, f, seq)
				}
				if seq != "" && ansi.StringWidth(seq) != 0 {
					t.Errorf("%s at %s/%s: %q measures printable cells", s, p, f, seq)
				}
			}
		}
	}
}

// TestCodeRampDimIsDerived re-derives every dimmed value from its base through
// [Mix], the way palette_test.go does for the palette: a hand-edited literal
// that stopped obeying the one dimming rule would fail here rather than ship.
func TestCodeRampDimIsDerived(t *testing.T) {
	for _, s := range CodeSlots() {
		want := Mix(s.Color(FocusNormal), Ground.Color(FocusNormal), DimTowardGround)
		if got := s.Color(FocusDimmed); got != want {
			t.Errorf("%s dimmed is %s, derivation says %s", s, got.Hex(), want.Hex())
		}
	}
}

// TestCodeTextIsThePrimaryTier pins the claim the file comment makes: code that
// the lexer had no opinion about is the SAME grey as ordinary prose, so an
// unhighlighted block and the uncoloured half of a highlighted one do not read
// as two different tiers of text.
func TestCodeTextIsThePrimaryTier(t *testing.T) {
	for f := Focus(0); f < focusCount; f++ {
		if got, want := CodeText.Color(f), TextPrimary.Color(f); got != want {
			t.Errorf("code.text at %s is %s, text.primary is %s", f, got.Hex(), want.Hex())
		}
	}
}

// TestCodeSlotNames pins the stable names, which golden tests key on.
func TestCodeSlotNames(t *testing.T) {
	want := []string{
		"code.text", "code.keyword", "code.string", "code.number",
		"code.comment", "code.function", "code.type",
	}
	slots := CodeSlots()
	if len(slots) != len(want) {
		t.Fatalf("ramp has %d slots, want %d", len(slots), len(want))
	}
	for i, s := range slots {
		if s.String() != want[i] {
			t.Errorf("slot %d is %q, want %q", i, s.String(), want[i])
		}
	}
}

// TestProseGlyphsShareTheirBytes is the anti-drift pin the file comment
// promises: each prose glyph is a NAMED SLOT over a byte the vocabulary already
// ships, and the two must never become two different marks for one meaning —
// the same argument blocks.CutMark's twin test makes.
func TestProseGlyphsShareTheirBytes(t *testing.T) {
	for _, pair := range []struct{ name, prose, twin string }{
		{"bullet", GlyphProseBullet, GlyphSeparator},
		{"quote", GlyphProseQuote, GlyphTreeVert},
	} {
		if pair.prose != pair.twin {
			t.Errorf("%s: prose slot is %q, its twin is %q", pair.name, pair.prose, pair.twin)
		}
	}
}

// TestProseGlyphsAreSingleCell holds the prose glyphs to 5.17's width law under
// both rulers the renderer is measured by, exactly as glyph_test.go does for
// the state vocabulary. The code gutter is the interesting one: it is a block
// element, and block elements are where a tempting mark most often turns out to
// be two cells wide.
func TestProseGlyphsAreSingleCell(t *testing.T) {
	for _, g := range []struct{ name, glyph string }{
		{"GlyphProseBullet", GlyphProseBullet},
		{"GlyphProseQuote", GlyphProseQuote},
		{"GlyphCodeGutter", GlyphCodeGutter},
	} {
		if n := len([]rune(g.glyph)); n != 1 {
			t.Errorf("%s (%q) is %d runes, must be 1", g.name, g.glyph, n)
			continue
		}
		if w := ansi.StringWidth(g.glyph); w != 1 {
			t.Errorf("%s (%q): grapheme width %d, must be 1", g.name, g.glyph, w)
		}
		if w := ansi.StringWidthWc(g.glyph); w != 1 {
			t.Errorf("%s (%q): wcwidth %d, must be 1", g.name, g.glyph, w)
		}
		if strings.ContainsAny(g.glyph, "\n\r\t") {
			t.Errorf("%s carries a control byte", g.name)
		}
	}
}
