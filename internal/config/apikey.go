package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PersistedAPIKey reads the api_key stored in the profile config file. It is
// the last rung of Load's key resolution: a timer-driven `aforge wake` runs
// with no shell environment, so the profile file is the only place a key can
// survive to reach it.
func PersistedAPIKey(profileDir string) string {
	raw, err := os.ReadFile(BudgetConfigPath(profileDir))
	if err != nil {
		return ""
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return ""
	}
	encoded, ok := values["api_key"]
	if !ok {
		return ""
	}
	var key string
	if err := json.Unmarshal(encoded, &key); err != nil {
		return ""
	}
	return strings.TrimSpace(key)
}

// EnsurePersistedAPIKey copies the session's environment key into the profile
// config exactly once, so the standing watch can authenticate after the shell
// that ratified it is gone. It never overwrites a key already on disk, and the
// file ends owner-readable only. Returns whether a key was newly persisted and
// the path that holds it.
func EnsurePersistedAPIKey(profileDir string) (bool, string, error) {
	path := BudgetConfigPath(profileDir)
	if PersistedAPIKey(profileDir) != "" {
		return false, path, nil
	}
	key := strings.TrimSpace(firstNonEmpty(os.Getenv("OPENROUTER_API_KEY"), os.Getenv("OPENAI_API_KEY")))
	if key == "" {
		return false, path, nil
	}

	values := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &values); err != nil {
			return false, path, fmt.Errorf("persist api key: existing config is not valid JSON: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return false, path, fmt.Errorf("persist api key: %w", err)
	}
	encoded, err := json.Marshal(key)
	if err != nil {
		return false, path, fmt.Errorf("persist api key: %w", err)
	}
	values["api_key"] = encoded

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, path, fmt.Errorf("persist api key: %w", err)
	}
	body, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return false, path, fmt.Errorf("persist api key: %w", err)
	}
	temporaryPath := path + ".tmp"
	if err := os.WriteFile(temporaryPath, append(body, '\n'), 0o600); err != nil {
		return false, path, fmt.Errorf("persist api key: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return false, path, fmt.Errorf("persist api key: %w", err)
	}
	// The file now carries a secret; tighten it even if it predated the key.
	if err := os.Chmod(path, 0o600); err != nil {
		return true, path, fmt.Errorf("persist api key: chmod: %w", err)
	}
	return true, path, nil
}
