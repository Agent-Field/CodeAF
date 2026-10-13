package config

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/codexauth"
)

func TestAppChatGPTRefreshStaysInMemoryAndDisconnectWins(t *testing.T) {
	profile := t.TempDir()
	original := codexauth.Tokens{AccessToken: "old-app-access", RefreshToken: "old-app-refresh", AccountID: "account", ExpiresAt: time.Now().Add(-time.Hour)}
	raw, _ := json.Marshal(original)
	source := NewAppCredentials()
	source.Set("chatgpt", string(raw))
	defer UseAppCredentials(source)()
	entered, release := make(chan struct{}), make(chan struct{})
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != original.RefreshToken {
			t.Error("refresh omitted stored token")
		}
		close(entered)
		<-release
		_, _ = io.WriteString(w, `{"access_token":"new-app-access","refresh_token":"new-app-refresh"}`)
	}))
	defer issuer.Close()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a removed credential reached the backend")
	}))
	defer backend.Close()
	client := codexauth.ClientWithOptions(profile, codexauth.Options{Issuer: issuer.URL})
	done := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", backend.URL+"/usage", nil)
		res, err := client.Do(req)
		if res != nil {
			_ = res.Body.Close()
		}
		done <- err
	}()
	<-entered
	source.Set("chatgpt", "")
	close(release)
	if err := <-done; !errors.Is(err, codexauth.ErrSignInExpired) {
		t.Fatalf("late refresh: %v", err)
	}
	if key, _ := source.Key("chatgpt"); key != "" {
		t.Fatal("late refresh undid disconnect")
	}
	files, err := os.ReadDir(profile)
	if err != nil || len(files) != 0 {
		t.Fatal("app refresh created token files or locks", files, err)
	}
}

func TestAppChatGPTBundleHandoffAndRefreshUseNativeTransport(t *testing.T) {
	profile := t.TempDir()
	original := codexauth.Tokens{AccessToken: "old-bundle-access", RefreshToken: "old-bundle-refresh", AccountID: "bundle-account", ExpiresAt: time.Now().Add(-time.Hour)}
	raw, _ := json.Marshal(original)
	handoff, _ := json.Marshal(map[string]any{"keys": map[string]string{"chatgpt": string(raw)}})
	source := NewAppCredentials()
	if err := ReadAppHandoff(bufio.NewReader(strings.NewReader(string(handoff)+"\n")), source); err != nil {
		t.Fatal(err)
	}
	defer UseAppCredentials(source)()
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"refreshed-bundle-access","refresh_token":"refreshed-bundle-refresh"}`)
	}))
	defer issuer.Close()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer refreshed-bundle-access" || r.Header.Get("chatgpt-account-id") != "bundle-account" {
			t.Error("native transport lost account identity or used old token")
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer backend.Close()
	client := codexauth.ClientWithOptions(profile, codexauth.Options{Issuer: issuer.URL})
	response, err := client.Get(backend.URL + "/usage")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	refreshed, err := source.LoadTokens()
	if err != nil || refreshed.AccessToken != "refreshed-bundle-access" || refreshed.RefreshToken != "refreshed-bundle-refresh" {
		t.Fatal("rotation did not update memory", err)
	}
	if _, err := os.Stat(filepath.Join(profile, "codex.json")); !os.IsNotExist(err) {
		t.Fatal("refresh wrote the terminal credential")
	}
	source.Set("chatgpt", "")
	if codexauth.Connected(profile) {
		t.Fatal("disconnect fell back to terminal")
	}
}
