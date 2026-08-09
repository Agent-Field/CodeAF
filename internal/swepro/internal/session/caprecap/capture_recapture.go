// Lincoln–Petersen / Chapman capture–recapture for residual-defect estimation
// — port of src/session/capture-recapture.ts.
//
// Two independent audits of the same work product are two "captures" of the
// same latent defect population. n1 defects surface in the first audit, n2 in
// the second, and m of them surface in BOTH. The Chapman-corrected
// Lincoln–Petersen estimator turns that overlap into an estimate of the TOTAL
// population — including the defects neither audit found — from which we back
// out a residual (still-hidden) count:
//
//	N̂ = ((n1 + 1)(n2 + 1) / (m + 1)) − 1        (Chapman bias correction)
//	distinct = n1 + n2 − m                       (defects at least one saw)
//	residual = max(0, N̂ − distinct)             (defects neither saw)
//
// MatchFindings pairs blocker/finding strings MECHANICALLY, via a normalized
// token-set (Jaccard) similarity ≥ 0.5 with greedy one-to-one assignment.
//
// Fidelity notes (deliberate, do not "fix"):
//   - Counts stay float64 end to end. The TS side never coerces to an integer
//     type, so a caller passing 1e308 still overflows the Chapman product to
//     Infinity and lands NaN in estimatedResidual — JSON.stringify emits null
//     for both, which jscompat.JSNumber reproduces.
//   - jsRound reproduces Math.round exactly, including the half-toward-+Inf
//     tie rule AND the cases where the math.Floor(x+0.5) shorthand is wrong.
//   - The tokenizer hand-rolls JS toLowerCase for the ASCII-alnum subset
//     rather than calling strings.ToLower: see jsLowerASCIIAlnum.
//   - MatchFindings seeds bestSim at 0 and requires a STRICT improvement, so a
//     threshold of 0 (or below) still matches nothing. Suspected TS bug, kept.
package caprecap

import (
	"math"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// ResidualEstimate mirrors the exported TS interface. Field order is the TS
// declaration order because JSON.stringify emits object keys in that order.
type ResidualEstimate struct {
	// EstimatedTotal is the Chapman point estimate of the total latent
	// defect population (integer).
	EstimatedTotal jscompat.JSNumber `json:"estimatedTotal"`
	// EstimatedResidual is the estimated count of defects neither audit
	// found: max(0, total − distinct-found).
	EstimatedResidual jscompat.JSNumber `json:"estimatedResidual"`
}

// ResidualInput is the inline object-literal parameter of
// estimateResidualDefects. The TS module does not name it; the key order here
// matches the TS type so a round-tripped JSON payload is byte-identical.
type ResidualInput struct {
	Sample1 jscompat.JSNumber `json:"sample1"`
	Sample2 jscompat.JSNumber `json:"sample2"`
	Overlap jscompat.JSNumber `json:"overlap"`
}

// clampCount mirrors clampCount(): non-finite or non-positive collapses to 0,
// otherwise Math.floor. Note that NaN and ±Infinity are indistinguishable in
// the output because all three map to 0.
func clampCount(n float64) float64 {
	if math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return 0
	}
	return math.Floor(n)
}

// jsRound reproduces Math.round: NaN/±0/±Infinity pass through, (0, 0.5)
// collapses to +0, [-0.5, 0) collapses to -0, and everything else is the
// nearest integer with ties broken toward +Infinity (so -1.5 → -1, not -2).
//
// math.Floor(x+0.5) is the usual shorthand but is NOT Math.round: wherever the
// x+0.5 sum itself rounds up to the next representable double the shorthand
// overshoots by one. V8 returns 0 for Math.round(0.49999999999999994) and
// 4503599627370497 for Math.round(4503599627370497); the shorthand returns 1
// and 4503599627370498. The r-x correction below restores both.
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x == 0 {
		return x
	}
	if x > 0 && x < 0.5 {
		return 0
	}
	if x < 0 && x >= -0.5 {
		return math.Copysign(0, -1)
	}
	r := math.Floor(x + 0.5)
	if r-x > 0.5 {
		r--
	}
	return r
}

// EstimateResidualDefects is the Chapman-corrected Lincoln–Petersen
// residual-defect estimate.
//
// input.Sample1 is the defect count found by the first audit (n1),
// input.Sample2 the second (n2), and input.Overlap the count found by both
// (m); the overlap is silently clamped to ≤ min(n1, n2).
func EstimateResidualDefects(input ResidualInput) ResidualEstimate {
	n1 := clampCount(float64(input.Sample1))
	n2 := clampCount(float64(input.Sample2))
	// Overlap can never exceed either capture. math.Min (not the builtin
	// min) because JS Math.min propagates NaN and the builtin does not.
	m := math.Min(math.Min(clampCount(float64(input.Overlap)), n1), n2)

	distinctFound := n1 + n2 - m
	chapman := ((n1+1)*(n2+1))/(m+1) - 1
	// The population can never be smaller than what the two audits already
	// distinctly saw; clamp the (fractional) estimator up to distinctFound.
	estimatedTotal := math.Max(distinctFound, jsRound(chapman))
	estimatedResidual := math.Max(0, estimatedTotal-distinctFound)
	return ResidualEstimate{
		EstimatedTotal:    jscompat.JSNumber(estimatedTotal),
		EstimatedResidual: jscompat.JSNumber(estimatedResidual),
	}
}

// ---------------------------------------------------------------------------
// Mechanical finding matcher.

// tokenSet stands in for the JS Set<string> built by tokenSet(). Membership
// lives in a map but iteration walks the insertion-ordered slice, so Go map
// ordering can never reach a result.
type tokenSet struct {
	order []string
	seen  map[string]struct{}
}

func (t *tokenSet) size() int { return len(t.order) }

func (t *tokenSet) has(s string) bool {
	_, ok := t.seen[s]
	return ok
}

func (t *tokenSet) add(s string) {
	if _, ok := t.seen[s]; ok {
		return
	}
	t.seen[s] = struct{}{}
	t.order = append(t.order, s)
}

// jsLowerASCIIAlnum reports whether r contributes an ASCII [a-z0-9] character
// to String.prototype.toLowerCase(), which is all the /[^a-z0-9]+/ split in
// tokenSet() can ever see. breaksRun marks a mapping that emits the token
// character AND a following non-token code point.
//
// The whole Unicode code-point space was diffed against V8: the only runes
// whose JS lowercase contains an ASCII alnum are 0-9, A-Z, a-z, U+0130 (LATIN
// CAPITAL LETTER I WITH DOT ABOVE) and U+212A (KELVIN SIGN). U+0130 is why
// this is hand-rolled instead of calling strings.ToLower: JS full case mapping
// expands it to "i" + U+0307 COMBINING DOT ABOVE — an 'i' immediately followed
// by a separator — whereas Go's simple mapping folds it to a bare "i". So
// "İİ" yields the two 1-char tokens "i","i" (both dropped by the length ≥ 2
// filter) in JS, but the single 2-char token "ii" under strings.ToLower.
func jsLowerASCIIAlnum(r rune) (c byte, isToken bool, breaksRun bool) {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'z':
		return byte(r), true, false
	case r >= 'A' && r <= 'Z':
		return byte(r) + ('a' - 'A'), true, false
	case r == 0x0130:
		return 'i', true, true
	case r == 0x212a:
		return 'k', true, false
	}
	return 0, false, false
}

// newTokenSet mirrors tokenSet(): lowercase, split on runs of [^a-z0-9], keep
// the tokens of length ≥ 2. The split is inlined as a scanner — RE2 would do,
// but a run scanner makes the U+0130 run break expressible. String.split's
// leading/trailing empty strings are irrelevant: the length filter drops them.
//
// Token length is compared in bytes, which is sound because tokens are ASCII
// by construction, so bytes == UTF-16 code units == JS .length.
func newTokenSet(s string) *tokenSet {
	out := &tokenSet{seen: make(map[string]struct{})}
	var cur []byte
	flush := func() {
		if len(cur) >= 2 {
			out.add(string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range s {
		c, isToken, breaksRun := jsLowerASCIIAlnum(r)
		if !isToken {
			flush()
			continue
		}
		cur = append(cur, c)
		if breaksRun {
			flush()
		}
	}
	flush()
	return out
}

// jaccard mirrors jaccard(). The two-empties guard is redundant with the
// union == 0 guard, but both are kept because both are in the TS.
func jaccard(a, b *tokenSet) float64 {
	if a.size() == 0 && b.size() == 0 {
		return 0
	}
	inter := 0
	for _, t := range a.order {
		if b.has(t) {
			inter++
		}
	}
	union := a.size() + b.size() - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// DefaultMatchThreshold is the TS default parameter value of matchFindings.
const DefaultMatchThreshold = 0.5

// MatchFindings counts how many of `a`'s findings mechanically match one of
// `b`'s, via normalized token-set similarity ≥ threshold (default 0.5),
// assigning each b-finding to at most one a-finding (greedy best-match). The
// result is the capture–recapture overlap m, and is always ≤ min(len(a),
// len(b)).
//
// threshold is variadic to model the TS default parameter; only the first
// value is read, matching the single-argument TS signature.
//
// Kept-as-is TS behavior: bestSim starts at 0 and a candidate must beat it
// STRICTLY, so a zero-similarity pair is never selected even when threshold is
// 0 or negative — matchFindings(["xx"], ["yy"], 0) is 0, not 1. Likewise only
// the single best candidate is threshold-tested; if it falls short, no
// runner-up is tried.
func MatchFindings(a, b []string, threshold ...float64) int {
	th := DefaultMatchThreshold
	if len(threshold) > 0 {
		th = threshold[0]
	}
	bSets := make([]*tokenSet, len(b))
	for i, s := range b {
		bSets[i] = newTokenSet(s)
	}
	usedB := make([]bool, len(b))
	overlap := 0
	for _, aStr := range a {
		aSet := newTokenSet(aStr)
		bestIdx := -1
		bestSim := 0.0
		for j := 0; j < len(bSets); j++ {
			if usedB[j] {
				continue
			}
			sim := jaccard(aSet, bSets[j])
			if sim > bestSim {
				bestSim = sim
				bestIdx = j
			}
		}
		if bestIdx >= 0 && bestSim >= th {
			usedB[bestIdx] = true
			overlap++
		}
	}
	return overlap
}
