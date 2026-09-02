package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/trace"
)

// A KEY THE SCRUB'S SHAPES HAVE NEVER SEEN IS STILL NOT IN THE RECORD. The
// three patterns catch `Bearer …`, `sk-…` and a field named `authorization`;
// they catch nothing at all in a Google `AIza…`, a Groq `gsk_…` or the plain
// token a self-hosted endpoint was handed, and a provider that echoes its own
// error body back is how one of those reaches a folder somebody is about to
// attach to a bug report. The door registers the exact values, so the shape
// does not matter.
func TestTheDoorRegistersConfiguredCredentialsWithTheRecord(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("AFORGE_PROFILE_DIR", profile)
	t.Setenv("AFORGE_HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")

	// Neither value looks like a credential to any regex in scrub.go: no
	// scheme, no `sk-` prefix, no field name beside it.
	const key = "AIzaSyD-plain-value-nothing-matches-1"
	const appSecret = "GOCSPX-another-plain-value-22"
	encoded, err := json.Marshal(map[string]string{
		config.KeyAPIKey:            key,
		config.KeyGoogleOAuthSecret: appSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(profile), encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	openDebugRecord("chat", "deepseek/deepseek-v4-flash", t.TempDir())

	// The body a provider might echo back, with both values in it.
	body := []byte(`{"error":{"message":"invalid key ` + key + ` for app ` + appSecret + `"}}`)
	got := string(trace.Scrub(body))
	for _, secret := range []string{key, appSecret} {
		if strings.Contains(got, secret) {
			t.Fatalf("a configured credential survived the scrub: %s", got)
		}
	}
	if strings.Count(got, "[redacted]") != 2 {
		t.Fatalf("the scrub did not redact both credentials: %s", got)
	}
}
