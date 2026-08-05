package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Web is search and page fetching, the one capability a shell does not already
// have. Fetching is plural because the shape of research is search once, then
// read several things — doing that one page at a time turns a single round-trip
// into five, each carrying the whole accumulated context.
type Web struct {
	apiKey string
	http   *http.Client
}

// NewWeb returns nil when no key is configured, so the tool reports itself
// unavailable rather than failing at the moment it is first needed.
func NewWeb() *Web {
	key := strings.TrimSpace(os.Getenv("EXA_API_KEY"))
	if key == "" {
		return nil
	}
	return &Web{apiKey: key, http: &http.Client{Timeout: 45 * time.Second}}
}

const perResultChars = 1200

// Search returns ranked results with enough text to judge them. The snippet is
// bounded deliberately: the point of a search result is to decide whether the
// page is worth fetching, and a full page pasted into the result defeats both
// the ranking and the budget.
func (w *Web) Search(ctx context.Context, query string, limit int) (string, error) {
	if limit <= 0 || limit > 15 {
		limit = 6
	}
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
