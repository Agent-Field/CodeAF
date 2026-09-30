package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistedAPIKeyIsTheLastRungOfLoadResolution(t *testing.T) {
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
