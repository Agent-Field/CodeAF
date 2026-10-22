package selector

import "testing"

// ---- inheritance ladder: scored -> line -> vendor -> global ----

// TestLinePriorPredecessor: an unscored row inherits the scored predecessor
// on the same canonical line (canonical_slug + created); no name parsing.
func TestLinePriorPredecessor(t *testing.T) {
	prev := mkM("acme/f-1-20260901", "acme/f-1-20260901", 1000, 300_000, true, false, 1)
	next := mkM("acme/f-1-20260920", "acme/f-1-20260920", 2000, 300_000, true, false, 1)
	cat := mkCat(prev, next)
	snap := mkSnap(
		map[string]float64{"worker": 0.22},
		map[string]float64{"quality/worker/acme/f-1-20260901": 0.7},
		map[string]float64{"quality/worker/acme/f-1-20260901": 0.0004}, // sigma 0.02
	)
	pe := NewPriorEngine(snap, cat)
	p := pe.PriorFor("worker", "acme/f-1-20260920")
	if p.Source != "line" {
		t.Fatalf("expected line prior, got source=%s", p.Source)
	}
	if p.From != "acme/f-1-20260901" {
		t.Fatalf("expected From=predecessor %q, got %q", "acme/f-1-20260901", p.From)
	}
	if p.Q != 0.7 {
		t.Fatalf("expected Q=0.7 inherited from predecessor, got %v", p.Q)
	}
	if p.Sigma != 0.02 {
		t.Fatalf("expected line sigma 0.02 from predecessor, got %v", p.Sigma)
	}
	// the scored row itself stays scored
	ps := pe.PriorFor("worker", "acme/f-1-20260901")
	if ps.Source != "scored" || ps.Q != 0.7 {
		t.Fatalf("scored row mishandled: %+v", ps)
	}
}

// TestLinePriorNoNameParsing: a sibling that SHARES a slug prefix but diverges
// in a non-stamp way is a DIFFERENT line. It must NOT inherit; it falls to the
// vendor mean. The line key uses only the trailing -YYYYMMDD stamp, never
// token parsing.
func TestLinePriorNoNameParsing(t *testing.T) {
	scored := mkM("acme/muse-spark-1.3-20260901", "acme/muse-spark-1.3-20260901", 1000, 300_000, true, false, 1)
	clone := mkM("acme/muse-spark-1.3-extra", "acme/muse-spark-1.3-extra", 2000, 300_000, true, false, 1)
	cat := mkCat(scored, clone)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/acme/muse-spark-1.3-20260901": 0.7},
		map[string]float64{"quality/worker/acme/muse-spark-1.3-20260901": 0.0004},
	)
	pe := NewPriorEngine(snap, cat)
	p := pe.PriorFor("worker", "acme/muse-spark-1.3-extra")
	if p.From != "" {
		t.Fatalf("no name parsing: From must be empty ({%+v})", p)
	}
	if p.Source != "vendor" {
		t.Fatalf("variant clone must fall to vendor mean (no name parsing), got source=%s", p.Source)
	}
	if p.Q != 0.7 { // vendor mean = the only scored acme row
		t.Fatalf("expected vendor mean 0.7, got %v", p.Q)
	}
}

// TestGlobalFallback: unscored, no line predecessor, no same-vendor score ->
// global prior (Q 0.5, residual sigma).
func TestGlobalFallback(t *testing.T) {
	rand := mkM("zebra/alpha-20261001", "zebra/alpha-20261001", 9000, 300_000, true, false, 1)
	cat := mkCat(rand)
	snap := mkSnap(map[string]float64{"worker": 0.22}, nil, nil)
	pe := NewPriorEngine(snap, cat)
	p := pe.PriorFor("worker", "zebra/alpha-20261001")
	if p.Source != "global" {
		t.Fatalf("expected global prior, got source=%s", p.Source)
	}
	if p.Q != 0.5 {
		t.Fatalf("expected Q=0.5 global default, got %v", p.Q)
	}
	if p.Sigma != 0.22 {
		t.Fatalf("expected global sigma 0.22, got %v", p.Sigma)
	}
}

// ---- vendor donor-pool hygiene: only PUBLISHED rows feed the vendor mean ----

// TestVendorMeanIgnoresUnscored: the vendor mean averages ONLY rows that carry
// a score. Inherited/vendor siblings must not dilute it.
func TestVendorMeanIgnoresUnscored(t *testing.T) {
	scored := mkM("acme/one-20260901", "acme/one-20260901", 1000, 300_000, true, false, 1)
	unscored := mkM("acme/two-20260910", "acme/two-20260910", 2000, 300_000, true, false, 1)
	unscored2 := mkM("acme/three-20260920", "acme/three-20260920", 3000, 300_000, true, false, 1)
	cat := mkCat(scored, unscored, unscored2)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/acme/one-20260901": 0.7},
		map[string]float64{"quality/worker/acme/one-20260901": 0.0004},
	)
	pe := NewPriorEngine(snap, cat)
	p := pe.PriorFor("worker", "acme/three-20260920")
	if p.Source != "vendor" {
		t.Fatalf("expected vendor prior, got %s", p.Source)
	}
	if p.Q != 0.7 {
		t.Fatalf("vendor mean polluted: expected 0.7 (only published row), got %v", p.Q)
	}
}

// TestVendorMeanAveragesScoredOnly: a vendor with two scored rows and several
// unscored ones averages exactly the two published scores.
func TestVendorMeanAveragesScoredOnly(t *testing.T) {
	s1 := mkM("acme/one-20260901", "acme/one-20260901", 1000, 300_000, true, false, 1)
	s2 := mkM("acme/four-20260901", "acme/four-20260901", 1000, 300_000, true, false, 1)
	unscored := mkM("acme/bulk-20261001", "acme/bulk-20261001", 4000, 300_000, true, false, 1)
	cat := mkCat(s1, s2, unscored)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/acme/one-20260901": 0.7, "quality/worker/acme/four-20260901": 0.5},
		map[string]float64{"quality/worker/acme/one-20260901": 0.0004, "quality/worker/acme/four-20260901": 0.0004},
	)
	pe := NewPriorEngine(snap, cat)
	p := pe.PriorFor("worker", "acme/bulk-20261001")
	if p.Q != 0.6 {
		t.Fatalf("expected vendor mean 0.6 (avg of 0.7 and 0.5), got %v", p.Q)
	}
}

// ---- vendor rule: model with only vendor rows inherits vendor mean ----

// TestVendorOnlyModelInheritsVendorMean: a model with NO scored row anywhere
// and no same-line predecessor, whose vendor has one scored row, gets the
// vendor prior (not global, not a line prior). This is the vendor-inheritance
// leg; it inherits and by itself never feeds a donor pool as scored.
func TestVendorOnlyModelInheritsVendorMean(t *testing.T) {
	vend := mkM("globex/core-20260801", "globex/core-20260801", 500, 300_000, true, false, 1)
	only := mkM("globex/lite-20261010", "globex/lite-20261010", 5000, 300_000, true, false, 1)
	cat := mkCat(vend, only)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/globex/core-20260801": 0.64},
		map[string]float64{"quality/worker/globex/core-20260801": 0.0004},
	)
	pe := NewPriorEngine(snap, cat)
	p := pe.PriorFor("worker", "globex/lite-20261010")
	if p.Source != "vendor" {
		t.Fatalf("expected vendor prior, got %s", p.Source)
	}
	if p.Q != 0.64 {
		t.Fatalf("expected Q=0.64 vendor mean, got %v", p.Q)
	}
	// the vendor-only model is NOT scored, so it must not appear in the donor
	// pool of its own vendor mean.
	if _, ok := snap.Parameters[roleKey("worker", only.ID)]; ok {
		t.Fatalf("vendor-only model must not be scored: %v", ok)
	}
}

// ---- alias resolution via ModelCatalog ----

// TestAliasResolvesToCanonicalScore: an alias row (AliasTarget set) with no
// score of its own resolves through the catalog to its canonical target and
// its PriorFor carries the canonical score (reachable through the target).
func TestAliasResolvesToCanonicalScore(t *testing.T) {
	canon := mkM("acme/canon-20260901", "acme/canon-20260901", 1000, 300_000, true, false, 1)
	alias := mkM("acme/alias-row", "acme/alias-row", 1000, 300_000, true, false, 1)
	alias.AliasTarget = canon.ID // canonical row, schema field only
	cat := mkCat(canon, alias)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/acme/canon-20260901": 0.66},
		map[string]float64{"quality/worker/acme/canon-20260901": 0.0004},
	)
	pe := NewPriorEngine(snap, cat)
	// the alias resolves to its target: ResolveTarget follows the chain.
	if rt := cat.ResolveTarget(&alias); rt == nil || rt.ID != canon.ID {
		t.Fatalf("ResolveTarget(alias) must yield canonical %s, got %v", canon.ID, rt)
	}
	// a query by the canonical id is scored, not inherited.
	p := pe.PriorFor("worker", canon.ID)
	if p.Source != "scored" || p.Q != 0.66 {
		t.Fatalf("canonical must be scored, got source=%s q=%v", p.Source, p.Q)
	}
	// the alias-by-id query collapses to the same canonical mean (Q 0.66),
	// because the vendor pool resolves candidate rows to their targets.
	pa := pe.PriorFor("worker", alias.ID)
	if pa.Q != 0.66 {
		t.Fatalf("alias query must reduce to canonical mean 0.66, got %v", pa.Q)
	}
}

// TestAliasBySlugResolution: alias resolution also works when AliasTarget
// names the canonical row by its canonical_slug rather than by id.
func TestAliasBySlugResolution(t *testing.T) {
	canon := mkM("acme/canon2-20260901", "acme/canon2-20260901", 1000, 300_000, true, false, 1)
	alias := mkM("acme/alias-by-slug", "acme/alias-by-slug", 1000, 300_000, true, false, 1)
	alias.AliasTarget = canon.CanonicalSlug
	cat := mkCat(canon, alias)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/acme/canon2-20260901": 0.72},
		map[string]float64{"quality/worker/acme/canon2-20260901": 0.0004},
	)
	pe := NewPriorEngine(snap, cat)
	if rt := cat.ResolveTarget(&alias); rt == nil || rt.ID != canon.ID {
		t.Fatalf("slug alias must resolve to canonical %s, got %v", canon.ID, rt)
	}
	pa := pe.PriorFor("worker", alias.ID)
	if pa.Q != 0.72 {
		t.Fatalf("slug-alias query must reduce to canonical mean 0.72, got %v", pa.Q)
	}
}
