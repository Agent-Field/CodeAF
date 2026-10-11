package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestFactoryTokenAddPrintsTheSecretAndRevokesTheRow(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	configPath := home.Join("factory", "config.yaml")
	if err := writeFactoryPrivate(configPath, []byte("listen: 127.0.0.1:7421\n")); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runFactory([]string{"token", "add", "--config", configPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.TrimSpace(out)
	if strings.Contains(secret, "\n") || len(secret) != 64 || strings.Contains(secret, "://") {
		t.Fatalf("token add printed %q", out)
	}
	rows, err := tokens.Load(factoryTokensPath(configPath))
	if err != nil || len(rows) != 1 || rows[0].Token != secret {
		t.Fatalf("row = %+v, %v", rows, err)
	}
	if rows[0].Expires.Before(time.Now().Add(7*24*time.Hour-time.Minute)) || rows[0].Expires.After(time.Now().Add(7*24*time.Hour+time.Minute)) {
		t.Fatalf("default expiry = %s", rows[0].Expires)
	}
	info, err := os.Stat(factoryTokensPath(configPath))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("tokens.json mode = %v, %v", info, err)
	}
	listed, err := captureStdout(t, func() error {
		return runFactory([]string{"token", "list", "--config", configPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, rows[0].Token) || !strings.HasPrefix(listed, rows[0].ID+" ") {
		t.Fatalf("list = %q", listed)
	}
	revoked, err := captureStdout(t, func() error {
		return runFactory([]string{"revoke", rows[0].ID, "--config", configPath})
	})
	if err != nil || strings.TrimSpace(revoked) != "revoked "+rows[0].ID {
		t.Fatalf("revoke = %q, %v", revoked, err)
	}
	if _, err := captureStdout(t, func() error {
		return runFactory([]string{"revoke", rows[0].ID, "--config", configPath})
	}); err == nil || !strings.Contains(err.Error(), "no access token "+rows[0].ID) {
		t.Fatalf("second revoke = %v", err)
	}
	left, err := tokens.Load(factoryTokensPath(configPath))
	if err != nil || len(left) != 1 || !left[0].Revoked || left[0].Token != "" || !left[0].Matches(secret) {
		t.Fatalf("rows after revoke = %+v, %v", left, err)
	}
	hidden, err := captureStdout(t, func() error {
		return runFactory([]string{"token", "list", "--config", configPath})
	})
	if err != nil || strings.TrimSpace(hidden) != "" {
		t.Fatalf("list after revoke = %q, %v", hidden, err)
	}
}

func TestFactoryTokenAddRefusesBadExpiry(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	configPath := home.Join("factory", "config.yaml")
	for _, expiry := range []string{"0", "0d", "-1h", "nope"} {
		_, err := captureStdout(t, func() error {
			return runFactory([]string{"token", "add", "--expires", expiry, "--config", configPath})
		})
		if err == nil || !strings.Contains(err.Error(), "expiry must be a duration like 7d or 24h") {
			t.Fatalf("expiry %q = %v", expiry, err)
		}
	}
	if _, statErr := os.Stat(factoryTokensPath(configPath)); !os.IsNotExist(statErr) {
		t.Fatal("a refused token was written")
	}
}

func TestFactoryTokenAddWorksUntilRevoke(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	configPath := home.Join("factory", "config.yaml")
	if err := writeFactoryPrivate(configPath, []byte("listen: 127.0.0.1:0\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(factoryReady, 1)
	done := make(chan error, 1)
	go func() { done <- serveFactory(ctx, configPath, "", ready, "", false) }()
	var address string
	select {
	case address = <-ready:
	case err := <-done:
		t.Fatalf("service failed before printing its address: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("service did not print its address")
	}
	out, err := captureStdout(t, func() error {
		return runFactory([]string{"token", "add", "--expires", "1h", "--config", configPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.TrimSpace(out)
	rows, err := tokens.Load(factoryTokensPath(configPath))
	if err != nil || len(rows) != 1 || rows[0].Token != secret {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	body, status := getFactoryMe(t, address, secret)
	if status != http.StatusOK || !strings.Contains(body, `"session_id":"`+rows[0].ID+`"`) {
		t.Fatalf("me = %d %s", status, body)
	}
	if rows[0].Expires.Before(time.Now().Add(time.Hour-time.Minute)) || rows[0].Expires.After(time.Now().Add(time.Hour+time.Minute)) {
		t.Fatalf("expiry = %s", rows[0].Expires)
	}
	if _, err := captureStdout(t, func() error {
		return runFactory([]string{"revoke", rows[0].ID, "--config", configPath})
	}); err != nil {
		t.Fatal(err)
	}
	body, status = getFactoryMe(t, address, secret)
	// Loopback stays open, so the call still answers. The bearer is no longer
	// a credential: the session id that belonged to the row is gone.
	if status != http.StatusOK || strings.Contains(body, `"session_id":"tok_`) {
		t.Fatalf("revoked token still a credential: %d %s", status, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("service did not stop")
	}
}

func getFactoryMe(t *testing.T, address, token string) (string, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, address+"/v1/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
	}
	return string(body), resp.StatusCode
}
