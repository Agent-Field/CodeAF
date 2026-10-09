package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// C7: A saved connection works without a shell key, and the OpenRouter variable wins.
func TestPersistedAPIKeyIsTheDurableRungOfLoadResolution(t *testing.T) {
	t.Setenv("CODEAF_BASE_URL", "")
	dir := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", dir)
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

// C7: The explanation follows the new ladder without exposing the credential.
func TestAPIKeySourceMatchesAPIKeyAtWithoutExposingTheKey(t *testing.T) {
	t.Setenv("CODEAF_BASE_URL", "")
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "sk-profile-source-123"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name   string
		router string
		openai string
		want   string
		source string
	}{
		{"shell OpenRouter", "sk-router-source-123", "sk-openai-source-123", "sk-router-source-123", "the shell's OPENROUTER_API_KEY"},
		{"profile over shell OpenAI", "", "sk-or-v1-test-fallback", "sk-profile-source-123", "the key saved in your profile"},
		{"profile", "", "", "sk-profile-source-123", "the key saved in your profile"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv(APIKeyEnv, row.router)
			t.Setenv("OPENAI_API_KEY", row.openai)
			if got := APIKeyAt(dir); got != row.want {
				t.Fatalf("APIKeyAt = %q, want %q", got, row.want)
			}
			if got := APIKeySourceAt(dir); got != row.source {
				t.Fatalf("APIKeySourceAt = %q, want %q", got, row.source)
			}
			if strings.Contains(row.source, row.want) {
				t.Fatalf("source %q exposes the key", row.source)
			}
		})
	}
}

// C11: Environment persistence preserves existing keys and unrelated settings.
func TestEnsurePersistedAPIKeyCopiesTheEnvKeyOnceAndTightensTheFile(t *testing.T) {
	t.Setenv("CODEAF_BASE_URL", "")
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
