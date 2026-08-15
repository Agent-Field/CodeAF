package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Exa: the keyed plug. It is a search API built for models rather than for
// people — it returns page text alongside the link, which is why it can serve
// both halves of this package, search and fetch, from one key.
//
// Nothing else in the package knows exa exists. It registers itself below, and
// deleting this file would leave a package that still searches.

// Endpoints as package vars rather than constants so a test can point them at
// an httptest server. Nothing outside the package can reach them.
var (
	exaSearchURL   = "https://api.exa.ai/search"
	exaContentsURL = "https://api.exa.ai/contents"
)

// exaTextChars is how much page text to ask for per result. Enough to be a
// real snippet, small enough that ten of them are not a prompt on their own —
// the agent that wants the whole page calls Fetch.
const exaTextChars = 400

func init() {
	RegisterSearch(exaSearch{})
	RegisterFetch(exaFetch{})
}

// exaSearch is the search half. The zero value is the registry prototype; Bind
// returns the copy that carries the key.
type exaSearch struct{ opts Options }

func (exaSearch) Name() string { return "exa" }

// Available is key presence, which is the whole of it — no probe.
func (exaSearch) Available(opts Options) bool { return strings.TrimSpace(opts.ExaKey) != "" }

func (exaSearch) Bind(opts Options) Provider { return exaSearch{opts: opts} }

// exaSearchRequest is the request body. contents.text asks exa to return page
// text with each hit, which is what fills Result.Snippet.
type exaSearchRequest struct {
	Query      string           `json:"query"`
	NumResults int              `json:"numResults"`
	Contents   exaContentsBlock `json:"contents"`
}

type exaContentsBlock struct {
	Text exaTextBlock `json:"text"`
}

type exaTextBlock struct {
	MaxCharacters int `json:"maxCharacters"`
}

type exaSearchResponse struct {
	Results []exaResult `json:"results"`
}

type exaResult struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Text          string `json:"text"`
	PublishedDate string `json:"publishedDate"`
}

func (e exaSearch) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("exa: empty query")
	}
	if !e.Available(e.opts) {
		// Reachable only through an explicit pin, which [Resolve] honours on
		// purpose. Saying which key is missing beats a 401 from a vendor.
		return nil, fmt.Errorf("exa: no API key")
	}

	body, err := json.Marshal(exaSearchRequest{
		Query:      query,
		NumResults: clampLimit(limit),
		Contents:   exaContentsBlock{Text: exaTextBlock{MaxCharacters: exaTextChars}},
	})
	if err != nil {
		return nil, fmt.Errorf("exa: encode request: %w", err)
	}

	raw, err := e.post(ctx, exaSearchURL, body, searchTimeout)
	if err != nil {
		return nil, err
	}

	var parsed exaSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("exa: decode response: %w", err)
	}

	results := make([]Result, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		if strings.TrimSpace(r.URL) == "" {
			continue // a hit with no link is nothing the agent can follow
		}
		results = append(results, Result{
			Title:     collapse(r.Title),
			URL:       r.URL,
			Snippet:   collapse(r.Text),
			Published: strings.TrimSpace(r.PublishedDate),
		})
	}
	return results, nil
}

// exaFetch is the fetch half: exa's contents API, given a URL, returns the
// page it already has indexed. It is the keyed alternative to jina, and wins
// the fetch side of [Resolve] whenever a key is present.
type exaFetch struct{ opts Options }

func (exaFetch) Name() string { return "exa-fetch" }

func (exaFetch) Available(opts Options) bool { return strings.TrimSpace(opts.ExaKey) != "" }

func (exaFetch) Bind(opts Options) Fetcher { return exaFetch{opts: opts} }

type exaContentsRequest struct {
	URLs []string `json:"urls"`
	Text bool     `json:"text"`
}

func (e exaFetch) Fetch(ctx context.Context, url string) (string, error) {
	if strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("exa-fetch: empty url")
	}
	if !e.Available(e.opts) {
		return "", fmt.Errorf("exa-fetch: no API key")
	}

	body, err := json.Marshal(exaContentsRequest{URLs: []string{url}, Text: true})
	if err != nil {
		return "", fmt.Errorf("exa-fetch: encode request: %w", err)
	}

	raw, err := e.post(ctx, exaContentsURL, body, fetchTimeout)
	if err != nil {
		return "", err
	}

	var parsed exaSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("exa-fetch: decode response: %w", err)
	}
	if len(parsed.Results) == 0 || strings.TrimSpace(parsed.Results[0].Text) == "" {
		return "", fmt.Errorf("exa-fetch: no content for %s", url)
	}
	return parsed.Results[0].Text, nil
}

func (e exaSearch) post(ctx context.Context, url string, body []byte, timeout time.Duration) ([]byte, error) {
	return exaPost(ctx, e.opts, "exa", url, body, timeout)
}

func (e exaFetch) post(ctx context.Context, url string, body []byte, timeout time.Duration) ([]byte, error) {
	return exaPost(ctx, e.opts, "exa-fetch", url, body, timeout)
}

// exaPost is the one place the key becomes a header and a status becomes an
// error. Both plugs go through it so they cannot drift on either.
func exaPost(ctx context.Context, opts Options, plug, url string, body []byte, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", plug, err)
	}
	req.Header.Set("x-api-key", strings.TrimSpace(opts.ExaKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := opts.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", plug, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", plug, err)
	}
	if resp.StatusCode != http.StatusOK {
		// The status is the diagnosis — 401 is a bad key, 429 is a rate
		// limit — so it goes in the message, with whatever the body said
		// about it, trimmed so a vendor's HTML error page is not the error.
		return nil, fmt.Errorf("%s: %s%s", plug, resp.Status, detail(raw))
	}
	return raw, nil
}

// detail renders an error body as a short parenthetical, or nothing.
func detail(raw []byte) string {
	text := collapse(string(raw))
	if text == "" {
		return ""
	}
	short, _ := clip(text, 200)
	return ": " + short
}
