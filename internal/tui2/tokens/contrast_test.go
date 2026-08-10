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
		if !tok.IsSurface() || tok == Ground {
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
