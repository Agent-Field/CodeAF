package config

import (
	"math"
	"testing"
)

// THE CHECKER'S CEILING GROWS WITH THE WORK, AND ONLY SO FAR. Work that cost
// what it was expected to leaves the ceiling where it was; work that overran
// stretches it by the same factor; and no overrun stretches it past
// CrewCheckCeilingStretch, so a ceiling is still a ceiling.
func TestTheChecksCeilingStretchesWithTheWorkAndStops(t *testing.T) {
	const base = 0.373248 // run19's: three times the checker's $0.124416
	for _, tc := range []struct {
		name            string
		spent, estimate float64
		want            float64
	}{
		{"no estimate", 0.06, 0, base},
		{"under its estimate", 0.005, 0.0106, base},
		{"exactly its estimate", 0.0106, 0.0106, base},
		{"twice its estimate", 0.0212, 0.0106, base * 2},
		{"run19's worker, 5.9 times", 0.062858484, 0.0106390908, base * CrewCheckCeilingStretch},
		{"a hundred times", 1.06, 0.0106, base * CrewCheckCeilingStretch},
	} {
		if got := CrewCheckCeilingFor(base, tc.spent, tc.estimate); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: ceiling = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := CrewCheckCeilingFor(0, 5, 1); got != 0 {
		t.Errorf("no ceiling stretched into one: %v", got)
	}
}
