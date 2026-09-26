//go:build !windows

package netpolicy

import (
	"net/http"
	"strings"
	"testing"
)

func lookup(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestFromLookupModes(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		mode    Mode
		warning bool
	}{
		{name: "unset defaults to allow", env: nil, mode: ModeAllow},
		{name: "explicit allow", env: map[string]string{EnvMode: "allow"}, mode: ModeAllow},
		{name: "off", env: map[string]string{EnvMode: "off"}, mode: ModeOff},
		{name: "case and space folded", env: map[string]string{EnvMode: "  OFF "}, mode: ModeOff},
		{
			name: "typo fails closed",
			env:  map[string]string{EnvMode: "on"},
			mode: ModeOff, warning: true,
		},
		{
			name: "unknown mode name fails closed",
			env:  map[string]string{EnvMode: "allowlist"},
			mode: ModeOff, warning: true,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			policy := FromLookup(lookup(test.env))
			if policy.Mode != test.mode {
				t.Fatalf("mode = %q, want %q", policy.Mode, test.mode)
			}
			if (policy.Warning != "") != test.warning {
				t.Fatalf("warning = %q, want present=%v", policy.Warning, test.warning)
			}
		})
	}
}

type recordingTransport struct{ dialed bool }

func (t *recordingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.dialed = true
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

func TestTransportEnforcesPolicy(t *testing.T) {
	base := &recordingTransport{}

	client := &http.Client{Transport: Policy{Mode: ModeOff}.Transport(base)}
	_, err := client.Get("http://example.com/")
	if err == nil {
		t.Fatal("off-mode request unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "[network-policy]") {
		t.Fatalf("off-mode error missing policy marker: %v", err)
	}
	if base.dialed {
		t.Fatal("off-mode request reached the base transport")
	}

	// Unrestricted policy must return the base transport untouched.
	if (Policy{Mode: ModeAllow}).Transport(base) != http.RoundTripper(base) {
		t.Fatal("allow-mode Transport(base) should be the base transport")
	}
	if (Policy{Mode: ModeAllow}).Transport(nil) != http.RoundTripper(http.DefaultTransport) {
		t.Fatal("allow-mode Transport(nil) should be http.DefaultTransport")
	}
}

func TestHostErrorSteersAwayFromRetry(t *testing.T) {
	err := Policy{Mode: ModeOff}.HostError("mcp.exa.ai:443")
	for _, want := range []string{"[network-policy]", "mcp.exa.ai", "do not retry", "SENIOR_DEV_NET"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestEnvironmentNotice(t *testing.T) {
	if notice := (Policy{Mode: ModeAllow}).EnvironmentNotice(); notice != "" {
		t.Fatalf("allow mode should carry no notice, got %q", notice)
	}
	notice := Policy{Mode: ModeOff}.EnvironmentNotice()
	for _, want := range []string{"Network access is disabled", "Do not attempt"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice %q missing %q", notice, want)
		}
	}
}
