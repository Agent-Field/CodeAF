package selector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A CATALOG BUILT IN CODE CARRIES THE SAME INDEX AS ONE READ FROM DISK:
// alias resolution and the line/vendor prior inheritance read that index,
// and a catalog without it seats every unscored model on the global prior —
// the arm-collapse the chain exists to prevent. The wire builds its catalog
// in code (config/selectorwire.go), so the constructor is the contract.
func TestNewModelCatalogIndexesForAliasAndLine(t *testing.T) {
	cat := NewModelCatalog([]Model{
		{ID: "vendor/new-model", CanonicalSlug: "vendor/new-model-20261001"},
		{ID: "vendor/new-model-alias", CanonicalSlug: "vendor/new-model-latest", AliasTarget: "vendor/new-model"},
	})
	alias := cat.Models[1]
	resolved, ok := cat.Resolve(&alias)
	if !ok || resolved.ID != "vendor/new-model" {
		t.Fatalf("the alias resolved to %q (ok=%v), want vendor/new-model", resolved.ID, ok)
	}
}

// THE CATALOG PRIOR KEYS BY ROLE — the string PriorFor is actually called
// with. Keyed by class id instead, the checker seat's benchmark prior
// silently missed and every unscored checker candidate fell to inheritance.
func TestCatalogPriorAnswersForEveryRole(t *testing.T) {
	cat := NewModelCatalog([]Model{{
		ID: "vendor/bench", CanonicalSlug: "vendor/bench-20261001",
		IntelligenceIndex: 60, CodingIndex: 60, AgenticIndex: 60,
	}})
	snap := &SharedSnapshot{
		Parameters:  map[string]float64{},
		Uncertainty: map[string]float64{},
		Counts:      map[string]int{},
	}
	pe := NewPriorEngine(snap, cat)
	for _, role := range []string{"worker", "checker", "thinker"} {
		if p := pe.PriorFor(role, "vendor/bench"); p.Source != "catalog" {
			t.Errorf("%s: prior source %q, want catalog — the seat's benchmark prior must answer", role, p.Source)
		}
	}
}

// A SNAPSHOT THE GATE CANNOT READ IS REFUSED AT LOAD: a negative variance
// square-roots to NaN and every gate comparison against it is arbitrary, so
// the load is where it stops. (NaN/Inf cannot arrive through encoding/json;
// negative values can.)
func TestLoadSnapshotRejectsNumbersTheGateCannotRead(t *testing.T) {
	write := func(t *testing.T, s SharedSnapshot) string {
		t.Helper()
		body, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "snap.json")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := SharedSnapshot{
		Parameters:  map[string]float64{"quality/worker/m": 0.7},
		Uncertainty: map[string]float64{"quality/worker/m": 0.01},
		Counts:      map[string]int{"quality/worker/m": 3},
	}
	if _, err := LoadSnapshot(write(t, good)); err != nil {
		t.Fatalf("a sound snapshot was refused: %v", err)
	}
	bad := good
	bad.Uncertainty = map[string]float64{"quality/worker/m": -0.5}
	if _, err := LoadSnapshot(write(t, bad)); err == nil {
		t.Fatal("a negative variance loaded: the gate would square-root it to NaN")
	}
	bad = good
	bad.Counts = map[string]int{"quality/worker/m": -2}
	if _, err := LoadSnapshot(write(t, bad)); err == nil {
		t.Fatal("a negative count loaded")
	}
}
