package tokens

import (
	"fmt"
	"testing"
)

// TestContrastGate is the shipping gate 5.16 demands: "every pastel must pass
// contrast on BOTH the default dark ground and a dimmed/unfocused variant; the
// palette ships as named tokens with tested pairs". Every legal pairing is
// walked and measured; a palette edit that dims a hue past legibility fails
// here rather than in someone's terminal.
func TestContrastGate(t *testing.T) {
	pairs := Pairings()
	if len(pairs) == 0 {
		t.Fatal("no pairings: the gate would pass vacuously")
	}
	worst := map[Token]float64{}
	for _, p := range pairs {
		got, min := p.Contrast(), p.Min()
		if got < min {
			t.Errorf("%s(%s) on %s(%s): contrast %.2f < required %.2f",
				p.Fg, p.FgFocus, p.Ground, p.GroundFocus, got, min)
		}
		if w, ok := worst[p.Fg]; !ok || got < w {
			worst[p.Fg] = got
		}
	}
	for _, tok := range All() {
		if tok.IsSurface() {
			continue
		}
		t.Logf("%-16s worst legal contrast %.2f (class gate %.1f/%.1f)",
			tok, worst[tok], tok.Class().MinContrast(FocusNormal), tok.Class().MinContrast(FocusDimmed))
	}
}

// TestContrastTableIsReadable prints the whole matrix once so a palette change
// can be reviewed as numbers, not as a screenshot. `go test -run
// TestContrastTable -v` is the palette's contact sheet.
func TestContrastTableIsReadable(t *testing.T) {
	grounds := []Token{Ground, Band, BandIdentity0, BandIdentity4}
	header := fmt.Sprintf("%-16s", "token/ground")
	for _, g := range grounds {
		header += fmt.Sprintf("%18s", g.String())
	}
	t.Log(header)
	for _, tok := range All() {
		if tok.IsSurface() {
			continue
		}
		for _, f := range []Focus{FocusNormal, FocusDimmed} {
			row := fmt.Sprintf("%-16s", tok.String()+"/"+f.String())
			for _, g := range grounds {
				if !Legal(tok, f, g, FocusNormal) {
					row += fmt.Sprintf("%18s", "—")
					continue
				}
				row += fmt.Sprintf("%18.2f", Contrast(tok.Color(f), g.Color(FocusNormal)))
			}
			t.Log(row)
		}
	}
}

// TestBandSeparation holds the band between invisible and boxy. 5.16 wants a
// "slightly raised background pill"; 5.13 forbids boxes.
func TestBandSeparation(t *testing.T) {
	for _, tok := range All() {
		if !tok.IsSurface() || tok == Ground || tok == Sheet {
			// [Sheet] is not a band and is gated by TestSheetLadder instead. The
			// two surfaces answer different questions: a band is a pill ON a
			// ground and must be seen at a glance, while a sheet IS a ground and
			// must be the smallest step that still reads as another plane — a
			// sheet held to the band's floor would be the slab 5.13 refuses, and
			// would leave the band it carries nowhere to go.
			continue
		}
		sep := Contrast(tok.Color(FocusNormal), Ground.Color(FocusNormal))
		if sep < BandSeparationMin || sep > BandSeparationMax {
			t.Errorf("%s separation from ground is %.3f, outside [%.2f, %.2f]",
				tok, sep, BandSeparationMin, BandSeparationMax)
		}
		t.Logf("%-18s separation %.3f", tok, sep)
	}
}

// TestSheetLadder is the dialog elevation, gated as a LADDER rather than as
// three separate colours: ground → sheet → band, each rung a real step, in that
// order, at the authored values and after the 256-colour degradation both.
//
// The 256 half is not ceremony. The xterm greyscale ramp steps by 10 per
// channel in exactly the corner this palette's grounds live in, so a sheet
// authored a shade too close to the ground resolves to the ground's own entry
// and the elevation silently disappears on the majority profile — visible in
// truecolor, gone everywhere else, which is the degradation ladder failing in
// the one direction nobody screenshots.
func TestSheetLadder(t *testing.T) {
	ground, sheet, band := Ground.Color(FocusNormal), Sheet.Color(FocusNormal), Band.Color(FocusNormal)
	if !(ground.Luminance() < sheet.Luminance() && sheet.Luminance() < band.Luminance()) {
		t.Errorf("the ladder is out of order: ground %s, sheet %s, band %s",
			ground.Hex(), sheet.Hex(), band.Hex())
	}
	rungs := []struct {
		name   string
		on, of Color
	}{
		{"sheet over ground", sheet, ground},
		{"band over sheet", band, sheet},
	}
	for _, r := range rungs {
		sep := Contrast(r.on, r.of)
		if sep < SheetSeparationMin {
			t.Errorf("%s separates by %.3f, under the %.2f floor", r.name, sep, SheetSeparationMin)
		}
		t.Logf("%-18s %.3f authored", r.name, sep)
	}
	// The sheet must stay UNDER the band's own floor: a sheet raised as far as a
	// band is a slab, and 5.13 spends the structure budget on whitespace.
	if sep := Contrast(sheet, ground); sep >= BandSeparationMin {
		t.Errorf("the sheet is %.3f over the ground, at or past the band's own floor %.2f",
			sep, BandSeparationMin)
	}

	i, j, k := Ground.Index(ANSI256, FocusNormal), Sheet.Index(ANSI256, FocusNormal), Band.Index(ANSI256, FocusNormal)
	if i == j || j == k {
		t.Errorf("at 256 colours the ladder collapses: ground %d, sheet %d, band %d", i, j, k)
	}
	g2, s2, b2 := color256(int(i)), color256(int(j)), color256(int(k))
	if !(g2.Luminance() < s2.Luminance() && s2.Luminance() < b2.Luminance()) {
		t.Errorf("at 256 colours the ladder is out of order: %s %s %s", g2.Hex(), s2.Hex(), b2.Hex())
	}
	t.Logf("at 256: ground %d %s, sheet %d %s, band %d %s (rungs %.3f, %.3f)",
		i, g2.Hex(), j, s2.Hex(), k, b2.Hex(),
		Contrast(s2, g2), Contrast(b2, s2))
}

// TestPairingCoverage proves the gate has no blind spot: every foreground token
// is measured in both focus states, and every surface token is measured as a
// ground.
func TestPairingCoverage(t *testing.T) {
	fgSeen := map[[2]int]bool{}
	groundSeen := map[Token]bool{}
	for _, p := range Pairings() {
		fgSeen[[2]int{int(p.Fg), int(p.FgFocus)}] = true
		groundSeen[p.Ground] = true
	}
	for _, tok := range All() {
		if tok.IsSurface() {
			if !groundSeen[tok] {
				t.Errorf("surface %s is never gated as a ground", tok)
			}
			continue
		}
		for _, f := range []Focus{FocusNormal, FocusDimmed} {
			if !fgSeen[[2]int{int(tok), int(f)}] {
				t.Errorf("%s(%s) is never gated against any ground", tok, f)
			}
		}
	}
}

// TestForbiddenPairingsAreNotGratuitous checks the one composition rule that
// removes work from the palette instead of adding it (Legal rule 3: a dimmed
// foreground may only sit on the ground). A rule that forbade combinations
// which would have passed anyway would be dead weight; this proves the rule is
// load-bearing by showing a forbidden pairing that genuinely cannot clear its
// gate.
func TestForbiddenPairingsAreNotGratuitous(t *testing.T) {
	got := Contrast(TextSecondary.Color(FocusDimmed), Band.Color(FocusNormal))
	min := TextSecondary.Class().MinContrast(FocusDimmed)
	if got >= min {
		t.Fatalf("dimmed secondary on the band now measures %.2f (gate %.2f): "+
			"the ban on dimmed-on-band is no longer load-bearing and should be revisited",
			got, min)
	}
	t.Logf("dimmed secondary on band = %.2f < gate %.2f — an unfocused pane draws "+
		"no band, it marks selection with the dim accent rail", got, min)
}

// TestContrastGateAtEveryProfile closes the hole a truecolor-only gate leaves.
// The palette is AUTHORED in 24-bit sRGB, and the numbers above measure those
// authored values — but most terminals will never see them. A 256-color
// terminal renders the nearest cube entry, and a cube entry is a DIFFERENT
// colour, several percent of luminance away in either direction.
//
// 5.16's law is about what a person can read, not about what was authored, so
// the gate runs again over the colours each profile will actually paint. This
// is the test that would catch a palette edit whose truecolor value clears 4.5
// and whose 256-color approximation does not.
//
// ANSI16 is excluded and the exclusion is the honest one: at sixteen colours
// the actual pixels belong to the user's terminal theme, so no contrast number
// we compute would be a fact about anything. That profile is carried by the
// glyph vocabulary (5.17), which is why every state that colour carries also
// has a shape.
func TestContrastGateAtEveryProfile(t *testing.T) {
	for _, p := range Pairings() {
		fg := color256(int(p.Fg.Index(ANSI256, p.FgFocus)))
		ground := color256(int(p.Ground.Index(ANSI256, p.GroundFocus)))
		got, min := Contrast(fg, ground), p.Min()
		if got < min {
			t.Errorf("at 256 colours %s(%s) on %s(%s) resolves to %s on %s: contrast %.2f < required %.2f",
				p.Fg, p.FgFocus, p.Ground, p.GroundFocus, fg.Hex(), ground.Hex(), got, min)
		}
	}
}

// TestBandSeparationSurvives256: the selection band must still read as a raised
// pill after degradation, or selection stops being visible on the majority of
// terminals. It is allowed to lose its identity TINT there
// ([Profile.BandTintDistinct] says so out loud); it is not allowed to lose the
// band.
func TestBandSeparationSurvives256(t *testing.T) {
	ground := color256(int(Ground.Index(ANSI256, FocusNormal)))
	band := color256(int(Band.Index(ANSI256, FocusNormal)))
	sep := Contrast(band, ground)
	if sep < BandSeparationMin || sep > BandSeparationMax {
		t.Errorf("at 256 colours the band separates from the ground by %.3f, outside [%.2f, %.2f]",
			sep, BandSeparationMin, BandSeparationMax)
	}
	t.Logf("band separation: %.3f authored, %.3f at 256 colours",
		Contrast(Band.Color(FocusNormal), Ground.Color(FocusNormal)), sep)
}
