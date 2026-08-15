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
		if !tok.IsSurface() || tok == Ground || tok == Sheet ||
			tok == HugGroundBar || tok == HugGroundInput ||
			tok == CardGroundWorking || tok == CardGroundDelivered {
			// The two card rungs are not bands either, and are gated by
			// TestCardLadder for the same reason the hug's are gated by
			// TestHugLadder: a card IS a ground, so it must be the smallest step
			// that still reads as another plane, and a card held to the band's
			// floor would be the slab §16 refuses — with nowhere left for a band
			// drawn on top of it.
			// The two hug rungs are not bands either, for the same reason and
			// one more: they are gated by TestHugLadder, and they are quieter
			// than the sheet by construction, so the band's floor would fail
			// them by design rather than by defect.
			//
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

// TestHugLadder is the composer hug's own elevation, gated as a ladder for the
// reason TestSheetLadder gates the dialog's: three named rungs whose ORDER and
// whose steps are the design, measured at the authored values and again after
// the 256-colour degradation, because that is the profile where a step this
// quiet silently disappears.
//
// The claims, in the order they matter:
//
//  1. ground < bar < input < sheet. The hug stands above the room's floor (it
//     is a different plane, and the transcript slides under it), the input row
//     stands above the bar row (that step IS the depth cue, §16's "surface
//     seams are grounds, not strokes"), and BOTH stay under the dialog's rung,
//     because permanent chrome may not announce itself as loudly as something
//     that arrived and will leave.
//  2. The step between the two rungs clears [HugSeparationMin] at truecolor.
//  3. At 256 colours the two rungs land on DIFFERENT greyscale entries, in the
//     same order. This is the tight one: the xterm ramp holds a single step
//     between the ground's entry and the band's, so a bar rung authored a
//     shade too high collapses onto the input rung and the two-tone exists
//     only on the profile nobody screenshots.
func TestHugLadder(t *testing.T) {
	ground := Ground.Color(FocusNormal)
	bar, input, sheet := HugGroundBar.Color(FocusNormal), HugGroundInput.Color(FocusNormal), Sheet.Color(FocusNormal)
	if !(ground.Luminance() < bar.Luminance() && bar.Luminance() < input.Luminance() &&
		input.Luminance() < sheet.Luminance()) {
		t.Errorf("the hug ladder is out of order: ground %s, bar %s, input %s, sheet %s",
			ground.Hex(), bar.Hex(), input.Hex(), sheet.Hex())
	}
	if sep := Contrast(input, bar); sep < HugSeparationMin {
		t.Errorf("the two hug rungs separate by %.4f, under the %.2f floor", sep, HugSeparationMin)
	}
	if sep := Contrast(bar, ground); sep <= 1 {
		t.Errorf("the bar rung is not a plane at all: %.4f over the ground", sep)
	}
	// Neither rung may reach the band's floor: a permanent strip raised as far
	// as a selection is the slab §16 refuses.
	for _, r := range []struct {
		name string
		on   Color
	}{{"bar", bar}, {"input", input}} {
		if sep := Contrast(r.on, ground); sep >= BandSeparationMin {
			t.Errorf("the hug %s rung is %.3f over the ground, at or past the band's floor %.2f",
				r.name, sep, BandSeparationMin)
		}
	}
	t.Logf("authored: bar %s %.4f over ground, input %s %.4f over ground, pair %.4f, input under sheet by %.4f",
		bar.Hex(), Contrast(bar, ground), input.Hex(), Contrast(input, ground),
		Contrast(input, bar), Contrast(sheet, input))

	g, b, i, s := Ground.Index(ANSI256, FocusNormal), HugGroundBar.Index(ANSI256, FocusNormal),
		HugGroundInput.Index(ANSI256, FocusNormal), Sheet.Index(ANSI256, FocusNormal)
	if b == i {
		t.Errorf("at 256 colours the two-tone collapses: bar %d, input %d", b, i)
	}
	b2, i2 := color256(int(b)), color256(int(i))
	if b2.Luminance() >= i2.Luminance() {
		t.Errorf("at 256 colours the hug ladder is out of order: bar %s, input %s", b2.Hex(), i2.Hex())
	}
	if sep := Contrast(i2, b2); sep < HugSeparationMin {
		t.Errorf("at 256 colours the rungs separate by %.4f, under the %.2f floor", sep, HugSeparationMin)
	}
	t.Logf("at 256: ground %d, bar %d %s, input %d %s, sheet %d (pair %.4f)",
		g, b, b2.Hex(), i, i2.Hex(), s, Contrast(i2, b2))
}

// TestHugRungsAreDerived pins the two rungs to their derivation, so a
// hand-edited literal that stops obeying [HugBarTowardBand] fails the build the
// way a hand-edited sheet does.
func TestHugRungsAreDerived(t *testing.T) {
	ground, band := Ground.Color(FocusNormal), Band.Color(FocusNormal)
	if want := Mix(ground, band, HugBarTowardBand); HugGroundBar.Color(FocusNormal) != want {
		t.Errorf("hug bar = %s, derivation says %s", HugGroundBar.Hex(FocusNormal), want.Hex())
	}
	if want := Mix(ground, band, HugInputTowardBand); HugGroundInput.Color(FocusNormal) != want {
		t.Errorf("hug input = %s, derivation says %s", HugGroundInput.Hex(FocusNormal), want.Hex())
	}
	if !(HugBarTowardBand < HugInputTowardBand && HugInputTowardBand < SheetTowardBand) {
		t.Errorf("the derivation constants are out of order: bar %.2f, input %.2f, sheet %.2f",
			HugBarTowardBand, HugInputTowardBand, SheetTowardBand)
	}
}

// TestCardLadder is [TestHugLadder] for the two card rungs, and it holds the
// three claims §4 rests on:
//
//  1. ground < working < delivered, and the delivered rung stops UNDER the
//     band's floor — a card raised as far as a selection is the slab §16
//     refuses, and leaves a band drawn on top of it nowhere to go.
//  2. The step between the two clears [SheetSeparationMin], so "this finished"
//     is a plane change a reader can see rather than a rounding difference.
//  3. At 256 colours the two land on DIFFERENT greyscale entries, in the same
//     order. The xterm ramp holds one step between the ground's entry and the
//     band's, so this is the constraint that actually picks the numbers.
func TestCardLadder(t *testing.T) {
	ground, band := Ground.Color(FocusNormal), Band.Color(FocusNormal)
	working := CardGroundWorking.Color(FocusNormal)
	delivered := CardGroundDelivered.Color(FocusNormal)

	if !(ground.Luminance() < working.Luminance() && working.Luminance() < delivered.Luminance()) {
		t.Errorf("the card ladder is out of order: ground %s, working %s, delivered %s",
			ground.Hex(), working.Hex(), delivered.Hex())
	}
	if sep := Contrast(delivered, working); sep < SheetSeparationMin {
		t.Errorf("the two card rungs separate by %.4f, under the %.2f floor",
			sep, SheetSeparationMin)
	}
	if sep := Contrast(working, ground); sep <= 1 {
		t.Errorf("the working rung is not a plane at all: %.4f over the ground", sep)
	}
	if sep := Contrast(delivered, ground); sep >= BandSeparationMin {
		t.Errorf("the delivered rung is %.3f over the ground, at or past the band's floor %.2f",
			sep, BandSeparationMin)
	}
	// The derivation, pinned the way the hug's is: a hand-edited literal that
	// stops obeying the constant fails the build.
	if want := Mix(ground, band, CardWorkingTowardBand); working != want {
		t.Errorf("card.working = %s, derivation says %s", working.Hex(), want.Hex())
	}
	if want := Mix(ground, band, CardDeliveredTowardBand); delivered != want {
		t.Errorf("card.delivered = %s, derivation says %s", delivered.Hex(), want.Hex())
	}

	w, d := CardGroundWorking.Index(ANSI256, FocusNormal), CardGroundDelivered.Index(ANSI256, FocusNormal)
	if w == d {
		t.Errorf("at 256 colours the card ladder collapses: working %d, delivered %d", w, d)
	}
	if w2, d2 := color256(int(w)), color256(int(d)); w2.Luminance() >= d2.Luminance() {
		t.Errorf("at 256 colours the card ladder is out of order: working %s, delivered %s",
			w2.Hex(), d2.Hex())
	}
	t.Logf("authored: working %s %.4f over ground, delivered %s %.4f over ground, pair %.4f; "+
		"at 256: ground %d, working %d, delivered %d",
		working.Hex(), Contrast(working, ground), delivered.Hex(), Contrast(delivered, ground),
		Contrast(delivered, working), Ground.Index(ANSI256, FocusNormal), w, d)
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
