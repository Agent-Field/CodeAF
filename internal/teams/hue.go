package teams

import "math"

// A team's colour is a hue angle in OKLCH degrees and a lightness tier. The
// arithmetic here spaces a new team's hue as far as it can from every hue in
// use and from the hues a palette reserves for meaning. Turning a hue into
// something a terminal can draw, and saying which hues are reserved, belongs to
// the interface (internal/tui3/teamhue.go).

// HueSpec is a team's colour as stored: a hue angle in degrees and a
// lightness tier, 0 or 1.
type HueSpec struct {
	Hue  float64
	Tier int
}

// HueBand is how many degrees either side of a reserved hue no team may take.
const HueBand = 25

// HueTierFree is how many teams are all drawn at tier 0 before the tiers
// start to alternate.
const HueTierFree = 6

// HueGap is the distance between two hues around the circle, 0..180.
func HueGap(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		d = 360 - d
	}
	return d
}

// HueAllowed reports whether a hue is clear of every reserved band.
func HueAllowed(h float64, reserved []float64) bool {
	for _, r := range reserved {
		if HueGap(h, r) < HueBand {
			return false
		}
	}
	return true
}

// TierFor is the tier the n-th team (from zero) is drawn at.
func TierFor(n int) int {
	if n < HueTierFree {
		return 0
	}
	return 1 - (n-HueTierFree)%2
}

// NextHue is the colour for a new team beside the used ones: the allowed whole
// degree farthest from every used hue, the lowest such degree on a tie.
func NextHue(used []HueSpec, reserved []float64) HueSpec {
	best, bestGap := -1.0, -1.0
	for d := 0; d < 360; d++ {
		h := float64(d)
		if !HueAllowed(h, reserved) {
			continue
		}
		gap := 1000.0
		for _, u := range used {
			gap = math.Min(gap, HueGap(h, u.Hue))
		}
		if gap > bestGap {
			best, bestGap = h, gap
		}
	}
	if best < 0 {
		best = 0
	}
	return HueSpec{Hue: best, Tier: TierFor(len(used))}
}

// HueChoices is k colours a new team could take, best first: each the
// farthest from the used hues and from the choices before it.
func HueChoices(used []HueSpec, reserved []float64, k int) []HueSpec {
	tier := TierFor(len(used))
	seen := append([]HueSpec(nil), used...)
	out := make([]HueSpec, 0, k)
	for len(out) < k {
		next := NextHue(seen, reserved)
		next.Tier = tier
		out = append(out, next)
		seen = append(seen, next)
	}
	return out
}
