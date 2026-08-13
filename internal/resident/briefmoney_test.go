package resident

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// §4. The arrival brief said "$0.0023" in its header and "$0.00 spent" in its
// own bullet, two lines apart, about one number. The header is drawn by the
// surface and the bullet is written here, so the two have to agree by test:
// nothing in the type system makes a sentence and a badge climb the same
// ladder.
//
// The bound is the range a brief reports. Past a thousand dollars the header
// abbreviates to a count while the sentence spells the figure out, which is a
// difference of resolution between two true renderings and never of rounding.
func TestTheBriefsSpendLineAgreesWithTheHeaderItSitsUnder(t *testing.T) {
	for _, usd := range []float64{
		0, 0.0000001, 0.00005, 0.0001, 0.000891, 0.0023, 0.004999,
		0.005, 0.01, 0.019, 0.10, 1, 1.005, 12.34, 999.99,
	} {
		if got, want := briefMoney(usd), tokens.Money(usd); got != want {
			t.Errorf("briefMoney(%g) = %q, but the header above it reads %q", usd, got, want)
		}
	}
}

// A POSITIVE FIGURE NEVER RENDERS AS ZERO, which is the whole reason the ladder
// has a sub-cent rung: "$0.00 spent" about money that was spent is the reader
// learning the wrong fact, and it is the fact they are least able to check.
func TestABriefNeverReportsRealSpendAsNothing(t *testing.T) {
	for _, usd := range []float64{0.0000001, 0.00005, 0.000891, 0.0023, 0.004999} {
		if briefMoney(usd) == "$0.00" {
			t.Errorf("briefMoney(%g) reported real spend as nothing", usd)
		}
	}
	if briefMoney(0) != "$0.00" {
		t.Errorf("briefMoney(0) = %q — zero is a fact, not a rounding", briefMoney(0))
	}
}
