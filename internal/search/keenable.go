package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Keenable: a zero-key search plug reached only by an explicit pin. Its public
// endpoint answers without an account or key; the X-Keenable-Title header names
// the calling app, and the service requires it on keyless calls.
//
// It is not listed in [keylessOrder], so an unpinned session keeps Firecrawl as
// its zero-key default: Keenable answers only when search.provider is
// "keenable".

// The endpoint is a package variable so tests can point it at an httptest
// server without reaching the network.
var keenableSearchURL = "https://api.keenable.ai/v1/search/public"

// keenableTitle is the X-Keenable-Title value this build sends.
const keenableTitle = "codeaf"

// keenableSnippetChars asks for a snippet long enough to fill the renderer's
// excerpt without returning a page per hit.
const keenableSnippetChars = 400

const keenableMaxResponseSize = 8 << 20

func init() {
	RegisterSearch(keenableSearch{})
}

type keenableSearch struct{ opts Options }

func (keenableSearch) Name() string { return "keenable" }

// Available is always true: the keyless endpoint needs nothing configured.
func (keenableSearch) Available(Options) bool { return true }

func (keenableSearch) Bind(opts Options) Provider { return keenableSearch{opts: opts} }

type keenableSearchRequest struct {
	Query            string `json:"query"`
	MaxResults       int    `json:"max_results"`
	SnippetMaxLength int    `json:"snippet_max_length"`
	DisableCards     bool   `json:"disable_cards"`
}

type keenableSearchResponse struct {
	Results []struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Snippet     string `json:"snippet"`
		Description string `json:"description"`
		PublishedAt string `json:"published_at"`
	} `json:"results"`
}

func (k keenableSearch) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("keenable: empty query")
	}
	limit = clampLimit(limit)
	body, err := json.Marshal(keenableSearchRequest{
		Query:            query,
		MaxResults:       limit,
		SnippetMaxLength: keenableSnippetChars,
		DisableCards:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("keenable: encode request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, keenableSearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("keenable: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Keenable-Title", keenableTitle)

	resp, err := k.opts.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("keenable: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, keenableMaxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("keenable: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keenable: %s%s", resp.Status, detail(raw))
	}

	var parsed keenableSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("keenable: decode response: %w", err)
	}
	results := make([]Result, 0, min(len(parsed.Results), limit))
	for _, hit := range parsed.Results {
		link := strings.TrimSpace(hit.URL)
		if link == "" {
			continue // a hit with no link is nothing the agent can follow
		}
		snippet := hit.Snippet
		if strings.TrimSpace(snippet) == "" {
			snippet = hit.Description
		}
		results = append(results, Result{
			Title:     collapse(hit.Title),
			URL:       link,
			Snippet:   collapse(snippet),
			Published: strings.TrimSpace(hit.PublishedAt),
		})
		if len(results) == limit {
			break
		}
	}
	return results, nil
}
