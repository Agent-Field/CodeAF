package config

import (
	"os"
	"testing"
)

func TestMemoryDefaultsOnAndKeepsAnExplicitChoice(t *testing.T) {
	dir := t.TempDir()
	if enabled, saved := MemorySettingAt(dir); !enabled || saved {
		t.Fatalf("new profile: enabled=%v saved=%v; want enabled without a saved override", enabled, saved)
	}
	for _, want := range []bool{false, true, false} {
		if err := WriteMemoryEnabled(dir, want); err != nil {
			t.Fatal(err)
		}
		if got, saved := MemorySettingAt(dir); got != want || !saved {
			t.Fatalf("saved choice %v: enabled=%v saved=%v", want, got, saved)
		}
	}
}

func TestMemorySavedValuesAndInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		name, raw      string
		enabled, saved bool
	}{
		{"absent", `{}`, true, false},
		{"on", `{"memory.enabled":"on"}`, true, true},
		{"off", `{"memory.enabled":"off"}`, false, true},
		{"true", `{"memory.enabled":true}`, true, true},
		{"false", `{"memory.enabled":false}`, false, true},
		{"invalid word", `{"memory.enabled":"invalid"}`, false, false},
		{"invalid number", `{"memory.enabled":1}`, false, false},
		{"malformed profile", `{`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(BudgetConfigPath(dir), []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			if got, saved := MemorySettingAt(dir); got != tc.enabled || saved != tc.saved {
				t.Fatalf("got (%v,%v), want (%v,%v)", got, saved, tc.enabled, tc.saved)
			}
			if got := MemoryEnabledAt(dir); got != tc.enabled {
				t.Fatalf("enabled=%v, want %v", got, tc.enabled)
			}
		})
	}
}
