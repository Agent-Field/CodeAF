package env

import (
	"os"
	"strings"
	"testing"
)

// H3: the static door prefers a non-empty CODEAF_ value, falls through an
// empty new spelling to AFORGE_, and reports absence like os.LookupEnv. // legacy-name
func TestH3StaticDoorPrefersNewAndFallsBackToLegacy(t *testing.T) {
	const name = "CODEAF_H3_STATIC"
	legacy := Legacy(name)
	t.Setenv(name, "new")
	t.Setenv(legacy, "old")
	if got := Get(name); got != "new" {
		t.Fatalf("new spelling = %q, want new", got)
	}
	t.Setenv(name, "")
	if got := Get(name); got != "old" {
		t.Fatalf("empty new spelling fell through to %q, want old", got)
	}
	t.Setenv(legacy, "")
	if got, ok := Lookup(name); got != "" || !ok {
		t.Fatalf("set-empty legacy lookup = (%q, %v), want (empty, true)", got, ok)
	}
	if err := os.Unsetenv(legacy); err != nil {
		t.Fatal(err)
	}
	if got, ok := Lookup(name); got != "" || !ok {
		t.Fatalf("set-empty new lookup = (%q, %v), want (empty, true)", got, ok)
	}
	t.Setenv(name, "")
	t.Setenv(legacy, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv(legacy); err != nil {
		t.Fatal(err)
	}
	if got, ok := Lookup(name); got != "" || ok {
		t.Fatalf("absent lookup = (%q, %v), want (empty, false)", got, ok)
	}
}

// H3: a foreign name at the static door is a programmer error.
func TestH3StaticDoorPanicsForForeignNames(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Get accepted a foreign variable")
		}
	}()
	Get("OPENROUTER_API_KEY")
}

// H3: the dynamic door gives owned names the same fallback and reads foreign
// names plainly, without a panic or an invented fallback.
func TestH3DynamicDoorFallsBackOnlyForOwnedNames(t *testing.T) {
	const owned = "CODEAF_H3_DYNAMIC"
	t.Setenv(owned, "")
	t.Setenv(Legacy(owned), "old")
	if got := Value(owned); got != "old" {
		t.Fatalf("dynamic owned value = %q, want old", got)
	}
	const foreign = "OPENROUTER_API_KEY"
	t.Setenv(foreign, "provider")
	if got := Value(foreign); got != "provider" {
		t.Fatalf("dynamic foreign value = %q, want provider", got)
	}
}

// H3: environment-list filtering removes both spellings of an owned name while
// treating a foreign name as a plain exact name.
func TestH3EnvironWithoutRemovesBothOwnedSpellings(t *testing.T) {
	const owned = "CODEAF_H3_LIST"
	t.Setenv(owned, "new")
	t.Setenv(Legacy(owned), "old")
	t.Setenv("H3_FOREIGN", "plain")
	kept := EnvironWithout(owned, "H3_FOREIGN")
	for _, entry := range kept {
		name, _, _ := strings.Cut(entry, "=")
		if name == owned || name == Legacy(owned) || name == "H3_FOREIGN" {
			t.Fatalf("filtered environment kept %q", entry)
		}
	}
}
