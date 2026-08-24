package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KeyAPIKey is the profile field the provider key lives in. It predates the
// settings registry — the background timer's copy of the environment key has
// always landed here — and the registry's row (settings.go) writes the same
// field, so a key pasted on the first run, one typed into /settings and one
// copied from the shell are one value in one place.
const KeyAPIKey = "api_key"

// APIKeyEnv is the environment variable that outranks the profile's key. It is
// spelled once here because three surfaces name it to a person: the missing-key
// sentence at the door, the settings row's pin, and the first-run page.
const APIKeyEnv = "OPENROUTER_API_KEY"

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
	encoded, ok := values[KeyAPIKey]
	if !ok {
		return ""
	}
	var key string
	if err := json.Unmarshal(encoded, &key); err != nil {
		return ""
	}
	return strings.TrimSpace(key)
}

// APIKeyAt is the key a session opened on this profile would talk with, in
// [Load]'s own order: the OpenRouter variable, the OpenAI one, then the profile
// file. It is the reading the settings row and the first-run setup share, so
// neither can say "no key" while Load would have found one.
func APIKeyAt(profileDir string) string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv(APIKeyEnv), os.Getenv("OPENAI_API_KEY"), PersistedAPIKey(profileDir)))
}

// WriteAPIKey persists a key a person handed over, through the same atomic
// writer every other setting uses. The file is created owner-readable only
// (writeProfileValues), which is the property [EnsurePersistedAPIKey] tightens
// after the fact on a file that predated any secret in it.
func WriteAPIKey(profileDir, key string) error {
	return writeProfileValue(profileDir, KeyAPIKey, strings.TrimSpace(key))
}

// LooksLikeAPIKey is the shape check the first-run setup applies to a pasted
// key, and it is deliberately only a shape check: it spends no network call,
// because the setup runs before a person has agreed to spend anything. An
// OpenRouter key reads `sk-or-v1-…`; an OpenAI-shaped key, which Load also
// accepts from the environment, reads `sk-…`. Whitespace inside is a paste that
// picked up a line break, which is the one thing worth refusing here rather
// than discovering as a 401 on the first turn.
func LooksLikeAPIKey(key string) bool {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, "sk-") || len(key) < 20 {
		return false
	}
	return !strings.ContainsAny(key, " \t\r\n")
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
	key := strings.TrimSpace(firstNonEmpty(os.Getenv(APIKeyEnv), os.Getenv("OPENAI_API_KEY")))
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
	values[KeyAPIKey] = encoded

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
