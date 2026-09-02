package lane

// ── A DISPERSION ACCOUNT SPLIT ACROSS TWO SPELLINGS ─────────────────────────
//
// The fold that re-keys a leaf's belief has to re-key everything filed under
// that leaf, and a run of thought is the one quantity that keeps a SECOND
// account there: how much one draw varies. Left behind under the alias it is
// orphaned — the clock reads the folded leaf, finds nothing, and answers
// [SpreadFloor] for ever, which is the prior the account existed to replace.

import (
	"math"
	"testing"
)

// TestAThinkingSpreadSplitAcrossTwoSpellingsIsPooledByTheFold holds the fold to
// the arithmetic: two halves of one leaf's account come back as the account the
// leaf would have held had every draw arrived under one name.
func TestAThinkingSpreadSplitAcrossTwoSpellingsIsPooledByTheFold(t *testing.T) {
	useShippedDefaultFold(t)
	const rung = "|high"
	underAlias := []float64{0.4, 1.1, 0.9}
	underServed := []float64{2.2, 0.3}

	var split chains
	for _, z := range underAlias {
		split.widen(foldAlias+rung, z)
	}
	for _, z := range underServed {
		split.widen(foldServable+rung, z)
	}
	split.refold()

	// The same draws, never split at all.
	var whole chains
	for _, z := range append(append([]float64{}, underAlias...), underServed...) {
		whole.widen(foldServable+rung, z)
	}

	leaf := foldServable + rung
	if len(split.Spread) != 1 {
		t.Fatalf("the fold left %d dispersion accounts, want one: %+v", len(split.Spread), split.Spread)
	}
	got, held := split.Spread[leaf]
	if !held {
		t.Fatalf("nothing is filed under the folded leaf %q: %+v", leaf, split.Spread)
	}
	want := whole.Spread[leaf]
	if got.N != want.N {
		t.Errorf("the pooled account counts %d draws, want %d", got.N, want.N)
	}
	if math.Abs(got.Mean-want.Mean) > 1e-9 || math.Abs(got.M2-want.M2) > 1e-9 {
		t.Errorf("the pooled account is %+v, want the undivided %+v", got, want)
	}

	// AND THE CLOCK READS IT. A merge that lost either half would answer the
	// floor, which is the failure this fold exists to stop.
	drawn := split.draw(leaf, SpreadFloor)
	if math.Abs(drawn-whole.draw(leaf, SpreadFloor)) > 1e-9 {
		t.Errorf("the folded leaf draws %v, want the undivided %v", drawn, whole.draw(leaf, SpreadFloor))
	}
	if drawn == SpreadFloor {
		t.Errorf("the folded leaf answered the prior %v, so the account did not survive the fold", SpreadFloor)
	}
}
