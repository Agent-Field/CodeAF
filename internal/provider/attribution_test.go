package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
)

// The release's attribution is spelled out byte for byte here rather than read
// from the constants, because what this file guards is that a stable or
// release-candidate binary keeps sending exactly what every build sent before
// the channels were split. A test that compared the headers to AppURL would
// pass just as happily after somebody edited AppURL.
var releaseHeaders = map[string]string{
	"HTTP-Referer":            "https://agentfield.ai",
	"X-OpenRouter-Title":      "AgentField AI",
	"X-Title":                 "AgentField AI",
	"X-OpenRouter-Categories": "cli-agent,programming-app",
}

func assertExactHeaders(t *testing.T, where string, got http.Header, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: wrote %d headers %v, want exactly %d", where, len(got), got, len(want))
	}
	for name, value := range want {
		if values := got.Values(name); len(values) != 1 || values[0] != value {
			t.Fatalf("%s: %s = %q, want exactly %q", where, name, values, value)
		}
	}
}

func TestAReleaseBuildReportsAsAgentFieldAIExactlyAsBefore(t *testing.T) {
	for _, tag := range []string{"v0.5.0", "v0.4.2", "v1.0.0", "v10.20.30", "v0.5.1-rc.1", "v0.4.2-rc.12"} {
		header := http.Header{}
		AppFor(tag).Apply(header)
		assertExactHeaders(t, tag, header, releaseHeaders)
	}
}

func TestAStagingBuildReportsAsTheStagingApp(t *testing.T) {
	header := http.Header{}
	AppFor("staging-20261001-0123456789ab").Apply(header)
	assertExactHeaders(t, "staging build", header, map[string]string{
		"HTTP-Referer":            "https://staging.codeaf.agentfield.ai",
		"X-OpenRouter-Title":      "codeaf staging",
		"X-Title":                 "codeaf staging",
		"X-OpenRouter-Categories": "cli-agent,programming-app",
	})
}

// Anything the release workflow did not cut is the dev app: a dev tag, a
// commit `make build` stamped, a module pseudo-version, an empty stamp, and
// every near miss of a release tag. A near miss must never count as the
// release, because the release page is the one this whole split protects.
func TestADevBuildAndEveryUntaggedBuildReportAsTheDevApp(t *testing.T) {
	dev := map[string]string{
		"HTTP-Referer":            "https://dev.codeaf.agentfield.ai",
		"X-OpenRouter-Title":      "codeaf dev",
		"X-Title":                 "codeaf dev",
		"X-OpenRouter-Categories": "cli-agent,programming-app",
	}
	for _, revision := range []string{
		"dev-20261001-0123456789ab",
		"4ad77ed45",
		"4ad77ed45c0123456789abcdef0123456789abcd",
		"",
		"dev",
		"(devel)",
		"v0.0.0-20261001120000-4ad77ed45c01",
		"v0.5",
		"v0.5.0-dirty",
		"v0.5.0-rc.0",
		"V0.5.0",
		"v05.0.0",
		"staging-2026101-0123456789ab",
		"staging-20261001-0123456789AB",
	} {
		header := http.Header{}
		AppFor(revision).Apply(header)
		assertExactHeaders(t, "revision "+revision, header, dev)
	}
}

// OpenRouter groups referers by origin, so a path under the release's origin is
// the release's app again. Each identity has to be an origin of its own or the
// split does nothing.
func TestEveryIdentityIsAnOriginOfItsOwn(t *testing.T) {
	origins := map[string]string{}
	for _, tag := range []string{"v0.5.0", "staging-20261001-0123456789ab", "dev-20261001-0123456789ab"} {
		app := AppFor(tag)
		parsed, err := url.Parse(app.URL)
		if err != nil {
			t.Fatalf("%s: %q is not a URL: %v", tag, app.URL, err)
		}
		if parsed.Scheme != "https" || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" {
			t.Fatalf("%s: %q is not a bare https origin", tag, app.URL)
		}
		if other, taken := origins[parsed.Host]; taken {
			t.Fatalf("%s and %s share the origin %s", tag, other, parsed.Host)
		}
		origins[parsed.Host] = tag
	}
}

// The running binary reports as its own stamp, read once through buildinfo.
func TestTheRunningBinaryReportsAsItsOwnStamp(t *testing.T) {
	if got, want := RunningApp(), AppFor(buildinfo.Revision()); got != want {
		t.Fatalf("RunningApp() = %+v, want the app for this binary's stamp %q: %+v", got, buildinfo.Revision(), want)
	}
	header := http.Header{}
	ApplyAttribution(header)
	app := RunningApp()
	assertExactHeaders(t, "ApplyAttribution", header, map[string]string{
		"HTTP-Referer":            app.URL,
		"X-OpenRouter-Title":      app.Name,
		"X-Title":                 app.Name,
		"X-OpenRouter-Categories": app.Categories,
	})
}

// The SDK's client is the second writer of these headers: it sends the referer
// and the title it was built with whenever it thinks it is talking to
// OpenRouter, which it decides from the base URL or from an `openrouter/` model
// id. The model id is how this test reaches that branch without the network.
// It must name the same app the package's own transport does.
func TestTheSDKClientReportsAsTheSameApp(t *testing.T) {
	// The SDK has an opt-out of its own, read from the environment on every
	// request; a machine that set it would turn this test into a test of that.
	t.Setenv("AGENTFIELD_OPENROUTER_ATTRIBUTION", "")
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case seen <- request.Header.Clone():
		default:
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL + "/v1", Model: "openrouter/probe"})
	if err != nil {
		t.Fatal(err)
	}
	base := client.sdkClient()
	if base == nil {
		t.Fatal("a client with a key built no SDK client")
	}
	if _, err := base.Complete(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	header := <-seen
	app := RunningApp()
	if got := header.Get("HTTP-Referer"); got != app.URL {
		t.Fatalf("SDK HTTP-Referer = %q, want %q", got, app.URL)
	}
	if got := header.Get("X-Title"); got != app.Name {
		t.Fatalf("SDK X-Title = %q, want %q", got, app.Name)
	}
}
