package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/id"
)

type webRoundTripFunc func(*http.Request) (*http.Response, error)

func (function webRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func newLocalWebServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("sandbox blocks loopback listeners: %v", err)
	}
	server := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: handler},
	}
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func executeWebTest(
	t *testing.T,
	registry *Registry,
	ctx context.Context,
	name string,
	input map[string]any,
) (steploop.ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return registry.Execute(ctx, steploop.ToolCall{
		ID: "call-web", Name: name, Input: raw, SessionID: "ses-web", Agent: "coder",
		ModelID: "fixture/model",
	})
}

func TestWebFetchHTMLFullExecution(t *testing.T) {
	html := `<!doctype html><html><head><title>T</title><style>x</style></head><body><h1>Hello</h1><p>plain <strong>bold</strong> &amp; <a href="/x">link</a>.</p><ul><li>one</li><li>two</li></ul></body></html>`
	server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != acceptHeader("markdown") {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		if request.Header.Get("User-Agent") != webFetchUserAgent {
			t.Errorf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, html)
	}))
	ctx := WithWebHTTPClient(context.Background(), server.Client())
	ctx = WithWebOutputDir(ctx, t.TempDir())
	result, err := executeWebTest(t, New(t.TempDir()), ctx, "webfetch", map[string]any{"url": server.URL})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "T\n\n# Hello\n\nplain **bold** & [link](/x).\n\n-   one\n-   two"
	if result.Output != want {
		t.Fatalf("Output = %q, want %q", result.Output, want)
	}
	if result.Title != server.URL+" (text/html; charset=utf-8)" {
		t.Fatalf("Title = %q", result.Title)
	}
	if string(result.Metadata) != `{"truncated":false}` {
		t.Fatalf("Metadata = %s", result.Metadata)
	}
}

func TestWebFetchHTMLConversionFixture(t *testing.T) {
	source := `<!doctype html><html><head><title>T</title><style>x</style></head><body><h1>Hello</h1><p>plain <strong>bold</strong> &amp; <a href="/x">link</a>.</p><ul><li>one</li><li>two</li></ul></body></html>`
	want := "T\n\n# Hello\n\nplain **bold** & [link](/x).\n\n-   one\n-   two"
	got, err := convertWebHTMLToMarkdown(source)
	if err != nil || got != want {
		t.Fatalf("conversion = (%q, %v), want %q", got, err, want)
	}
}

func TestWebFetchHTMLMarkdownEscapesTurndownPunctuation(t *testing.T) {
	tests := []struct {
		html string
		want string
	}{
		{`<p># not heading</p>`, `\# not heading`},
		{`<p>1. not list</p>`, `1\. not list`},
		{`<p>a_b*c</p>`, `a\_b\*c`},
		{"<p>`literal`</p>", `\` + "`literal\\`"},
		{`<p>[not a link]</p>`, `\[not a link\]`},
		{`<p>&gt; not a quote</p>`, `\> not a quote`},
	}
	for _, test := range tests {
		got, err := convertWebHTMLToMarkdown(test.html)
		if err != nil || got != test.want {
			t.Errorf("convert %q = (%q, %v), want %q", test.html, got, err, test.want)
		}
	}
}

func TestWebFetchHTMLTextFixture(t *testing.T) {
	got, err := extractWebHTMLText(`<p>Hello <b>world</b></p><script>bad()</script><div>tail</div>`)
	if err != nil || got != "Hello worldtail" {
		t.Fatalf("text = (%q, %v)", got, err)
	}
}

func TestWebFetchTruncatesToolOutput(t *testing.T) {
	full := strings.Repeat("x", webOutputMaxBytes+1)
	server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(writer, full)
	}))
	spill := t.TempDir()
	ctx := WithWebHTTPClient(context.Background(), server.Client())
	ctx = WithWebOutputDir(ctx, spill)
	result, err := executeWebTest(t, New(t.TempDir()), ctx, "webfetch", map[string]any{
		"url": server.URL, "format": "text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "...51201 bytes truncated...") ||
		!strings.Contains(result.Output, "The tool call succeeded but the output was truncated.") {
		t.Fatalf("Output = %q", result.Output)
	}
	var metadata webFetchMetadata
	if err := json.Unmarshal(result.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if !metadata.Truncated || metadata.OutputPath == "" {
		t.Fatalf("Metadata = %s", result.Metadata)
	}
	saved, err := os.ReadFile(metadata.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != full {
		t.Fatalf("saved output length = %d", len(saved))
	}
}

func TestWebFetchTruncatesWithoutSocket(t *testing.T) {
	full := strings.Repeat("x", webOutputMaxBytes+1)
	client := &http.Client{Transport: webRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader(full)),
			Request:    request,
		}, nil
	})}
	ctx := WithWebHTTPClient(context.Background(), client)
	ctx = WithWebOutputDir(ctx, t.TempDir())
	result, err := executeWebTest(t, New(t.TempDir()), ctx, "webfetch", map[string]any{
		"url": "https://fixture.invalid/large", "format": "text",
	})
	if err != nil || !strings.Contains(result.Output, "...51201 bytes truncated...") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestWebOutputSweepExpiresSevenDayOldSpills(t *testing.T) {
	directory := t.TempDir()
	now := time.Now()
	oldID, err := id.Create("tool", id.AscendingDirection, now.Add(-8*24*time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	recentID, err := id.Create("tool", id.AscendingDirection, now.Add(-6*24*time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{oldID, recentID, "unrelated"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("spill"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	registry := New(t.TempDir())
	if _, _, err := registry.truncateWebOutput(WithWebOutputDir(context.Background(), directory), steploop.ToolCall{}, "short"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, oldID)); !os.IsNotExist(err) {
		t.Fatalf("expired spill still exists: %v", err)
	}
	for _, name := range []string{recentID, "unrelated"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("retained file %s: %v", name, err)
		}
	}
}

func TestWebFetchResponseAndFailureParity(t *testing.T) {
	registry := New(t.TempDir())
	t.Run("invalid scheme", func(t *testing.T) {
		_, err := executeWebTest(t, registry, context.Background(), "webfetch", map[string]any{"url": "ftp://example.com"})
		if err == nil || err.Error() != "URL must start with http:// or https://" {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid URL", func(t *testing.T) {
		_, err := executeWebTest(t, registry, context.Background(), "webfetch", map[string]any{"url": "http://["})
		if err == nil || err.Error() != "InvalidUrl error (GET http://[)" {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("connection refused shape", func(t *testing.T) {
		client := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: connection refused")
		})}
		ctx := WithWebHTTPClient(context.Background(), client)
		_, err := executeWebTest(t, registry, ctx, "webfetch", map[string]any{"url": "http://127.0.0.1:1/x"})
		if err == nil || err.Error() != "Transport error (GET http://127.0.0.1:1/x)" {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("non-2xx", func(t *testing.T) {
		server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusTeapot)
		}))
		ctx := WithWebHTTPClient(context.Background(), server.Client())
		_, err := executeWebTest(t, registry, ctx, "webfetch", map[string]any{"url": server.URL})
		want := "StatusCode error (418 GET " + server.URL + ")"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})
	t.Run("unsupported MIME is decoded", func(t *testing.T) {
		// webfetch.ts:104-151 has no unsupported-content-type rejection: all
		// non-image bodies are decoded as text. This pins that observable fact.
		server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(writer, "opaque")
		}))
		ctx := WithWebHTTPClient(context.Background(), server.Client())
		result, err := executeWebTest(t, registry, ctx, "webfetch", map[string]any{"url": server.URL})
		if err != nil || result.Output != "opaque" {
			t.Fatalf("result = %#v, error = %v", result, err)
		}
	})
	t.Run("declared over 5 MiB", func(t *testing.T) {
		server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Length", "5242881")
			_, _ = io.WriteString(writer, "short")
		}))
		ctx := WithWebHTTPClient(context.Background(), server.Client())
		_, err := executeWebTest(t, registry, ctx, "webfetch", map[string]any{"url": server.URL})
		if err == nil || err.Error() != "Response too large (exceeds 5MB limit)" {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestWebFetchFailuresWithoutSocket(t *testing.T) {
	executeResponse := func(status int, headers http.Header, body string) (steploop.ToolResult, error) {
		client := &http.Client{Transport: webRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status, Header: headers,
				Body: io.NopCloser(strings.NewReader(body)), Request: request,
			}, nil
		})}
		ctx := WithWebHTTPClient(context.Background(), client)
		ctx = WithWebOutputDir(ctx, t.TempDir())
		return executeWebTest(t, New(t.TempDir()), ctx, "webfetch", map[string]any{
			"url": "https://fixture.invalid/value",
		})
	}

	_, err := executeResponse(http.StatusBadGateway, http.Header{}, "")
	if err == nil || err.Error() != "StatusCode error (502 GET https://fixture.invalid/value)" {
		t.Fatalf("status error = %v", err)
	}
	result, err := executeResponse(http.StatusOK, http.Header{
		"Content-Type": []string{"application/octet-stream"},
	}, "opaque")
	if err != nil || result.Output != "opaque" {
		t.Fatalf("unsupported MIME result = %#v, error = %v", result, err)
	}
	_, err = executeResponse(http.StatusOK, http.Header{
		"Content-Length": []string{"5242881"},
	}, "")
	if err == nil || err.Error() != "Response too large (exceeds 5MB limit)" {
		t.Fatalf("size error = %v", err)
	}
}

func TestWebFetchRedirectBoundaryWithoutSocket(t *testing.T) {
	resolver := func(_ context.Context, host string) ([]net.IP, error) {
		addresses := map[string]string{
			"public-origin.test":  "203.0.113.10",
			"public-next.test":    "198.51.100.20",
			"public-final.test":   "192.0.2.30",
			"private-origin.test": "10.0.0.10",
			"private-next.test":   "127.0.0.2",
		}
		return []net.IP{net.ParseIP(addresses[host])}, nil
	}
	redirectClient := func(routes map[string]string) *http.Client {
		return &http.Client{Transport: webRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if destination := routes[request.URL.String()]; destination != "" {
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": []string{destination}},
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    request,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("redirected")),
				Request:    request,
			}, nil
		})}
	}
	execute := func(origin string, routes map[string]string) (steploop.ToolResult, error) {
		ctx := WithWebHTTPClient(context.Background(), redirectClient(routes))
		ctx = WithWebHostResolver(ctx, resolver)
		ctx = WithWebOutputDir(ctx, t.TempDir())
		return executeWebTest(t, New(t.TempDir()), ctx, "webfetch", map[string]any{
			"url": origin, "format": "text",
		})
	}

	publicOrigin := "https://public-origin.test/start"
	publicNext := "https://public-next.test/next"
	publicFinal := "https://public-final.test/final"
	result, err := execute(publicOrigin, map[string]string{
		publicOrigin: publicNext,
		publicNext:   publicFinal,
	})
	if err != nil || result.Output != "redirected" {
		t.Fatalf("public redirect chain = (%#v, %v)", result, err)
	}

	for _, destination := range []string{
		"http://127.0.0.1/private",
		"http://169.254.169.254/latest/meta-data",
	} {
		_, err := execute(publicOrigin, map[string]string{publicOrigin: destination})
		want := "Transport error (GET " + destination + ")"
		if err == nil || err.Error() != want {
			t.Errorf("redirect to %s error = %v, want %q", destination, err, want)
		}
	}

	privateOrigin := "http://private-origin.test/start"
	privateNext := "http://private-next.test/inside"
	result, err = execute(privateOrigin, map[string]string{privateOrigin: privateNext})
	if err != nil || result.Output != "redirected" {
		t.Fatalf("private-origin redirect = (%#v, %v)", result, err)
	}
}
