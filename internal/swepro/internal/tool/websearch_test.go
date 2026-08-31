package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func clearWebSearchFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"CODEAF_EXPERIMENTAL", "CODEAF_ENABLE_EXA", "CODEAF_EXPERIMENTAL_EXA",
		"CODEAF_ENABLE_PARALLEL", "CODEAF_EXPERIMENTAL_PARALLEL",
	} {
		t.Setenv(name, "")
	}
}

func TestWebSearchFullExecutionWithoutAPIKey(t *testing.T) {
	// V7: An openrouter caller with no flags or keys reaches Firecrawl, sends
	// the minimum-credit request, and receives rendered results with Firecrawl
	// named in the title and metadata.
	t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "")
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("EXA_API_KEY", "")
	clearWebSearchFlags(t)
	server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" {
			t.Errorf("keyless Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Accept") != "application/json, text/event-stream" {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		var payload struct {
			JSONRPC string `json:"jsonrpc"`
			Method  string `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.JSONRPC != "2.0" || payload.Method != "tools/call" || payload.Params.Name != "firecrawl_search" {
			t.Errorf("payload = %#v", payload)
		}
		if payload.Params.Arguments["query"] != "go tools" || payload.Params.Arguments["limit"] != float64(8) {
			t.Errorf("arguments = %#v", payload.Params.Arguments)
		}
		if _, found := payload.Params.Arguments["scrapeOptions"]; found {
			t.Errorf("default search requested page content: %#v", payload.Params.Arguments)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"result":{"content":[{"type":"text","text":"{\"success\":true,\"data\":{\"web\":[{\"title\":\"Go tools\",\"url\":\"https://go.dev/doc\",\"description\":\"Go documentation.\"}]}}"}]}}`)
	}))
	ctx := WithWebHTTPClient(context.Background(), server.Client())
	ctx = WithWebSearchEndpoints(
		ctx, "http://127.0.0.1:1/unexpected-exa", "http://127.0.0.1:1/unexpected-parallel", server.URL,
	)
	ctx = WithWebOutputDir(ctx, t.TempDir())
	result, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "go tools"})
	if err != nil {
		t.Fatal(err)
	}
	wantOutput := "1. Go tools — https://go.dev/doc\n   Go documentation."
	if result.Output != wantOutput || result.Title != "Firecrawl Web Search: go tools" {
		t.Fatalf("result = %#v", result)
	}
	if string(result.Metadata) != `{"provider":"firecrawl","truncated":false}` {
		t.Fatalf("Metadata = %s", result.Metadata)
	}
}

func TestFirecrawlWebSearchSendsOptionalBearer(t *testing.T) {
	// V7: FIRECRAWL_API_KEY raises the keyless default's ceiling by adding one
	// bearer header; it does not select or unlock the provider.
	t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "")
	t.Setenv("FIRECRAWL_API_KEY", "fc-secret")
	clearWebSearchFlags(t)
	client := &http.Client{Transport: webRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer fc-secret" {
			t.Errorf("Authorization = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"result":{"content":[{"type":"text","text":"{\"success\":true,\"data\":{\"web\":[]}}"}]}}`,
			)),
			Request: request,
		}, nil
	})}
	ctx := WithWebHTTPClient(context.Background(), client)
	ctx = WithWebSearchEndpoints(
		ctx, "http://127.0.0.1:1/unexpected-exa", "http://127.0.0.1:1/unexpected-parallel", "https://firecrawl.test/mcp",
	)
	ctx = WithWebOutputDir(ctx, t.TempDir())
	if _, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "fixture"}); err != nil {
		t.Fatal(err)
	}
}

func TestFirecrawlPreferredLivecrawlRequestsAndCapsContent(t *testing.T) {
	// V7: Firecrawl requests paid page content only for livecrawl: preferred,
	// then caps each Markdown body by runes before rendering it.
	t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "")
	t.Setenv("FIRECRAWL_API_KEY", "")
	clearWebSearchFlags(t)
	server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		scrape, ok := payload.Params.Arguments["scrapeOptions"].(map[string]any)
		if !ok || scrape["onlyMainContent"] != true {
			t.Fatalf("scrapeOptions = %#v", payload.Params.Arguments["scrapeOptions"])
		}
		formats, _ := scrape["formats"].([]any)
		if len(formats) != 1 || formats[0] != "markdown" {
			t.Fatalf("formats = %#v", formats)
		}
		_, _ = io.WriteString(writer, `{"result":{"content":[{"type":"text","text":"{\"success\":true,\"data\":{\"web\":[{\"title\":\"Unicode\",\"url\":\"https://example.com\",\"description\":\"A page.\",\"markdown\":\"éclair and more\"}]}}"}]}}`)
	}))
	ctx := WithWebHTTPClient(context.Background(), server.Client())
	ctx = WithWebSearchEndpoints(
		ctx, "http://127.0.0.1:1/unexpected-exa", "http://127.0.0.1:1/unexpected-parallel", server.URL,
	)
	ctx = WithWebOutputDir(ctx, t.TempDir())
	result, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{
		"query": "unicode", "livecrawl": "preferred", "contextMaxCharacters": 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "1. Unicode — https://example.com\n   A page.\n\nécla"
	if result.Output != want {
		t.Fatalf("Output = %q, want %q", result.Output, want)
	}

	// Review findings 1 and 10: The model's JSON number is bounded before its
	// float-to-int conversion, so a value beyond MaxInt64 cannot become a
	// negative rune-slice bound and crash the tool loop.
	result, err = executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{
		"query": "unicode", "livecrawl": "preferred", "contextMaxCharacters": 1e30,
	})
	if err != nil {
		t.Fatal(err)
	}
	want = "1. Unicode — https://example.com\n   A page.\n\néclair and more"
	if result.Output != want {
		t.Fatalf("huge content cap Output = %q, want %q", result.Output, want)
	}
}

func TestWebSearchFullExecutionWithoutSocket(t *testing.T) {
	t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "exa")
	client := &http.Client{Transport: webRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.Header.Get("Accept") != "application/json, text/event-stream" {
			t.Errorf("request = %s, Accept = %q", request.Method, request.Header.Get("Accept"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"result":{"content":[{"type":"text","text":"fixture result"}]}}`,
			)),
			Request: request,
		}, nil
	})}
	ctx := WithWebHTTPClient(context.Background(), client)
	ctx = WithWebSearchEndpoints(ctx, "https://fixture.invalid/mcp", "", "")
	ctx = WithWebOutputDir(ctx, t.TempDir())
	result, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "fixture"})
	if err != nil || result.Output != "fixture result" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestParallelWebSearchSendsModelNameAndFinalMetadata(t *testing.T) {
	t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "parallel")
	client := &http.Client{Transport: webRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload struct {
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Params.Arguments["model_name"] != "fixture/model" {
			t.Errorf("parallel arguments = %#v", payload.Params.Arguments)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"result":{"content":[{"type":"text","text":"parallel result"}]}}`,
			)),
			Request: request,
		}, nil
	})}
	ctx := WithWebHTTPClient(context.Background(), client)
	ctx = WithWebSearchEndpoints(ctx, "", "https://parallel.test/mcp", "")
	ctx = WithWebOutputDir(ctx, t.TempDir())
	result, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "fixture"})
	if err != nil || result.Output != "parallel result" || string(result.Metadata) != `{"provider":"parallel","truncated":false}` {
		t.Fatalf("parallel result = (%#v, %v)", result, err)
	}
}

func TestWebSearchResponseIsCappedAtFiveMiB(t *testing.T) {
	t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "exa")
	client := &http.Client{Transport: webRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", webSearchMaxResponseSize+1))),
			Request:    request,
		}, nil
	})}
	ctx := WithWebHTTPClient(context.Background(), client)
	ctx = WithWebSearchEndpoints(ctx, "https://exa.test/mcp", "", "")
	_, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "large"})
	if err == nil || err.Error() != "Response too large (exceeds 5MB limit)" {
		t.Fatalf("oversized search error = %v", err)
	}
}

func TestWebSearchRegistrationAndDescriptions(t *testing.T) {
	// V7: The keyless Firecrawl back end puts websearch on every coder belt,
	// including an openrouter caller with no feature flags.
	registry := New(t.TempDir())
	definitions := registry.Definitions()
	all := definitionNames(definitions)
	if !containsName(all, "webfetch") || !containsName(all, "websearch") {
		t.Fatalf("Definitions = %v", all)
	}
	withoutBackend := definitionNames(registry.DefinitionsFor(FilterInput{
		ProviderID: "openrouter", ModelID: "claude", AgentName: "coder",
	}))
	if !containsName(withoutBackend, "webfetch") || !containsName(withoutBackend, "websearch") {
		t.Fatalf("keyless openrouter belt = %v", withoutBackend)
	}
	for _, definition := range definitions {
		switch definition.Provider.Name {
		case "webfetch":
			if definition.Provider.Description != webFetchDescription {
				t.Fatal("webfetch description differs from embedded bytes")
			}
		case "websearch":
			if definition.Provider.Description != webSearchDescription() {
				t.Fatal("websearch description did not substitute the current year")
			}
		}
	}
}

func TestCurrentWebSearchFlags(t *testing.T) {
	clearWebSearchFlags(t)
	if got := CurrentWebSearchFlags(); got != (WebSearchFlags{}) {
		t.Fatalf("empty flags = %#v", got)
	}
	t.Setenv("CODEAF_EXPERIMENTAL", "TRUE")
	if got := CurrentWebSearchFlags(); !got.Exa || got.Parallel {
		t.Fatalf("experimental flags = %#v", got)
	}
	t.Setenv("CODEAF_EXPERIMENTAL", "")
	t.Setenv("CODEAF_EXPERIMENTAL_PARALLEL", "1")
	if got := CurrentWebSearchFlags(); got.Exa || !got.Parallel {
		t.Fatalf("parallel alias flags = %#v", got)
	}
}

func TestWebSearchProviderAndResponseParity(t *testing.T) {
	t.Run("encodeURIComponent API key", func(t *testing.T) {
		input := "a b!~*'()+/?=:&"
		if got, want := encodeURIComponent(input), "a%20b!~*'()%2B%2F%3F%3D%3A%26"; got != want {
			t.Fatalf("encoded = %q, want %q", got, want)
		}
	})
	t.Run("provider priority", func(t *testing.T) {
		// V7: The normalized override wins, then the existing flag order wins,
		// and session identity never splits the keyless default.
		for _, test := range []struct {
			override string
			want     string
		}{
			{"exa", "exa"},
			{"parallel", "parallel"},
			{"FIRECRAWL", "firecrawl"},
			{"  Firecrawl  ", "firecrawl"},
		} {
			t.Setenv("CODEAF_WEBSEARCH_PROVIDER", test.override)
			if got := selectWebSearchProvider("session-a", WebSearchFlags{}); got != test.want {
				t.Errorf("override %q selected %q, want %q", test.override, got, test.want)
			}
		}
		t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "")
		if got := selectWebSearchProvider("session", WebSearchFlags{Exa: true, Parallel: true}); got != "parallel" {
			t.Fatalf("both flags selected %q, want parallel", got)
		}
		if got := selectWebSearchProvider("session", WebSearchFlags{Exa: true}); got != "exa" {
			t.Fatalf("Exa flag selected %q", got)
		}
		for _, sessionID := range []string{"session-a", "session-b"} {
			if got := selectWebSearchProvider(sessionID, WebSearchFlags{}); got != "firecrawl" {
				t.Errorf("%s selected %q, want firecrawl", sessionID, got)
			}
		}
	})
	t.Run("SSE", func(t *testing.T) {
		got, err := parseMCPWebSearchResponse("event: message\ndata: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"from sse\"}]}}\n")
		if err != nil || got != "from sse" {
			t.Fatalf("parse = (%q, %v)", got, err)
		}
	})
	t.Run("empty fallback", func(t *testing.T) {
		t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "exa")
		server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, `{"result":{"content":[]}}`)
		}))
		ctx := WithWebHTTPClient(context.Background(), server.Client())
		ctx = WithWebSearchEndpoints(ctx, server.URL, "", "")
		result, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "none"})
		if err != nil || result.Output != "No search results found. Please try a different query." {
			t.Fatalf("result = %#v, error = %v", result, err)
		}
	})
	t.Run("non-2xx", func(t *testing.T) {
		t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "exa")
		server := newLocalWebServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusUnauthorized)
		}))
		ctx := WithWebHTTPClient(context.Background(), server.Client())
		ctx = WithWebSearchEndpoints(ctx, server.URL, "", "")
		_, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "denied"})
		want := "StatusCode error (401 POST " + server.URL + ")"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})
	t.Run("connection refused shape", func(t *testing.T) {
		t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "exa")
		client := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: connection refused")
		})}
		ctx := WithWebHTTPClient(context.Background(), client)
		ctx = WithWebSearchEndpoints(ctx, "http://127.0.0.1:1/mcp", "", "")
		_, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "offline"})
		if err == nil || err.Error() != "Transport error (POST http://127.0.0.1:1/mcp)" {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("non-2xx without socket", func(t *testing.T) {
		t.Setenv("CODEAF_WEBSEARCH_PROVIDER", "exa")
		client := &http.Client{Transport: webRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusForbidden, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader("denied")), Request: request,
			}, nil
		})}
		ctx := WithWebHTTPClient(context.Background(), client)
		ctx = WithWebSearchEndpoints(ctx, "https://fixture.invalid/mcp", "", "")
		_, err := executeWebTest(t, New(t.TempDir()), ctx, "websearch", map[string]any{"query": "denied"})
		if err == nil || err.Error() != "StatusCode error (403 POST https://fixture.invalid/mcp)" {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("malformed MCP response", func(t *testing.T) {
		_, err := parseMCPWebSearchResponse(`{"result":{}}`)
		if err == nil || !strings.Contains(err.Error(), "invalid MCP response") {
			t.Fatalf("error = %v", err)
		}
	})
}
