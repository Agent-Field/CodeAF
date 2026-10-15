package config

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// THE APP'S KEYS ARE MEMORY ONLY. Installed, they outrank the profile and the
// shell for every reader; a removal is not filled back in from either; the
// profile writer refuses a secret while they are installed and writes every
// other setting; and a key handed over is covered by the redactor.
func TestAppCredentialsOutrankTheProfileAndNeverReachIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	if err := writeProfileValue(dir, KeyAPIKey, "sk-or-profile-0000000001"); err != nil {
		t.Fatal(err)
	}
	creds := NewAppCredentials()
	restore := UseAppCredentials(creds)
	defer restore()
	if !AppMode() {
		t.Fatal("installed keys are not the app's mode")
	}
	if got := APIKeyAt(dir); got != "sk-or-profile-0000000001" {
		t.Fatalf("with no app key the profile's is %q", got)
	}
	creds.Set("OpenRouter", "opaque-app-token-a91f")
	if got := APIKeyAt(dir); got != "opaque-app-token-a91f" {
		t.Fatalf("the app's key lost to %q", got)
	}
	if said := redact.Secrets("token opaque-app-token-a91f here"); strings.Contains(said, "opaque-app-token") {
		t.Fatalf("the redactor let the key through: %q", said)
	}
	creds.Set(modelsource.DefaultID, "sk-or-app-0000000002")
	creds.Set("deepseek", "sk-deepseek-app-0000000003")
	sources := ResolveSources(dir, APIKeyAt(dir), DefaultBaseURL)
	if service, ok := sources.ByID("deepseek"); !ok || service.Key != "sk-deepseek-app-0000000003" {
		t.Fatalf("deepseek = %+v, %v", service, ok)
	}
	creds.Remove("openrouter")
	if got := APIKeyAt(dir); got != "" {
		t.Fatalf("a removed key was filled back in with %q", got)
	}
	if err := writeProfileValue(dir, KeyAPIKey, "sk-or-written-0000000004"); !errors.Is(err, ErrAppKeys) {
		t.Fatalf("a key write was answered %v", err)
	}
	if err := writeProfileValue(dir, KeyToolApprovalMode, "deny"); err != nil {
		t.Fatalf("an ordinary setting was refused: %v", err)
	}
	raw, _ := os.ReadFile(BudgetConfigPath(dir))
	if strings.Contains(string(raw), "app-0") || strings.Contains(string(raw), "written") {
		t.Fatalf("an app key reached the profile: %s", raw)
	}
	restore()
	if AppMode() {
		t.Fatal("the app's mode outlived its keys")
	}
}
