package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistedAPIKeyIsTheLastRungOfLoadResolution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AFORGE_PROFILE_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatal("no key anywhere should still be an error")
	}

	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"api_key": "sk-on-disk"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.APIKey != "sk-on-disk" {
		t.Fatalf("persisted key ignored: %q", settings.APIKey)
	}

	t.Setenv("OPENROUTER_API_KEY", "sk-env")
	settings, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.APIKey != "sk-env" {
		t.Fatalf("environment must outrank the persisted key: %q", settings.APIKey)
	}
}

func TestEnsurePersistedAPIKeyCopiesTheEnvKeyOnceAndTightensTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Pre-existing unrelated config must survive, including its budget key.
	if err := os.WriteFile(path, []byte(`{"daily_budget_usd": 7.5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENROUTER_API_KEY", "sk-copy-me")
	t.Setenv("OPENAI_API_KEY", "")

	persisted, got, err := EnsurePersistedAPIKey(dir)
	if err != nil || !persisted || got != path {
		t.Fatalf("persist = %v %q %v", persisted, got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret-bearing config is %v, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	if string(values["daily_budget_usd"]) != "7.5" {
		t.Fatalf("unrelated config lost: %s", values["daily_budget_usd"])
	}
	if PersistedAPIKey(dir) != "sk-copy-me" {
		t.Fatalf("round trip = %q", PersistedAPIKey(dir))
	}

	// A second call with a different env key must not overwrite the stored one.
	t.Setenv("OPENROUTER_API_KEY", "sk-different")
	persisted, _, err = EnsurePersistedAPIKey(dir)
	if err != nil || persisted {
		t.Fatalf("existing key overwritten: persisted=%v err=%v", persisted, err)
	}
	if PersistedAPIKey(dir) != "sk-copy-me" {
		t.Fatalf("stored key changed to %q", PersistedAPIKey(dir))
	}

	// No env key and nothing stored is a quiet no-op, never an error.
	empty := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "")
	persisted, _, err = EnsurePersistedAPIKey(empty)
	if err != nil || persisted {
		t.Fatalf("no-op case: persisted=%v err=%v", persisted, err)
	}
	if _, statErr := os.Stat(filepath.Join(empty, "config.json")); !os.IsNotExist(statErr) {
		t.Fatal("no-op case must not create the config file")
	}
}
