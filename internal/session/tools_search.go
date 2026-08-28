package session

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/search"
)

// The two hands the session has outside this machine: find a page, read a page.
//
// They are one file and two tools because that is how the work actually goes —
// a model searches, reads the list, picks a link, fetches it — and a single
// merged tool would either fetch every hit (slow, and mostly pages nobody
// wanted) or make the model say which hit to fetch before it has seen the list.
//
// Neither tool knows a vendor. Both call an interface [Config] was handed
// (internal/search resolved it against the person's settings at boot), so
// adding a back end changes nothing here, and a test drives a scripted one
// without a network.

// Result-count bounds for one web_search call. The default is what a model
// asking for nothing gets; the cap is [search.RenderResults]'s own render
// bound, restated here so the number the model is PROMISED in the schema is the
// number it can actually receive. A schema advertising twenty results that the
// renderer then cuts to eight is a schema that lies.
const (
	searchDefaultCount = 5
	searchMaxCount     = 8
)

const webSearchDescription = "Search the web and get back a numbered list of results: title, URL, and a snippet of each page. Use it for anything outside this machine and outside your training data — current events, release notes, error messages you do not recognise, library documentation. Follow it with web_fetch on the URLs worth reading in full: the snippets are extracts, not the page."

const webSearchSchemaJSON = `{"type":"object","properties":{"query":{"type":"string","description":"What to search for, as you would type it into a search engine"},"count":{"type":"integer","description":"How many results to return (default: 5, maximum: 8)"}},"required":["query"],"additionalProperties":false}`

const webFetchDescription = "Fetch one web page and return its text, with the markup stripped. Use it on a URL from web_search, or on any URL the user gives you. Long pages are truncated and say how much was left behind; the read tool, not this one, is what opens a file on this machine."

const webFetchSchemaJSON = `{"type":"object","properties":{"url":{"type":"string","description":"The absolute URL of the page to read, including its scheme (https://…)"}},"required":["url"],"additionalProperties":false}`

// searchTools is the web half of the belt, and it is CONDITIONAL: a nil half
// contributes no tool.
//
// The condition is the point. A belt is a promise — every tool on it is a thing
// the model has been told it can do — and a web_search that answers "no search
// back end is configured" breaks that promise in the most expensive way
// available: the model pays for the call, reads a refusal that sounds
// temporary, and keeps the capability in its plan for the rest of the turn. A
// model that was never told about web_search simply says it cannot look things
// up, which is both true and free.
//
// In a built binary neither half is ever nil — internal/search's zero-key plugs
// self-register — so this is the shape a stripped build, a test, or a caller
// that never wired the pair sees.
func (a *Agent) searchTools() []bare.Tool {
	var tools []bare.Tool
	if a.config.SearchProvider != nil {
		tools = append(tools, a.webSearchTool(a.config.SearchProvider))
	}
	if a.config.SearchFetcher != nil {
		tools = append(tools, a.webFetchTool(a.config.SearchFetcher))
	}
	return tools
}

func (a *Agent) webSearchTool(provider search.Provider) bare.Tool {
	return bare.Tool{
		Name:        "web_search",
		Description: webSearchDescription,
		Schema:      json.RawMessage(webSearchSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Query string `json:"query"`
				Count *int   `json:"count"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			query := strings.TrimSpace(parsed.Query)
			if query == "" {
				return "Invalid arguments: query is required", true, nil
			}
			count := searchCount(parsed.Count)
			results, err := provider.Search(ctx, query, count)
			if err != nil {
				// A failed search is a TOOL ERROR and never a Go error: the
				// network is down, the key expired, the back end rate-limited
				// us — all of them things the model can act on (try again,
				// narrow the query, say it could not look it up) and none of
				// them a reason to fail the turn. The plug's name is in the
				// message because "search failed" without it leaves a person
				// reading the transcript no way to tell which back end broke.
				return "Search failed (" + provider.Name() + "): " + err.Error(), true, nil
			}
			return search.RenderResults(results, count), false, nil
		},
	}
}

func (a *Agent) webFetchTool(fetcher search.Fetcher) bare.Tool {
	return bare.Tool{
		Name:        "web_fetch",
		Description: webFetchDescription,
		Schema:      json.RawMessage(webFetchSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				URL string `json:"url"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			url := strings.TrimSpace(parsed.URL)
			if url == "" {
				return "Invalid arguments: url is required", true, nil
			}
			text, err := fetcher.Fetch(ctx, url)
			if err != nil {
				return "Fetch failed (" + fetcher.Name() + "): " + err.Error(), true, nil
			}
			return search.RenderFetch(text), false, nil
		},
	}
}

// searchCount clamps the model's ask. An absent count is the default; a zero,
// a negative or an over-ask is corrected silently rather than refused, because
// a tool call that did the sensible thing is worth more to the person watching
// than one that spent a round trip arguing about a number.
func searchCount(asked *int) int {
	if asked == nil {
		return searchDefaultCount
	}
	switch {
	case *asked < 1:
		return searchDefaultCount
	case *asked > searchMaxCount:
		return searchMaxCount
	}
	return *asked
}
