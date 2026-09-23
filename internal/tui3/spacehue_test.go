package tui3

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// spaceHueSequence is the first n hues the generator hands out, one space at a
// time.
func spaceHueSequence(n int, reserved []float64) []spaceHueSpec {
	var used []spaceHueSpec
	for len(used) < n {
		used = append(used, nextSpaceHue(used, reserved))
	}
	return used
}

// THE FARTHEST POINT SPREADS THE HUES: however many spaces there are, the
// closest two are about as far apart as an even share of the allowed circle
// would put them.
func TestSpaceHueFarthestPointIsWellSpread(t *testing.T) {
	reserved := spaceReservedFrom(darkRamp)
	// The circle's allowed share: every degree outside the bands.
	allowed := 0
	for d := 0; d < 360; d++ {
		if spaceHueAllowed(float64(d), reserved) {
			allowed++
		}
	}
	for n := 2; n <= 12; n++ {
		seq := spaceHueSequence(n, reserved)
		least := 360.0
		for i := range seq {
			for j := range seq[:i] {
				least = math.Min(least, hueGap(seq[i].Hue, seq[j].Hue))
			}
		}
		// Farthest-point insertion is within a factor of two of the best
		// spacing; on this circle it does far better, and the floor is the
		// share each would get minus the part the bands take from it.
		floor := 0.9*float64(allowed)/float64(n) - float64(2*spaceHueBand)
		if least < floor {
			t.Errorf("n=%d: the closest two hues are %.0f° apart, want at least %.0f (%v)", n, least, floor, seq)
		}
	}
}

// NO SPACE TAKES A HUE THAT ALREADY MEANS SOMETHING.
func TestSpaceHueKeepsOutOfTheReservedBands(t *testing.T) {
	for name, r := range map[string]ramp{"dark": darkRamp, "light": lightRamp} {
		reserved := spaceReservedFrom(r)
		if len(reserved) != 4 {
			t.Fatalf("%s: %d reserved hues, want amber, live, red and accent", name, len(reserved))
		}
		for _, h := range spaceHueSequence(24, reserved) {
			for _, res := range reserved {
				if hueGap(h.Hue, res) < spaceHueBand {
					t.Fatalf("%s: hue %.0f sits %.1f° from reserved %.1f", name, h.Hue, hueGap(h.Hue, res), res)
				}
			}
		}
	}
}

// THE TIERS: six at the first lightness, then alternating.
func TestSpaceHueTiersAlternateAfterSix(t *testing.T) {
	seq := spaceHueSequence(10, spaceReservedFrom(darkRamp))
	want := []int{0, 0, 0, 0, 0, 0, 1, 0, 1, 0}
	for i, h := range seq {
		if h.Tier != want[i] {
			t.Fatalf("space %d: tier %d, want %d", i, h.Tier, want[i])
		}
	}
}

// OKLCH TO sRGB AND BACK: the conversion is sane, and every colour a space is
// given is inside the gamut and reads at 3:1 on the dark ramp's ground.
func TestSpaceHueRoundTripGamutAndContrast(t *testing.T) {
	for _, c := range [][3]uint8{{0x9D, 0xC3, 0xE6}, {0xEB, 0xCB, 0x8B}, {0x80, 0x80, 0x80}, {0xFF, 0x00, 0x00}} {
		L, A, B := rgbToOKLab(c[0], c[1], c[2])
		C := math.Hypot(A, B)
		h := math.Atan2(B, A) * 180 / math.Pi
		r, g, b := oklchToSRGB(L, C, h)
		if got := [3]uint8{to8(r), to8(g), to8(b)}; got != c {
			if absDiff(got[0], c[0]) > 1 || absDiff(got[1], c[1]) > 1 || absDiff(got[2], c[2]) > 1 {
				t.Fatalf("#%02x%02x%02x came back as #%02x%02x%02x", c[0], c[1], c[2], got[0], got[1], got[2])
			}
		}
	}
	pal := newPalette(tokens.TrueColor, false)
	ground := spaceGround(pal)
	for i, h := range spaceHueSequence(12, spaceReservedHues(pal)) {
		r, g, b := spaceRGB(pal, h)
		if ratio := contrastRatio(luminanceOf(r, g, b), ground); ratio < 3 {
			t.Fatalf("space %d #%02x%02x%02x is %.2f:1 on the ground", i, r, g, b, ratio)
		}
		// The drawn colour carries the hue it was given.
		if got := oklabHue(r, g, b); hueGap(got, h.Hue) > 6 {
			t.Fatalf("space %d asked for %.0f° and drew %.0f°", i, h.Hue, got)
		}
	}
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// THE 256-COLOUR MAPPING IS THE SAME EVERY TIME, and never one of the first
// sixteen, which are the person's own theme.
func TestSpaceHueANSI256IsDeterministic(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	for _, h := range spaceHueSequence(8, spaceReservedHues(pal)) {
		a, b := spaceHueOf(pal, h).idx, spaceHueOf(pal, h).idx
		if a != b || a < 16 {
			t.Fatalf("hue %.0f maps to %d then %d", h.Hue, a, b)
		}
	}
	if newPalette(tokens.ANSI16, false).spaceInk(spaceHueSpec{Hue: 100}) != nil {
		t.Fatal("sixteen colours drew a space colour")
	}
	if newPalette(tokens.TrueColor, true).spaceInk(spaceHueSpec{Hue: 100}) != nil {
		t.Fatal("the ASCII floor drew a space colour")
	}
}

// THE FIRST EIGHT COLOURS ON THE DARK RAMP, for the record.
func TestSpaceHuePrintsTheFirstEight(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	for i, h := range spaceHueSequence(8, spaceReservedHues(pal)) {
		r, g, b := spaceRGB(pal, h)
		t.Logf("space %d: hue %3.0f° tier %d  #%02x%02x%02x  %.2f:1", i+1, h.Hue, h.Tier, r, g, b,
			contrastRatio(luminanceOf(r, g, b), spaceGround(pal)))
	}
	t.Logf("reserved: %v", fmtHues(spaceReservedHues(pal)))
}

func fmtHues(hs []float64) string {
	s := ""
	for _, h := range hs {
		s += fmt.Sprintf("%.0f° ", h)
	}
	return s
}

// A FILE FROM BEFORE COLOURS IS COLOURED ON LOAD, THE SAME WAY EVERY TIME, and
// around the spaces that already have theirs.
func TestSpaceHueLegacyLoadIsStable(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"spaces":[{"name":"a","members":[{"key":"k1"}]},{"name":"b","members":[],"hue":200,"tier":0},{"name":"c","members":[]}]}`
	if err := os.WriteFile(filepath.Join(dir, spacesFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	load := func() []space {
		a := newTestAppWithProfile(dir, nil)
		a.spacesEnsure()
		return a.wall.spaces
	}
	first, second := load(), load()
	if first[1].Hue != 200 {
		t.Fatalf("a stored hue was replaced: %v", first[1].Hue)
	}
	for i := range first {
		if first[i].Hue != second[i].Hue || first[i].Tier != second[i].Tier {
			t.Fatalf("space %d coloured %v then %v", i, first[i].hueSpec(), second[i].hueSpec())
		}
	}
	if hueGap(first[0].Hue, 200) < 60 || hueGap(first[2].Hue, 200) < 60 || hueGap(first[0].Hue, first[2].Hue) < 60 {
		t.Fatalf("legacy spaces crowd each other: %v %v %v", first[0].Hue, first[1].Hue, first[2].Hue)
	}
}

// EACH NEW SPACE TAKES A HUE NO OTHER HAS; toggling, recolouring and renaming
// are saved.
func TestSpaceColourAndEditsPersist(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithProfile(dir, nil)
	seen := map[float64]bool{}
	for i, name := range []string{"one", "two", "three", "four"} {
		at, err := a.spaceMake(name, []chatTab{{key: fmt.Sprintf("k%d", i)}})
		if err != nil {
			t.Fatal(err)
		}
		h := a.wall.spaces[at].Hue
		if seen[h] {
			t.Fatalf("space %s took hue %.0f, which another space has", name, h)
		}
		seen[h] = true
	}
	tab := chatTab{key: "k9", word: "nine"}
	if err := a.spaceToggleMember(0, tab); err != nil {
		t.Fatal(err)
	}
	if got := a.spacesOf("k9"); len(got) != 1 || got[0] != 0 {
		t.Fatalf("after toggling in: %v", got)
	}
	if err := a.spaceToggleMember(1, tab); err != nil {
		t.Fatal(err)
	}
	if got := a.spacesOf("k9"); len(got) != 2 {
		t.Fatalf("a conversation in two spaces is in %v", got)
	}
	if err := a.spaceRecolor(0, spaceHueSpec{Hue: 123, Tier: 1}); err != nil {
		t.Fatal(err)
	}
	if err := a.spaceRename(0, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := a.spaceRename(1, "RENAMED"); err == nil {
		t.Fatal("a second space took a name already used")
	}
	if err := a.spaceToggleMember(1, tab); err != nil {
		t.Fatal(err)
	}
	got, _ := loadSpaces(dir)
	if got[0].Name != "renamed" || got[0].Hue != 123 || got[0].Tier != 1 || !spaceHolds(got[0], "k9") || spaceHolds(got[1], "k9") {
		t.Fatalf("on disk: %+v", got[:2])
	}
}
