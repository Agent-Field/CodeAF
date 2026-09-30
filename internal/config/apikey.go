package config

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/modelsource"
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
// the last rung of Load's key resolution: a timer-driven `codeaf wake` runs
// with no shell environment, so the profile file is the only place a key can
// survive to reach it.
func PersistedAPIKey(profileDir string) string {
	values, err := readProfileConfig(profileDir)
	if err != nil {
		return ""
	}
	return persistedAPIKeyFrom(values)
}

// persistedAPIKeyFrom is the file-free half of PersistedAPIKey. Load uses it
// with the same profile snapshot that contains model_sources, so resolving the
// default key and the service set is literally one read.
func persistedAPIKeyFrom(values map[string]json.RawMessage) string {
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

func apiKeyFrom(values map[string]json.RawMessage) string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv(APIKeyEnv), os.Getenv("OPENAI_API_KEY"), persistedAPIKeyFrom(values)))
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
	return modelsource.LooksLikeAPIKey(key)
}
