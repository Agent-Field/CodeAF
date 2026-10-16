package config

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
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

// THE APP KEEPS A CONNECTION WITHOUT ITS KEY. Proving a service writes
// nothing; keeping it writes the row with no key, even when handed one; a row
// the terminal already wrote with its own key is rewritten beside it, and
// over it, without losing that key; Codex's sign-in is kept with no key in its
// row, and its marker is no exception: like any key the profile does not
// already hold, it is refused.
func TestAppKeepsAConnectionWithoutItsKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "")
	stub := sourcestub.New("llama-local")
	defer stub.Close()
	if err := WriteSources(dir, []PersistedSource{{ID: "deepseek", Written: "deepseek", Key: "sk-deepseek-terminal-0000000001", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	restore := UseAppCredentials(NewAppCredentials())
	defer restore()

	ollama := vendoredSource(t, "ollama")
	row := PersistedSource{ID: "ollama", Written: "ollama", Address: stub.URL() + "/"}
	outcome, proved, keep := ProveService(context.Background(), row, ollama, "")
	if !keep || outcome.Kind != modelsource.OutcomeConnected || len(outcome.ModelIDs) != 1 || proved.Listed == nil || !*proved.Listed {
		t.Fatalf("proof = %+v, %+v, %v", outcome, proved, keep)
	}
	if rows := PersistedSources(dir); len(rows) != 1 {
		t.Fatalf("a proof wrote %+v", rows)
	}
	proved.Key = "sk-ollama-app-0000000002"
	if err := KeepAppSource(dir, proved); err != nil {
		t.Fatalf("keeping beside the terminal's own row: %v", err)
	}
	rows := PersistedSources(dir)
	if len(rows) != 2 || rows[1].ID != "ollama" || rows[1].Key != "" || rows[1].Order != 2 || rows[1].Address != stub.URL() {
		t.Fatalf("kept rows = %+v", rows)
	}
	if rows[0].Key != "sk-deepseek-terminal-0000000001" {
		t.Fatal("the terminal's own row lost its key")
	}
	// Ollama answers at the address it was kept at, not only at its usual one.
	if service, ok := ResolveSources(dir, "", DefaultBaseURL).ByID("ollama"); !ok || service.Address != stub.URL() {
		t.Fatalf("ollama resolves to %+v", service)
	}
	// A hosted vendor's address is never the person's to move.
	if err := KeepAppSource(dir, PersistedSource{ID: "deepseek", Written: "deepseek", Address: "https://elsewhere.example/v1"}); err != nil {
		t.Fatal(err)
	}
	if service, _ := ResolveSources(dir, "", DefaultBaseURL).ByID("deepseek"); service.Address != "https://api.deepseek.com/v1" {
		t.Fatalf("deepseek moved to %q", service.Address)
	}
	if rows := PersistedSources(dir); rows[0].Key != "sk-deepseek-terminal-0000000001" {
		t.Fatal("keeping the app's row took the terminal's key away")
	}
	if err := persistConnectedSource(dir, PersistedSource{ID: "codex", Written: "codex"}); err != nil {
		t.Fatalf("the keyless sign-in row was refused: %v", err)
	}
	if err := persistConnectedSource(dir, PersistedSource{ID: "codex", Written: "codex", Key: codexauth.Sentinel}); !errors.Is(err, ErrAppKeys) {
		t.Fatalf("the sign-in marker the profile does not hold was answered %v", err)
	}
	if err := persistConnectedSource(dir, PersistedSource{ID: "minimax", Written: "minimax", Key: "sk-minimax-app-0000000003"}); !errors.Is(err, ErrAppKeys) {
		t.Fatalf("a new key was answered %v", err)
	}
	raw, _ := os.ReadFile(BudgetConfigPath(dir))
	if strings.Contains(string(raw), "app-0") {
		t.Fatalf("an app key reached the profile: %s", raw)
	}
}
