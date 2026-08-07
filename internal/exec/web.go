package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
)

// Web is search and page fetching, the one capability a shell does not already
// have. Fetching is plural because the shape of research is search once, then
// read several things — doing that one page at a time turns a single round-trip
// into five, each carrying the whole accumulated context.
type Web struct {
	apiKey string
	http   *http.Client
}

// NewWeb is always available: fetching needs no key at all, and search
// degrades from Exa (when EXA_API_KEY is set) to DuckDuckGo's keyless HTML
// endpoint rather than disappearing. A worker without internet is a worker
// that fails jobs it could have finished.
func NewWeb() *Web {
	return &Web{
		apiKey: strings.TrimSpace(os.Getenv("EXA_API_KEY")),
		http:   &http.Client{Timeout: 45 * time.Second},
	}
}

const perResultChars = 1200

// Search returns ranked results with enough text to judge them. Exa answers
// when a key is configured; DuckDuckGo covers both the keyless case and an
// Exa outage, so one provider having a bad day never blinds the workforce.
func (w *Web) Search(ctx context.Context, query string, limit int) (string, error) {
	if limit <= 0 || limit > 15 {
		limit = 6
	}
	if w.apiKey == "" {
		return w.searchDuckDuckGo(ctx, query, limit)
	}
	found, err := w.searchExa(ctx, query, limit)
	if err == nil {
		return found, nil
	}
	fallback, ddgErr := w.searchDuckDuckGo(ctx, query, limit)
	if ddgErr != nil {
		return "", fmt.Errorf("exa: %v; duckduckgo fallback: %v", err, ddgErr)
	}
	return fallback, nil
}

// searchExa is the paid path: ranked results with page text inline, so a good
// hit often needs no follow-up fetch at all.
func (w *Web) searchExa(ctx context.Context, query string, limit int) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"query":      query,
		"numResults": limit,
		"contents":   map[string]any{"text": map[string]any{"maxCharacters": perResultChars}},
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.exa.ai/search", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("x-api-key", w.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := w.http.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("exa %d: %s", response.StatusCode, clamp(string(body)))
	}
	var decoded struct {
		Results []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
			Text  string `json:"text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Results) == 0 {
		return "no results", nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "search: %s\n", query)
	for index, item := range decoded.Results {
		fmt.Fprintf(&out, "\n[%d] %s\n%s\n%s\n", index+1, item.Title, item.URL, strings.TrimSpace(item.Text))
	}
	return out.String(), nil
}

var (
	ddgResult  = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippet = regexp.MustCompile(`(?s)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
)

// searchDuckDuckGo is the free path: the keyless HTML endpoint, parsed just
// enough to yield title, real URL, and snippet. Quality is below Exa's and
// there is no inline page text, but it turns "no key, no internet" into
// "search, then fetch what looks right".
func (w *Web) searchDuckDuckGo(ctx context.Context, query string, limit int) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://html.duckduckgo.com/html/?q="+neturl.QueryEscape(query), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; aforge/1.0)")
	response, err := w.http.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("duckduckgo http %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	page := string(body)
	links := ddgResult.FindAllStringSubmatch(page, limit)
	snippets := ddgSnippet.FindAllStringSubmatch(page, limit)
	if len(links) == 0 {
		return "no results", nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "search: %s\n", query)
	for index, link := range links {
		title := htmlToText(link[2])
		fmt.Fprintf(&out, "\n[%d] %s\n%s\n", index+1, title, decodeDDGURL(link[1]))
		if index < len(snippets) {
			out.WriteString(htmlToText(snippets[index][1]) + "\n")
		}
	}
	return out.String(), nil
}

// decodeDDGURL unwraps DuckDuckGo's redirect links (//duckduckgo.com/l/?uddg=…)
// back to the destination URL, so fetch calls hit the page rather than the
// redirector.
func decodeDDGURL(raw string) string {
	raw = strings.ReplaceAll(raw, "&amp;", "&")
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return raw
	}
	if target := parsed.Query().Get("uddg"); target != "" {
		return target
	}
	return raw
}

// Fetch retrieves several pages at once and returns them as text. A page that
// fails is reported in place rather than failing the batch — one dead link
// should not cost the other four.
func (w *Web) Fetch(ctx context.Context, urls []string) string {
	if len(urls) > 8 {
		urls = urls[:8]
	}
	results := make([]string, len(urls))
	var group sync.WaitGroup
	for index, url := range urls {
		group.Add(1)
		go func(index int, url string) {
			defer group.Done()
			// A dead link already reports in place; a fault in the fetch does the
			// same, so one bad page never takes the leaf — or the surface — down.
			defer func() {
				if recovered := recover(); recovered != nil {
					_ = guard.Note("exec/web fetch", recovered)
					results[index] = fmt.Sprintf("--- %s\ncould not fetch: internal fault, recorded to the log", url)
				}
			}()
			text, err := w.fetchOne(ctx, url)
			if err != nil {
				results[index] = fmt.Sprintf("--- %s\ncould not fetch: %v", url, err)
				return
			}
			results[index] = fmt.Sprintf("--- %s\n%s", url, text)
		}(index, url)
	}
	group.Wait()
	return strings.Join(results, "\n\n")
}

const perPageChars = 6000

func (w *Web) fetchOne(ctx context.Context, url string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; aforge/1.0)")
	response, err := w.http.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("http %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return "", err
	}
	text := htmlToText(string(body))
	if len(text) > perPageChars {
		text = text[:perPageChars] + "\n... [truncated]"
	}
	return text, nil
}

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

// htmlToText strips markup well enough to read. It is intentionally crude: the
// consumer is a language model, which tolerates ragged text far better than it
// tolerates paying for markup it will ignore.
func htmlToText(page string) string {
	page = dropElements.ReplaceAllString(page, " ")
	page = blockBreaks.ReplaceAllString(page, "\n")
	page = anyTag.ReplaceAllString(page, " ")
	replacer := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&mdash;", "—", "&ndash;", "–",
	)
	page = replacer.Replace(page)
	page = manySpaces.ReplaceAllString(page, " ")
	page = manyBlanks.ReplaceAllString(page, "\n\n")
	return strings.TrimSpace(page)
}
