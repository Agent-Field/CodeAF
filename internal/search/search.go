// Package search is the web-search layer the v3 session agent calls through:
// one [Provider] that answers a query with a list of links, one [Fetcher] that
// turns a link into readable text, and an OPEN registry that decides which
// implementation of each the surface actually gets.
//
// The registry is the design. Exa is one plug and not the point — it is the
// keyed plug that happens to exist today. A search back end that arrives later
// declares itself from its own file's init, and no caller changes. The one
// deliberate list in this file orders zero-key defaults: without it, Go's
// filename-ordered inits would silently decide which free service wins.
//
// The other half of the design is that search WORKS with no configuration at
// all. A person who has typed no keys still gets results, because the last rung
// of the ladder is a zero-key plug (Firecrawl's keyless endpoint) that is
// always available. Keys are an upgrade someone opts into when they care about
// result quality or rate ceilings, not a prerequisite for the agent to look
// anything up. DuckDuckGo remains registered as a safety-valve pin.
//
// The package imports nothing of the surface — no session, no config, no
// provider, no store. Configuration arrives as a plain [Options] value, so the
// resolution law can be tested without a profile directory on disk and the
// wiring wave can fill Options from whichever settings store it likes.
package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Result is one hit: what every back end has in common after mapping. The
// fields are deliberately flat strings — this is what gets rendered into a
// prompt, not a model of anybody's API.
type Result struct {
	// Title is the page title, tags stripped.
	Title string
	// URL is the destination, already unwrapped from any redirect the back
	// end put in front of it.
	URL string
	// Snippet is the back end's extract: the sentences that made it a hit.
	Snippet string
	// Published is the publication date as the back end reported it, in
	// whatever format that was, and empty when it reported none. It is
	// carried as a string rather than a time.Time on purpose: back ends
	// disagree about format and precision, half of them guess, and the only
	// consumer is a renderer that shows it to a model. Parsing it would
	// invent certainty the source does not have.
	Published string
}

// Provider is a search back end.
type Provider interface {
	// Name is the plug's identifier — the string a person pins in settings
	// and the name that appears in an error.
	Name() string
	// Search answers a query with at most limit results. A limit of zero or
	// less means "the plug's own sensible default".
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}

// Fetcher turns one page into clean text — the second half of a search: the
// agent finds a link, then reads it. Markdown-ish output is expected and
// welcome; raw HTML is not.
type Fetcher interface {
	// Name is the plug's identifier, as on [Provider].
	Name() string
	// Fetch returns the page at url as text.
	Fetch(ctx context.Context, url string) (string, error)
}

// SearchPlug is the optional half a [Provider] implements when it needs
// configuration to run. The registry stores prototypes — a plug registers
// itself once from an init, before any key is known — and hands each prototype
// the [Options] at resolve time to produce the configured provider.
//
// A registered Provider that does NOT implement SearchPlug is legal: it is
// treated as always available, needing nothing, and is used as-is.
type SearchPlug interface {
	Provider
	// Available reports whether opts carry what this plug needs. It must be
	// cheap and LOCAL — key presence and nothing else. Never a network
	// probe: resolution happens on every call, and a resolution that can
	// hang or cost money is a resolution that will do both at the worst
	// moment.
	Available(opts Options) bool
	// Bind returns the plug configured for opts. The prototype is not
	// mutated, so one registration serves every session in the process.
	Bind(opts Options) Provider
}

// FetchPlug is [SearchPlug] for the fetch side.
type FetchPlug interface {
	Fetcher
	Available(opts Options) bool
	Bind(opts Options) Fetcher
}

// Options is everything the layer is configured with. A plain struct, filled
// by the caller: a sibling package owns settings, and this package must not
// learn its shape.
type Options struct {
	// Provider pins one plug by name. Empty means auto — the resolution law
	// in [Resolve] picks. The pin is matched against both registries, so
	// naming a fetch plug pins the fetcher and leaves search on auto.
	Provider string
	// ExaKey is the Exa API key. Its presence is what makes the exa plugs
	// available.
	ExaKey string
	// FirecrawlKey is optional: Firecrawl search is keyless, and a key raises
	// its ceiling rather than gating availability. The fetch plug does require
	// it, so a paid account upgrades page reads while search keeps working with
	// or without one.
	FirecrawlKey string
	// JinaKey is optional: r.jina.ai answers unauthenticated at 20 requests
	// per minute, and a key only raises that ceiling. So it does NOT gate
	// availability — jina is a zero-key plug that happens to take a key.
	JinaKey string
	// HTTPClient is the client every plug makes its requests with. Nil means
	// the package default. A caller that wants a proxy, a custom transport
	// or a recording client in a test sets it here.
	HTTPClient *http.Client
}

// ErrNoAPIKey is shared by keyed plugs so the tool, settings hint, and tests
// all describe an unavailable pinned search with the same words.
var ErrNoAPIKey = errors.New("no API key")

// client is the client the plugs use: the caller's, or a shared default.
//
// The default's own timeout is the outer backstop; the real per-call deadline
// is a context the plug sets, which is the one that also cuts a slow body read.
func (o Options) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return defaultClient
}

var defaultClient = &http.Client{Timeout: 60 * time.Second}

// Per-call deadlines. Search is interactive — the agent is waiting on it and a
// slow back end is worth abandoning. A fetch pulls and cleans a whole page, so
// it gets longer.
const (
	searchTimeout = 10 * time.Second
	fetchTimeout  = 20 * time.Second
)

// Result-count bounds for a call to a back end. defaultLimit is what a caller
// asking for nothing gets; maxLimit stops an agent that asked for a thousand
// from paying for a thousand.
const (
	defaultLimit = 5
	maxLimit     = 25
)

func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultLimit
	case limit > maxLimit:
		return maxLimit
	}
	return limit
}

var (
	registryMu sync.RWMutex
	searchReg  []Provider
	fetchReg   []Fetcher
)

// keylessOrder is the one explicit preference among zero-key plugs. Go runs
// init functions in filename order, and a product default decided by that
// order would change when a file was renamed. Names absent from this list keep
// their registration order, preserving the registry's open extension seam.
var keylessOrder = []string{"firecrawl", "duckduckgo"}

// RegisterSearch adds a search plug. Meant to be called from a package's init,
// which is why it panics rather than returning an error: a plug that failed to
// register would not fail at registration but silently at resolution, by
// resolving to something else that looked fine.
//
// Order of registration is recorded but deliberately does NOT decide the auto
// winner between a keyed and a zero-key plug — see [Resolve]. Go runs a
// package's init functions in filename order, and a law that depended on that
// would be a law that changed when a file was renamed.
func RegisterSearch(p Provider) {
	if p == nil {
		panic("search: register nil provider")
	}
	if strings.TrimSpace(p.Name()) == "" {
		panic("search: register provider with empty name")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	searchReg = append(searchReg, p)
}

// RegisterFetch adds a fetch plug, on the same terms as [RegisterSearch].
func RegisterFetch(f Fetcher) {
	if f == nil {
		panic("search: register nil fetcher")
	}
	if strings.TrimSpace(f.Name()) == "" {
		panic("search: register fetcher with empty name")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	fetchReg = append(fetchReg, f)
}

// RegisteredSearch lists the search plugs in registration order, and
// RegisteredFetch the fetch plugs — for a settings screen that wants to show
// what exists rather than what won.
func RegisteredSearch() []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return append([]Provider(nil), searchReg...)
}

// RegisteredFetch lists the fetch plugs in registration order.
func RegisteredFetch() []Fetcher {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return append([]Fetcher(nil), fetchReg...)
}

// Resolve answers which plugs a session's searches and fetches go to.
//
// The law, in order:
//
//  1. A PIN WINS. opts.Provider naming a registered plug ends it, whether or
//     not that plug has its key. An explicit instruction is honoured, and the
//     resulting error names the missing key plainly — which is a better
//     outcome than silently searching somewhere the person did not ask for. A
//     pin naming nothing registered falls through rather than failing, so a
//     stale settings value cannot take search away.
//  2. Otherwise the first AVAILABLE KEYED plug in registration order. Keyed
//     means the plug reports itself unavailable under empty Options — exa,
//     when ExaKey is set.
//  3. Otherwise the zero-key default: the earliest named plug in
//     [keylessOrder], then unlisted plugs in registration order. That is
//     Firecrawl for search and jina for fetch. This rung cannot fail to
//     produce a plug, which is why Resolve returns no error.
//
// Availability is key presence and nothing more, so the whole ladder is a few
// string comparisons and can run per call.
//
// The two registries resolve independently — pinning "ddg" for search still
// leaves fetch free to pick exa-fetch when a key is present — because the two
// jobs are different jobs and the best plug for one is not the best for the
// other.
//
// Resolve returns nil for a side whose registry is empty. That does not happen
// in a built binary, where the zero-key plugs self-register, but it is what a
// test that empties the registry sees.
func Resolve(opts Options) (Provider, Fetcher) {
	registryMu.RLock()
	providers := append([]Provider(nil), searchReg...)
	fetchers := append([]Fetcher(nil), fetchReg...)
	registryMu.RUnlock()

	pin := strings.ToLower(strings.TrimSpace(opts.Provider))

	var provider Provider
	if pin != "" {
		for _, p := range providers {
			if strings.ToLower(p.Name()) == pin {
				provider = bindSearch(p, opts)
				break
			}
		}
	}
	if provider == nil {
		provider = autoSearch(providers, opts)
	}

	var fetcher Fetcher
	if pin != "" {
		for _, f := range fetchers {
			if strings.ToLower(f.Name()) == pin {
				fetcher = bindFetch(f, opts)
				break
			}
		}
	}
	if fetcher == nil {
		fetcher = autoFetch(fetchers, opts)
	}

	return provider, fetcher
}

// Live returns a provider and fetcher that resolve again for every operation.
// The first resolution decides whether each capability exists at all; built
// binaries always have both, while an intentionally empty registry still
// leaves the capability off the session's belt. The wrappers retain only the
// options function, so a copy inherited by child work observes the same
// settings as its parent.
func Live(options func() Options) (Provider, Fetcher) {
	if options == nil {
		return nil, nil
	}
	provider, fetcher := Resolve(options())
	if provider != nil {
		provider = liveProvider{options: options}
	}
	if fetcher != nil {
		fetcher = liveFetcher{options: options}
	}
	return provider, fetcher
}

type liveProvider struct {
	options func() Options
}

func (p liveProvider) Name() string {
	current, _ := Resolve(p.options())
	if current == nil {
		return ""
	}
	return current.Name()
}

func (p liveProvider) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	results, _, err := p.searchWithName(ctx, query, limit)
	return results, err
}

func (p liveProvider) searchWithName(ctx context.Context, query string, limit int) ([]Result, string, error) {
	current, _ := Resolve(p.options())
	if current == nil {
		return nil, "", errors.New("search is unavailable")
	}
	name := current.Name()
	results, err := current.Search(ctx, query, limit)
	return results, name, err
}

type liveFetcher struct {
	options func() Options
}

func (f liveFetcher) Name() string {
	_, current := Resolve(f.options())
	if current == nil {
		return ""
	}
	return current.Name()
}

func (f liveFetcher) Fetch(ctx context.Context, url string) (string, error) {
	text, _, err := f.fetchWithName(ctx, url)
	return text, err
}

func (f liveFetcher) fetchWithName(ctx context.Context, url string) (string, string, error) {
	_, current := Resolve(f.options())
	if current == nil {
		return "", "", errors.New("page reading is unavailable")
	}
	name := current.Name()
	text, err := current.Fetch(ctx, url)
	return text, name, err
}

// SearchWithName runs one search and returns the plug chosen for that same
// operation. Live providers implement the private method so resolving current
// settings and naming the receipt share one snapshot; an ordinary Provider is
// already one fixed plug and needs no extra machinery on its public interface.
func SearchWithName(ctx context.Context, provider Provider, query string, limit int) ([]Result, string, error) {
	if provider == nil {
		return nil, "", errors.New("search is unavailable")
	}
	if live, ok := provider.(interface {
		searchWithName(context.Context, string, int) ([]Result, string, error)
	}); ok {
		return live.searchWithName(ctx, query, limit)
	}
	name := provider.Name()
	results, err := provider.Search(ctx, query, limit)
	return results, name, err
}

// FetchWithName is SearchWithName for page reads. Its name is used only on a
// failed receipt today, but it must still describe the fetcher that actually
// ran when settings change while a request is in flight.
func FetchWithName(ctx context.Context, fetcher Fetcher, url string) (string, string, error) {
	if fetcher == nil {
		return "", "", errors.New("page reading is unavailable")
	}
	if live, ok := fetcher.(interface {
		fetchWithName(context.Context, string) (string, string, error)
	}); ok {
		return live.fetchWithName(ctx, url)
	}
	name := fetcher.Name()
	text, err := fetcher.Fetch(ctx, url)
	return text, name, err
}

// Status describes the search plug the same options would select now. It is
// intentionally resolved from Options rather than from saved session state,
// because both the status deck and the next call must move together.
func Status(opts Options) string {
	provider, _ := Resolve(opts)
	if provider == nil {
		return ""
	}
	name := provider.Name()
	switch name {
	case "exa":
		if strings.TrimSpace(opts.ExaKey) == "" {
			return name + " · key not set — searches fail"
		}
		return name + " · with your key"
	case "jina-search":
		if strings.TrimSpace(opts.JinaKey) == "" {
			return name + " · key not set — searches fail"
		}
		return name + " · with your key"
	case "firecrawl":
		if strings.TrimSpace(opts.FirecrawlKey) != "" {
			return name + " · with your key"
		}
		return name + " · keyless"
	default:
		return name + " · keyless"
	}
}

// Failure spells a search failure once for the tool and for settings guidance
// that quotes the exact answer a pinned, unconfigured plug will produce.
func Failure(name string, err error) string {
	return fmt.Sprintf("Search failed (%s): %v", name, err)
}

// autoSearch runs rungs 2 and 3 of the ladder: keyed-and-available first, the
// zero-key default second.
func autoSearch(providers []Provider, opts Options) Provider {
	var fallback Provider
	fallbackRank := len(keylessOrder)
	for _, p := range providers {
		plug, ok := p.(SearchPlug)
		if !ok || plug.Available(Options{}) {
			// Needs nothing: the zero-key rung. Prefer only an explicitly
			// earlier default; equal-ranked unlisted plugs keep init order.
			rank := keylessRank(p.Name())
			if fallback == nil || rank < fallbackRank {
				fallback = bindSearch(p, opts)
				fallbackRank = rank
			}
			continue
		}
		if plug.Available(opts) {
			return plug.Bind(opts)
		}
	}
	return fallback
}

func autoFetch(fetchers []Fetcher, opts Options) Fetcher {
	var fallback Fetcher
	fallbackRank := len(keylessOrder)
	for _, f := range fetchers {
		plug, ok := f.(FetchPlug)
		if !ok || plug.Available(Options{}) {
			rank := keylessRank(f.Name())
			if fallback == nil || rank < fallbackRank {
				fallback = bindFetch(f, opts)
				fallbackRank = rank
			}
			continue
		}
		if plug.Available(opts) {
			return plug.Bind(opts)
		}
	}
	return fallback
}

func keylessRank(name string) int {
	for index, preferred := range keylessOrder {
		if strings.EqualFold(name, preferred) {
			return index
		}
	}
	return len(keylessOrder)
}

func bindSearch(p Provider, opts Options) Provider {
	if plug, ok := p.(SearchPlug); ok {
		return plug.Bind(opts)
	}
	return p
}

func bindFetch(f Fetcher, opts Options) Fetcher {
	if plug, ok := f.(FetchPlug); ok {
		return plug.Bind(opts)
	}
	return f
}

// Render bounds. These are prompt-budget numbers, not display numbers: what
// goes back to the agent has to be worth its tokens. Eight results is more
// than enough for a model to choose a link to fetch; a snippet past three
// hundred characters is the page's opening paragraph, which is what Fetch is
// for.
const (
	maxRendered   = 8
	maxSnippet    = 300
	maxFetchChars = 4000
)

// RenderResults formats results for the agent: a compact numbered list of
// "title — url" with the snippet indented under it, and a count footer so the
// model can tell "these are all of them" from "these are the first few". The
// footer also names the plug that answered so both the model transcript and
// the compact tool receipt can report the same fact.
//
// limit is the caller's own cap, further clamped to [maxRendered].
func RenderResults(results []Result, limit int, backend string) string {
	if len(results) == 0 {
		return namedSummary("no results", backend)
	}
	if limit <= 0 || limit > maxRendered {
		limit = maxRendered
	}
	shown := results
	if len(shown) > limit {
		shown = shown[:limit]
	}

	var b strings.Builder
	for i, r := range shown {
		title := collapse(r.Title)
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "%d. %s — %s\n", i+1, title, r.URL)
		if r.Published != "" {
			fmt.Fprintf(&b, "  %s\n", r.Published)
		}
		if snippet := collapse(r.Snippet); snippet != "" {
			text, _ := clip(snippet, maxSnippet)
			fmt.Fprintf(&b, "  %s\n", text)
		}
	}
	if len(results) > len(shown) {
		fmt.Fprintf(&b, "\n%s", namedSummary(fmt.Sprintf("%d of %d results", len(shown), len(results)), backend))
	} else {
		fmt.Fprintf(&b, "\n%s", namedSummary(fmt.Sprintf("%d result%s", len(shown), plural(len(shown))), backend))
	}
	return b.String()
}

func namedSummary(summary, backend string) string {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return summary
	}
	return summary + " · " + backend
}

// ResultSummary returns the named count footer from rendered search output.
// It accepts only the shapes RenderResults emits, which keeps an error or an
// arbitrary final body line from becoming a misleading success receipt.
func ResultSummary(rendered string) string {
	lines := strings.Split(strings.TrimSpace(rendered), "\n")
	if len(lines) == 0 {
		return ""
	}
	footer := strings.TrimSpace(lines[len(lines)-1])
	parts := strings.Split(footer, " · ")
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" || !validResultCount(parts[0]) {
		return ""
	}
	return footer
}

func validResultCount(summary string) bool {
	if summary == "no results" {
		return true
	}
	fields := strings.Fields(summary)
	switch len(fields) {
	case 2:
		count, err := strconv.Atoi(fields[0])
		return err == nil && count > 0 && (fields[1] == "result" || fields[1] == "results")
	case 4:
		shown, shownErr := strconv.Atoi(fields[0])
		total, totalErr := strconv.Atoi(fields[2])
		return shownErr == nil && totalErr == nil && shown > 0 && total >= shown && fields[1] == "of" && fields[3] == "results"
	default:
		return false
	}
}

// RenderFetch formats fetched page text for the agent, capped, with the
// overflow announced rather than hidden. A model that can see it was cut can
// ask for the rest or narrow its question; a model handed a silently truncated
// page concludes the page ended there.
func RenderFetch(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "(empty page)"
	}
	kept, more := clip(text, maxFetchChars)
	if more == 0 {
		return kept
	}
	return fmt.Sprintf("%s… (%d more bytes)", kept, more)
}

// clip cuts text to at most max bytes without splitting a rune, and reports
// how many bytes it dropped.
func clip(text string, max int) (string, int) {
	if len(text) <= max {
		return text, 0
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimRight(text[:cut], " \t\n"), len(text) - cut
}

// collapse folds every run of whitespace to one space. Back ends return
// snippets with the source page's newlines and indentation still in them, and
// a multi-line snippet breaks the one-line-per-field shape the renderer's
// output relies on.
func collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
