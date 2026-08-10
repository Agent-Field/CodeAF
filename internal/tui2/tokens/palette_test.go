package tokens

import (
	"math"
	"testing"
)

// goldenPalette pins every shipped value. It is the diff a reviewer reads when
// someone changes a color: one line per token, base and dimmed.
var goldenPalette = map[string][2]string{
	"text.primary":   {"#E6E6F0", "#878790"},
	"text.secondary": {"#A0A6BB", "#606373"},
	"text.tertiary":  {"#7C8296", "#4C505E"},

	"amber": {"#EECE96", "#8B795E"},
	"cyan":  {"#A4D7EA", "#627E8C"},
	"green": {"#A2E2BC", "#618473"},
	"coral": {"#EFA99F", "#8C6563"},

	"identity.0": {"#DDE6B3", "#82876E"},
	"identity.1": {"#BDE6B3", "#70876E"},
	"identity.2": {"#B3E6DD", "#6B8785"},
	"identity.3": {"#BDC7E5", "#70768A"},
	"identity.4": {"#C9BDE5", "#77708A"},
	"identity.5": {"#DBB3E6", "#816B8A"},
	"identity.6": {"#E6B3D6", "#876B81"},
	"identity.7": {"#E6B3BF", "#876B75"},

	"ground": {"#12121A", "#12121A"},
	"band":   {"#262633", "#1D1D28"},

	"band.identity.0": {"#35353D", "#25252D"},
	"band.identity.1": {"#32353D", "#24252D"},
	"band.identity.2": {"#313541", "#23252F"},
	"band.identity.3": {"#323341", "#24242F"},
	"band.identity.4": {"#333241", "#24242F"},
	"band.identity.5": {"#343141", "#25232F"},
	"band.identity.6": {"#353140", "#25232F"},
	"band.identity.7": {"#35313E", "#25232E"},
}

func TestPaletteGolden(t *testing.T) {
	if len(goldenPalette) != int(tokenCount) {
		t.Fatalf("golden palette has %d entries, inventory has %d", len(goldenPalette), tokenCount)
	}
	for _, tok := range All() {
		want, ok := goldenPalette[tok.String()]
		if !ok {
			t.Errorf("token %s is not pinned in goldenPalette", tok)
			continue
		}
		if got := tok.Hex(FocusNormal); got != want[0] {
			t.Errorf("%s base = %s, pinned %s", tok, got, want[0])
		}
		if got := tok.Hex(FocusDimmed); got != want[1] {
			t.Errorf("%s dimmed = %s, pinned %s", tok, got, want[1])
		}
	}
}

// TestDerivation is 10.1.1's requirement made mechanical: the derived layer is
// derived, not typed in. Every dimmed variant is its base mixed toward the
// ground by exactly DimTowardGround, and every identity band is the band mixed
// with its hue by exactly BandIdentityTint.
func TestDerivation(t *testing.T) {
	ground := Ground.Color(FocusNormal)
	for _, tok := range All() {
		if tok == Ground {
			continue // the floor does not move; see buildTable
		}
		want := Mix(tok.Color(FocusNormal), ground, DimTowardGround)
		if got := tok.Color(FocusDimmed); got != want {
			t.Errorf("%s dimmed = %s, derivation says %s", tok, got.Hex(), want.Hex())
		}
	}
	for i := range IdentityCount {
		want := Mix(Band.Color(FocusNormal), Identity(i).Color(FocusNormal), BandIdentityTint)
		if got := BandFor(Identity(i)).Color(FocusNormal); got != want {
			t.Errorf("band for identity %d = %s, derivation says %s", i, got.Hex(), want.Hex())
		}
	}
}

// TestGroundDoesNotDim: every contrast number in this package is stated against
// a floor, and a floor that moved would invalidate all of them.
func TestGroundDoesNotDim(t *testing.T) {
	if Ground.Color(FocusNormal) != Ground.Color(FocusDimmed) {
		t.Error("the ground dimmed; every contrast number in the package is now unmoored")
	}
}

// TestHueSeparation keeps identity from being mistaken for meaning (5.16: five
// words, and identity is not one of them). Semantic hues sit at 8, 38, 145 and
// 196 degrees; the identity wheel is placed in the gaps.
func TestHueSeparation(t *testing.T) {
	const (
		minFromSemantic = 20.0
		minBetweenWheel = 25.0
	)
	semantic := []Token{Coral, Amber, Green, Cyan}
	for i := range IdentityCount {
		ih := hueOf(Identity(i).Color(FocusNormal))
		for _, s := range semantic {
			if d := hueDistance(ih, hueOf(s.Color(FocusNormal))); d < minFromSemantic {
				t.Errorf("identity %d (%.0f°) is only %.0f° from %s (%.0f°); minimum is %.0f°",
					i, ih, d, s, hueOf(s.Color(FocusNormal)), minFromSemantic)
			}
		}
		for j := range IdentityCount {
			if i == j {
				continue
			}
			jh := hueOf(Identity(j).Color(FocusNormal))
			if d := hueDistance(ih, jh); d < minBetweenWheel {
				t.Errorf("identity %d (%.0f°) and %d (%.0f°) are only %.0f° apart; minimum is %.0f°",
					i, ih, j, jh, d, minBetweenWheel)
			}
		}
	}
	for _, s := range semantic {
		t.Logf("%-6s hue %5.1f° sat %.3f", s, hueOf(s.Color(FocusNormal)), satOf(s.Color(FocusNormal)))
	}
	for i := range IdentityCount {
		c := Identity(i).Color(FocusNormal)
		t.Logf("id%-4d hue %5.1f° sat %.3f", i, hueOf(c), satOf(c))
	}
}

// TestIdentityIsQuieterThanMeaning: a semantic hue must always be the more
// chromatic of the two, so that when an identity accent and a state glyph share
// a row, meaning wins the eye. This is the saturation half of the separation
// the hue-angle test does geometrically.
func TestIdentityIsQuieterThanMeaning(t *testing.T) {
	minSemantic := math.Inf(1)
	for _, s := range []Token{Coral, Amber, Green, Cyan} {
		minSemantic = math.Min(minSemantic, satOf(s.Color(FocusNormal)))
	}
	for i := range IdentityCount {
		if got := satOf(Identity(i).Color(FocusNormal)); got >= minSemantic {
			t.Errorf("identity %d saturation %.3f >= quietest semantic %.3f", i, got, minSemantic)
		}
	}
}

// TestTokenMetadata: names are unique and stable (golden tests key on them),
// classes are assigned, and no two tokens are the same color — two names for
// one value is a vocabulary with a redundant word.
func TestTokenMetadata(t *testing.T) {
	names := map[string]bool{}
	colors := map[Color]Token{}
	for _, tok := range All() {
		name := tok.String()
		if name == "" {
			t.Errorf("token %d has no name", tok)
		}
		if names[name] {
			t.Errorf("duplicate token name %q", name)
		}
		names[name] = true
		c := tok.Color(FocusNormal)
		if other, ok := colors[c]; ok {
			t.Errorf("%s and %s are both %s", tok, other, c.Hex())
		}
		colors[c] = tok
	}
}

// TestBandForAndIdentityIndex covers the two small mappings the rail depends on.
func TestBandForAndIdentityIndex(t *testing.T) {
	for i := range IdentityCount {
		band := BandFor(Identity(i))
		idx, ok := IdentityIndex(band)
		if !ok || idx != i {
			t.Errorf("BandFor(identity %d) = %s, IdentityIndex = %d,%v", i, band, idx, ok)
		}
	}
	if BandFor(TextPrimary) != Band {
		t.Error("a scope with no identity must select the untinted band")
	}
	if _, ok := IdentityIndex(Amber); ok {
		t.Error("a semantic hue is not an identity")
	}
	if Identity(IdentityCount+3) != Identity(3) {
		t.Error("Identity must wrap")
	}
}

func TestColorRoundTrip(t *testing.T) {
	for _, tok := range All() {
		got, ok := ParseHex(tok.Hex(FocusNormal))
		if !ok || got != tok.Color(FocusNormal) {
			t.Errorf("%s did not survive a hex round trip", tok)
		}
	}
	if _, ok := ParseHex("#GGGGGG"); ok {
		t.Error("ParseHex accepted a non-hex string")
	}
	if _, ok := ParseHex("12345"); ok {
		t.Error("ParseHex accepted a short string")
	}
}

func TestMixEndpoints(t *testing.T) {
	a, b := Color{0, 0, 0}, Color{255, 255, 255}
	if Mix(a, b, 0) != a || Mix(a, b, 1) != b {
		t.Error("Mix must be the identity at its endpoints")
	}
	if got := Mix(a, b, 0.5); got != (Color{128, 128, 128}) {
		t.Errorf("Mix midpoint = %v", got)
	}
}

// hueOf and satOf are HSL projections used only by these tests; the shipping
// palette stores sRGB and never needs them.
func hueOf(c Color) float64 {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	d := max - min
	if d == 0 {
		return 0
	}
	var h float64
	switch max {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h
}

func satOf(c Color) float64 {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	d := max - min
	if d == 0 {
		return 0
	}
	l := (max + min) / 2
	return d / (1 - math.Abs(2*l-1))
}

func hueDistance(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > 180 {
		d = 360 - d
	}
	return d
}
