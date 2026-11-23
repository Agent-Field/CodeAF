//go:build e2e

package connect

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// TestRealGitHubToolServer is the one claim about GitHub no stand-in can
// answer: that GitHub's hosted tool server takes an OAuth-app token as a
// bearer, lists its tools and runs a read. The token is gh's own (an OAuth-app
// token, the same kind the device flow mints), held in memory by a fake set of
// doors, and never printed. It SKIPS with no gh login.
//
//	go test -tags e2e ./internal/connect/ -run TestRealGitHub -v
func TestRealGitHubToolServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	token := strings.TrimSpace(string(out))
	if err != nil || token == "" {
		t.Skip("gh is not signed in on this machine")
	}

	held := ""
	doors := GitHubDoors{
		Token:      func(context.Context) (string, string) { return held, "token" },
		Keep:       func(_ context.Context, t string) error { held = t; return nil },
		KeepLocal:  func(context.Context) (string, error) { held = token; return token, nil },
		Forget:     func(context.Context) error { held = ""; return nil },
		LocalLogin: func(context.Context) string { return "" },
		Login: func(ctx context.Context, t string) (string, error) {
			login, err := forge.NewClient(t).Me(ctx)
			var apiErr *forge.APIError
			if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
				return "", ErrGitHubRefused
			}
			return login, err
		},
	}
	withPlugs(t, findPlug(t, GitHubID))
	manager, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.UseGitHub(doors)
	t.Cleanup(func() { forgetTools(GitHubID) })

	// (a) GET /user, through both ways a token comes in.
	status, err := manager.ConnectKey(ctx, GitHubID, token)
	if err != nil || status.Account == "" {
		t.Fatalf("connect_key: %v", err)
	}
	t.Logf("connect_key: checked=%v account=%s", status.Checked, status.Account)
	_ = manager.Disconnect(GitHubID)
	status, err = manager.ConnectLocal(ctx, GitHubID)
	if err != nil || status.Account == "" {
		t.Fatalf("connect_local: %v", err)
	}
	t.Logf("connect_local: checked=%v account=%s", status.Checked, status.Account)
	if _, err := manager.ConnectKey(ctx, GitHubID, "gho_not_a_real_token_000000000000000000"); err == nil {
		t.Fatal("GitHub accepted a made-up token")
	} else {
		t.Logf("a made-up token: %v", err)
	}

	// (b) the hosted tool server: tools/list, then one read.
	tools, err := manager.MCPTools(ctx, GitHubID)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	reads := 0
	for _, tool := range tools {
		if tool.ReadOnly {
			reads++
		}
	}
	t.Logf("tools/list: %d tools, %d read-only", len(tools), reads)
	said, err := manager.MCPCall(ctx, GitHubID, "get_me", nil)
	if err != nil {
		t.Fatalf("get_me: %v", err)
	}
	if !strings.Contains(said, status.Account) {
		t.Fatalf("get_me did not name %s", status.Account)
	}
	t.Logf("get_me named %s (%d characters)", status.Account, len(said))

	d, err := manager.DescribePlugin(ctx, GitHubID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range d.Capabilities {
		t.Logf("capability %s: %s, %d tools", c.ID, c.State, len(c.Tools))
	}
}
