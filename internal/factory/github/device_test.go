package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type deviceTripper func(*http.Request) (*http.Response, error)

func (f deviceTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answering(t *testing.T, host string, body string) *http.Client {
	return &http.Client{Transport: deviceTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != host {
			t.Errorf("asked %s, want %s", r.URL.Host, host)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Error("GitHub answers form-encoded unless asked for JSON")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

// A STAND-IN GITHUB IS HTTPS, OR ON THIS MACHINE: no setting can send a
// device code across a network in the clear.
func TestDeviceStandInMustBeHTTPSOrLoopback(t *testing.T) {
	for raw, ok := range map[string]bool{
		"":                       true,
		"https://ghe.example":    true,
		"http://127.0.0.1:9000/": true,
		"http://localhost:9000":  true,
		"http://[::1]:9000":      true,
		"http://10.0.0.5:9000":   false,
		"http://evil.example":    false,
		"ftp://127.0.0.1":        false,
		"not a url":              false,
	} {
		t.Setenv("CODEAF_GITHUB_LOGIN_URL", raw)
		_, err := deviceBase()
		if (err == nil) != ok {
			t.Errorf("%q: err %v, want ok=%v", raw, err, ok)
		}
	}
}

func TestDeviceClientIDComesFromTheRunOverTheBuild(t *testing.T) {
	was := clientID
	t.Cleanup(func() { clientID = was })
	clientID = "Ov23-stamped"
	t.Setenv("CODEAF_GITHUB_CLIENT_ID", "")
	if got := DeviceClientID(); got != "Ov23-stamped" {
		t.Fatalf("stamped: %q", got)
	}
	t.Setenv("CODEAF_GITHUB_CLIENT_ID", " Ov23-run ")
	if got := DeviceClientID(); got != "Ov23-run" {
		t.Fatalf("run: %q", got)
	}
	clientID = ""
	t.Setenv("CODEAF_GITHUB_CLIENT_ID", "")
	if _, err := (DeviceClient{}).StartDevice(context.Background()); err == nil {
		t.Fatal("a build with no client ID started a sign-in")
	}
}

func TestDevicePollReadsEveryAnswerGitHubGives(t *testing.T) {
	t.Setenv("CODEAF_GITHUB_LOGIN_URL", "")
	for body, want := range map[string]DeviceGrant{
		`{"access_token":"gho_x","scope":"repo"}`:  {Token: "gho_x", Scope: "repo"},
		`{"error":"authorization_pending"}`:        {Error: DevicePending},
		`{"error":"slow_down","interval":10}`:      {Error: DeviceSlowDown, Interval: 10},
		`{"error":"access_denied"}`:                {Error: DeviceDenied},
		`{"error":"expired_token"}`:                {Error: DeviceExpired},
		`{"error":"incorrect_device_code"}`:        {Error: DeviceExpired},
		`{"error":"unsupported_grant_type","x":1}`: {Error: DeviceExpired},
	} {
		c := DeviceClient{ClientID: "Ov23", HTTP: answering(t, "github.com", body)}
		got, err := c.PollDevice(context.Background(), "dc")
		if err != nil || got != want {
			t.Errorf("%s: %+v %v, want %+v", body, got, err, want)
		}
	}
	c := DeviceClient{ClientID: "Ov23", HTTP: answering(t, "github.com", `{"error":"device_flow_disabled"}`)}
	if _, err := c.StartDevice(context.Background()); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("device flow disabled: %v", err)
	}
}
