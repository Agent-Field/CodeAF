package park

import (
	"context"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

const (
	// neighbourK is how many nearest chats each chat contributes to the
	// calibration. It is the neighbour stage's kept list, so the floor is
	// read off the same neighbourhood the graph was built from.
	neighbourK = 10

	// floorQuantile is the lower quartile of those neighbour cosines. A
	// place that matches a chat worse than three quarters of ordinary
	// neighbour links is not a home. On the primary model this lands near
	// the reference run's 0.45 floor.
	floorQuantile = 0.25

	// marginQuantile is the point on each chat's nearest-versus-second gap
	// where the reference run's 0.04 margin sits. The median gap is too
	// small: a smaller place that is only slightly closer would win, and
	// the homes would come out deeper than that run. Every space uses this
	// same percentile of its own gaps.
	marginQuantile = 0.60

	// deltaQuantile is the median of that same gap. The link stage treats
	// a second place within delta of the best as another home. It is a
	// different percentile from the margin on purpose: the margin is the
	// bar for descending, and the median gap is the bar for "about as close".
	deltaQuantile = 0.50

	// priorFloor, priorMargin and priorDelta are the primary model's
	// measured operating point. They are used only when a space has fewer
	// than two chats, or no second neighbour, so there is no distribution
	// to read. A space that has one calibrates from its own quantiles and
	// does not copy these.
	priorFloor  float32 = 0.45
	priorMargin float32 = 0.04
	priorDelta  float32 = 0.03
)

// calibrate reads the floor, margin and delta off this space's own neighbour
// cosines. Vectors from two spaces are never mixed here: the caller passes
// one space, and the figures are stored under that space alone.
func calibrate(ctx context.Context, vectors [][]float32) (placeorg.Thresholds, error) {
	prior := placeorg.Thresholds{Floor: priorFloor, Margin: priorMargin, Delta: priorDelta}
	n := countable(vectors)
	if n < 2 {
		return prior, nil
	}
	k := neighbourK
	if k > n-1 {
		k = n - 1
	}
	sims := make([]float64, 0, n*k)
	gaps := make([]float64, 0, n)
	for i := range vectors {
		if i%64 == 0 {
			if err := ctx.Err(); err != nil {
				return placeorg.Thresholds{}, err
			}
		}
		if len(vectors[i]) == 0 {
			continue
		}
		nearest := largestCosines(vectors, i, k)
		if len(nearest) == 0 {
			continue
		}
		sims = append(sims, nearest...)
		if len(nearest) >= 2 {
			// nearest is ascending, so the last two are the closest and
			// the runner-up. The gap is how much closer the nearest is.
			gaps = append(gaps, nearest[len(nearest)-1]-nearest[len(nearest)-2])
		}
	}
	if len(sims) == 0 {
		return prior, nil
	}
	sort.Float64s(sims)
	th := placeorg.Thresholds{
		Floor:  float32(quantile(sims, floorQuantile)),
		Margin: priorMargin,
		Delta:  priorDelta,
	}
	if len(gaps) > 0 {
		sort.Float64s(gaps)
		th.Margin = float32(quantile(gaps, marginQuantile))
		th.Delta = float32(quantile(gaps, deltaQuantile))
	}
	return th, nil
}

func countable(vectors [][]float32) int {
	n := 0
	for _, v := range vectors {
		if len(v) > 0 {
			n++
		}
	}
	return n
}

// largestCosines returns up to k cosines to other chats, smallest first.
// The full matrix is never stored: each row keeps a list of length k.
func largestCosines(vectors [][]float32, i, k int) []float64 {
	var best []float64
	for j := range vectors {
		if j == i {
			continue
		}
		s, ok := cosine(vectors[i], vectors[j])
		if !ok {
			continue
		}
		best = keepLargest(best, s, k)
	}
	return best
}

// keepLargest holds the k largest values, sorted ascending, so index 0 is
// the one a new value has to beat to enter.
func keepLargest(best []float64, s float64, k int) []float64 {
	if len(best) < k {
		best = append(best, s)
		sort.Float64s(best)
		return best
	}
	if s <= best[0] {
		return best
	}
	best[0] = s
	sort.Float64s(best)
	return best
}

func cosine(a, b []float32) (float64, bool) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, false
	}
	var dot, na, nb float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0, false
	}
	s := dot / sqrt(na*nb)
	if s != s { // NaN
		return 0, false
	}
	return s, true
}

// quantile is the linear quantile (the ordinary one: position q*(n-1)
// interpolated between the two surrounding values). An empty slice returns
// zero; the caller does not ask for a quantile of nothing.
func quantile(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 || q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[n-1]
	}
	pos := q * float64(n-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
