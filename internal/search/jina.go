package search

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Jina: the zero-key FETCH default. r.jina.ai/<url> returns the page at <url>
// rendered to markdown-ish text — headings, links and prose, none of the
// chrome — which is exactly the shape an agent can read and roughly a tenth
// the tokens of the HTML.
//
// It answers unauthenticated at about 20 requests a minute, which is why
// [Options.JinaKey] does NOT gate availability: the key raises the ceiling for
// someone who has one, and its absence costs nothing but rate. That is what
// makes this the fetch counterpart to Firecrawl search — the rung that is
// always there — with exa-fetch or firecrawl-fetch taking over when its key is
// present.

var jinaBaseURL = "https://r.jina.ai/"

func init() {
	RegisterFetch(jina{})
	RegisterSearch(jinaSearch{})
}

// ── jina SEARCH: the keyed search rung ──────────────────────────────────────
//
// s.jina.ai is Search Foundation: the same key the fetch plug takes, spent on
// the finding rather than the reading. It exists in the registry for one
// reason — a paid Jina search remains a keyed upgrade over every zero-key
// search plug. It wins [Resolve]'s keyed rung whenever the key is present, so
// the person who already pays for Jina keeps using that service for search.

// jinaSearchBaseURL is the endpoint the query is appended to, escaped.
var jinaSearchBaseURL = "https://s.jina.ai/"

type jinaSearch struct{ opts Options }

func (jinaSearch) Name() string { return "jina-search" }

// Available is key presence: search foundation is a paid endpoint, and
// answering 402 to every query would be worse than the zero-key rung.
func (jinaSearch) Available(opts Options) bool { return strings.TrimSpace(opts.JinaKey) != "" }

func (jinaSearch) Bind(opts Options) Provider { return jinaSearch{opts: opts} }

func (j jinaSearch) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("jina-search: empty query")
	}
	if !j.Available(j.opts) {
		// An explicit pin wins even when its key is absent, so answer that
		// choice directly instead of making an unauthenticated vendor call.
		return nil, ErrNoAPIKey
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jinaSearchBaseURL+url.QueryEscape(query), nil)
	if err != nil {
		return nil, fmt.Errorf("jina-search: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(j.opts.JinaKey))
	req.Header.Set("Accept", "text/plain")

	resp, err := j.opts.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("jina-search: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("jina-search: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jina-search: %s%s", resp.Status, detail(body))
	}
	return jinaSearchResults(string(body), limit), nil
}

// jinaSearchResults parses the endpoint's answer: one block per hit, the
// Title and URL Source lines heading the snippet. A block with no URL is
// dropped for the reason exa drops one: a hit with no link is nothing the
// agent can follow. The parser is deliberately line-shaped — the endpoint's
// prose varies, and the two labelled lines are its only contract.
func jinaSearchResults(body string, limit int) []Result {
	if limit <= 0 {
		limit = 5
	}
	var results []Result
	for _, block := range strings.Split(body, "\n\n") {
		var title, link string
		var snippet []string
		for _, line := range strings.Split(block, "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "Title:"):
				title = strings.TrimSpace(strings.TrimPrefix(trimmed, "Title:"))
			case strings.HasPrefix(trimmed, "URL Source:"):
				link = strings.TrimSpace(strings.TrimPrefix(trimmed, "URL Source:"))
			case trimmed != "":
				snippet = append(snippet, trimmed)
			}
		}
		if link == "" {
			continue
		}
		results = append(results, Result{
			Title:   collapse(title),
			URL:     link,
			Snippet: collapse(strings.Join(snippet, " ")),
		})
		if len(results) >= limit {
			break
		}
	}
	return results
}

type jina struct{ opts Options }

func (jina) Name() string { return "jina" }

// Available is unconditionally true: no key required. See the note above on
// why JinaKey does not appear here.
func (jina) Available(Options) bool { return true }

func (jina) Bind(opts Options) Fetcher { return jina{opts: opts} }

func (j jina) Fetch(ctx context.Context, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("jina: empty url")
	}
	// Checked here rather than left to the service: r.jina.ai concatenates
	// whatever it is given, so a relative path becomes a request for a page
	// on jina itself, and the agent gets a confusing 404 instead of "that is
	// not a URL".
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("jina: %q is not an http(s) url", target)
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	// The target is appended verbatim, not escaped: r.jina.ai's route is the
	// literal URL, and percent-encoding it yields a request for a page whose
	// name contains %3A%2F%2F.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jinaBaseURL+target, nil)
	if err != nil {
		return "", fmt.Errorf("jina: build request: %w", err)
	}
	if key := strings.TrimSpace(j.opts.JinaKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "text/plain")
	// The reader keeps a 3600s page cache and answers from it by default;
	// X-No-Cache asks it not to. The ask is not a guarantee — a HIT can still
	// come back (see [cacheHit] below), but without the header every answer
	// would be the cache's.
	req.Header.Set("X-No-Cache", "true")

	resp, err := j.opts.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("jina: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("jina: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jina: %s%s", resp.Status, detail(body))
	}

	// A HIT is the reader serving its OWN 3600s page cache, which is someone
	// else's snapshot of the page, not the page. It is deliberately not
	// passed through: an agent that was handed it would believe it had read
	// the page. Discard it and fetch the target itself.
	if cacheHit(resp.Header) {
		text, err := jinaDirectFetch(ctx, j.opts, target)
		if err != nil {
			return "", fmt.Errorf("jina: cached snapshot rejected; direct fetch of %s: %w", target, err)
		}
		return text, nil
	}

	// Returned whole. Capping is the RENDERER's job ([RenderFetch]) so that a
	// caller doing something other than prompting a model — a summariser, a
	// grep — is not handed a page someone already cut for a different budget.
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", fmt.Errorf("jina: empty body for %s", target)
	}
	return text, nil
}

// cacheHit reports whether the reader answered from its own page cache: the
// X-Cache response header is HIT. Trimmed and case-insensitive, because a
// header's casing and whitespace are the answerer's, not ours.
func cacheHit(header http.Header) bool {
	return strings.EqualFold(strings.TrimSpace(header.Get("X-Cache")), "HIT")
}

// jinaDirectFetch is the freshness fallback: GET the target itself when the
// reader answered from its cache, and reduce its HTML to text so the result
// has the same readable shape a fresh reader answer would have had.
func jinaDirectFetch(ctx context.Context, opts Options, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; codeaf/1.0)")

	resp, err := opts.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("http %d%s", resp.StatusCode, detail(body))
	}

	text := htmlToText(string(body))
	if text == "" {
		return "", fmt.Errorf("empty body")
	}
	return text, nil
}

// HTML stripping for the direct fetch, modeled on the resident's helper of
// the same shape: crude on purpose, because the consumer is a language model
// that tolerates ragged text far better than it tolerates paying for markup.
var (
	// Written out per element rather than with a backreference: Go's regexp is
	// RE2, which has none, and a pattern that compiles everywhere except here
	// would fail at init rather than at review.
	dropElements = regexp.MustCompile(`(?is)<script[^>]*>.*?</script\s*>` +
		`|<style[^>]*>.*?</style\s*>` +
		`|<noscript[^>]*>.*?</noscript\s*>` +
		`|<svg[^>]*>.*?</svg\s*>` +
		`|<head[^>]*>.*?</head\s*>`)
	blockBreaks = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|h[1-6]|section|article)[^>]*>`)
	anyTag      = regexp.MustCompile(`(?s)<[^>]+>`)
	manyBlanks  = regexp.MustCompile(`\n{3,}`)
	manySpaces  = regexp.MustCompile(`[ \t]{2,}`)
)

// htmlToText strips markup well enough to read. It is intentionally crude:
// the consumer is a language model, which tolerates ragged text far better
// than it tolerates paying for markup it will ignore.
func htmlToText(page string) string {
	page = dropElements.ReplaceAllString(page, " ")
	page = blockBreaks.ReplaceAllString(page, "\n")
	page = anyTag.ReplaceAllString(page, " ")
	page = strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&mdash;", "—", "&ndash;", "–",
	).Replace(page)
	page = manySpaces.ReplaceAllString(page, " ")
	page = manyBlanks.ReplaceAllString(page, "\n\n")
	return strings.TrimSpace(page)
}
