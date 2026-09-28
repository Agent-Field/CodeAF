//go:build !windows

package tool

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/netpolicy"
)

func TestWebFetchBlockedWhenNetOff(t *testing.T) {
	t.Setenv(netpolicy.EnvMode, "off")
	dialed := false
	client := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		dialed = true
		return nil, nil
	})}
	ctx := WithWebHTTPClient(context.Background(), client)
	_, err := executeWebTest(t, New(t.TempDir()), ctx, "webfetch", map[string]any{
		"url": "https://example.com/doc",
	})
	if err == nil || !strings.Contains(err.Error(), "[network-policy]") {
		t.Fatalf("want [network-policy] error, got %v", err)
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Fatalf("policy error should name the blocked host: %v", err)
	}
	if dialed {
		t.Fatal("request reached the transport despite SENIOR_DEV_NET=off")
	}
}

// TestWebClientTransportWrapEnforcesPolicy pins the policy wrap inside
// webClient itself, past the tools' pre-execute checks: any HTTP issued
// through the shared client while the policy is restricted must be refused at
// the transport with the [network-policy] no-retry framing rather than
// dialing (or collapsing into a retryable-looking transport error). If the
// wrap is ever dropped from webClient, the request reaches the base
// transport and this test fails.
func TestWebClientTransportWrapEnforcesPolicy(t *testing.T) {
	t.Setenv(netpolicy.EnvMode, "off")
	dialed := false
	injected := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		dialed = true
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})}
	ctx := WithWebHTTPClient(context.Background(), injected)

	_, err := webClient(ctx).Get("https://example.com/doc")
	var blocked *netpolicy.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("want *netpolicy.BlockedError, got %v", err)
	}
	if dialed {
		t.Fatal("request reached the base transport despite SENIOR_DEV_NET=off")
	}
	if injected.Transport == nil {
		t.Fatal("webClient mutated the injected client instead of copying it")
	}
	if _, ok := injected.Transport.(webRoundTripFunc); !ok {
		t.Fatalf("webClient replaced the injected client's own transport: %T", injected.Transport)
	}
}

func TestWebSearchEndpointBlockedWhenNetOff(t *testing.T) {
	t.Setenv(netpolicy.EnvMode, "off")
	_, err := callMCPWebSearch(
		context.Background(), defaultExaWebSearchURL, "web_search_exa",
		map[string]any{"query": "q"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "[network-policy]") {
		t.Fatalf("want [network-policy] error, got %v", err)
	}
}

func TestShellEnvironmentInjectsBlackholeProxyWhenNetOff(t *testing.T) {
	t.Setenv(netpolicy.EnvMode, "off")
	// The shared-cache early return must not skip the network gate.
	t.Setenv("SENIOR_DEV_SHARED_BUILD_CACHE", "1")
	environment := shellEnvironment("ses-netpolicy")
	var proxy, noProxy string
	for _, entry := range environment {
		if value, ok := strings.CutPrefix(entry, "HTTPS_PROXY="); ok {
			proxy = value
		}
		if value, ok := strings.CutPrefix(entry, "NO_PROXY="); ok {
			noProxy = value
		}
	}
	if !strings.HasPrefix(proxy, "http://127.0.0.1:") {
		t.Fatalf("HTTPS_PROXY = %q, want local black-hole", proxy)
	}
	if noProxy != "localhost,127.0.0.1,::1" {
		t.Fatalf("NO_PROXY = %q", noProxy)
	}
}

func TestShellEnvironmentUntouchedWhenNetAllow(t *testing.T) {
	t.Setenv(netpolicy.EnvMode, "allow")
	for _, entry := range shellEnvironment("") {
		if strings.HasPrefix(entry, "HTTP_PROXY=http://127.0.0.1:") {
			t.Fatalf("allow mode injected proxy entry %q", entry)
		}
	}
}
