package config

import "testing"

// A service's inline key travels by id and lands only on a row this profile
// has, without touching the row's other fields; a row that names a variable
// holds no inline key and is never given one.
func TestProviderKeysWriteOnlyTheKeyOfAKnownService(t *testing.T) {
	dir := t.TempDir()
	rows := []PersistedSource{
		{ID: "acme", Written: "acme", Region: "us", Order: 1},
		{ID: "envy", Written: "envy", Region: "us", KeyEnv: "ENVY_KEY", Order: 2},
	}
	if err := WriteSources(dir, rows); err != nil {
		t.Fatal(err)
	}
	want := ProviderKeys{Sources: map[string]string{"acme": "FAKE-acme", "envy": "FAKE-envy", "gone": "FAKE-gone"}}
	if err := WriteProviderKeys(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadProviderKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 1 || got.Sources["acme"] != "FAKE-acme" {
		t.Fatalf("only the known inline-key service may hold a key, got %d entries", len(got.Sources))
	}
	if held := PersistedSources(dir); held[0].Region != "us" || held[1].KeyEnv != "ENVY_KEY" || held[1].Key != "" {
		t.Fatal("a service row's other fields changed")
	}
	if ProviderKeysHeldBy(dir, want).Sources["gone"] != "" {
		t.Fatal("a service this profile lacks is reported held")
	}
}
