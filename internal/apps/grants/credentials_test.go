package grants

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/config"
	accounts "github.com/Agent-Field/codeaf/internal/connect"
)

func TestLocalPersonalStore(t *testing.T) {
	for _, tt := range []struct{ name, entry, service, kind, token, status string }{
		{name: "refresh", entry: `{"keys":{"access_token":"access","refresh_token":"refresh"},"account":"mona@acme.com","since":"2026-09-02T00:00:00Z"}`, service: "gmail", kind: "refresh", token: "refresh", status: "connected"},
		{name: "expired access", entry: `{"keys":{"access_token":"access","expiry":"2020-01-01T00:00:00Z"}}`, service: "calendar", kind: "grant", token: "access", status: "expired"},
		{name: "pasted key", entry: `{"auth":"key","key":"stripe-key"}`, service: "stripe", kind: "grant", token: "stripe-key", status: "connected"},
		{name: "key reference", entry: `{"auth":"key","keyEnv":"CODEAF_GRANTS_TEST_KEY"}`, service: "stripe", kind: "grant", token: "env-key", status: "connected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CODEAF_GRANTS_TEST_KEY", "env-key")
			home := t.TempDir()
			raw := []byte(`{"` + serviceID(tt.service) + `":` + tt.entry + `}`)
			if err := os.WriteFile(filepath.Join(home, accounts.StoreFileName), raw, 0600); err != nil {
				t.Fatal(err)
			}
			store := &localConnections{home: home}
			list, err := store.Personal(testCtx, svc.Owner)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, c := range list {
				if c.Service == tt.service {
					found = true
					if c.Status != tt.status {
						t.Fatalf("status=%s want=%s", c.Status, tt.status)
					}
				}
			}
			if !found {
				t.Fatal("connection not listed")
			}
			c, err := store.PersonalCredential(testCtx, svc.Owner, tt.service)
			if err != nil || c.Kind != tt.kind || c.Token != tt.token {
				t.Fatal("credential not read")
			}
			_, err = store.PersonalCredential(testCtx, "other", tt.service)
			assertReason(t, err, bridge.CodeDenied, "")
			after, err := os.ReadFile(filepath.Join(home, accounts.StoreFileName))
			if err != nil || string(after) != string(raw) {
				t.Fatal("read changed personal store")
			}
		})
	}
}

func TestKeychainSourcePreventsDiskFallback(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, accounts.StoreFileName), []byte(`{"google":{"keys":{"access_token":"old-disk-secret"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	source := config.NewAppCredentials()
	restore := config.UseAppCredentials(source)
	defer restore()
	source.Set("google", `{"account":"mona@acme.com","keys":{"access_token":"access","refresh_token":"keychain-refresh"}}`)
	store := &localConnections{home: home}
	c, err := store.PersonalCredential(testCtx, svc.Owner, "gmail")
	if err != nil || c.Token != "keychain-refresh" {
		t.Fatal("did not use desktop source")
	}
	source.Set("google", "")
	_, err = store.PersonalCredential(testCtx, svc.Owner, "gmail")
	assertReason(t, err, bridge.CodeNotFound, "")
	list, err := store.Personal(testCtx, svc.Owner)
	if err != nil || len(list) != 0 {
		t.Fatal("disconnected Keychain account fell back to disk")
	}
}

func TestSecretsOmittedFromFormatting(t *testing.T) {
	secret := "must-not-be-logged"
	for _, value := range []any{bridge.GrantCredential{Kind: "grant", Token: secret}, svc.Credential{Token: secret}, bridge.AllowParams{Credential: &bridge.GrantCredential{Token: secret}}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, value), secret) {
				t.Fatal("format exposed secret")
			}
		}
	}
	raw, err := json.Marshal(bridge.CredentialExportResult{Credential: bridge.GrantCredential{Kind: "refresh", Token: secret}})
	if err != nil || !strings.Contains(string(raw), secret) {
		t.Fatal("authenticated export omitted intended credential")
	}
}
