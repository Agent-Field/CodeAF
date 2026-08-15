package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every test here talks to an httptest server. Nothing in this file reaches
// the network: the endpoints are package vars precisely so they can be pointed
// somewhere local, and a test that hit the real Exa or DuckDuckGo would be a
// test that fails on a plane.

// serve stands up a test server, points the given endpoint var at it for the
// duration of the test, and hands back the client to configure Options with.
func serve(t *testing.T, endpoint *string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	saved := *endpoint
	*endpoint = server.URL
	t.Cleanup(func() {
		*endpoint = saved
		server.Close()
	})
	return server
}

// restore snapshots the registries and puts them back when the test ends.
// Registration is global by design — plugs come from init functions — so a
// test that empties or adds to a registry has to clean up after itself.
func restore(t *testing.T) {
	t.Helper()
	registryMu.RLock()
	savedSearch := append([]Provider(nil), searchReg...)
	savedFetch := append([]Fetcher(nil), fetchReg...)
	registryMu.RUnlock()
	t.Cleanup(func() {
		registryMu.Lock()
		searchReg, fetchReg = savedSearch, savedFetch
		registryMu.Unlock()
	})
}

// --- exa ------------------------------------------------------------------

func TestExaSearchRequestShape(t *testing.T) {
	var (
		gotMethod string
		gotKey    string
		gotType   string
		gotBody   exaSearchRequest
	)
	serve(t, &exaSearchURL, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotKey = r.Header.Get("x-api-key")
		gotType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("request body is not the documented JSON: %v (%s)", err, raw)
		}
		fmt.Fprint(w, `{"results":[
			{"title":"Go 1.25 released","url":"https://go.dev/blog/go1.25","text":"The Go team\n  is pleased…","publishedDate":"2025-08-12"},
			{"title":"No link here","url":"","text":"dropped"},
			{"title":"Release notes","url":"https://go.dev/doc/go1.25"}
		]}`)
	})

	provider := exaSearch{}.Bind(Options{ExaKey: "sk-test"})
	results, err := provider.Search(context.Background(), "go 1.25", 3)
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotKey != "sk-test" {
		t.Errorf("x-api-key = %q, want sk-test", gotKey)
	}
	if gotType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotType)
	}
	if gotBody.Query != "go 1.25" {
		t.Errorf("body.query = %q, want %q", gotBody.Query, "go 1.25")
	}
	if gotBody.NumResults != 3 {
		t.Errorf("body.numResults = %d, want 3", gotBody.NumResults)
	}
	if gotBody.Contents.Text.MaxCharacters != exaTextChars {
		t.Errorf("body.contents.text.maxCharacters = %d, want %d", gotBody.Contents.Text.MaxCharacters, exaTextChars)
	}

	// Mapping: text becomes the snippet with its newlines folded,
	// publishedDate becomes Published, and a hit with no URL is dropped
	// because nothing can follow it.
	want := []Result{
		{Title: "Go 1.25 released", URL: "https://go.dev/blog/go1.25", Snippet: "The Go team is pleased…", Published: "2025-08-12"},
		{Title: "Release notes", URL: "https://go.dev/doc/go1.25"},
	}
	if len(results) != len(want) {
		t.Fatalf("Search returned %d results, want %d: %+v", len(results), len(want), results)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, results[i], want[i])
		}
	}
}

func TestExaSearchDefaultAndClampedLimit(t *testing.T) {
	var got exaSearchRequest
	serve(t, &exaSearchURL, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		fmt.Fprint(w, `{"results":[]}`)
	})

	provider := exaSearch{}.Bind(Options{ExaKey: "sk-test"})
	for _, test := range []struct{ asked, want int }{
		{asked: 0, want: defaultLimit},
		{asked: -4, want: defaultLimit},
		{asked: 1000, want: maxLimit},
	} {
		if _, err := provider.Search(context.Background(), "q", test.asked); err != nil {
			t.Fatalf("Search(limit=%d) error = %v", test.asked, err)
		}
		if got.NumResults != test.want {
			t.Errorf("Search(limit=%d) asked for %d, want %d", test.asked, got.NumResults, test.want)
		}
	}
}

func TestExaSearchErrorStatus(t *testing.T) {
	serve(t, &exaSearchURL, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid api key"}`)
	})

	provider := exaSearch{}.Bind(Options{ExaKey: "sk-wrong"})
	_, err := provider.Search(context.Background(), "go 1.25", 3)
	if err == nil {
		t.Fatal("Search on a 401 returned no error")
	}
	// The status is the diagnosis; the body says which kind of 401.
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("Search error = %v, want the status and the body in it", err)
	}
}

func TestExaWithoutKeySaysSo(t *testing.T) {
	// Only reachable through an explicit pin, which Resolve honours. The
	// error must name the missing key rather than let the vendor say 401.
	if _, err := (exaSearch{}).Search(context.Background(), "q", 3); err == nil || !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("unkeyed exa Search error = %v, want a missing-key error", err)
	}
	if _, err := (exaFetch{}).Fetch(context.Background(), "https://go.dev"); err == nil || !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("unkeyed exa Fetch error = %v, want a missing-key error", err)
	}
}

func TestExaFetch(t *testing.T) {
	var got exaContentsRequest
	serve(t, &exaContentsURL, func(w http.ResponseWriter, r *http.Request) {
		if key := r.Header.Get("x-api-key"); key != "sk-test" {
			t.Errorf("x-api-key = %q, want sk-test", key)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		fmt.Fprint(w, `{"results":[{"url":"https://go.dev","text":"# Go\n\nBuild simple things."}]}`)
	})

	fetcher := exaFetch{}.Bind(Options{ExaKey: "sk-test"})
	text, err := fetcher.Fetch(context.Background(), "https://go.dev")
	if err != nil {
		t.Fatalf("Fetch error = %v", err)
	}
	if len(got.URLs) != 1 || got.URLs[0] != "https://go.dev" || !got.Text {
		t.Errorf("contents request = %+v, want the one url with text:true", got)
	}
	if text != "# Go\n\nBuild simple things." {
		t.Errorf("Fetch = %q, want the body verbatim", text)
	}
}

func TestExaFetchEmptyContents(t *testing.T) {
	serve(t, &exaContentsURL, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[]}`)
	})
	fetcher := exaFetch{}.Bind(Options{ExaKey: "sk-test"})
	if _, err := fetcher.Fetch(context.Background(), "https://go.dev"); err == nil {
		t.Fatal("Fetch of a page exa has no content for returned no error")
	}
}

// --- duckduckgo -----------------------------------------------------------

// ddgFixture is html.duckduckgo.com's no-JavaScript results page, trimmed to
// the structure this plug reads: a sponsored block first (y.js, which must be
// dropped), then three results — one wrapped in the /l/?uddg= redirect, one
// linked directly, one with no snippet of its own.
const ddgFixture = `<!DOCTYPE html><html><head><title>go 1.25 at DuckDuckGo</title></head>
<body>
<div id="links" class="results">

  <div class="result results_links results_links_deep result--ad">
    <div class="links_main links_deep result__body">
      <h2 class="result__title">
        <a rel="nofollow" class="result__a" href="//duckduckgo.com/y.js?ad_provider=bing&amp;u3=https%3A%2F%2Fads.example">Learn Go &amp; Cloud Today</a>
      </h2>
      <a class="result__snippet" href="//duckduckgo.com/y.js">Sponsored training courses.</a>
    </div>
  </div>

  <div class="result results_links results_links_deep web-result">
    <div class="links_main links_deep result__body">
      <h2 class="result__title">
        <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2Fgo1.25&amp;rut=9c1f">Go 1.25 Release Notes &amp; <b>Changes</b></a>
      </h2>
      <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2Fgo1.25">Go <b>1.25</b> arrives with
        a rewritten garbage collector &amp; a faster linker.</a>
    </div>
  </div>

  <div class="result results_links results_links_deep web-result">
    <div class="links_main links_deep result__body">
      <h2 class="result__title">
        <a href="https://tip.golang.org/doc/go1.25" class="result__a" rel="nofollow">Draft notes — tip.golang.org</a>
      </h2>
      <a class="result__snippet" href="https://tip.golang.org/doc/go1.25">The in-progress draft.</a>
    </div>
  </div>

  <div class="result results_links results_links_deep web-result">
    <div class="links_main links_deep result__body">
      <h2 class="result__title">
        <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fnews.example%2Fgo125">Go 1.25 on the news</a>
      </h2>
    </div>
  </div>

</div>
</body></html>`

func TestDuckDuckGoParsesTheResultsPage(t *testing.T) {
	var (
		gotQuery string
		gotAgent string
	)
	serve(t, &ddgURL, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotAgent = r.Header.Get("User-Agent")
		fmt.Fprint(w, ddgFixture)
	})

	provider := duckDuckGo{}.Bind(Options{})
	results, err := provider.Search(context.Background(), "go 1.25 release", 10)
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}

	if gotQuery != "go 1.25 release" {
		t.Errorf("q = %q, want the query", gotQuery)
	}
	if !strings.HasPrefix(gotAgent, "Mozilla/") {
		t.Errorf("User-Agent = %q, want a browser agent (the endpoint blocks Go's default)", gotAgent)
	}

	want := []Result{
		{
			// Redirect unwrapped, entities decoded, <b> highlighting
			// stripped, the snippet's source newline folded away.
			Title:   "Go 1.25 Release Notes & Changes",
			URL:     "https://go.dev/doc/go1.25",
			Snippet: "Go 1.25 arrives with a rewritten garbage collector & a faster linker.",
		},
		{
			// A direct href, and class after href — attribute order must
			// not matter.
			Title:   "Draft notes — tip.golang.org",
			URL:     "https://tip.golang.org/doc/go1.25",
			Snippet: "The in-progress draft.",
		},
		{
			// No snippet: the positional pairing must not borrow the
			// previous result's, and must not shift anything.
			Title: "Go 1.25 on the news",
			URL:   "https://news.example/go125",
		},
	}
	if len(results) != len(want) {
		t.Fatalf("Search returned %d results, want %d (the ad must be dropped): %+v", len(results), len(want), results)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("result %d =\n  %+v\nwant\n  %+v", i, results[i], want[i])
		}
	}
}

func TestDuckDuckGoRespectsLimit(t *testing.T) {
	serve(t, &ddgURL, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, ddgFixture) })

	provider := duckDuckGo{}.Bind(Options{})
	results, err := provider.Search(context.Background(), "go", 2)
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search(limit=2) returned %d results", len(results))
	}
}

func TestDuckDuckGoMalformedHTMLIsACleanError(t *testing.T) {
	// The day the classes are renamed — or a challenge page is served — the
	// plug must say so, not return silence that reads as "nothing matched
	// your query".
	tests := []struct {
		name string
		page string
	}{
		{name: "layout changed", page: `<html><body><div class="serp__results">no anchors we know</div></body></html>`},
		{name: "challenge page", page: `<html><body><h1>Unfortunately, bots use DuckDuckGo too.</h1></body></html>`},
		{name: "truncated", page: `<div class="result"><h2 class="result__title"><a rel="nofollow" class="result_`},
		{name: "empty", page: ``},
		{name: "links we cannot use", page: `<a class="result__a" href="//duckduckgo.com/y.js?u3=x">Ad</a>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serve(t, &ddgURL, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, test.page) })

			provider := duckDuckGo{}.Bind(Options{})
			results, err := provider.Search(context.Background(), "go", 5)
			if err == nil {
				t.Fatalf("Search on %s HTML returned %d results and no error", test.name, len(results))
			}
			if !strings.HasPrefix(err.Error(), "duckduckgo: ") {
				t.Fatalf("Search error = %v, want it named by the plug", err)
			}
		})
	}
}

func TestDuckDuckGoErrorStatus(t *testing.T) {
	serve(t, &ddgURL, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, ddgFixture) // a body that would parse, on a status that must not
	})

	provider := duckDuckGo{}.Bind(Options{})
	_, err := provider.Search(context.Background(), "go", 5)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("Search error = %v, want the 429 in it", err)
	}
}

// --- jina -----------------------------------------------------------------

func TestJinaFetch(t *testing.T) {
	const page = "# Go 1.25\n\nThe release notes, as markdown."
	var (
		gotPath string
		gotAuth string
	)
	server := serve(t, &jinaBaseURL, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, page)
	})
	jinaBaseURL = server.URL + "/"

	fetcher := jina{}.Bind(Options{})
	text, err := fetcher.Fetch(context.Background(), "https://go.dev/doc/go1.25?x=1")
	if err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	// The target is appended verbatim — r.jina.ai's route IS the literal URL,
	// so escaping it would ask for a page whose name contains %3A%2F%2F.
	if want := "/https://go.dev/doc/go1.25?x=1"; gotPath != want {
		t.Errorf("request = %q, want %q", gotPath, want)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none without a key", gotAuth)
	}
	if text != page {
		t.Errorf("Fetch = %q, want the body passed through", text)
	}

	// A key only raises the rate ceiling, but when present it must be sent.
	if _, err := (jina{}).Bind(Options{JinaKey: "jina-key"}).Fetch(context.Background(), "https://go.dev"); err != nil {
		t.Fatalf("keyed Fetch error = %v", err)
	}
	if gotAuth != "Bearer jina-key" {
		t.Errorf("Authorization = %q, want Bearer jina-key", gotAuth)
	}
}

func TestJinaFetchRejectsNonURLs(t *testing.T) {
	// Caught here rather than at the service, which would concatenate and
	// return a confusing 404 for a page on jina itself.
	for _, target := range []string{"", "   ", "go.dev/doc", "/etc/passwd", "ftp://go.dev", "::not a url"} {
		if _, err := (jina{}).Fetch(context.Background(), target); err == nil {
			t.Errorf("Fetch(%q) returned no error", target)
		}
	}
}

func TestJinaFetchErrorStatus(t *testing.T) {
	server := serve(t, &jinaBaseURL, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "rate limit exceeded")
	})
	jinaBaseURL = server.URL + "/"

	_, err := jina{}.Bind(Options{}).Fetch(context.Background(), "https://go.dev")
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("Fetch error = %v, want the status and the body in it", err)
	}
}

// TestJinaFetchIsCappedByTheRenderer: Fetch returns the page whole and
// [RenderFetch] is what cuts it, so a caller that is not filling a prompt is
// not handed a page someone already cut for a different budget.
func TestJinaFetchIsCappedByTheRenderer(t *testing.T) {
	page := strings.Repeat("go", 5000) // 10_000 bytes, well past the render cap
	server := serve(t, &jinaBaseURL, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) })
	jinaBaseURL = server.URL + "/"

	text, err := jina{}.Bind(Options{}).Fetch(context.Background(), "https://go.dev")
	if err != nil {
		t.Fatalf("Fetch error = %v", err)
	}
	if len(text) != len(page) {
		t.Fatalf("Fetch returned %d bytes, want the whole %d-byte page", len(text), len(page))
	}

	rendered := RenderFetch(text)
	if !strings.HasSuffix(rendered, fmt.Sprintf("… (%d more bytes)", len(page)-maxFetchChars)) {
		t.Fatalf("RenderFetch tail = %q, want the overflow announced", rendered[len(rendered)-40:])
	}
	if got := len(rendered); got > maxFetchChars+40 {
		t.Fatalf("RenderFetch returned %d bytes, want about %d", got, maxFetchChars)
	}
}

// --- resolution -----------------------------------------------------------

func TestResolveAuto(t *testing.T) {
	tests := []struct {
		name       string
		opts       Options
		wantSearch string
		wantFetch  string
	}{
		{
			// A fresh install: nothing configured, and search still works.
			name:       "no keys falls to the zero-key plugs",
			opts:       Options{},
			wantSearch: "duckduckgo",
			wantFetch:  "jina",
		},
		{
			// The keyed plug beats the always-available one on both sides,
			// from one key.
			name:       "exa wins both sides when keyed",
			opts:       Options{ExaKey: "sk-test"},
			wantSearch: "exa",
			wantFetch:  "exa-fetch",
		},
		{
			// JinaKey raises a rate ceiling; it is not a plug selector, so
			// it changes nothing about who wins.
			name:       "a jina key does not make jina keyed",
			opts:       Options{JinaKey: "jina-key"},
			wantSearch: "duckduckgo",
			wantFetch:  "jina",
		},
		{
			name:       "a pin wins over the keyed plug",
			opts:       Options{Provider: "duckduckgo", ExaKey: "sk-test"},
			wantSearch: "duckduckgo",
			wantFetch:  "exa-fetch", // the sides resolve independently
		},
		{
			// An explicit instruction is honoured even unkeyed; the call
			// then fails naming the missing key, which beats silently
			// searching somewhere else. See TestExaWithoutKeySaysSo.
			name:       "a pin wins even without the key it needs",
			opts:       Options{Provider: "exa"},
			wantSearch: "exa",
			wantFetch:  "jina",
		},
		{
			name:       "the pin is matched case- and space-insensitively",
			opts:       Options{Provider: "  ExA  "},
			wantSearch: "exa",
			wantFetch:  "jina",
		},
		{
			// A pin naming a fetch plug pins the fetcher and leaves search
			// on auto.
			name:       "a fetch plug can be pinned",
			opts:       Options{Provider: "jina", ExaKey: "sk-test"},
			wantSearch: "exa",
			wantFetch:  "jina",
		},
		{
			// A stale settings value must not take search away.
			name:       "an unknown pin falls through to auto",
			opts:       Options{Provider: "bing", ExaKey: "sk-test"},
			wantSearch: "exa",
			wantFetch:  "exa-fetch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, fetcher := Resolve(test.opts)
			if provider == nil || provider.Name() != test.wantSearch {
				t.Errorf("Resolve search = %v, want %s", name(provider), test.wantSearch)
			}
			if fetcher == nil || fetcher.Name() != test.wantFetch {
				t.Errorf("Resolve fetch = %v, want %s", name(fetcher), test.wantFetch)
			}
		})
	}
}

func TestResolveBindsTheOptions(t *testing.T) {
	// The resolved plug must carry the key and the client, not be the bare
	// registry prototype — otherwise every call would go out unconfigured.
	serve(t, &exaSearchURL, func(w http.ResponseWriter, r *http.Request) {
		if key := r.Header.Get("x-api-key"); key != "sk-test" {
			t.Errorf("x-api-key = %q, want the key from Options", key)
		}
		fmt.Fprint(w, `{"results":[{"title":"t","url":"https://go.dev"}]}`)
	})

	provider, _ := Resolve(Options{ExaKey: "sk-test", HTTPClient: http.DefaultClient})
	if _, err := provider.Search(context.Background(), "go", 1); err != nil {
		t.Fatalf("Search through the resolved provider error = %v", err)
	}
}

// stub is a plug for the registry tests: it declares its own availability, so
// one type covers both a keyed and a zero-key newcomer.
type stub struct {
	name  string
	keyed bool
}

func (s stub) Name() string { return s.name }
func (s stub) Available(opts Options) bool {
	return !s.keyed || strings.TrimSpace(opts.ExaKey) != ""
}
func (s stub) Bind(Options) Provider                               { return s }
func (stub) Search(context.Context, string, int) ([]Result, error) { return nil, nil }

func TestRegistryIsOpen(t *testing.T) {
	restore(t)

	// A back end that arrives later takes the keyed rung without any change
	// to Resolve — the point of the registry.
	registryMu.Lock()
	searchReg = []Provider{stub{name: "zero"}, stub{name: "keyed", keyed: true}}
	registryMu.Unlock()

	if provider, _ := Resolve(Options{}); provider.Name() != "zero" {
		t.Errorf("Resolve with no keys = %s, want zero", provider.Name())
	}
	if provider, _ := Resolve(Options{ExaKey: "k"}); provider.Name() != "keyed" {
		t.Errorf("Resolve with a key = %s, want keyed", provider.Name())
	}

	// And registration order must NOT decide it: Go runs inits in filename
	// order, and a law that depended on that would change when a file is
	// renamed. Same two plugs, opposite order, same answers.
	registryMu.Lock()
	searchReg = []Provider{stub{name: "keyed", keyed: true}, stub{name: "zero"}}
	registryMu.Unlock()

	if provider, _ := Resolve(Options{}); provider.Name() != "zero" {
		t.Errorf("Resolve with no keys (reordered) = %s, want zero", provider.Name())
	}
	if provider, _ := Resolve(Options{ExaKey: "k"}); provider.Name() != "keyed" {
		t.Errorf("Resolve with a key (reordered) = %s, want keyed", provider.Name())
	}
}

func TestResolveEmptyRegistry(t *testing.T) {
	restore(t)
	registryMu.Lock()
	searchReg, fetchReg = nil, nil
	registryMu.Unlock()

	provider, fetcher := Resolve(Options{ExaKey: "sk-test"})
	if provider != nil || fetcher != nil {
		t.Fatalf("Resolve on an empty registry = %v, %v, want nil, nil", provider, fetcher)
	}
}

func TestRegisterRejectsBadInput(t *testing.T) {
	restore(t)

	for _, test := range []struct {
		name string
		call func()
	}{
		{name: "nil provider", call: func() { RegisterSearch(nil) }},
		{name: "nil fetcher", call: func() { RegisterFetch(nil) }},
		{name: "unnamed provider", call: func() { RegisterSearch(stub{name: "  "}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s did not panic", test.name)
				}
			}()
			test.call()
		})
	}
}

func TestBuiltInPlugsAreRegistered(t *testing.T) {
	// The zero-key rungs must exist in a built binary, which is what makes
	// Resolve total.
	wantSearch := map[string]bool{"exa": false, "duckduckgo": false}
	for _, p := range RegisteredSearch() {
		wantSearch[p.Name()] = true
	}
	for name, found := range wantSearch {
		if !found {
			t.Errorf("search plug %q did not register itself", name)
		}
	}

	wantFetch := map[string]bool{"exa-fetch": false, "jina": false}
	for _, f := range RegisteredFetch() {
		wantFetch[f.Name()] = true
	}
	for name, found := range wantFetch {
		if !found {
			t.Errorf("fetch plug %q did not register itself", name)
		}
	}
}

// --- renderers ------------------------------------------------------------

func TestRenderResults(t *testing.T) {
	got := RenderResults([]Result{
		{Title: "Go 1.25 Release Notes", URL: "https://go.dev/doc/go1.25", Snippet: "A rewritten\n garbage collector.", Published: "2025-08-12"},
		{Title: "  ", URL: "https://example.com/x", Snippet: ""},
	}, 5)

	want := "1. Go 1.25 Release Notes — https://go.dev/doc/go1.25\n" +
		"  2025-08-12\n" +
		"  A rewritten garbage collector.\n" +
		"2. (untitled) — https://example.com/x\n" +
		"\n2 results"
	if got != want {
		t.Fatalf("RenderResults =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderResultsCaps(t *testing.T) {
	many := make([]Result, 20)
	for i := range many {
		many[i] = Result{
			Title:   fmt.Sprintf("Result %d", i),
			URL:     fmt.Sprintf("https://example.com/%d", i),
			Snippet: strings.Repeat("x", 900),
		}
	}

	t.Run("never more than maxRendered", func(t *testing.T) {
		for _, limit := range []int{0, -1, 8, 50} {
			got := RenderResults(many, limit)
			if n := strings.Count(got, " — https://"); n != maxRendered {
				t.Errorf("RenderResults(limit=%d) rendered %d results, want %d", limit, n, maxRendered)
			}
			// The footer tells the model these are the first few, not all.
			if want := fmt.Sprintf("\n%d of %d results", maxRendered, len(many)); !strings.HasSuffix(got, want) {
				t.Errorf("RenderResults(limit=%d) footer = %q, want it to end %q", limit, got, want)
			}
		}
	})

	t.Run("a caller's own limit is respected below the cap", func(t *testing.T) {
		got := RenderResults(many, 3)
		if n := strings.Count(got, " — https://"); n != 3 {
			t.Errorf("RenderResults(limit=3) rendered %d results, want 3", n)
		}
	})

	t.Run("snippets are clipped", func(t *testing.T) {
		got := RenderResults(many, 1)
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, "  x") && len(line) > maxSnippet+2 {
				t.Fatalf("snippet line is %d bytes, want at most %d", len(line), maxSnippet+2)
			}
		}
	})

	t.Run("one result is singular, none is a sentence", func(t *testing.T) {
		if got := RenderResults(many[:1], 5); !strings.HasSuffix(got, "\n1 result") {
			t.Errorf("RenderResults(one) = %q, want a singular footer", got)
		}
		if got := RenderResults(nil, 5); got != "no results" {
			t.Errorf("RenderResults(nil) = %q, want %q", got, "no results")
		}
	})
}

func TestRenderFetch(t *testing.T) {
	if got := RenderFetch("  # Go\n\nshort page.  "); got != "# Go\n\nshort page." {
		t.Errorf("RenderFetch(short) = %q, want it passed through trimmed", got)
	}
	if got := RenderFetch("   "); got != "(empty page)" {
		t.Errorf("RenderFetch(blank) = %q, want %q", got, "(empty page)")
	}

	// A multi-byte rune straddling the cap must not be cut in half.
	page := strings.Repeat("é", maxFetchChars) // two bytes each
	got := RenderFetch(page)
	if !strings.Contains(got, "more bytes)") {
		t.Fatalf("RenderFetch(long) did not announce the overflow: %q", got[len(got)-30:])
	}
	body := strings.TrimSuffix(got, fmt.Sprintf("… (%d more bytes)", len(page)-maxFetchChars))
	if strings.ContainsRune(body, '�') || len(body) != maxFetchChars {
		t.Fatalf("RenderFetch clipped mid-rune: %d bytes kept", len(body))
	}
}

// name reports a plug's name without panicking on a nil interface, so a failed
// assertion prints a diagnosis rather than crashing the test binary.
func name(v any) string {
	switch p := v.(type) {
	case nil:
		return "<nil>"
	case Provider:
		return p.Name()
	case Fetcher:
		return p.Name()
	}
	return fmt.Sprint(v)
}
