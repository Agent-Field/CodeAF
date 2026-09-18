package index

import "testing"

// The seed this build carries is a document a reader accepts: it parses,
// declares the pool's one learned metric — acceptable, bernoulli — and
// carries at least one measurement for each of the three seats the picker
// resolves, every one saying which source graded it, so a machine with no
// cache has numbers on its first run rather than an empty index.
func TestSeedCarriesAcceptableForEverySeat(t *testing.T) {
	seed, err := SeedIndex()
	if err != nil {
		t.Fatalf("the embedded seed does not parse: %v", err)
	}
	if kind, ok := seed.Kind("acceptable"); !ok || kind != "bernoulli" {
		t.Fatalf("acceptable: kind %q, declared %v", kind, ok)
	}
	if cells := seed.Cells("role_quality"); len(cells) != 0 {
		t.Fatalf("the seed still carries %d role_quality cells; the judge's opinion is not the seed's scoreboard", len(cells))
	}
	byRole := map[string]int{}
	for _, cell := range seed.Cells("acceptable") {
		byRole[cell.Role]++
		if cell.Dims["source"] == "" {
			t.Errorf("seed cell %s/%s names no source", cell.Role, cell.Model)
		}
	}
	for _, role := range []string{"worker", "high", "mastermind"} {
		if byRole[role] == 0 {
			t.Errorf("the seed carries no acceptable cell for %q", role)
		}
	}
}

// Seed hands back a copy: the document is shared by many readers, and one that
// mutated the slice it was given would change what every later call returns.
func TestSeedHandsBackACopy(t *testing.T) {
	first := Seed()
	if len(first) == 0 {
		t.Fatal("the seed is empty")
	}
	first[0] ^= 0xff
	if second := Seed(); second[0] == first[0] {
		t.Fatal("mutating a Seed copy changed the next one")
	}
}
