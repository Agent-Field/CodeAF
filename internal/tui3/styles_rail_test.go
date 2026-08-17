package tui3

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRailFormsAreOneWidth holds the three rail markers to the constant that
// replaced measuring them.
//
// toolLine used to call ansi.StringWidth on the rail once per tool row per
// frame to learn a number that cannot change: all three forms are four cells
// by construction, which is what makes a cluster's names start in one column
// whatever the terminal can draw. The constant is only safe while that stays
// true, so this is the test that makes changing one of them a failure here
// rather than a column that drifts on somebody's screen.
func TestRailFormsAreOneWidth(t *testing.T) {
	for _, form := range []struct {
		name string
		text string
	}{
		{"mid", railMid},
		{"last", railLast},
		{"ascii", railASCII},
	} {
		if got := ansi.StringWidth(form.text); got != railWidth {
			t.Errorf("rail %s measures %d cells, railWidth says %d", form.name, got, railWidth)
		}
	}
}
