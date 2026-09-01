package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
)

const (
	defaultExaWebSearchURL       = "https://mcp.exa.ai/mcp"
	defaultParallelWebSearchURL  = "https://search.parallel.ai/mcp"
	defaultFirecrawlWebSearchURL = "https://mcp.firecrawl.dev/v2/mcp"
	webSearchTimeout             = 25 * time.Second
	webSearchMaxResponseSize     = 5 * 1024 * 1024
	firecrawlContextCharacters   = 4000
	// A per-result rune cap cannot usefully exceed the accepted whole response.
	firecrawlMaxContextCharacters = webSearchMaxResponseSize
)

// aforge-embed: D13 — the schema names each provider's knobs and derives the
// Firecrawl bounds from the constants that enforce them.
var webSearchSchema = fmt.Sprintf(`{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"type":"object",
	"properties":{
		"query":{"type":"string","description":"Websearch query"},
		"numResults":{"type":"number","description":"Number of search results for Exa or Firecrawl (default: 8; Parallel ignores it)"},
		"livecrawl":{"type":"string","enum":["fallback","preferred"],"description":"Exa live-crawl mode; Firecrawl always returns main-page Markdown and ignores this knob"},
		"type":{"type":"string","enum":["auto","fast","deep"],"description":"Search type for Exa only; Firecrawl and Parallel ignore it"},
		"contextMaxCharacters":{"type":"number","description":"Maximum context characters for Exa, or Markdown runes per Firecrawl result (Firecrawl default: %d, maximum: %d; Parallel ignores it)"}
	},
	"required":["query"]
}`, firecrawlContextCharacters, firecrawlMaxContextCharacters)

type webSearchInput struct {
	Query                string   `json:"query"`
	NumResults           *float64 `json:"numResults,omitempty"`
	Livecrawl            string   `json:"livecrawl,omitempty"`
	Type                 string   `json:"type,omitempty"`
	ContextMaxCharacters *float64 `json:"contextMaxCharacters,omitempty"`
}

type webSearchMetadata struct {
	Provider   string `json:"provider"`
	Truncated  bool   `json:"truncated"`
	OutputPath string `json:"outputPath,omitempty"`
}

func webSearchDescription() string {
	// websearch.ts:107-110 replaces only the first {{year}} placeholder each
	// time the description is read.
	return strings.Replace(webSearchDescriptionTemplate, "{{year}}", strconv.Itoa(time.Now().Year()), 1)
}

func validateWebSearch(raw json.RawMessage) error {
	var input webSearchInput
	if err := decodeWebInput(raw, &input, []string{"query", "numResults", "livecrawl", "type", "contextMaxCharacters"}, "query"); err != nil {
		return err
	}
	if input.Livecrawl != "" && input.Livecrawl != "fallback" && input.Livecrawl != "preferred" {
		return fmt.Errorf("livecrawl must be fallback or preferred")
	}
	if input.Type != "" && input.Type != "auto" && input.Type != "fast" && input.Type != "deep" {
		return fmt.Errorf("type must be auto, fast, or deep")
	}
	return nil
}

// CurrentWebSearchFlags mirrors flag.ts:72,81, including the legacy
// experimental aliases. API keys are intentionally not feature flags.
func CurrentWebSearchFlags() WebSearchFlags {
	truthy := func(name string) bool {
		return config.ParseBoolean(config.Truthy, environmentValue(name))
	}
	experimental := truthy("CODEAF_EXPERIMENTAL")
	return WebSearchFlags{
		Exa:      experimental || truthy("CODEAF_ENABLE_EXA") || truthy("CODEAF_EXPERIMENTAL_EXA"),
		Parallel: truthy("CODEAF_ENABLE_PARALLEL") || truthy("CODEAF_EXPERIMENTAL_PARALLEL"),
	}
}

func (r *Registry) executeWebSearch(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input webSearchInput
	if err := decodeWebInput(call.Input, &input, []string{"query", "numResults", "livecrawl", "type", "contextMaxCharacters"}, "query"); err != nil {
		return steploop.ToolResult{}, err
	}
	provider := selectWebSearchProvider(call.SessionID, CurrentWebSearchFlags())
	label := webSearchProviderLabel(provider)
	permissionMetadata := map[string]any{"query": input.Query, "provider": provider}
	if input.NumResults != nil {
		permissionMetadata["numResults"] = *input.NumResults
	}
	if input.Livecrawl != "" {
		permissionMetadata["livecrawl"] = input.Livecrawl
	}
	if input.Type != "" {
		permissionMetadata["type"] = input.Type
	}
	if input.ContextMaxCharacters != nil {
		permissionMetadata["contextMaxCharacters"] = *input.ContextMaxCharacters
	}
	if err := r.ask(ctx, call, "websearch", []string{input.Query}, permissionMetadata); err != nil {
		return steploop.ToolResult{}, err
	}

	result, err := callWebSearchProvider(ctx, provider, input, call)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	if result == "" {
		result = "No search results found. Please try a different query."
	}
	output, truncation, err := r.truncateWebOutput(ctx, call, result)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	return steploop.ToolResult{
		Title: label + ": " + input.Query, Output: output,
		Metadata: rawMetadata(webSearchMetadata{
			Provider: provider, Truncated: truncation.Truncated, OutputPath: truncation.OutputPath,
		}),
	}, nil
}

func selectWebSearchProvider(sessionID string, flags WebSearchFlags) string {
	// aforge-embed: D13 — the normalized override remains first, the legacy
	// enables keep their order, and every other session uses Firecrawl. Session
	// identity is deliberately ignored: there is no experiment split now.
	_ = sessionID
	if override := strings.ToLower(strings.TrimSpace(os.Getenv("CODEAF_WEBSEARCH_PROVIDER"))); override == "exa" || override == "parallel" || override == "firecrawl" {
		return override
	}
	if flags.Parallel {
		return "parallel"
	}
	if flags.Exa {
		return "exa"
	}
	return "firecrawl"
}

func webSearchProviderLabel(provider string) string {
	if provider == "parallel" {
		return "Parallel Web Search"
	}
	if provider == "exa" {
		return "Exa Web Search"
	}
	if provider == "firecrawl" {
		return "Firecrawl Web Search"
	}
	return "Web Search"
}

func callWebSearchProvider(
	ctx context.Context,
	provider string,
	input webSearchInput,
	call steploop.ToolCall,
) (string, error) {
	options := webOptions(ctx)
	endpoint := options.exaURL
	toolName := "web_search_exa"
	arguments := map[string]any{
		"query":      input.Query,
		"type":       valueOr(input.Type, "auto"),
		"numResults": nonzeroOr(input.NumResults, 8),
		"livecrawl":  valueOr(input.Livecrawl, "fallback"),
	}
	if input.ContextMaxCharacters != nil {
		arguments["contextMaxCharacters"] = *input.ContextMaxCharacters
	}
	headers := map[string]string{}
	if endpoint == "" {
		endpoint = defaultExaWebSearchURL
	}
	if key := os.Getenv("EXA_API_KEY"); provider == "exa" && key != "" {
		// mcp-websearch.ts:4-6 puts an optional Exa key in the query and
		// otherwise calls the public endpoint unchanged.
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		endpoint += separator + "exaApiKey=" + encodeURIComponent(key)
	}
	if provider == "parallel" {
		endpoint = options.parallelURL
		if endpoint == "" {
			endpoint = defaultParallelWebSearchURL
		}
		toolName = "web_search"
		arguments = map[string]any{
			"objective": input.Query, "search_queries": []string{input.Query},
			"session_id": call.SessionID,
		}
		if call.ModelID != "" {
			arguments["model_name"] = firstRunes(call.ModelID, 100)
		}
		version := options.version
		if version == "" {
			version = "local"
		}
		headers["User-Agent"] = "codeaf/" + version
		if key := os.Getenv("PARALLEL_API_KEY"); key != "" {
			headers["Authorization"] = "Bearer " + key
		}
	}
	// aforge-embed: D13 — Firecrawl returns the main-page Markdown the issue's
	// zero-setup contract promises. The optional key raises the public quota; it
	// does not decide whether a result carries content.
	if provider == "firecrawl" {
		endpoint = options.firecrawlURL
		if endpoint == "" {
			endpoint = defaultFirecrawlWebSearchURL
		}
		toolName = "firecrawl_search"
		arguments = map[string]any{
			"query": input.Query,
			"limit": nonzeroOr(input.NumResults, 8),
			"scrapeOptions": map[string]any{
				"formats": []string{"markdown"}, "onlyMainContent": true,
			},
		}
		if key := strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY")); key != "" {
			headers["Authorization"] = "Bearer " + key
		}
	}
	result, err := callMCPWebSearch(ctx, endpoint, toolName, arguments, headers)
	if err != nil || provider != "firecrawl" {
		return result, err
	}
	return parseFirecrawlResults(result, true, firecrawlContentLimit(input))
}

func firecrawlContentLimit(input webSearchInput) int {
	if input.ContextMaxCharacters == nil {
		return firecrawlContextCharacters
	}
	limit := *input.ContextMaxCharacters
	if math.IsNaN(limit) || limit <= 0 {
		return firecrawlContextCharacters
	}
	// Clamp in floating-point space before converting so a huge model-supplied
	// JSON number cannot wrap into a negative slice bound on amd64.
	if math.IsInf(limit, 0) || limit > float64(firecrawlMaxContextCharacters) {
		return firecrawlMaxContextCharacters
	}
	return int(limit)
}

// aforge-embed: D13 — Firecrawl's nested JSON text becomes the readable result
// blocks returned by the embedded engine rather than leaking a vendor dump.
func parseFirecrawlResults(payload string, includeContent bool, maxCharacters int) (string, error) {
	var response struct {
		Success bool            `json:"success"`
		Error   json.RawMessage `json:"error"`
		Data    struct {
			Web []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Markdown    string `json:"markdown"`
			} `json:"web"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return "", fmt.Errorf("invalid Firecrawl response: %w", err)
	}
	if !response.Success {
		message := strings.TrimSpace(string(response.Error))
		var plain string
		if json.Unmarshal(response.Error, &plain) == nil && strings.TrimSpace(plain) != "" {
			message = strings.TrimSpace(plain)
		}
		if message == "" || message == "null" {
			message = "request was unsuccessful"
		}
		return "", fmt.Errorf("Firecrawl search failed: %s", message)
	}
	blocks := make([]string, 0, len(response.Data.Web))
	for index, hit := range response.Data.Web {
		title := strings.TrimSpace(hit.Title)
		if title == "" {
			title = "(untitled)"
		}
		var block strings.Builder
		fmt.Fprintf(&block, "%d. %s — %s", index+1, title, strings.TrimSpace(hit.URL))
		if description := strings.Join(strings.Fields(hit.Description), " "); description != "" {
			fmt.Fprintf(&block, "\n   %s", description)
		}
		if includeContent {
			if markdown := strings.TrimSpace(hit.Markdown); markdown != "" {
				fmt.Fprintf(&block, "\n\n%s", firstRunes(markdown, maxCharacters))
			}
		}
		blocks = append(blocks, block.String())
	}
	return strings.Join(blocks, "\n\n"), nil
}

func callMCPWebSearch(
	ctx context.Context,
	endpoint, toolName string,
	arguments map[string]any,
	headers map[string]string,
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
	payload.Params.Name = toolName
	payload.Params.Arguments = arguments
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("InvalidUrl error (POST %s)", endpoint)
	}
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := webClient(ctx).Do(request)
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s request timed out", toolName)
		}
		return "", transportError(http.MethodPost, endpoint)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", statusCodeError(http.MethodPost, endpoint, response.StatusCode)
	}
	if declared := response.Header.Get("Content-Length"); declared != "" {
		if size, parseErr := strconv.ParseInt(declared, 10, 64); parseErr == nil && size > webSearchMaxResponseSize {
			return "", fmt.Errorf("Response too large (exceeds 5MB limit)")
		}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, webSearchMaxResponseSize+1))
	if err != nil {
		return "", err
	}
	if len(raw) > webSearchMaxResponseSize {
		return "", fmt.Errorf("Response too large (exceeds 5MB limit)")
	}
	// mcp-websearch.ts:22-40 accepts either a direct JSON-RPC object or SSE
	// data lines and returns the first non-empty content text.
	return parseMCPWebSearchResponse(string(raw))
}

func parseMCPWebSearchResponse(body string) (string, error) {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") {
		return parseMCPWebSearchPayload(trimmed)
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		result, err := parseMCPWebSearchPayload(line[6:])
		if err != nil || result != "" {
			return result, err
		}
	}
	return "", nil
}

func parseMCPWebSearchPayload(payload string) (string, error) {
	trimmed := strings.TrimSpace(payload)
	if !strings.HasPrefix(trimmed, "{") {
		return "", nil
	}
	var value struct {
		Result *struct {
			Content []struct {
				Type *string `json:"type"`
				Text *string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return "", err
	}
	if value.Result == nil || value.Result.Content == nil {
		return "", fmt.Errorf("invalid MCP response")
	}
	for _, item := range value.Result.Content {
		if item.Type == nil || item.Text == nil {
			return "", fmt.Errorf("invalid MCP response")
		}
	}
	for _, item := range value.Result.Content {
		if *item.Text != "" {
			return *item.Text, nil
		}
	}
	return "", nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func nonzeroOr(value *float64, fallback float64) float64 {
	if value == nil || *value == 0 {
		return fallback
	}
	return *value
}

func encodeURIComponent(value string) string {
	const hexadecimal = "0123456789ABCDEF"
	var output strings.Builder
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("-_.!~*'()", rune(character)) {
			output.WriteByte(character)
			continue
		}
		output.WriteByte('%')
		output.WriteByte(hexadecimal[character>>4])
		output.WriteByte(hexadecimal[character&15])
	}
	return output.String()
}
