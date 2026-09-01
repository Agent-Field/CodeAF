package search

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// DuckDuckGo: the zero-key safety valve a person can pin when Firecrawl is not
// the back end they want.
//
// It has no API. What it has is html.duckduckgo.com, the no-JavaScript results
// page, which we request and read with regexes over its stable result__a and
// result__snippet classes. SCRAPING IS THE PRICE OF ZERO CONFIGURATION: the
// markup is not a contract, and the day DuckDuckGo renames a class this plug
// starts returning a parse error instead of results. That failure is loud —
// see the error in parseDuckDuckGo — and it is survivable precisely because
// the registry is the upgrade path: a person with a key resolves to exa and
// never touches this code, while the explicit zero-key order in search.go
// makes Firecrawl the fresh-install default without unregistering this plug.
// The alternative — no results at all without a key — would make the agent
// useless for anyone who has not signed up for anything.

var ddgURL = "https://html.duckduckgo.com/html/"

// A plain browser User-Agent. The endpoint serves a challenge page to clients
// that send Go's default agent, which is not evasion so much as the minimum
// needed to receive the ordinary public page a browser receives.
const ddgUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func init() { RegisterSearch(duckDuckGo{}) }

// duckDuckGo is the plug. It takes no key, so [Options] only ever gives it a
// client — but it still binds, because the caller's client is how a test or a
// proxy reaches it.
type duckDuckGo struct{ opts Options }

func (duckDuckGo) Name() string { return "duckduckgo" }

// Available is unconditionally true: this remains an always-available rung of
// the resolution ladder, and [Resolve] identifies it by exactly this answer
// under empty Options.
func (duckDuckGo) Available(Options) bool { return true }

func (duckDuckGo) Bind(opts Options) Provider { return duckDuckGo{opts: opts} }

func (d duckDuckGo) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("duckduckgo: empty query")
	}

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	endpoint := ddgURL + "?" + url.Values{"q": {query}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("duckduckgo: build request: %w", err)
	}
	req.Header.Set("User-Agent", ddgUserAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := d.opts.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("duckduckgo: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("duckduckgo: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo: %s", resp.Status)
	}

	results, err := parseDuckDuckGo(string(body))
	if err != nil {
		return nil, err
	}
	if n := clampLimit(limit); len(results) > n {
		results = results[:n]
	}
	return results, nil
}

// The markup this plug reads:
//
//	<h2 class="result__title">
//	  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=…">Title</a>
//	</h2>
//	<a class="result__snippet" href="…">Snippet <b>with</b> markup</a>
//
// The anchor's attributes are matched as a blob rather than in a fixed order,
// because attribute order is the first thing to change in a template edit and
// is the cheapest thing to stop depending on.
var (
	ddgResultRe  = regexp.MustCompile(`(?is)<a([^>]*\bclass="[^"]*\bresult__a\b[^"]*"[^>]*)>(.*?)</a>`)
	ddgSnippetRe = regexp.MustCompile(`(?is)<a[^>]*\bclass="[^"]*\bresult__snippet\b[^"]*"[^>]*>(.*?)</a>`)
	ddgHrefRe    = regexp.MustCompile(`(?i)\bhref="([^"]*)"`)
	ddgTagRe     = regexp.MustCompile(`(?s)<[^>]*>`)
)

// parseDuckDuckGo pulls results out of the HTML page.
//
// Snippets are paired to titles POSITIONALLY — a snippet belongs to the last
// result anchor before it — rather than by index into two flat lists, so an ad
// block or a missing snippet shifts nothing after it.
func parseDuckDuckGo(body string) ([]Result, error) {
	anchors := ddgResultRe.FindAllStringSubmatchIndex(body, -1)
	if len(anchors) == 0 {
		// Either the layout changed or we were served a challenge page. Both
		// are the same thing to a caller: this plug can no longer read this
		// endpoint, and the fix is a key or a different plug — not a retry.
		return nil, fmt.Errorf("duckduckgo: no results in %d bytes of HTML (layout changed, or the request was blocked)", len(body))
	}
	snippets := ddgSnippetRe.FindAllStringSubmatchIndex(body, -1)

	results := make([]Result, 0, len(anchors))
	for i, a := range anchors {
		attrs, inner := body[a[2]:a[3]], body[a[4]:a[5]]

		href := ""
		if m := ddgHrefRe.FindStringSubmatch(attrs); m != nil {
			href = m[1]
		}
		link := unwrapDDG(href)
		if link == "" {
			continue // an ad or a redirect we could not read through
		}

		// The window this result owns: from the end of its anchor to the
		// start of the next one.
		end := len(body)
		if i+1 < len(anchors) {
			end = anchors[i+1][0]
		}
		snippet := ""
		for _, s := range snippets {
			if s[0] >= a[1] && s[0] < end {
				snippet = cleanHTML(body[s[2]:s[3]])
				break
			}
		}

		results = append(results, Result{
			Title:   cleanHTML(inner),
			URL:     link,
			Snippet: snippet,
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("duckduckgo: %d result links, none with a usable URL", len(anchors))
	}
	return results, nil
}

// unwrapDDG turns the href on a result anchor into a real destination.
//
// DuckDuckGo wraps most links in //duckduckgo.com/l/?uddg=<escaped>, and its
// ads in /y.js — so anything still pointing at duckduckgo.com after unwrapping
// is not a search result and is dropped.
func unwrapDDG(href string) string {
	href = strings.TrimSpace(html.UnescapeString(href))
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if target := parsed.Query().Get("uddg"); target != "" {
		parsed, err = url.Parse(target)
		if err != nil {
			return ""
		}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if host := strings.ToLower(parsed.Hostname()); host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com") {
		return ""
	}
	return parsed.String()
}

// cleanHTML strips the <b> highlighting DuckDuckGo wraps query terms in,
// unescapes entities, and folds the whitespace — leaving the plain sentence
// the renderer wants.
func cleanHTML(fragment string) string {
	return collapse(html.UnescapeString(ddgTagRe.ReplaceAllString(fragment, "")))
}
