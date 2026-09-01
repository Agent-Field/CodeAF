package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

type firecrawlTestCall struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

func writeFirecrawlReply(t *testing.T, w http.ResponseWriter, sse bool, content any) {
	t.Helper()
	text, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	writeFirecrawlTextReply(t, w, sse, string(text))
}

func writeFirecrawlTextReply(t *testing.T, w http.ResponseWriter, sse bool, text string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{"content": []map[string]any{
			{"type": "text", "text": text},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sse {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func TestFirecrawlSearchUsesTheKeylessSnippetContract(t *testing.T) {
	// V1 and V4: The zero-key default sends one stateless snippet-only MCP call
	// and maps the SSE reply in result order.
	serve(t, &firecrawlMCPURL, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Accept"); got != "application/json, text/event-stream" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("keyless Authorization = %q", got)
		}
		var call firecrawlTestCall
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.JSONRPC != "2.0" || call.Method != "tools/call" || call.Params.Name != "firecrawl_search" {
			t.Errorf("call = %+v", call)
		}
		if call.Params.Arguments["query"] != "go release notes" || call.Params.Arguments["limit"] != float64(2) {
			t.Errorf("arguments = %#v", call.Params.Arguments)
		}
		if _, found := call.Params.Arguments["scrapeOptions"]; found {
			t.Errorf("snippet search requested page content: %#v", call.Params.Arguments)
		}
		writeFirecrawlReply(t, w, true, map[string]any{
			"success": true,
			"data": map[string]any{"web": []map[string]any{
				{"url": "https://go.dev/doc/go1.25", "title": "Go 1.25", "description": "Release notes."},
				{"url": "https://go.dev/blog/go1.25", "title": "Go blog", "description": "The announcement."},
			}},
		})
	})

	provider := firecrawlSearch{}.Bind(Options{})
	results, err := provider.Search(context.Background(), "go release notes", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Title: "Go 1.25", URL: "https://go.dev/doc/go1.25", Snippet: "Release notes."},
		{Title: "Go blog", URL: "https://go.dev/blog/go1.25", Snippet: "The announcement."},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("results = %#v, want %#v", results, want)
	}
}

func TestFirecrawlSearchClampsTheRequestedLimit(t *testing.T) {
	// V4: The default search pays only for the bounded number of snippets the
	// shared search ladder permits.
	var got float64
	serve(t, &firecrawlMCPURL, func(w http.ResponseWriter, r *http.Request) {
		var call firecrawlTestCall
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		got, _ = call.Params.Arguments["limit"].(float64)
		writeFirecrawlReply(t, w, false, map[string]any{
			"success": true, "data": map[string]any{"web": []any{}},
		})
	})
	provider := firecrawlSearch{}.Bind(Options{})
	for _, test := range []struct{ asked, want int }{
		{0, defaultLimit},
		{-1, defaultLimit},
		{1000, maxLimit},
	} {
		if _, err := provider.Search(context.Background(), "q", test.asked); err != nil {
			t.Fatal(err)
		}
		if got != float64(test.want) {
			t.Errorf("limit %d sent %v, want %d", test.asked, got, test.want)
		}
	}
}

func TestFirecrawlBearerHeaderIsOptional(t *testing.T) {
	// V3: A key raises Firecrawl's ceiling and is sent only when present; it
	// never decides whether the search plug exists.
	for _, test := range []struct {
		name string
		key  string
		want string
	}{
		{name: "keyless"},
		{name: "keyed", key: "  fc-secret  ", want: "Bearer fc-secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			serve(t, &firecrawlMCPURL, func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != test.want {
					t.Errorf("Authorization = %q, want %q", got, test.want)
				}
				writeFirecrawlReply(t, w, false, map[string]any{
					"success": true, "data": map[string]any{"web": []any{}},
				})
			})
			provider := firecrawlSearch{}.Bind(Options{FirecrawlKey: test.key})
			if _, err := provider.Search(context.Background(), "q", 1); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFirecrawlAcceptsBareJSON(t *testing.T) {
	// V1: The default accepts Firecrawl's bare-JSON framing as well as its SSE
	// framing, so transport negotiation cannot take search away.
	serve(t, &firecrawlMCPURL, func(w http.ResponseWriter, _ *http.Request) {
		writeFirecrawlReply(t, w, false, map[string]any{
			"success": true,
			"data": map[string]any{"web": []map[string]any{
				{"url": "https://example.com", "title": "Example", "description": "Bare JSON."},
			}},
		})
	})
	results, err := (firecrawlSearch{}).Bind(Options{}).Search(context.Background(), "q", 1)
	if err != nil || len(results) != 1 || results[0].Title != "Example" {
		t.Fatalf("bare JSON results = (%#v, %v)", results, err)
	}
}

func TestFirecrawlFailuresAreCleanErrors(t *testing.T) {
	// V1: Every backend refusal is a normal Go error for the belt to turn into
	// Search failed (firecrawl), never a panic or malformed result.
	tests := []struct {
		name      string
		body      string
		code      int
		want      string
		notWant   string
		wantCount int
	}{
		{
			name:      "json-rpc error",
			body:      `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"MCP error -32602: Tool 'firecrawl_search' parameter validation failed: query is too small"}}`,
			want:      "query is too small",
			wantCount: 1,
		},
		{
			name: "unsuccessful tool result",
			body: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"success\":false,\"error\":\"rate limited\"}"}]}}`,
			want: "rate limited",
		},
		{name: "empty reply", want: "firecrawl answered nothing", notWant: "MCP"},
		{
			name:    "unknown reply shape",
			body:    `{"jsonrpc":"2.0","id":1,"result":{}}`,
			want:    "firecrawl answered in a shape this build does not know",
			notWant: "MCP",
		},
		{
			name:    "no readable text",
			body:    `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"image","text":"bytes"}]}}`,
			want:    "firecrawl answered without readable text",
			notWant: "MCP",
		},
		{name: "http status", code: http.StatusInternalServerError, body: "unavailable", want: "500"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serve(t, &firecrawlMCPURL, func(w http.ResponseWriter, _ *http.Request) {
				if test.code != 0 {
					w.WriteHeader(test.code)
				}
				_, _ = fmt.Fprint(w, test.body)
			})
			_, err := (firecrawlSearch{}).Bind(Options{}).Search(context.Background(), "q", 1)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to contain %q", err, test.want)
			}
			if test.notWant != "" && strings.Contains(err.Error(), test.notWant) {
				t.Fatalf("error = %q, do not want %q", err, test.notWant)
			}
			if test.wantCount > 0 && strings.Count(err.Error(), "MCP error -32602") != test.wantCount {
				t.Fatalf("error = %q, want one upstream error prefix", err)
			}
		})
	}
}

func TestFirecrawlFetchReturnsMarkdownWhenKeyed(t *testing.T) {
	// V3: Fetch is the keyed Firecrawl upgrade and asks the scrape tool for one
	// main-content Markdown page.
	serve(t, &firecrawlMCPURL, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fc-secret" {
			t.Errorf("Authorization = %q", got)
		}
		var call firecrawlTestCall
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name != "firecrawl_scrape" || call.Params.Arguments["url"] != "https://go.dev" {
			t.Errorf("call = %+v", call)
		}
		if !reflect.DeepEqual(call.Params.Arguments["formats"], []any{"markdown"}) || call.Params.Arguments["onlyMainContent"] != true {
			t.Errorf("scrape arguments = %#v", call.Params.Arguments)
		}
		writeFirecrawlReply(t, w, true, map[string]any{
			"markdown": "# Go\n\nBuild simple things.",
		})
	})

	prototype := firecrawlFetch{}
	if prototype.Available(Options{}) {
		t.Fatal("unkeyed Firecrawl fetch is available")
	}
	fetcher := prototype.Bind(Options{FirecrawlKey: "fc-secret"})
	text, err := fetcher.Fetch(context.Background(), "https://go.dev")
	if err != nil || text != "# Go\n\nBuild simple things." {
		t.Fatalf("Fetch = (%q, %v)", text, err)
	}
}

func TestFirecrawlFetchKeepsBraceLeadingPageText(t *testing.T) {
	// Review finding 3: A raw page that begins with a brace is still page text;
	// only recognized scrape-envelope fields may change how it is decoded.
	for index, page := range []string{
		`{"name":"pkg","version":"1.2.3"}`,
		"{ this is a code block\n  not json }",
	} {
		t.Run(fmt.Sprintf("page-%d", index+1), func(t *testing.T) {
			serve(t, &firecrawlMCPURL, func(w http.ResponseWriter, _ *http.Request) {
				writeFirecrawlTextReply(t, w, true, page)
			})
			text, err := (firecrawlFetch{}).Bind(Options{FirecrawlKey: "fc-secret"}).Fetch(
				context.Background(), "https://example.com/page",
			)
			if err != nil || text != page {
				t.Fatalf("Fetch = (%q, %v), want raw page %q", text, err, page)
			}
		})
	}
}

func TestFirecrawlLiveSearch(t *testing.T) {
	// V1: The shipped keyless endpoint returns a real result without a key.
	if os.Getenv("AFORGE_LIVE_FIRECRAWL") != "1" {
		t.Skip("set AFORGE_LIVE_FIRECRAWL=1 to call the keyless endpoint")
	}
	results, err := (firecrawlSearch{}).Bind(Options{}).Search(
		context.Background(), "Go programming language release notes", 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("live Firecrawl search returned no results")
	}
	t.Logf("first result title: %s", results[0].Title)
}
