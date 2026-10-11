package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestFactoryInvitePrintsBothLinksAndRevokesTheRow(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	configPath := home.Join("factory", "config.yaml")
	if err := writeFactoryPrivate(configPath, []byte("listen: 127.0.0.1:7421\n")); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runFactory([]string{"invite", "--handle", "@Sam", "--config", configPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	codeafLink, httpsLink := inviteLines(t, out)
	if codeafLink.Scheme != "codeaf" || httpsLink.Scheme != "https" || codeafLink.Host != "attach" || httpsLink.Host != "attach" {
		t.Fatalf("links:\n%s", out)
	}
	if codeafLink.RawQuery != httpsLink.RawQuery {
		t.Fatalf("https form differs:\n%s\n%s", codeafLink.RawQuery, httpsLink.RawQuery)
	}
	q := codeafLink.Query()
	if q.Get("url") != "http://127.0.0.1:7421" || q.Get("by") != "sam" || len(q.Get("token")) != 64 {
		t.Fatalf("query = %v", q)
	}
	if !strings.HasPrefix(codeafLink.RawQuery, "url=") || !strings.Contains(codeafLink.RawQuery, "&token=") || !strings.HasSuffix(codeafLink.RawQuery, "&by=sam") {
		t.Fatalf("query order = %s", codeafLink.RawQuery)
	}
	rows, err := tokens.Load(factoryTokensPath(configPath))
	if err != nil || len(rows) != 1 || rows[0].InvitedBy != "sam" || rows[0].Token != q.Get("token") {
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
		return runFactory([]string{"token", "--list", "--config", configPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, rows[0].Token) || !strings.Contains(listed, rows[0].ID+" sam ") {
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
	if err != nil || len(left) != 0 {
		t.Fatalf("rows after revoke = %+v, %v", left, err)
	}
}

func TestFactoryInviteRefusesBadExpiryAndAMissingHandle(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	configPath := home.Join("factory", "config.yaml")
	for _, expiry := range []string{"0", "0d", "-1h", "nope"} {
		_, err := captureStdout(t, func() error {
			return runFactory([]string{"invite", "--handle", "@sam", "--expires", expiry, "--config", configPath})
		})
		if err == nil || !strings.Contains(err.Error(), "expiry must be a duration like 7d or 24h") {
			t.Fatalf("expiry %q = %v", expiry, err)
		}
	}
	_, err := captureStdout(t, func() error {
		return runFactory([]string{"invite", "--config", configPath})
	})
	if err == nil || !strings.Contains(err.Error(), "codeaf factory invite needs --handle @name; this factory has no GitHub login") {
		t.Fatalf("missing handle = %v", err)
	}
	if _, statErr := os.Stat(factoryTokensPath(configPath)); !os.IsNotExist(statErr) {
		t.Fatal("a refused invite wrote a token")
	}
}

func TestFactoryInviteUsesTheRunningServiceAndMeNamesTheInviter(t *testing.T) {
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
		return runFactory([]string{"invite", "--handle", "@Sam", "--expires", "1h", "--config", configPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	link, _ := inviteLines(t, out)
	q := link.Query()
	if q.Get("url") != address || q.Get("by") != "sam" {
		t.Fatalf("link address %q, service %q, by %q", q.Get("url"), address, q.Get("by"))
	}
	body := getFactoryMe(t, address, q.Get("token"))
	if !strings.Contains(body, `"invited_by":"sam"`) {
		t.Fatalf("me = %s", body)
	}
	rows, err := tokens.Load(factoryTokensPath(configPath))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	if rows[0].Expires.Before(time.Now().Add(time.Hour-time.Minute)) || rows[0].Expires.After(time.Now().Add(time.Hour+time.Minute)) {
		t.Fatalf("expiry = %s", rows[0].Expires)
	}
	if _, err := captureStdout(t, func() error {
		return runFactory([]string{"revoke", rows[0].ID, "--config", configPath})
	}); err != nil {
		t.Fatal(err)
	}
	body = getFactoryMe(t, address, q.Get("token"))
	if strings.Contains(body, "invited_by") {
		t.Fatalf("revoked token still invited: %s", body)
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

func TestAttachAcceptsAJoinLink(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	for _, scheme := range []string{"codeaf", "https"} {
		t.Run(scheme, func(t *testing.T) {
			link := factoryJoinLink(scheme, "http://factory.example:7420", "invite-secret", "sam")
			if err := runAttach([]string{link, "--name", "laptop"}); err != nil {
				t.Fatal(err)
			}
			got, ok := readFactoryAttachment()
			if !ok || got.URL != "http://factory.example:7420" || got.Token != "invite-secret" || got.Name != "laptop" {
				t.Fatalf("attach.json = %+v", got)
			}
		})
	}
	overridden := factoryJoinLink("codeaf", "http://factory.example:7420", "invite-secret", "sam")
	if err := runAttach([]string{overridden, "--token", "other-secret"}); err != nil {
		t.Fatal(err)
	}
	got, ok := readFactoryAttachment()
	if !ok || got.Token != "other-secret" {
		t.Fatalf("flag did not override the link token: %+v", got)
	}
	_, stderr := captureUsage(t)
	if err := runAttach([]string{"codeaf://attach?url=http%3A%2F%2F127.0.0.1%3A1"}); err == nil || !strings.Contains(stderr.String(), "a join link needs a factory address and a token") {
		t.Fatalf("incomplete link = %v\n%s", err, stderr.String())
	}
	if err := runAttach([]string{"codeaf://nope?url=http%3A%2F%2F127.0.0.1%3A1&token=x"}); err == nil {
		t.Fatal("a codeaf link that is not attach was accepted")
	}
}

func inviteLines(t *testing.T, out string) (codeafLink, httpsLink *url.URL) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("invite printed %d lines:\n%s", len(lines), out)
	}
	var err error
	codeafLink, err = url.Parse(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	httpsLink, err = url.Parse(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	return codeafLink, httpsLink
}

func getFactoryMe(t *testing.T, address, token string) string {
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status %d: %s", resp.StatusCode, body)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	return string(body)
}
