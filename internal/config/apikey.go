package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
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
// the durable rung of Load's key resolution: a key pasted into the first-run
// setup is kept there, where a process that never saw the shell environment
// that set a variable can still find one.
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
	key, _ := apiKeyResolution(values)
	return key
}

// The source names are shared with doctor so its row names the same rung as
// an authentication refusal, without revealing a credential.
const (
	APIKeySourceOpenRouter = "the shell's OPENROUTER_API_KEY"
	APIKeySourceOpenAI     = "the shell's OPENAI_API_KEY"
	APIKeySourceProfile    = "the key saved in your profile"
)

// usableOpenAIKey keeps an unrelated OpenAI credential off OpenRouter. A custom
// endpoint still accepts the compatible key its operator supplied.
func usableOpenAIKey(base string) string {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if !defaultKeyEndpoint(base) || strings.HasPrefix(key, openRouterKeyPrefix) {
		return key
	}
	return ""
}

const openRouterKeyPrefix = "sk-or-"

// defaultKeyEndpoint compares hosts so a trailing slash or a different API path
// cannot turn OpenRouter into a custom endpoint and admit an unrelated key.
func defaultKeyEndpoint(base string) bool {
	if strings.TrimSpace(base) == "" {
		return true
	}
	base = strings.TrimSpace(base)
	if !strings.Contains(base, "://") {
		base = "https://" + strings.TrimPrefix(base, "//")
	}
	address, err := url.Parse(base)
	if err != nil {
		return false
	}
	defaultAddress, _ := url.Parse(DefaultBaseURL)
	host := strings.TrimSuffix(strings.ToLower(address.Hostname()), ".")
	host = strings.TrimPrefix(host, "www.")
	return strings.EqualFold(host, defaultAddress.Hostname())
}

// apiKeyResolution is the one ladder shared by the key and its explanation.
// Keeping the source beside the value prevents an auth message from naming a
// different rung than the client actually used. On OpenRouter a connected key
// outranks the compatibility variable; custom endpoints retain their old order.
func apiKeyResolution(values map[string]json.RawMessage) (string, string) {
	base := env.Get("CODEAF_BASE_URL")
	profile, fallback := persistedAPIKeyFrom(values), usableOpenAIKey(base)
	candidates := []struct{ value, source string }{
		{strings.TrimSpace(os.Getenv(APIKeyEnv)), APIKeySourceOpenRouter},
		{profile, APIKeySourceProfile},
		{fallback, APIKeySourceOpenAI},
	}
	if !defaultKeyEndpoint(base) {
		candidates[1], candidates[2] = candidates[2], candidates[1]
	}
	for _, candidate := range candidates {
		if candidate.value != "" {
			return candidate.value, candidate.source
		}
	}
	return "", ""
}

// APIKeyAt is the key a session opened on this profile would talk with, in
// [Load]'s own order: the OpenRouter variable, the profile, then an OpenRouter
// key in the OpenAI variable. Custom endpoints put the OpenAI variable second.
// It is the reading the settings row and the first-run setup share, so
// neither can say "no key" while Load would have found one.
func APIKeyAt(profileDir string) string {
	values, _ := readProfileConfig(profileDir)
	key, _ := apiKeyResolution(values)
	return key
}

// APIKeySourceAt names the rung APIKeyAt resolved without exposing the key.
// An empty result means no key was found.
func APIKeySourceAt(profileDir string) string {
	values, _ := readProfileConfig(profileDir)
	_, source := apiKeyResolution(values)
	return source
}

// APIKeySourceForModel explains the default provider's credential only when
// that provider actually serves the model. Connected providers have their own
// key ladders, so borrowing the default explanation would name another key.
func APIKeySourceForModel(profileDir string, sources modelsource.Set, model string) string {
	service, _ := sources.For(model)
	if !strings.EqualFold(service.Source.ID, modelsource.DefaultID) {
		return ""
	}
	return APIKeySourceAt(profileDir)
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
// accepts for custom endpoints, reads `sk-…`. Whitespace inside is a paste that
// picked up a line break, which is the one thing worth refusing here rather
// than discovering as a 401 on the first turn.
func LooksLikeAPIKey(key string) bool {
	return modelsource.LooksLikeAPIKey(key)
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
	key, _ := apiKeyResolution(nil)
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
