package subharness

import (
	"strconv"
	"strings"
	"testing"
)

// The catalog is the guide's half of the manifest: what it says is what
// Validate holds. These tests pin the rendering, not the prose.
func TestTheCatalogTeachesEveryKindInDeclarationOrder(t *testing.T) {
	catalog := Catalog()
	at := -1
	for _, name := range kindOrder {
		next := strings.Index(catalog, name)
		if next < 0 {
			t.Fatalf("the catalog never mentions %s", name)
		}
		if next < at {
			t.Fatalf("%s is out of declaration order in the catalog", name)
		}
		at = next
	}
}

func TestTheCatalogRendersTheLawNotProse(t *testing.T) {
	catalog := Catalog()
	for _, want := range []string{
		"REQUIRED",
		"all | any | first",
		"hosted | idle | watch | source.command",
		"1.." + strconv.Itoa(MaxTurns),
		"1.." + strconv.Itoa(MaxRounds) + " (default " + strconv.Itoa(DefaultRounds) + ")",
		"1.." + strconv.Itoa(MaxWidth),
	} {
		if !strings.Contains(catalog, want) {
			t.Errorf("the catalog is missing %q", want)
		}
	}
	if strings.Contains(catalog, "«") {
		t.Error("the catalog still carries an unfilled placeholder")
	}
	for _, line := range strings.Split(strings.TrimRight(catalog, "\n"), "\n") {
		if len(line) > catalogWidth {
			t.Errorf("a catalog line is %d cells, past %d: %q", len(line), catalogWidth, line)
		}
	}
}

// Every field the catalog teaches must be a field the kind accepts, and every
// field a kind accepts must be taught — the one edit moves both because both
// read the same specs.
func TestTheCatalogAndTheValidatorReadTheSameSpecs(t *testing.T) {
	catalog := Catalog()
	for _, name := range kindOrder {
		k := kinds[name]
		for _, s := range k.Specs {
			if !strings.Contains(catalog, s.name) {
				t.Errorf("%s.%s is validated but never taught", name, s.name)
			}
		}
	}
}
