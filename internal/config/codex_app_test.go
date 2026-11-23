package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/codexauth"
)

func TestCodexAccountUsesExistingTokenStoreWithoutAppAPIKeyPersistence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"slug":"test-model","visibility":"list","context_window":10000}]}`))
	}))
	defer server.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", server.URL)
	restore := UseAppCredentials(NewAppCredentials())
	defer restore()
	dir := t.TempDir()
	if _, err := ConnectCodex(t.Context(), dir, codexauth.Tokens{AccessToken: "test-private-access", RefreshToken: "test-private-refresh", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if !codexauth.Connected(dir) {
		t.Fatal("account not durable")
	}
	rows := PersistedSources(dir)
	if len(rows) != 1 || rows[0].ID != "codex" || rows[0].Key != "" {
		t.Fatal("account persisted a generic API key")
	}
	raw, err := os.ReadFile(codexauth.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "test-private-access") {
		t.Fatal("existing token store was not used")
	}
	stat, err := os.Stat(codexauth.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm()&0077 != 0 {
		t.Fatal("account token store is not owner-only")
	}
}
