package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// EVERY ROW THE REGISTRY MARKS AS A CREDENTIAL IS ONE THE RECORD KNOWS TO
// REDACT. This is the test that keeps the two together: a credential row added
// later has to appear here without anybody editing this file, and if [Credentials]
// ever grew a hand-kept list instead, this is where the drift would show.
func TestCredentialsFindsEverySecretRowInTheRegistry(t *testing.T) {
	dir := t.TempDir()
	registry := NewSettings(SettingsOptions{ProfileDir: dir})

	// The environment outranks the profile file on every one of these rows, so
	// a shell that happens to hold a real key must not decide what this test
	// reads. Both spellings of the model key go too ([APIKeyAt]).
	t.Setenv(APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	written := map[string]string{}
	values := map[string]any{}
	for _, row := range registry.Rows() {
		if !row.Secret {
			continue
		}
		if row.Env != "" {
			t.Setenv(row.Env, "")
		}
		// A value that no shape-matching regex could ever find, which is the
		// whole reason the exact values are registered at all.
		value := "configured-" + row.Key + "-9F3B7A2C"
		values[row.Key] = value
		written[row.Key] = value
	}
	if len(written) == 0 {
		t.Fatal("the settings registry marks no row as a credential")
	}
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BudgetConfigPath(dir), encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	found := Credentials(dir)
	for key, value := range written {
		if !contains(found, value) {
			t.Fatalf("Credentials missed the %s row: %v", key, found)
		}
	}
	// And an unconfigured machine registers nothing, so a record on a machine
	// with no keys is exactly what was written.
	if got := Credentials(t.TempDir()); len(got) != 0 {
		t.Fatalf("a profile with no credentials returned %v", got)
	}
}

// The model key resolves from two environment variables and the row names only
// one, so the second spelling is asked for by hand — and this is the test that
// says so.
func TestCredentialsReadsBothSpellingsOfTheModelKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "a-plain-token-nothing-would-match")
	if got := Credentials(dir); !contains(got, "a-plain-token-nothing-would-match") {
		t.Fatalf("Credentials missed OPENAI_API_KEY: %v", got)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}
