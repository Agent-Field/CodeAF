package index

import "testing"

// The seed this build carries is a document a reader accepts: it parses,
// declares the pool's quality metric as gaussian, and carries at least one
// measurement for each of the three seats the picker resolves — so a machine
// with no cache has numbers on its first run rather than an empty index.
func TestSeedCarriesRoleQualityForEverySeat(t *testing.T) {
	seed, err := SeedIndex()
	if err != nil {
		t.Fatalf("the embedded seed does not parse: %v", err)
	}
	if kind, ok := seed.Kind("role_quality"); !ok || kind != "gaussian" {
		t.Fatalf("role_quality: kind %q, declared %v", kind, ok)
	}
	byRole := map[string]int{}
	for _, cell := range seed.Cells("role_quality") {
		byRole[cell.Role]++
	}
	for _, role := range []string{"worker", "high", "mastermind"} {
		if byRole[role] == 0 {
			t.Errorf("the seed carries no role_quality cell for %q", role)
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
