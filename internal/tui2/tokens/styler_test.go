package tokens

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/charmbracelet/x/ansi"
)

// TestEnumsMatchTheBlocksSeam is the load-bearing test of styler.go. The two
// packages declare the same two vocabularies independently — blocks so it can
// be built without this package, tokens because the vocabulary is its subject —
// and [Styler.Paint] converts between them with a numeric cast. That cast is
// free only while the ordinals agree, and silently wrong the moment they do
// not, so the agreement is checked rather than assumed.
func TestEnumsMatchTheBlocksSeam(t *testing.T) {
	hues := []struct {
		mine  Hue
		their blocks.Hue
	}{
		{HueNone, blocks.HueNone},
		{HueAttention, blocks.HueAttention},
		{HueAlive, blocks.HueAlive},
		{HueMoney, blocks.HueMoney},
		{HueBroken, blocks.HueBroken},
		{HueIdentity, blocks.HueIdentity},
	}
	for _, h := range hues {
		if uint8(h.mine) != uint8(h.their) {
			t.Errorf("hue %s is %d here and %d in blocks", h.mine, h.mine, h.their)
		}
	}
	if int(hueCount) != len(hues) {
		t.Errorf("this package has %d hues but the correspondence table lists %d", hueCount, len(hues))
	}

	states := []struct {
		mine  State
		their blocks.State
	}{
		{StateSettled, blocks.StateSettled},
		{StateLive, blocks.StateLive},
		{StateChrome, blocks.StateChrome},
	}
	for _, s := range states {
		if uint8(s.mine) != uint8(s.their) {
			t.Errorf("state %s is %d here and %d in blocks", s.mine, s.mine, s.their)
		}
	}
	if int(stateCount) != len(states) {
		t.Errorf("this package has %d states but the correspondence table lists %d", stateCount, len(states))
	}

	// The zero values must agree on meaning too: a bare struct in blocks paints
	// settled and unhued, and it must land on the same token here.
	var zeroState blocks.State
	var zeroHue blocks.Hue
	if got := ResolveToken(Hue(zeroHue), State(zeroState)); got != TextPrimary {
		t.Errorf("the zero value of the seam resolves to %s, want the settled primary tier", got)
	}
}

// TestPaintNeverChangesWidth is blocks' explicit contract: "Paint must not
// change the printable width of text: it may only wrap text in escape
// sequences." Every width measurement in that package is ANSI-aware, and a
// Styler that inserted a printable byte would break layout everywhere at once.
func TestPaintNeverChangesWidth(t *testing.T) {
	samples := []string{
		"", " ", "hello", "wisp-parity", GlyphSettled + " done",
		"◐ ▎ ⋯ — ~", "a\tb", "多字节文本", "é", GlyphCut,
	}
	for p := Profile(0); p < profileCount; p++ {
		for _, f := range []Focus{FocusNormal, FocusDimmed} {
			s := NewStyler(p, f)
			for _, text := range samples {
				want := ansi.StringWidth(text)
				for st := blocks.State(0); st < 3; st++ {
					for h := blocks.Hue(0); h < 6; h++ {
						got := s.Paint(text, st, h)
						if w := ansi.StringWidth(got); w != want {
							t.Fatalf("%s/%s Paint(%q, %d, %d) is %d cells, want %d",
								p, f, text, st, h, w, want)
						}
						if ansi.Strip(got) != text {
							t.Fatalf("%s/%s Paint(%q) changed the printable bytes to %q",
								p, f, text, ansi.Strip(got))
						}
					}
				}
				if w := ansi.StringWidth(s.PaintIdentity(text, 7, blocks.StateLive)); w != want {
					t.Fatalf("%s/%s PaintIdentity(%q) changed width", p, f, text)
				}
				if w := ansi.StringWidth(s.PaintOn(text, TextPrimary, Band)); w != want {
					t.Fatalf("%s/%s PaintOn(%q) changed width", p, f, text)
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
		for st := blocks.State(0); st < 3; st++ {
			for h := blocks.Hue(0); h < 6; h++ {
				if got := s.Paint(text, st, h); got != text {
					t.Errorf("NoColor Paint(%q, %d, %d) = %q", text, st, h, got)
				}
			}
		}
		if got := s.PaintIdentity(text, 3, blocks.StateLive); got != text {
			t.Errorf("NoColor PaintIdentity(%q) = %q", text, got)
		}
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
	if got := s.Paint("", blocks.StateLive, blocks.HueBroken); got != "" {
		t.Errorf("empty text came back as %q", got)
	}
	if got := s.PaintOn("", TextPrimary, Band); got != "" {
		t.Errorf("empty text came back as %q from PaintOn", got)
	}
}

// TestPaintResolvesTheAxes walks the composition rule through the seam, so the
// law stated at [ResolveToken] is the law blocks actually gets.
func TestPaintResolvesTheAxes(t *testing.T) {
	s := NewStyler(TrueColor, FocusNormal)
	cases := []struct {
		state blocks.State
		hue   blocks.Hue
		want  Token
		why   string
	}{
		{blocks.StateLive, blocks.HueNone, Cyan, "an unhued live row is the accent, and cyan is the word for alive"},
		{blocks.StateSettled, blocks.HueNone, TextPrimary, "a settled row is plain text"},
		{blocks.StateChrome, blocks.HueNone, TextTertiary, "chrome is dim"},
		{blocks.StateSettled, blocks.HueMoney, Green, "a settled ✓ is still green (5.16)"},
		{blocks.StateSettled, blocks.HueBroken, Coral, "a settled ✕ is still coral"},
		{blocks.StateChrome, blocks.HueAttention, Amber, "a question badge in a fold line is still amber"},
		{blocks.StateLive, blocks.HueAlive, Cyan, "working"},
	}
	for _, c := range cases {
		if got := s.Token(c.state, c.hue); got != c.want {
			t.Errorf("state %d hue %d resolved to %s, want %s — %s", c.state, c.hue, got, c.want, c.why)
		}
		want := s.PaintToken("x", c.want)
		if got := s.Paint("x", c.state, c.hue); got != want {
			t.Errorf("Paint disagrees with PaintToken for state %d hue %d", c.state, c.hue)
		}
	}
}

// TestPaintIdentityIsStableAndOnTheWheel: the seed comes from the block engine,
// which knows a task by a hash rather than by an id. Whatever arrives, the
// answer must be an identity hue, and the same seed must always give the same
// one — a rail that recoloured between frames is the disco 5.16 forbids.
func TestPaintIdentityIsStableAndOnTheWheel(t *testing.T) {
	s := NewStyler(TrueColor, FocusNormal)
	for _, seed := range []uint64{0, 1, 7, 8, 1 << 63, ^uint64(0)} {
		first := s.PaintIdentity("row", seed, blocks.StateLive)
		for range 10 {
			if got := s.PaintIdentity("row", seed, blocks.StateLive); got != first {
				t.Fatalf("seed %d is not stable", seed)
			}
		}
		want := s.PaintToken("row", Identity(int(seed%IdentityCount)))
		if first != want {
			t.Errorf("seed %d did not land on wheel entry %d", seed, seed%IdentityCount)
		}
	}
	// Chrome has no identity: structure is structure in every room.
	if got, want := s.PaintIdentity("·", 3, blocks.StateChrome), s.PaintToken("·", TextTertiary); got != want {
		t.Error("a chrome row must not carry an identity accent")
	}
}

// TestStylerIsTotal: the seam takes values from another package, and a render
// must never die because one of them was out of range. Zero panics is the bar.
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
	for st := blocks.State(0); st < 40; st++ {
		for h := blocks.Hue(0); h < 40; h++ {
			if got := live.Paint("x", st, h); ansi.Strip(got) != "x" {
				t.Fatalf("Paint mangled text at state %d hue %d", st, h)
			}
		}
	}
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

// TestPaintAllocatesOnce is the hot-path budget. Returning a string means at
// least one allocation; the point is that it is EXACTLY one — the escape
// sequences are table constants, the builder is pre-sized, and nothing is
// formatted per frame.
func TestPaintAllocatesOnce(t *testing.T) {
	s := NewStyler(TrueColor, FocusNormal)
	if n := testing.AllocsPerRun(200, func() {
		_ = s.Paint("wisp-parity", blocks.StateLive, blocks.HueAttention)
	}); n > 1 {
		t.Errorf("Paint allocates %.1f times per call, want at most 1", n)
	}
	plain := NewStyler(NoColor, FocusNormal)
	if n := testing.AllocsPerRun(200, func() {
		_ = plain.Paint("wisp-parity", blocks.StateLive, blocks.HueAttention)
	}); n != 0 {
		t.Errorf("NoColor Paint allocates %.1f times per call", n)
	}
	if n := testing.AllocsPerRun(200, func() {
		_ = s.Token(blocks.StateLive, blocks.HueMoney)
	}); n != 0 {
		t.Errorf("Token allocates %.1f times per call", n)
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

// TestStylerSatisfiesBlocksAtRuntime: the compile-time assertions in styler.go
// prove the method set. This proves a Styler actually works where blocks
// expects one, through the interface rather than through the concrete type.
func TestStylerSatisfiesBlocksAtRuntime(t *testing.T) {
	var s blocks.Styler = NewStyler(TrueColor, FocusNormal)
	if got := ansi.Strip(s.Paint("hi", blocks.StateLive, blocks.HueAlive)); got != "hi" {
		t.Errorf("through the interface: %q", got)
	}
	id, ok := s.(blocks.IdentityStyler)
	if !ok {
		t.Fatal("a Styler must also be an IdentityStyler")
	}
	if got := ansi.Strip(id.PaintIdentity("hi", 5, blocks.StateLive)); got != "hi" {
		t.Errorf("through the identity interface: %q", got)
	}
}

// TestTierChangesTheGlyphAndNothingElse is 12.7 F.8: the tier changes the
// character between the escape sequences and not one byte of the escape
// sequences themselves. Colour is resolved by the state × hue axes through
// [ResolveToken], and that composition is exactly what a repertoire tier is not
// allowed to touch — it is the reason a mono icon is admissible in this chrome
// where an emoji is not (5.17 reason 2).
func TestTierChangesTheGlyphAndNothingElse(t *testing.T) {
	for p := Profile(0); p < profileCount; p++ {
		for f := Focus(0); f < focusCount; f++ {
			plain := NewStylerIn(p, f, Plain)
			nf := NewStylerIn(p, f, NerdFont)
			for st := blocks.State(0); st <= blocks.StateChrome; st++ {
				for h := blocks.Hue(0); h <= blocks.HueIdentity; h++ {
					if plain.Token(st, h) != nf.Token(st, h) {
						t.Fatalf("%v/%v/%v/%v: the tier moved a token", p, f, st, h)
					}
					// A word is not a glyph, so both tiers must paint it
					// byte-identically — same sequence, same reset.
					if a, b := plain.Paint("aforge", st, h), nf.Paint("aforge", st, h); a != b {
						t.Fatalf("%v/%v/%v/%v: prose differs between tiers\nplain %q\nnf    %q", p, f, st, h, a, b)
					}
					if a, b := plain.PaintIdentity("aforge", 7, st), nf.PaintIdentity("aforge", 7, st); a != b {
						t.Fatalf("%v/%v/%v: identity prose differs between tiers", p, f, st)
					}
					// A glyph cell differs in exactly one place: the cell.
					a := plain.PaintGlyph(GWorking, st, h)
					b := nf.PaintGlyph(GWorking, st, h)
					if strings.Replace(a, GlyphWorking, NerdFont.Glyph(GWorking), 1) != b {
						t.Fatalf("%v/%v/%v/%v: the tier changed more than the glyph\nplain %q\nnf    %q",
							p, f, st, h, a, b)
					}
				}
			}
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
// Styler, so the derived Stylers a compositor makes when a pane loses focus
// must keep it. A WithFocus that quietly dropped back to the plain tier would
// redraw half the screen in the other repertoire.
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
	// still draws icons, and painting adds no escape bytes there.
	if got := NewStylerIn(NoColor, FocusNormal, NerdFont).Paint(GlyphWorking, blocks.StateLive, blocks.HueAlive); got != NerdFont.Glyph(GWorking) {
		t.Errorf("NoColor + NerdFont painted %q", got)
	}
}

// BenchmarkPaintGlyph is 12.7 F.12: the tier must not cost the paint path
// anything a frame would notice. Under Plain it is one predictable branch;
// under NerdFont it is a rune decode and a binary search over a table built at
// package initialization.
func BenchmarkPaintGlyph(b *testing.B) {
	for _, c := range []struct {
		name string
		set  GlyphSet
	}{{"plain", Plain}, {"nerdfont", NerdFont}} {
		s := NewStylerIn(TrueColor, FocusNormal, c.set)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sink = s.Paint(GlyphWorking, blocks.StateLive, blocks.HueAlive)
			}
		})
	}
}

// BenchmarkPaintProse is the same measurement for the case that dominates a
// frame: text that is not a glyph and must fall straight through the tier.
func BenchmarkPaintProse(b *testing.B) {
	for _, c := range []struct {
		name string
		set  GlyphSet
	}{{"plain", Plain}, {"nerdfont", NerdFont}} {
		s := NewStylerIn(TrueColor, FocusNormal, c.set)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sink = s.Paint("the answer the reader came for", blocks.StateSettled, blocks.HueNone)
			}
		})
	}
}

var sink string

// TestNilStylerResolvesTheFloor: a nil *Styler is a real state in this tree —
// a block built before a profile was chosen holds one — and adopting the tier
// means replacing a package-level constant with a method call. A lookup that
// panicked where the constant could not would make a one-token adoption change
// when a renderer crashes.
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
