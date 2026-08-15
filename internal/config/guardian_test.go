package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The guardian row is off until somebody turns it on, reads back what they
// wrote, and refuses a word nobody defined.
func TestGuardianDefaultsOffAndPersists(t *testing.T) {
	dir := t.TempDir()

	if mode := GuardianAt(dir); mode != GuardianOff {
		t.Fatalf("a fresh profile reads %q, want %q", mode, GuardianOff)
	}
	if GuardianEnabledAt(dir) {
		t.Fatal("the guardian is on in a profile nobody has written")
	}

	row, ok := rowFor(registry(t, dir), KeyGuardian)
	if !ok {
		t.Fatalf("the sheet has no %q row", KeyGuardian)
	}
	if err := row.Apply(GuardianOn); err != nil {
		t.Fatalf("Apply(on): %v", err)
	}
	if !GuardianEnabledAt(dir) {
		t.Fatal("the row was set to on and the accessor still reads off")
	}
	if value := row.Value(); value != GuardianOn {
		t.Fatalf("the row reads %q after being set to on", value)
	}

	if err := row.Apply("sometimes"); err == nil {
		t.Fatal("the row accepted a word that is not one of its two")
	}

	// A garbled value on disk reads as off: a setting nobody can read must never
	// be the one that appoints a stand-in.
	writeGuardianRaw(t, dir, 3)
	if GuardianEnabledAt(dir) {
		t.Fatal("an unreadable value turned the guardian on")
	}
}

func rowFor(rows *Settings, key string) (Setting, bool) {
	for _, row := range rows.Rows() {
		if row.Key == key {
			return row, true
		}
	}
	return Setting{}, false
}

// writeGuardianRaw puts a value of the wrong shape under the row's key.
func writeGuardianRaw(t *testing.T, dir string, value any) {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	contents := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &contents)
	}
	contents[KeyGuardian] = value
	raw, err := json.Marshal(contents)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
