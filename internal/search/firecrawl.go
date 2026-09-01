package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Firecrawl is the zero-key SEARCH default. Its MCP endpoint accepts a
// stateless tools/call, so a fresh install needs no handshake, session id, key,
// or settings row before it can search. Plain search returns snippets only;
// page content belongs to Fetch because scraping every hit costs extra credits
// and can return hundreds of kilobytes nobody asked to read.

// The endpoint is a package variable so tests can exercise the real transport
// and parser against an httptest server without reaching the network.
var firecrawlMCPURL = "https://mcp.firecrawl.dev/v2/mcp"

const firecrawlMaxResponseSize = 8 << 20

func init() {
	RegisterSearch(firecrawlSearch{})
	RegisterFetch(firecrawlFetch{})
}

type firecrawlSearch struct{ opts Options }

func (firecrawlSearch) Name() string { return "firecrawl" }

// Available is always true. A key raises Firecrawl's ceiling; it never opens
// the search plug because the keyless tier already does that.
func (firecrawlSearch) Available(Options) bool { return true }

func (firecrawlSearch) Bind(opts Options) Provider { return firecrawlSearch{opts: opts} }

type firecrawlFetch struct{ opts Options }

func (firecrawlFetch) Name() string { return "firecrawl-fetch" }

// Fetch is the paid upgrade only. Jina remains the zero-key fetch default, so
// an account key is what makes this half available.
func (firecrawlFetch) Available(opts Options) bool {
	return strings.TrimSpace(opts.FirecrawlKey) != ""
}

func (firecrawlFetch) Bind(opts Options) Fetcher { return firecrawlFetch{opts: opts} }

type firecrawlSearchResponse struct {
	Success bool            `json:"success"`
	Error   json.RawMessage `json:"error"`
	Data    struct {
		Web []struct {
			URL         string `json:"url"`
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"web"`
	} `json:"data"`
}

func (f firecrawlSearch) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("firecrawl: empty query")
	}
	limit = clampLimit(limit)
	text, err := firecrawlCall(ctx, f.opts, "firecrawl", "firecrawl_search", map[string]any{
		"query": query,
		"limit": limit,
	}, searchTimeout)
	if err != nil {
		return nil, err
	}

	var response firecrawlSearchResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		return nil, fmt.Errorf("firecrawl: decode search result: %w", err)
	}
	if !response.Success {
		return nil, firecrawlToolError("firecrawl", response.Error)
	}
	results := make([]Result, 0, min(len(response.Data.Web), limit))
	for _, hit := range response.Data.Web {
		if strings.TrimSpace(hit.URL) == "" {
			continue
		}
		results = append(results, Result{
			Title:   collapse(hit.Title),
			URL:     strings.TrimSpace(hit.URL),
			Snippet: collapse(hit.Description),
		})
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

func (f firecrawlFetch) Fetch(ctx context.Context, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("firecrawl-fetch: empty url")
	}
	if !f.Available(f.opts) {
		return "", fmt.Errorf("firecrawl-fetch: no API key")
	}
	text, err := firecrawlCall(ctx, f.opts, "firecrawl-fetch", "firecrawl_scrape", map[string]any{
		"url":             target,
		"formats":         []string{"markdown"},
		"onlyMainContent": true,
	}, fetchTimeout)
	if err != nil {
		return "", err
	}
	return firecrawlMarkdown(text)
}

func firecrawlCall(
	ctx context.Context,
	opts Options,
	plug, tool string,
	arguments map[string]any,
	timeout time.Duration,
) (string, error) {
	payload := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}{JSONRPC: "2.0", ID: 1, Method: "tools/call"}
	payload.Params.Name = tool
	payload.Params.Arguments = arguments
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%s: encode request: %w", plug, err)
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, firecrawlMCPURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%s: build request: %w", plug, err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(opts.FirecrawlKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := opts.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", plug, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, firecrawlMaxResponseSize+1))
	if err != nil {
		return "", fmt.Errorf("%s: read response: %w", plug, err)
	}
	if len(raw) > firecrawlMaxResponseSize {
		return "", fmt.Errorf("%s: response exceeds %d bytes", plug, firecrawlMaxResponseSize)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s: %s%s", plug, resp.Status, detail(raw))
	}
	text, err := parseFirecrawlRPC(string(raw))
	if err != nil {
		return "", fmt.Errorf("%s: %w", plug, err)
	}
	return text, nil
}

func parseFirecrawlRPC(body string) (string, error) {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") {
		return parseFirecrawlRPCPayload(trimmed)
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		text, err := parseFirecrawlRPCPayload(payload)
		if err != nil || text != "" {
			return text, err
		}
	}
	return "", fmt.Errorf("firecrawl answered nothing")
}

func parseFirecrawlRPCPayload(payload string) (string, error) {
	var response struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return "", fmt.Errorf("firecrawl answered in a shape this build does not know: %w", err)
	}
	if response.Error != nil {
		message := strings.TrimSpace(response.Error.Message)
		if strings.HasPrefix(message, "MCP error") {
			return "", errors.New(message)
		}
		if message == "" {
			message = "the request was refused"
		}
		return "", fmt.Errorf("firecrawl error %d: %s", response.Error.Code, message)
	}
	if response.Result == nil || len(response.Result.Content) == 0 {
		return "", fmt.Errorf("firecrawl answered in a shape this build does not know")
	}
	first := response.Result.Content[0]
	if first.Type != "text" || strings.TrimSpace(first.Text) == "" {
		return "", fmt.Errorf("firecrawl answered without readable text")
	}
	return first.Text, nil
}

func firecrawlMarkdown(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", fmt.Errorf("firecrawl-fetch: empty page")
	}
	// The live scrape tool wraps Markdown in a JSON object, but accepting raw
	// Markdown keeps this plug tolerant of endpoint versions that return the
	// page directly. A leading brace is page content until recognized envelope
	// fields prove otherwise.
	var response struct {
		Success  *bool           `json:"success"`
		Error    json.RawMessage `json:"error"`
		Markdown *string         `json:"markdown"`
		Data     *struct {
			Markdown *string `json:"markdown"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(trimmed), &response); err != nil {
		return trimmed, nil
	}
	isEnvelope := response.Success != nil || response.Markdown != nil ||
		(response.Data != nil && response.Data.Markdown != nil)
	if !isEnvelope {
		return trimmed, nil
	}
	if response.Success != nil && !*response.Success {
		return "", firecrawlToolError("firecrawl-fetch", response.Error)
	}
	markdown := ""
	if response.Markdown != nil {
		markdown = *response.Markdown
	}
	if strings.TrimSpace(markdown) == "" && response.Data != nil && response.Data.Markdown != nil {
		markdown = *response.Data.Markdown
	}
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return "", fmt.Errorf("firecrawl-fetch: empty page")
	}
	return markdown, nil
}

func firecrawlToolError(plug string, raw json.RawMessage) error {
	var message string
	if len(raw) > 0 && json.Unmarshal(raw, &message) == nil {
		message = strings.TrimSpace(message)
	} else {
		message = collapse(string(raw))
	}
	if message == "" || message == "null" {
		message = "request was unsuccessful"
	}
	return fmt.Errorf("%s: %s", plug, message)
}
