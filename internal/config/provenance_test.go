package config

import (
	"slices"
	"testing"
)

func TestPersistedKeysListsOnlyWhatIsWrittenDown(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)

	if keys := rows.PersistedKeys(); len(keys) != 0 {
		t.Fatalf("a fresh profile has nothing written down, got %v", keys)
	}

	row, ok := rows.Row(KeyDailyBudget)
	if !ok {
		t.Fatalf("registry lost %s", KeyDailyBudget)
	}
	if err := row.Apply("12"); err != nil {
		t.Fatalf("apply daily budget: %v", err)
	}

	keys := rows.PersistedKeys()
	if !slices.Contains(keys, KeyDailyBudget) {
		t.Fatalf("PersistedKeys() = %v, want it to contain %s", keys, KeyDailyBudget)
	}
	if slices.Contains(keys, KeyPlanConsent) {
		t.Fatalf("PersistedKeys() = %v, want an untouched row to stay absent", keys)
	}
}

// The model slots and the chat divider live beside the graph, not in
// config.json. A key of that name appearing in the profile file is somebody
// else's data and must never be read as this row's provenance.
func TestPersistedKeysIgnoresRowsBackedByAnotherStore(t *testing.T) {
	dir := t.TempDir()
	if err := writeProfileValue(dir, KeySplitPct, 60); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	for _, key := range registry(t, dir).PersistedKeys() {
		if key == KeySplitPct {
			t.Fatalf("PersistedKeys() claimed %s, which this file does not own", key)
		}
	}
}
