package config

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/env"
)

// C7 and C8: Load and every key door agree at all rungs, including alternate
// OpenRouter URL paths and the legacy base-URL spelling.
func TestKeyLadderContractAtEveryEndpoint(t *testing.T) {
	for _, base := range []string{"", DefaultBaseURL, "https://OPENROUTER.AI/another/path/", "https://custom.invalid/v1"} {
		for _, row := range []struct{ name, router, profile, openai, wantDefault, wantCustom string }{
			{"router wins", "sk-or-v1-test-router", "sk-or-v1-test-profile", "sk-proj-test-openai", "sk-or-v1-test-router", "sk-or-v1-test-router"},
			{"profile over usable fallback", "", "sk-or-v1-test-profile", "sk-or-v1-test-fallback", "sk-or-v1-test-profile", "sk-or-v1-test-fallback"},
			{"profile over unrelated key", "", "sk-or-v1-test-profile", "sk-proj-test-openai", "sk-or-v1-test-profile", "sk-proj-test-openai"},
			{"usable fallback", "", "", "sk-or-v1-test-fallback", "sk-or-v1-test-fallback", "sk-or-v1-test-fallback"},
			{"project key alone", "", "", "sk-proj-test-openai", "", "sk-proj-test-openai"},
			{"OpenAI key alone", "", "", "sk-test-openai", "", "sk-test-openai"},
			{"nothing", "", "", "", "", ""},
			{"whitespace", " \t", "", " sk-or-v1-test-fallback \t", "sk-or-v1-test-fallback", "sk-or-v1-test-fallback"},
		} {
			t.Run(base+"/"+row.name, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv(ProfileDirEnv, dir)
				t.Setenv("CODEAF_BASE_URL", base)
				t.Setenv(env.Legacy("CODEAF_BASE_URL"), "")
				t.Setenv(APIKeyEnv, row.router)
				t.Setenv("OPENAI_API_KEY", row.openai)
				if row.profile != "" {
					if err := WriteAPIKey(dir, row.profile); err != nil {
						t.Fatal(err)
					}
				}
				want := row.wantDefault
				if base == "https://custom.invalid/v1" {
					want = row.wantCustom
				}
				if got := APIKeyAt(dir); got != want {
					t.Fatalf("APIKeyAt = %q, want %q", got, want)
				}
				values, _ := readProfileConfig(dir)
				if got := apiKeyFrom(values); got != want {
					t.Fatalf("snapshot key = %q, want %q", got, want)
				}
				settings, err := Load()
				if want == "" {
					if err == nil {
						t.Fatal("a keyless machine passed Load")
					}
				} else if err != nil || settings.APIKey != want {
					t.Fatalf("Load key = %q, err %v, want %q", settings.APIKey, err, want)
				}
			})
		}
	}
	t.Run("legacy custom endpoint", func(t *testing.T) {
		t.Setenv("CODEAF_BASE_URL", "")
		t.Setenv(env.Legacy("CODEAF_BASE_URL"), "https://custom.invalid/v1")
		t.Setenv(APIKeyEnv, "")
		t.Setenv("OPENAI_API_KEY", "sk-proj-test-legacy")
		if got := APIKeyAt(t.TempDir()); got != "sk-proj-test-legacy" {
			t.Fatalf("legacy endpoint key = %q", got)
		}
	})
}

// C11: Persistence accepts only an environment rung the ladder could use,
// writes it once, and leaves no file behind for an unusable fallback.
func TestPersistenceContractUsesOnlyUsableEnvironmentKeys(t *testing.T) {
	for _, row := range []struct{ name, base, router, openai, want string }{
		{"ignored project key", "", "", "sk-proj-test-ignored", ""},
		{"ignored OpenAI key", "", "", "sk-test-ignored", ""},
		{"usable fallback", "", "", "sk-or-v1-test-fallback", "sk-or-v1-test-fallback"},
		{"router wins", "", "sk-or-v1-test-router", "sk-proj-test-ignored", "sk-or-v1-test-router"},
		{"custom fallback", "https://custom.invalid/v1", "", "sk-proj-test-custom", "sk-proj-test-custom"},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CODEAF_BASE_URL", row.base)
			t.Setenv(env.Legacy("CODEAF_BASE_URL"), "")
			t.Setenv(APIKeyEnv, row.router)
			t.Setenv("OPENAI_API_KEY", row.openai)
			copied, path, err := EnsurePersistedAPIKey(dir)
			if err != nil || copied != (row.want != "") || PersistedAPIKey(dir) != row.want {
				t.Fatalf("persisted %v, key %q, err %v", copied, PersistedAPIKey(dir), err)
			}
			if row.want == "" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("unusable key created a file: %v", err)
				}
				return
			}
			if err := WriteAPIKey(dir, "sk-or-v1-test-saved"); err != nil {
				t.Fatal(err)
			}
			if copied, _, err := EnsurePersistedAPIKey(dir); copied || err != nil || PersistedAPIKey(dir) != "sk-or-v1-test-saved" {
				t.Fatalf("overwrote saved key: %v, %v", copied, err)
			}
		})
	}
}

// C10: An ignored OpenAI variable is still a credential for exact redaction.
func TestIgnoredOpenAIKeyIsStillRedacted(t *testing.T) {
	t.Setenv("CODEAF_BASE_URL", "")
	t.Setenv(env.Legacy("CODEAF_BASE_URL"), "")
	t.Setenv(APIKeyEnv, "")
	const secret = "sk-proj-test-redact-this-forgotten-key"
	t.Setenv("OPENAI_API_KEY", secret)
	dir := t.TempDir()
	if APIKeyAt(dir) != "" {
		t.Fatal("unusable key answered the door")
	}
	if !contains(Credentials(dir), secret) {
		t.Fatal("ignored key absent from redaction")
	}
}

// C12 and F9: The short settings hint states the OpenRouter ladder and when a
// saved change reaches a conversation, without exposing engine plumbing.
func TestAPIKeyHintExplainsTheNewLadder(t *testing.T) {
	row, ok := NewSettings(SettingsOptions{ProfileDir: t.TempDir()}).Row(KeyAPIKey)
	if !ok {
		t.Fatal("missing key row")
	}
	for _, needle := range []string{"OPENROUTER_API_KEY in the shell outranks this row, and this row outranks OPENAI_API_KEY", "the next time you open it or switch models"} {
		if !strings.Contains(row.Hint, needle) {
			t.Errorf("hint missing %q: %s", needle, row.Hint)
		}
	}
	if strings.Count(row.Hint, ".") > 3 || strings.Contains(row.Hint, "--no-host") || strings.Contains(row.Hint, "workspace stop command") {
		t.Fatalf("hint is not three short sentences in a person's words: %s", row.Hint)
	}
}

// C7, C11 and F4: OpenRouter's DNS aliases and scheme-free addresses retain
// the default key policy for resolution, source attribution and persistence.
func TestOpenRouterAddressVariantsKeepTheDefaultKeyPolicy(t *testing.T) {
	for _, base := range []string{
		"https://openrouter.ai./api/v1", "https://OPENROUTER.AI./other/",
		"https://www.openrouter.ai", "http://WWW.OPENROUTER.AI./any/path/",
		"openrouter.ai/api/v1", "OPENROUTER.AI./other/",
		"www.openrouter.ai/api/v1/", "WWW.OPENROUTER.AI./",
	} {
		t.Run(base, func(t *testing.T) {
			t.Setenv("CODEAF_BASE_URL", base)
			t.Setenv(APIKeyEnv, "")
			for _, fallback := range []string{"sk-proj-test-ignored", "sk-or-v1-test-usable"} {
				t.Run(fallback, func(t *testing.T) {
					t.Setenv("OPENAI_API_KEY", fallback)
					dir := t.TempDir()
					if err := WriteAPIKey(dir, "sk-or-v1-test-profile"); err != nil {
						t.Fatal(err)
					}
					if got := APIKeyAt(dir); got != "sk-or-v1-test-profile" {
						t.Fatalf("saved connection was outranked: %q", got)
					}
					if got := APIKeySourceAt(dir); got != APIKeySourceProfile {
						t.Fatalf("source = %q", got)
					}
					empty := t.TempDir()
					want, source := "", ""
					if fallback == "sk-or-v1-test-usable" {
						want, source = fallback, APIKeySourceOpenAI
					}
					if got := APIKeyAt(empty); got != want || APIKeySourceAt(empty) != source {
						t.Fatalf("fallback key %q, source %q; want %q, %q", got, APIKeySourceAt(empty), want, source)
					}
					copied, _, err := EnsurePersistedAPIKey(empty)
					if err != nil || copied != (want != "") || PersistedAPIKey(empty) != want {
						t.Fatalf("persistence = %v, %q, %v", copied, PersistedAPIKey(empty), err)
					}
				})
			}
		})
	}
	// Similar-looking hosts remain custom endpoints and accept their own key.
	for _, base := range []string{"https://openrouter.ai.example/v1", "https://example/openrouter.ai", "https://api.openrouter.ai/v1", "https://%invalid"} {
		t.Run(base, func(t *testing.T) {
			t.Setenv("CODEAF_BASE_URL", base)
			t.Setenv(APIKeyEnv, "")
			t.Setenv("OPENAI_API_KEY", "sk-proj-test-custom")
			if got := APIKeyAt(t.TempDir()); got != "sk-proj-test-custom" {
				t.Fatalf("custom endpoint rejected its compatibility key: %q", got)
			}
		})
	}
}
