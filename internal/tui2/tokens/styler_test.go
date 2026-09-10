package tokens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestPaintOnNeverChangesWidth holds this package's painting law: adding a
// background may add escape sequences but never a printable byte.
func TestPaintOnNeverChangesWidth(t *testing.T) {
	samples := []string{
		"", " ", "hello", "wisp-parity", GlyphSettled + " done",
		"◐ ▎ ⋯ — ~", "a\tb", "多字节文本", "é", GlyphCut,
	}
	for p := Profile(0); p < profileCount; p++ {
		for _, f := range []Focus{FocusNormal, FocusDimmed} {
			s := NewStyler(p, f)
			for _, text := range samples {
				want := ansi.StringWidth(text)
				got := s.PaintOn(text, TextPrimary, Band)
				if w := ansi.StringWidth(got); w != want {
					t.Fatalf("%s/%s PaintOn(%q) changed width", p, f, text)
				}
				if ansi.Strip(got) != text {
					t.Fatalf("%s/%s PaintOn(%q) changed the printable bytes to %q",
						p, f, text, ansi.Strip(got))
				}
			}
		}
	}
}

// TestNoColorIsTheIdentityFunction: under NO_COLOR, a pipe or TERM=dumb the
// surface must remain fully legible, which means the Styler returns text
// untouched and every state colour carries a glyph instead (5.17). Byte
// identity is the strong form of that claim, and it is also what makes the
// golden harness's plain renders comparable.
func TestNoColorIsTheIdentityFunction(t *testing.T) {
	s := NewStyler(NoColor, FocusNormal)
	for _, text := range []string{"", "hello", GlyphNeedsHuman} {
		if got := s.PaintOn(text, Amber, Band); got != text {
			t.Errorf("NoColor PaintOn(%q) = %q", text, got)
		}
		if got := s.PaintToken(text, Coral); got != text {
			t.Errorf("NoColor PaintToken(%q) = %q", text, got)
		}
	}
}

// TestEmptyTextIsNotPainted: an escape pair around nothing is bytes the
// terminal parses for no reason, and a zero-width painted string would make
// every "is this row empty" check downstream answer wrong.
func TestEmptyTextIsNotPainted(t *testing.T) {
	s := NewStyler(TrueColor, FocusNormal)
	if got := s.PaintOn("", TextPrimary, Band); got != "" {
		t.Errorf("empty text came back as %q from PaintOn", got)
	}
}

// TestStylerIsTotal ensures a render never dies because a caller handed it an
// out-of-range value. Zero panics is the bar.
func TestStylerIsTotal(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the Styler panicked: %v", r)
		}
	}()
	s := NewStyler(Profile(200), Focus(200))
	if s.Profile() != NoColor || s.Focus() != FocusNormal {
		t.Errorf("an out-of-range profile/focus was not clamped: %s/%s", s.Profile(), s.Focus())
	}
	live := NewStyler(TrueColor, FocusNormal)
	if got := live.PaintToken("x", Token(250)); got != "x" {
		t.Errorf("an out-of-range token was painted: %q", got)
	}
	if got := live.PaintOn("x", Token(250), Token(251)); got != "x" {
		t.Errorf("an out-of-range pair was painted: %q", got)
	}
}

// TestWithFocusReusesTheReceiver: dimming is a pane-level change that happens
// on every focus move, so the no-op case must not allocate a new Styler.
func TestWithFocusReusesTheReceiver(t *testing.T) {
	s := NewStyler(TrueColor, FocusNormal)
	if s.WithFocus(FocusNormal) != s {
		t.Error("WithFocus allocated for a no-op")
	}
	dim := s.WithFocus(FocusDimmed)
	if dim.Focus() != FocusDimmed || dim.Profile() != s.Profile() {
		t.Error("WithFocus lost the profile")
	}
	if s.Focus() != FocusNormal {
		t.Error("WithFocus mutated its receiver")
	}
	if s.WithFocus(Focus(9)).Focus() != FocusNormal {
		t.Error("WithFocus did not clamp")
	}
}

// TestSelectionDegradesToReverse: 5.16 wants a background band, and below 256
// colours the only available raised background is bright black — a different
// shade in every theme, which can land on top of the text tier. Reverse video
// is the honest fallback because it is defined relative to whatever the
// terminal's own colours are.
func TestSelectionDegradesToReverse(t *testing.T) {
	for p := Profile(0); p < profileCount; p++ {
		got := NewStyler(p, FocusNormal).PaintOn("row", TextPrimary, Band)
		switch {
		case p == NoColor:
			if got != "row" {
				t.Errorf("NoColor selection emitted %q", got)
			}
		case p.SelectionStyle() == SelectionReverse:
			if !strings.HasPrefix(got, "\x1b[7m") {
				t.Errorf("%s selection = %q, want reverse video", p, got)
			}
		default:
			if !strings.Contains(got, "\x1b[48;") {
				t.Errorf("%s selection = %q, want a background band", p, got)
			}
		}
	}
}

// TestTierDoesNotChangeTokenPainting is 12.7 F.8's colour half: a repertoire
// tier changes glyph selection, never the escape sequences around prose or a
// raised row.
func TestTierDoesNotChangeTokenPainting(t *testing.T) {
	for p := Profile(0); p < profileCount; p++ {
		for f := Focus(0); f < focusCount; f++ {
			plain := NewStylerIn(p, f, Plain)
			nf := NewStylerIn(p, f, NerdFont)
			for tok := Token(0); tok < tokenCount; tok++ {
				if a, b := plain.PaintToken("aforge", tok), nf.PaintToken("aforge", tok); a != b {
					t.Fatalf("%v/%v/%v: PaintToken differs between tiers", p, f, tok)
				}
				if a, b := plain.PaintOn("aforge", tok, Band), nf.PaintOn("aforge", tok, Band); a != b {
					t.Fatalf("%v/%v/%v: PaintOn differs between tiers", p, f, tok)
				}
			}
		}
	}
}

// TestStylerCarriesTheTierAcrossEveryDoor: the tier is a property of the
// Styler, so a derived Styler made when a pane loses focus must keep it. A
// WithFocus that quietly dropped back to the plain tier would redraw half the
// screen in the other repertoire.
func TestStylerCarriesTheTierAcrossEveryDoor(t *testing.T) {
	s := NewStylerIn(TrueColor, FocusNormal, NerdFont)
	if s.GlyphSet() != NerdFont {
		t.Fatalf("GlyphSet() = %v", s.GlyphSet())
	}
	if got := s.WithFocus(FocusDimmed).GlyphSet(); got != NerdFont {
		t.Errorf("WithFocus dropped the tier: %v", got)
	}
	if got := s.WithGlyphSet(Plain); got.GlyphSet() != Plain || got.Profile() != TrueColor {
		t.Errorf("WithGlyphSet(%v) = tier %v, profile %v", Plain, got.GlyphSet(), got.Profile())
	}
	if s.WithGlyphSet(NerdFont) != s {
		t.Error("WithGlyphSet to the same tier should return the same Styler")
	}
	if got := s.WithGlyphSet(GlyphSet(200)).GlyphSet(); got != Plain {
		t.Errorf("an out-of-range tier resolved to %v, want the plain floor", got)
	}
	if got := NewStyler(TrueColor, FocusNormal).GlyphSet(); got != Plain {
		t.Errorf("NewStyler must stay on the plain tier for every caller written before the tier: %v", got)
	}
	// The tier is orthogonal to colour: a NoColor terminal with a patched font
	// still resolves icons through the same explicit door.
	if got := NewStylerIn(NoColor, FocusNormal, NerdFont).Glyph(GWorking); got != NerdFont.Glyph(GWorking) {
		t.Errorf("NoColor + NerdFont resolved %q", got)
	}
}

// TestNilStylerResolvesTheFloor: a nil *Styler is a real state in this tree —
// a renderer built before a profile was chosen may hold one — and adopting the
// tier means replacing a package-level constant with a method call. A lookup
// that panicked where the constant could not would make a one-token adoption
// change when a renderer crashes.
func TestNilStylerResolvesTheFloor(t *testing.T) {
	var s *Styler
	if got := s.GlyphSet(); got != Plain {
		t.Errorf("a nil Styler reports tier %v", got)
	}
	for _, id := range []GlyphID{GNeedsHuman, GWorking, GSpend, GFolder} {
		if got := s.Glyph(id); got != Plain.Glyph(id) {
			t.Errorf("a nil Styler resolved slot %d to %q, want the plain glyph %q", id, got, Plain.Glyph(id))
		}
	}
}
