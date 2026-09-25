// Package catalog owns OpenRouter model discovery across every modality.
// Callers ask narrow capability questions; fetching, TTLs, and offline
// fallbacks stay behind this seam so chat, graph tools, and future voice input
// do not grow separate model caches.
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/roles"
)

const (
	TTL             = 24 * time.Hour
	maxCatalogBytes = 16 << 20
	cacheName       = "model-catalog.json"
	// DefaultBaseURL lives beside the compiled-in fallback rows because those
	// rows are this service's ids, so the package that serves them has to be
	// able to recognise its own base.
	DefaultBaseURL = "https://openrouter.ai/api/v1"
)

// Model is the small, durable part of one OpenRouter catalog row. Pricing is
// display-ready economics for the existing picker; architecture is retained
// verbatim for modality queries.
type Model struct {
	ID string `json:"id"`
	// CanonicalSlug is the concrete model behind a floating alias. OpenRouter
	// publishes ids like `deepseek/deepseek-v4-flash-latest` that resolve, at
	// request time and on its side, to whatever is current — which is why every
	// call codeaf makes with the alias simply works. A second catalog that does
	// not float, keyed by concrete id, has never heard of the alias, and this is
	// the field that translates between them. Empty for the great majority of
	// rows, and empty for every row in a cache written before it was read.
	CanonicalSlug string `json:"canonical_slug,omitempty"`
	// AliasTarget is where a floating id actually points, and it is the field
	// that makes [Catalog.Concrete] true.
	//
	// The comment above CanonicalSlug describes what that field was believed to
	// do. The live catalog on 2026-08-11 disagrees: for the eleven alias rows
	// it publishes, `canonical_slug` repeats the ALIAS ("~x-ai/grok-latest"),
	// and the concrete model sits in `alias_target.slug` ("x-ai/grok-4.5").
	// Resolving through canonical_slug alone therefore hands a floating id
	// straight back, which is precisely the failure Concrete exists to prevent.
	// Empty for every row that does not float.
	AliasTarget string `json:"alias_target,omitempty"`
	Name        string `json:"name,omitempty"`
	// ContextLength is how many tokens the model will actually accept, and it
	// was being thrown away by the row that already fetched it. Nothing priced
	// it, so nothing kept it — and downstream the loop that has to decide how
	// much transcript to carry was left sizing its memory from a spend ceiling
	// instead, which is how a leaf ended up with a 25KB window in front of a
	// 200k-token model. Zero means the provider did not say, or the row was
	// cached before this field existed; every reader must have an answer for
	// that case rather than treating zero as a tiny model.
	ContextLength   int     `json:"context_length,omitempty"`
	PromptPrice     float64 `json:"prompt_price,omitempty"`
	CompletionPrice float64 `json:"completion_price,omitempty"`
	RequestPrice    float64 `json:"request_price,omitempty"`
	// PriceUnknown says the provider published no number, which is a different
	// fact from a number that is zero and must not be shown as one.
	//
	// OpenRouter spells "it depends" as "-1": its own routers
	// (openrouter/auto and friends) charge whatever the model they pick
	// charges, and there were five such rows in the live catalog on
	// 2026-08-11 beside eighteen genuinely free ones priced "0". Collapsing
	// both to 0.0 — which is what this package did until this field existed —
	// tells a reader that a router is free. It is not; nobody yet knows what
	// it costs. A surface reads this before it reads the two prices, and
	// renders absence rather than "$0.00" (design-law-v2 §16 EMPTINESS).
	PriceUnknown bool `json:"price_unknown,omitempty"`
	// CacheReadPrice is what a token served off the provider's warm prefix
	// costs, per token — OpenRouter's `pricing.input_cache_read`. 246 of 413
	// rows published one on 2026-08-15; it is typically a tenth of PromptPrice,
	// and the difference between the two is the whole of what a prompt cache is
	// worth to a session that re-sends its transcript every step.
	//
	// Zero is "the provider did not say", exactly as with the other prices, and
	// a surface must render absence rather than a saving of the full prompt
	// price — a cache read is never free.
	CacheReadPrice float64 `json:"cache_read_price,omitempty"`
	// ArenaElo is the best Elo the row publishes across Design Arena's boards —
	// OpenRouter's `benchmarks.design_arena`, a LIST of
	// {arena, category, elo, win_rate, rank} objects, 155 of 413 rows non-empty
	// on 2026-08-15.
	//
	// The list is reduced to its MAXIMUM rather than averaged, and the choice is
	// about what the number is for: a row shows one figure, the boards are
	// different tasks rather than repeated measurements of one, and a model that
	// tops the webapps board and sits mid-table on 3d has a real strength an
	// average would report as mediocrity. Zero means nobody published one.
	ArenaElo float64 `json:"arena_elo,omitempty"`
	// The three Artificial Analysis scores the catalog keeps, carried verbatim
	// and never computed here.
	//
	// OpenRouter's rows may carry a `benchmarks` block, and inside it an
	// `artificial_analysis` object with `intelligence_index`, `coding_index`
	// and `agentic_index` — Artificial Analysis's numbers, republished. 155 of
	// 528 rows had one on 2026-08-11. Zero means NOBODY published a score, and
	// never a model that scored zero: a surface showing these must render the
	// zero as absence the way it renders an absent price.
	//
	// All three are kept because they are read seat by seat: the agentic and
	// coding indexes describe a long tool loop, the intelligence index a single
	// reasoning call, and the seat asking decides which one it needs.
	IntelligenceIndex float64 `json:"intelligence_index,omitempty"`
	CodingIndex       float64 `json:"coding_index,omitempty"`
	AgenticIndex      float64 `json:"agentic_index,omitempty"`
	// Created is when the model was listed, in Unix seconds — the release date
	// the crew router reads a model's age from. Zero means the row did not say
	// or was cached before this field was kept; a reader then falls back to a
	// date in the canonical slug, or to none.
	Created int64 `json:"created,omitempty"`
	// OpenWeights says the row's weights are published — OpenRouter's
	// `hugging_face_id`, kept as the one-word answer to whether the weights are
	// public. A row cached before this field existed reads false, which every
	// reader must take as unknown rather than closed.
	OpenWeights      bool     `json:"open_weights,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
	// Parameters is which request fields the provider says this model accepts —
	// OpenRouter's `supported_parameters`, lowercased and deduped.
	//
	// It is the only published answer to "may this call carry a reasoning knob",
	// and until it was kept, nothing could ask: the adapter's gate for that
	// question was wired to nil in production, so a harness economy went to
	// every model blind and 400ed the ones that do not take it. An empty list
	// means the provider said nothing or the row predates this field, which is
	// unknown rather than "supports nothing" — see [Catalog.SupportsParameter].
	Parameters []string `json:"parameters,omitempty"`
	// Reasoning is what the provider publishes about this model's thinking
	// pass — OpenRouter's `reasoning` block on the row. It is the second
	// published answer the adapter reads on the request path, beside
	// Parameters: whether the pass can be turned off at all, and which effort
	// words the model takes. Absent (Known false) on a row cached before it was
	// kept and on a row the provider published nothing for — see
	// [Catalog.ReasoningProfile].
	Reasoning ReasoningProfile `json:"reasoning,omitempty"`
}

// ReasoningProfile is the provider's own account of a model's thinking pass.
//
// Mandatory says the pass cannot be disabled: OpenRouter's rule for such a row
// is "hide disable controls and do not send effort: none — the model rejects
// it", and 83 of the rows carried it on 2026-08-28 (GLM 5.3, Gemini 3.7 Flash,
// Grok 4.6 among them). Efforts is the ladder of words the model accepts,
// lowercased and in the provider's order; DefaultEffort is where the model
// sits when nobody sends a word — for GLM 5.3 that is "max", which is why a
// request that sent nothing spent ten thousand tokens thinking.
type ReasoningProfile struct {
	Known         bool     `json:"known,omitempty"`
	Mandatory     bool     `json:"mandatory,omitempty"`
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
}

// Reasons says the provider accepts a reasoning knob on this model — the
// published fact, not an inference about how the model thinks. A row that
// carries no parameter list answers false, which is the same answer it gives
// for a model that genuinely takes no knob; a surface that needs to tell those
// apart should ask [Catalog.SupportsParameter], which reports its confidence.
func (m Model) Reasons() bool {
	return m.accepts("reasoning") || m.accepts("include_reasoning")
}

// ReasoningLevels says the effort can be dialled — `reasoning_effort` — rather
// than only switched on. It is the difference between a model whose thinking
// this harness can economize and one whose thinking it can only accept: MiniMax
// M2.7 takes `reasoning` and no level, and refuses to have it turned off at all.
func (m Model) ReasoningLevels() bool { return m.accepts("reasoning_effort") }

// ReasoningWord is the short phrase a surface shows beside a model for what it
// does with reasoning, and the empty string when there is nothing to say.
//
// Three published states are worth telling apart while choosing a model. Most
// of the catalog takes no reasoning knob at all and stays silent here. A model
// that takes one but publishes no level can be asked to think, but not how
// hard. A model that publishes `reasoning_effort` is the only kind whose
// thinking this harness can dial, which is what the planning economy does.
//
// alwaysOn is the fourth state and the one nobody publishes: an endpoint that
// has refused to have its reasoning turned off (provider.ReasoningMandatory).
// It is passed in rather than looked up because it is learned from rejected
// calls, and a published catalog is not where learned facts live.
//
// The phrase lives here, once, because three surfaces show it — the picker, the
// v2 palette, and `codeaf models` — and three spellings of one fact is how a
// product ends up meaning three different things by the same word.
func ReasoningWord(model Model, alwaysOn bool) string {
	if !model.Reasons() {
		return ""
	}
	if alwaysOn {
		return "reasoning · always on"
	}
	if model.ReasoningLevels() {
		return "reasoning · effort"
	}
	return "reasoning"
}

func (m Model) accepts(parameter string) bool {
	for _, supported := range m.Parameters {
		if supported == parameter {
			return true
		}
	}
	return false
}

type cache struct {
	FetchedAt time.Time `json:"fetched_at"`
	Models    []Model   `json:"models"`
	Source    string    `json:"source,omitempty"`
	Base      string    `json:"base,omitempty"`
}

// Options describes the one catalog fetch. Dir is the codeaf configuration
// directory (CODEAF_PROFILE_DIR when configured, ~/.codeaf otherwise).
type Options struct {
	// Source is the stable service identity. AN EMPTY SOURCE IS THE DEFAULT
	// SERVICE, whose ids are the only ones the compiled fallbacks describe.
	Source  string
	BaseURL string
	APIKey  string
	Dir     string
	// HTTPClient is the service-owned request road. Most OpenAI-compatible
	// catalogs leave it nil; services whose listing needs rotating credentials
	// or a wire translation supply the same client their model calls use.
	HTTPClient *http.Client
	Now        func() time.Time

	// Refresh spends the network even when the cache is inside [TTL]. It is
	// the ONLY way a fetch happens off the daily clock, and it exists so a
	// person who just watched a provider ship a model can ask for it by hand
	// rather than being told to wait a day or delete a file.
	//
	// A refresh that fails still degrades to the cache it was trying to
	// replace: asking for fresher facts must never leave a surface with fewer
	// facts than it had.
	Refresh bool
}

// Catalog is immutable once resolved and therefore safe to share among the
// head, executor leaves, and the terminal lens. A lazily loaded catalog holds
// the fetch as a future instead: the value is handed out immediately and the
// first capability question waits, if anything still has to wait at all.
type Catalog struct {
	ready   *rows
	resolve func() *rows
	// cancel and warmDone give a lazy catalog ownership of its background
	// discovery. Close cancels the fetch and joins the goroutine before returning.
	cancel   context.CancelFunc
	warmDone chan struct{}
	// warm is the resolved value published the instant resolution finishes, so
	// a question that must not wait can still be answered once the answer
	// exists. [Catalog.rows] blocks on the future; [Catalog.rowsNow] reads this
	// and takes "not yet" for an answer.
	warm atomic.Pointer[rows]
	// warmed is closed the moment that publish happens, and is the door
	// [Catalog.Warmed] waits at. Nil on a catalog that resolved eagerly —
	// there was never a warm to wait for — and on a zero catalog, where nothing
	// is in flight and nothing will land.
	warmed chan struct{}
	// blocking counts the questions asked through [Catalog.rows] — the door that
	// can wait. See [Catalog.BlockingReads].
	blocking atomic.Int64
}

// rows is one resolved catalog: the cleaned model list every listing walks,
// beside the index every single-model question is answered from. Building the
// index once turns each Supports call from a scan of the whole catalog into a
// lookup, which matters because the palette asks it per candidate.
type rows struct {
	models []Model
	byID   map[string]Model
	// fetchedAt is when these rows left OpenRouter, which is not when they were
	// read: a catalog served from disk after a failed fetch is a day-old answer
	// and a surface that showed it as today's would be dating someone else's
	// facts with its own clock. Zero means these rows never came from the
	// network at all — the built-in fallbacks — and a reader must say so rather
	// than print the epoch.
	fetchedAt time.Time
}

// Load fetches at most once. A fresh cache avoids I/O; a failed fetch degrades
// to a stale cache, then on the default base to a very small set of known
// modality defaults.
func Load(ctx context.Context, options Options) *Catalog {
	resolved, _ := loadOrFallback(ctx, options)
	return &Catalog{ready: resolved}
}

// Refresh is [Load] with [Options.Refresh] set, for the one caller that has to
// SAY what happened: a person who pressed a key asking for today's list.
//
// The catalog it hands back is exactly the one Load would have — a failed fetch
// still degrades to the cache and then to the built-ins, because asking for
// fresher facts must never leave a surface with fewer. The error beside it is
// why the fetch did not land, and nil when it did. Load drops that error on
// purpose, since a launch nobody asked for has nobody to tell; a refresh
// somebody asked for owes them a sentence.
func Refresh(ctx context.Context, options Options) (*Catalog, error) {
	options.Refresh = true
	resolved, err := loadOrFallback(ctx, options)
	return &Catalog{ready: resolved}, err
}

// Remember writes rows already learned from a service's successful /models
// response into that service-and-base compartment. It performs no network
// request. A later Refresh may replace these minimal rows with richer catalog
// facts, but a second read is never allowed to erase a listing the connection
// probe just proved exists.
func Remember(options Options, models []Model) error {
	models = cleanModels(models)
	if len(models) == 0 {
		return nil
	}
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	source := strings.TrimSpace(options.Source)
	base := normalizeBase(options.BaseURL)
	return writeCache(cachePath(options.Dir, source, base), cache{
		FetchedAt: now().UTC(), Models: models, Source: source, Base: base,
	})
}

// Recall reads only rows already remembered for one service and base. It never
// reaches the network and never substitutes the default service's fallbacks, so
// a launch can put a just-connected service on its picker without turning the
// first frame into a catalog refresh.
func Recall(options Options) *Catalog {
	source := strings.TrimSpace(options.Source)
	base := normalizeBase(options.BaseURL)
	cached, ok := readCache(cachePath(options.Dir, source, base), source, base)
	if !ok {
		return &Catalog{ready: newRows(nil)}
	}
	return &Catalog{ready: newRowsAt(cached.Models, cached.FetchedAt)}
}

// errUnreadable is what a fault inside discovery is reported as. The fault
// itself goes to the guard's log; the person who asked is told only that the
// list could not be read, which is the whole of what they can act on.
var errUnreadable = &statusError{status: "the list could not be read"}

// loadOrFallback is the only way a catalog is resolved, because a fault in
// discovery must degrade the way a failed fetch does — to the known defaults —
// rather than escape. Inside a sync.OnceValue it would escape twice over: once
// on the warming goroutine, and again on whichever caller first asked a
// capability question, since the future replays the panic to every reader.
func loadOrFallback(ctx context.Context, options Options) (resolved *rows, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("catalog/load", recovered)
			// THE FALLBACK ROWS ARE THE DEFAULT SERVICE'S IDS and nobody
			// else's, so a panic on another service resolves to nothing
			// rather than to eleven names it never published.
			if strings.TrimSpace(options.Source) == "" && normalizeBase(options.BaseURL) == DefaultBaseURL {
				resolved, err = newRows(hardcodedFallbacks()), errUnreadable
			} else {
				resolved, err = newRows(nil), errUnreadable
			}
		}
	}()
	return load(ctx, options)
}

// LoadLazy starts the same discovery immediately but never makes the caller
// wait for it. On a cold cache the fetch is a network round-trip with a
// fifteen-second ceiling, and a launch path that awaits it holds the first
// frame behind a dead terminal. Nothing a catalog answers can be asked before
// the surface is up, so the goroutine warms the value while the caller carries
// on, and only a question that genuinely arrives first ever blocks.
func LoadLazy(ctx context.Context, options Options) *Catalog {
	warmCtx, cancel := context.WithCancel(ctx)
	resolved := &Catalog{
		warmed:   make(chan struct{}),
		cancel:   cancel,
		warmDone: make(chan struct{}),
	}
	resolve := sync.OnceValue(func() *rows {
		loaded, _ := loadOrFallback(warmCtx, options)
		resolved.warm.Store(loaded)
		// The wait door ([Catalog.Warmed]) reads the close, not the value, and
		// the two land together so a caller that arrived between them would
		// see the rows and still wait.
		close(resolved.warmed)
		return loaded
	})
	resolved.resolve = resolve
	guard.Go("catalog/warm", func() {
		defer close(resolved.warmDone)
		resolve()
	})
	return resolved
}

// Close cancels and joins a lazy catalog warm. It is safe to call more than
// once. Eager and zero catalogs have no background work and return immediately.
func (c *Catalog) Close() {
	if c == nil || c.cancel == nil {
		return
	}
	c.cancel()
	<-c.warmDone
}

// load resolves one catalog, and reports why the fetch failed when it spent the
// network and did not land. A fresh cache that needed no fetch is not a failure
// and answers nil.
func load(ctx context.Context, options Options) (*rows, error) {
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	base := normalizeBase(options.BaseURL)
	source := strings.TrimSpace(options.Source)
	path := cachePath(options.Dir, source, base)
	cached, cachedOK := readCache(path, source, base)
	if cachedOK && !options.Refresh && now().Before(cached.FetchedAt.Add(TTL)) {
		return newRowsAt(cached.Models, cached.FetchedAt), nil
	}

	// fetch refuses an empty listing itself, so a nil error here is always rows.
	models, err := fetch(ctx, options)
	if err == nil {
		// A transport may report success after cancellation. Closing the owner
		// must still prevent that late response from changing persistent state.
		err = ctx.Err()
	}
	if err == nil {
		fetchedAt := now().UTC()
		if path != "" {
			_ = writeCache(path, cache{FetchedAt: fetchedAt, Models: models, Source: source, Base: base})
		}
		return newRowsAt(models, fetchedAt), nil
	}
	if cachedOK {
		// The network is gone and the cache is old. It is still the truest
		// answer anyone has, so it is served WITH ITS DATE rather than
		// withheld: a stale catalog a surface can date is worth more than an
		// empty one it cannot explain.
		return newRowsAt(cached.Models, cached.FetchedAt), err
	}
	if source == "" && base == DefaultBaseURL {
		return newRows(hardcodedFallbacks()), err
	}
	// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN — handing eleven
	// OpenRouter ids to a service that never published them puts four verbs on
	// the belt that cannot succeed.
	return newRows(nil), err
}

// FetchedAt is when this catalog's rows left the provider, or the zero time
// when they never did — an unloaded catalog, or the built-in fallbacks. A
// surface that shows a model list may date it from here; nothing inside this
// package reads it, because a decision made on the age of a catalog would be a
// second TTL living somewhere the first one cannot see.
func (c *Catalog) FetchedAt() time.Time {
	resolved := c.rows()
	if resolved == nil {
		return time.Time{}
	}
	return resolved.fetchedAt
}

// FetchedAtNow is [Catalog.FetchedAt] for a caller that must not wait: it
// reads the rows already in hand and answers the zero time while a lazy
// catalog is still warming, exactly as [Catalog.ModelsNow] answers nil.
//
// It exists for the model warm (cmd/codeaf's warmV3Models), which gates on
// [Catalog.Warmed] before it reads anything — so the rows are always there
// when this answers — and must not ask a blocking question of a catalog it is
// about to stop waiting on.
func (c *Catalog) FetchedAtNow() time.Time {
	resolved := c.rowsNow()
	if resolved == nil {
		return time.Time{}
	}
	return resolved.fetchedAt
}

// rows resolves the catalog, waiting on the future when Load was lazy.
func (c *Catalog) rows() *rows {
	if c == nil {
		return nil
	}
	// THIS IS THE DOOR THAT CAN WAIT, and the count of who came through it is
	// what lets a launch path be held to never coming through it at all. See
	// [Catalog.BlockingReads]: on a lazy catalog the first caller here pays a
	// network fetch with a fifteen-second ceiling, and whether it actually paid
	// on any given run is a race with the warming goroutine — so the fact worth
	// counting is the QUESTION, not the wait it happened to cost. One
	// uncontended atomic add against a map lookup and, sometimes, a GET.
	c.blocking.Add(1)
	if c.ready != nil {
		return c.ready
	}
	if c.resolve != nil {
		return c.resolve()
	}
	return nil
}

// BlockingReads is how many questions this catalog has been asked through the
// door that can wait ([Catalog.rows]), as against the ones asked through
// [Catalog.ModelsNow] and its neighbours, which never can.
//
// IT EXISTS SO THAT A LAUNCH PATH CAN BE HELD TO A NUMBER. "Nothing before the
// first frame resolves the catalog" is a law about the shape of the code, and
// the only honest way to test it is to count the blocking questions a launch
// asks and pin the total: a wall-clock assertion would pass or fail on whether
// the warming goroutine happened to land first, which is a fact about the
// network and not about the change under review. cmd/codeaf's launch pins read
// this; nothing inside this package does.
func (c *Catalog) BlockingReads() int64 {
	if c == nil {
		return 0
	}
	return c.blocking.Load()
}

// rowsNow is [Catalog.rows] for a caller that must not wait: it answers nil
// while a lazy catalog is still warming rather than blocking on the fetch.
func (c *Catalog) rowsNow() *rows {
	if c == nil {
		return nil
	}
	if c.ready != nil {
		return c.ready
	}
	return c.warm.Load()
}

// Warmed answers whether this catalog's rows have landed, waiting within the
// bound ctx carries for a lazily loaded one — from the disk cache or from the
// fetch, whichever wins — and answering false when the bound ends first. A
// catalog that resolved eagerly ([Load], [Refresh]) answers true at once, and
// a nil catalog answers false.
//
// It exists for a caller whose next step reads the rows through a seam that
// must not wait ([Catalog.ModelsNow], and the binaries that set config's
// AutoModels from it): a bounded wait turns "not yet" into "the rows" when the
// rows are a disk read away, without ever turning the caller into a fetch. The
// caller owns the bound; this only honors it.
func (c *Catalog) Warmed(ctx context.Context) bool {
	if c == nil {
		return false
	}
	if c.ready != nil {
		return true
	}
	if c.warm.Load() != nil {
		return true
	}
	if c.warmed == nil {
		// Not a lazily loaded catalog: nothing is in flight and nothing will
		// land, so a wait would only spend the caller's bound. What is in hand
		// is the whole answer, and it is nothing.
		return false
	}
	select {
	case <-c.warmed:
		return true
	case <-ctx.Done():
		return false
	}
}

// ModelsNow is the whole model list for a caller that MUST NOT WAIT, and nil
// while a lazily loaded catalog is still warming.
//
// Every other listing here resolves through [Catalog.rows], which on a cold
// cache means a fifteen-second fetch — fine for `codeaf models`, wrong for a
// picker a person just opened. Nil is the honest answer for "nobody has the
// facts yet": a surface that gets it falls back to whatever list it can read
// off disk, and the next time the picker opens the warm catalog answers.
//
// The rows are cloned for the same reason [Catalog.Model] clones: the catalog
// is immutable and shared, and a caller that sorted the returned slice's models
// in place would be sorting everyone's.
func (c *Catalog) ModelsNow() []Model {
	resolved := c.rowsNow()
	if resolved == nil {
		return nil
	}
	models := make([]Model, 0, len(resolved.models))
	for _, model := range resolved.models {
		models = append(models, cloneModel(model))
	}
	return models
}

// ModelsWithInput returns a stable copy of models advertising modality.
func (c *Catalog) ModelsWithInput(modality string) []Model {
	return c.modelsWith("input", modality)
}

// ModelsWithOutput returns a stable copy of models advertising modality.
func (c *Catalog) ModelsWithOutput(modality string) []Model {
	return c.modelsWith("output", modality)
}

// Model returns one catalog row by slug. The returned slices do not alias the
// immutable catalog, so callers may safely retain or amend the result.
func (c *Catalog) Model(modelID string) (Model, bool) {
	resolved := c.rows()
	if resolved == nil {
		return Model{}, false
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok {
		return Model{}, false
	}
	return cloneModel(model), true
}

// ContextLength is how many tokens the named model accepts, or zero when this
// catalog cannot say — an unknown slug, a catalog that never loaded, a row
// cached before the field was kept. Zero is the honest answer and never a small
// model: a caller sizing anything from this must have its own default for the
// case where the provider was silent, because being wrong downward here means
// forgetting material the model could have held.
func (c *Catalog) ContextLength(modelID string) int {
	model, ok := c.Model(modelID)
	if !ok {
		return 0
	}
	return model.ContextLength
}

// ContextLengthNow is [Catalog.ContextLength] for a caller that must not wait:
// it reads through the rows already in hand and answers zero while a lazy
// catalog is still warming, exactly as [Catalog.ModelsNow] answers nil, and it
// matches ids the same way [Catalog.ContextLength] does — normalizing the
// reasoning-effort suffix and a leading ~ ([normalizeID]) — so a conversation
// started on `model:high` answers its window and not zero.
//
// It exists for the model warm (cmd/codeaf's warmV3Models), which gates on
// [Catalog.Warmed] before it reads anything — so the rows are always there
// when this answers — and must not ask a blocking question of a catalog it is
// about to stop waiting on.
func (c *Catalog) ContextLengthNow(modelID string) int {
	resolved := c.rowsNow()
	if resolved == nil {
		return 0
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok {
		return 0
	}
	return model.ContextLength
}

// Resolves is the target catalog's own answer to "do you have this id?", asked
// by [Catalog.Concrete] before it hands a subprocess a spelling other than the
// one it was given. It is a function rather than an import because the only
// catalog that matters here lives behind another package's internal/ wall, and
// because the question — not the table — is what this package needs.
//
// A nil Resolves means nobody can be asked, which is a different answer from
// "no": see Concrete.
type Resolves func(modelID string) bool

// Concrete is the model id in the spelling a foreign catalog can actually find,
// and it VERIFIES before it substitutes.
//
// Nothing inside codeaf needs it: an alias is a model id OpenRouter accepts,
// and every call codeaf makes with one is answered. It matters at exactly one
// boundary — a subprocess that looks a model up in a *different* catalog, one
// keyed by that catalog's own spellings and with no idea what floats. Handed a
// name that catalog does not carry, the process dies before it has spent a cent.
//
// The candidates are tried in the order that a wrong answer costs least:
//
//  1. the alias target, because a floating id resolves to nothing anywhere but
//     here (see [Model.AliasTarget]);
//  2. the id as written, because it is what the person and the panel actually
//     chose, and foreign catalogs key on undated names far more often than the
//     comment on CanonicalSlug assumed;
//  3. the canonical slug, the dated spelling, which is a real id in some
//     catalogs and in others is a name nobody has ever published.
//
// The third is why this takes a resolver at all. Substituting the canonical
// slug unasked is what broke a leaf on the default model of every install:
// models.dev carries deepseek/deepseek-v4-flash and deepseek/deepseek-v4-flash-0731
// and has never heard of deepseek/deepseek-v4-flash-20260423, so a translation
// meant to help handed the far side a name that could not exist.
//
// When nothing resolves, the id as written is forwarded — never a substitution
// the target catalog is KNOWN not to have — so the far side's own error names
// the model the operator chose. When resolves is nil nothing can be asked, and
// only the alias target is applied: it is a fact about this catalog rather than
// a guess about another's spelling. The leading "~" — OpenRouter's own alias
// marker, and part of no model's name — is dropped throughout, exactly as Model
// and Supports already drop it.
func (c *Catalog) Concrete(modelID string, resolves Resolves) string {
	id := normalizeID(modelID)
	model, ok := c.Model(id)
	if !ok {
		// A model nobody chose must never enter another engine's pools, so an
		// id this catalog cannot vouch for is forwarded verbatim.
		return id
	}
	candidates := make([]string, 0, 3)
	if target := normalizeID(model.AliasTarget); target != "" && target != id {
		candidates = append(candidates, target)
	}
	candidates = append(candidates, id)
	if canonical := normalizeID(model.CanonicalSlug); canonical != "" && canonical != id {
		candidates = append(candidates, canonical)
	}
	if resolves == nil {
		return candidates[0]
	}
	for _, candidate := range candidates {
		if resolves(candidate) {
			return candidate
		}
	}
	return id
}

// Identity is the one model behind a spelling of it, and it is what an
// accumulated history has to be keyed by.
//
// Concrete answers a different question — which spelling a FOREIGN catalog will
// accept — and it is deliberately conservative about substituting, because a
// name that catalog does not carry kills a subprocess. Nothing is being handed
// to anybody here. This is the local question: two spellings the operator used
// on two days, and whether the measurements taken under them describe one model.
// The alias target and the canonical slug both say they do, so both are applied
// and the dated spelling wins, because it is the one name that cannot float.
//
// It never waits. Identity is asked on the launch path, before anything has been
// planned, and a still-warming catalog blocking there would put a fetch in front
// of the first frame of every run. A catalog that has not resolved yet answers
// the id as written, which is what every caller did before this existed — and the
// records written under it are merged into the resolved identity by the first
// process that can see one (see profile.Load).
func (c *Catalog) Identity(modelID string) string {
	// Lowercased as well as ~-stripped, which Concrete does not do: Concrete is
	// building a name to hand to another process and must not alter one beyond
	// what this catalog can vouch for, while this is asking whether two things
	// somebody typed are the same thing, and case never was a difference.
	id := strings.ToLower(normalizeID(modelID))
	resolved := c.rowsNow()
	if resolved == nil {
		return id
	}
	model, ok := resolved.byID[id]
	if !ok {
		return id
	}
	if target := normalizeID(model.AliasTarget); target != "" && target != id {
		aliased, known := resolved.byID[target]
		if !known {
			return target
		}
		model, id = aliased, target
	}
	if canonical := normalizeID(model.CanonicalSlug); canonical != "" {
		return canonical
	}
	return id
}

// Servable is the id the router will actually serve this spelling under, and it
// is the only fold a per-model LEDGER may use.
//
// [Catalog.Identity] answers a neighbouring question and answers it wrongly for
// this one. Identity is asked whether two spellings describe one model, and it
// prefers the dated canonical slug because that is the name which cannot float.
// For a floating alias the router publishes canonical_slug as the ALIAS itself
// and, one hop on, as a dated id it lists nowhere and serves through no
// endpoints page: on 2026-09-01 `~deepseek/deepseek-v4-flash-latest` resolved
// through Identity to `deepseek/deepseek-v4-flash-20260731`, which is not a
// model anybody can send to. A ledger keyed on that would hold beliefs about a
// name no request will ever wear — the same split this fixes, one spelling
// further out. The alias target, `deepseek/deepseek-v4-flash-0731`, is the real
// servable id and the endpoints page is published under it.
//
// The bare undated id is not the answer either, and it is worth saying because
// it looks like one: `deepseek/deepseek-v4-flash` is the 0423 snapshot, a
// DIFFERENT MODEL with its own machines and its own speeds.
//
// So: one hop, alias target only, and never the canonical slug. A row without
// an alias target is already servable and comes back as written.
//
// It never waits, for [Catalog.Identity]'s reason and one more: this is read on
// the send path as well as at launch, and a fold that could block would put a
// fetch in front of a request.
//
// A CATALOG WITH NO ROWS YET ANSWERS NOTHING, and the empty string is that
// answer rather than a fold to nothing. Saying "the id as written" would be a
// lie a reader cannot tell from a fact, and the reader that matters memoises:
// [lane.LedgerModel] remembers the first answer for the life of the process, so
// a process that asked while the catalog was still in flight would key its
// whole run on the alias — the split this fold exists to end, made permanent by
// a guess. Nothing is the one answer a caller can act on correctly, by using
// the name it already has and asking again.
func (c *Catalog) Servable(modelID string) string {
	// Lowercased as well as ~-stripped, exactly as Identity does it, so that the
	// two folds agree about which spellings are one spelling.
	id := strings.ToLower(normalizeID(modelID))
	resolved := c.rowsNow()
	if resolved == nil {
		return ""
	}
	model, ok := resolved.byID[id]
	if !ok {
		return id
	}
	if target := strings.ToLower(normalizeID(model.AliasTarget)); target != "" && target != id {
		return target
	}
	return id
}

// Supports answers whether modelID advertises modality in direction. Unknown
// models and directions calmly return false.
func (c *Catalog) Supports(modelID, direction, modality string) bool {
	resolved := c.rows()
	if resolved == nil {
		return false
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok {
		return false
	}
	var values []string
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "input":
		values = model.InputModalities
	case "output":
		values = model.OutputModalities
	default:
		return false
	}
	return hasModality(values, modality)
}

// SupportsParameter answers whether modelID accepts a request field, and
// whether anyone actually knows.
//
// The second bool is the whole point, and it is why this cannot be a plain
// predicate. A model the catalog has never heard of, a catalog that never
// loaded, and a row cached before parameters were kept all say "no idea" — and
// a caller that read that as "does not support it" would silently drop a knob
// the operator asked for, while one that read it as "supports it" would send a
// field that 400s. Only the caller knows which way to fail, so the fact and its
// confidence travel together.
//
// It never waits. This is the one catalog question asked on the request path,
// where the caller is the model adapter shaping a body it is about to send, and
// a still-warming catalog blocking there would put a fifteen-second fetch in
// front of the first call of every run. A catalog that has not resolved yet is
// simply one more way of not knowing.
func (c *Catalog) SupportsParameter(modelID, parameter string) (bool, bool) {
	resolved := c.rowsNow()
	if resolved == nil {
		return false, false
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok || len(model.Parameters) == 0 {
		return false, false
	}
	parameter = strings.ToLower(strings.TrimSpace(parameter))
	if parameter == "" {
		return false, false
	}
	for _, supported := range model.Parameters {
		if supported == parameter {
			return true, true
		}
	}
	return false, true
}

// ReasoningProfile answers what the provider published about modelID's
// thinking pass, and whether it published anything.
//
// The second bool matters for the same reason it does on SupportsParameter: a
// row nobody has seen, a catalog still warming, and a row cached before the
// block was kept all say "no idea", and the adapter falls back to the one
// other way it can learn the fact — being told no by the endpoint. It never
// waits, for SupportsParameter's reason.
func (c *Catalog) ReasoningProfile(modelID string) (ReasoningProfile, bool) {
	resolved := c.rowsNow()
	if resolved == nil {
		return ReasoningProfile{}, false
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok || !model.Reasoning.Known {
		return ReasoningProfile{}, false
	}
	return model.Reasoning, true
}

// reasoningProfile reads the published block into the row. Known is set by
// the block's presence, not by any field inside it: a provider that published
// {mandatory:false, efforts:[]} has still said something.
func reasoningProfile(wire *struct {
	Mandatory        bool     `json:"mandatory"`
	DefaultEnabled   bool     `json:"default_enabled"`
	SupportedEfforts []string `json:"supported_efforts"`
	DefaultEffort    string   `json:"default_effort"`
}) ReasoningProfile {
	if wire == nil {
		return ReasoningProfile{}
	}
	return ReasoningProfile{
		Known:         true,
		Mandatory:     wire.Mandatory,
		Efforts:       cleanLowerList(wire.SupportedEfforts),
		DefaultEffort: strings.ToLower(strings.TrimSpace(wire.DefaultEffort)),
	}
}

// PriceNow is what modelID's own published tariff is, per token in US dollars,
// and whether anybody actually published one.
//
// The third value carries the whole distinction the price fields cannot: a zero
// price is a real figure — eighteen rows really are free — and "the provider
// said nothing" is not. A caller that read the two the same way would either
// invent a free model or throw away a real one. `PriceUnknown` is the row's own
// word for the second case, and a row the catalog has never seen is the same
// answer arrived at differently.
//
// It never waits, for [Catalog.SupportsParameter]'s reason: the caller is the
// model adapter shaping a body it is about to send, and a still-warming catalog
// blocking there would put a fetch in front of the first call of every run. A
// catalog that has not resolved is one more way of not knowing.
func (c *Catalog) PriceNow(modelID string) (prompt, completion float64, known bool) {
	resolved := c.rowsNow()
	if resolved == nil {
		return 0, 0, false
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok || model.PriceUnknown {
		return 0, 0, false
	}
	if model.PromptPrice < 0 || model.CompletionPrice < 0 {
		return 0, 0, false
	}
	return model.PromptPrice, model.CompletionPrice, true
}

// NearestModels names the models most like modelID, closest first, for a caller
// that has to move off it and would rather not ask a person which way to go.
//
// SAME CLASS MEANS SERVES THE SAME CONVERSATION, and it is four published facts
// rather than a judgement: the row answers in text only, it accepts tool calls,
// its window is not dramatically smaller, and it is not the model we are leaving.
// A chat turn that moved to a model with no tools or a quarter of the window
// would be a fallback that fails differently rather than one that works.
//
// The ordering is by the same vendor first — the endpoints of one vendor's line
// are the likeliest to accept the same request shape — and then by published
// intelligence, nearest first, with an unpublished score ranking last. Elo
// breaks the remaining ties, so two rows that published nothing but a name still
// come back in a stable order rather than in map order.
//
// IT NEVER WAITS, exactly as [Catalog.SupportsParameter] never does: this is
// asked on the request path, by an adapter that has just been refused, with a
// person watching. A catalog that has not resolved, or a model it has never
// heard of, answers nil — nobody knows, which is a fine answer and better than a
// fifteen-second fetch in front of an error.
func (c *Catalog) NearestModels(modelID string, limit int) []string {
	resolved := c.rowsNow()
	if resolved == nil || limit <= 0 {
		return nil
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok {
		return nil
	}
	family := vendorOf(model.ID)
	floor := model.ContextLength / 2

	candidates := make([]Model, 0, len(resolved.models))
	for _, row := range resolved.models {
		if normalizeID(row.ID) == normalizeID(model.ID) || !row.accepts("tools") {
			continue
		}
		if !answersTextOnly(row.OutputModalities) || row.ContextLength < floor {
			continue
		}
		candidates = append(candidates, row)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if kin := vendorOf(left.ID) == family; kin != (vendorOf(right.ID) == family) {
			return kin
		}
		leftKnown, rightKnown := left.IntelligenceIndex > 0, right.IntelligenceIndex > 0
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftKnown && rightKnown {
			leftGap := math.Abs(left.IntelligenceIndex - model.IntelligenceIndex)
			rightGap := math.Abs(right.IntelligenceIndex - model.IntelligenceIndex)
			if leftGap != rightGap {
				return leftGap < rightGap
			}
		}
		return left.ArenaElo > right.ArenaElo
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	nearest := make([]string, 0, len(candidates))
	for _, row := range candidates {
		nearest = append(nearest, row.ID)
	}
	return nearest
}

// vendorOf is the part of a slug before the slash — "openai" in
// "openai/gpt-5-mini" — and the whole id when there is no slash.
func vendorOf(id string) string {
	id = strings.ToLower(normalizeID(id))
	if index := strings.Index(id, "/"); index > 0 {
		return id[:index]
	}
	return id
}

// answersTextOnly keeps the rows a conversation can be held with. A row that
// declares nothing is KEPT — silence is a cache written before modalities were
// recorded, not a model that answers in nothing — and a row that also draws
// pictures is not, because a fallback into an image model is a fallback into a
// different product.
func answersTextOnly(outputs []string) bool {
	if len(outputs) == 0 {
		return true
	}
	for _, modality := range outputs {
		if !strings.EqualFold(strings.TrimSpace(modality), "text") {
			return false
		}
	}
	return true
}

func (c *Catalog) modelsWith(direction, modality string) []Model {
	resolved := c.rows()
	if resolved == nil {
		return nil
	}
	models := make([]Model, 0)
	for _, model := range resolved.models {
		var values []string
		if direction == "input" {
			values = model.InputModalities
		} else {
			values = model.OutputModalities
		}
		if hasModality(values, modality) {
			models = append(models, cloneModel(model))
		}
	}
	return models
}

func hasModality(values []string, requested string) bool {
	requested = strings.ToLower(strings.TrimSpace(requested))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == requested {
			return true
		}
		// OpenRouter currently describes synthesized sound as either audio or
		// speech across model families, while music models may say music or the
		// broader audio. Keep that provider vocabulary behind the catalog seam so
		// callers can ask stable capability questions.
		if requested == "speech" && value == "audio" {
			return true
		}
		if requested == "music" && value == "audio" {
			return true
		}
	}
	return false
}

func fetch(ctx context.Context, options Options) ([]Model, error) {
	endpoint := strings.TrimSuffix(strings.TrimSpace(options.BaseURL), "/") + "/models?output_modalities=all"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(options.APIKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, &statusError{status: response.Status}
	}
	// The rows are decoded ONE AT A TIME, out of a slice of raw messages,
	// because this is a third party's schema and it drifts. Decoded whole, a
	// single row that grew a field of a shape this struct does not expect —
	// `benchmarks` was an object for some models and absent for others on the
	// day this was written — fails the entire Decode, and the catalog degrades
	// from four hundred models to the five hardcoded fallbacks. A row that
	// cannot be read is skipped, and every row that can be read still arrives.
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxCatalogBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(payload.Data))
	for _, raw := range payload.Data {
		var item modelWire
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		prompt, promptOK := parsePrice(item.Pricing.Prompt)
		completion, completionOK := parsePrice(item.Pricing.Completion)
		request, _ := parsePrice(item.Pricing.Request)
		cacheRead, _ := parsePrice(item.Pricing.InputCacheRead)
		models = append(models, Model{
			ID: strings.TrimSpace(item.ID), CanonicalSlug: strings.TrimSpace(item.CanonicalSlug),
			AliasTarget:       strings.TrimSpace(item.AliasTarget.Slug),
			Name:              strings.TrimSpace(item.Name),
			ContextLength:     item.ContextLength,
			PromptPrice:       prompt,
			CompletionPrice:   completion,
			RequestPrice:      request,
			CacheReadPrice:    cacheRead,
			PriceUnknown:      !promptOK || !completionOK,
			ArenaElo:          arenaElo(item.Benchmarks),
			Created:           int64(item.Created),
			IntelligenceIndex: analysisScore(item.Benchmarks, "intelligence_index"),
			CodingIndex:       analysisScore(item.Benchmarks, "coding_index"),
			AgenticIndex:      analysisScore(item.Benchmarks, "agentic_index"),
			OpenWeights:       item.HuggingFaceID != "",
			InputModalities:   cleanLowerList(item.Architecture.Input),
			OutputModalities:  cleanLowerList(item.Architecture.Output),
			Parameters:        cleanLowerList(item.SupportedParameters),
			Reasoning:         reasoningProfile(item.Reasoning),
		})
	}
	// The one cleaning pass for the fetched path; what is cached and what is
	// indexed are the same cleaned rows.
	models = cleanModels(models)
	if len(models) == 0 {
		return nil, &statusError{status: "empty catalog"}
	}
	return models, nil
}

// modelWire is one row of OpenRouter's /models listing, in the shape this
// package reads it. Everything absent from it — description,
// per_request_limits, top_provider, links — is either prose nobody renders or
// provider bookkeeping, and a field added here is a field something on screen
// has to be able to explain.
//
// supported_parameters was in that list until MiniMax M2.7 failed every
// planning call on it. It is not bookkeeping: it is the provider's own answer
// to which knobs a model accepts, the picker explains it in one word, and the
// adapter needs it to decide whether a reasoning knob may travel at all.
type modelWire struct {
	ID            string `json:"id"`
	CanonicalSlug string `json:"canonical_slug"`
	AliasTarget   struct {
		Slug string `json:"slug"`
	} `json:"alias_target"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	// Created is when the row was listed, in Unix seconds.
	Created      float64 `json:"created"`
	Architecture struct {
		// Modality is the coarse "text->text" string. It is read for nothing:
		// input_modalities and output_modalities say the same thing as lists,
		// and a list is what every question this package answers is asked in.
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
		Request    string `json:"request"`
		// InputCacheRead is the warm-prefix read price. The write prices
		// (input_cache_write, input_cache_write_1h) are published beside it and
		// deliberately not kept: nothing on screen explains them, and a session
		// pays a write once for a prefix it then reads on every step.
		InputCacheRead string `json:"input_cache_read"`
	} `json:"pricing"`
	SupportedParameters []string `json:"supported_parameters"`
	// Reasoning is a pointer so that a row without the block reads as "the
	// provider said nothing" rather than as a model that can be switched off.
	Reasoning *struct {
		Mandatory        bool     `json:"mandatory"`
		DefaultEnabled   bool     `json:"default_enabled"`
		SupportedEfforts []string `json:"supported_efforts"`
		DefaultEffort    string   `json:"default_effort"`
	} `json:"reasoning"`
	// HuggingFaceID is the row's `hugging_face_id`, read for the one fact it
	// carries — the weights behind the row are published — and not kept on the
	// Model beside that fact.
	HuggingFaceID string `json:"hugging_face_id"`
	// Benchmarks stays raw so its shape cannot break the row around it. It
	// carried an object beside a LIST on 2026-08-11 (`design_arena: []` next
	// to `artificial_analysis: {…}`), which is exactly the kind of thing that
	// becomes an object next quarter.
	Benchmarks json.RawMessage `json:"benchmarks"`
}

// analysisScore digs one published Artificial Analysis score out of a raw
// benchmarks block by its field name, and answers zero for every shape it does
// not recognize. Nothing here is allowed to fail loudly: a score is a nicety on
// a row, and a catalog that refused to load because a benchmark changed shape
// would have traded four hundred models for one number. The block is decoded to
// raw messages and only the asked-for field parsed, so one drifted score costs
// itself and never its siblings.
func analysisScore(raw json.RawMessage, field string) float64 {
	if len(raw) == 0 {
		return 0
	}
	var block struct {
		ArtificialAnalysis map[string]json.RawMessage `json:"artificial_analysis"`
	}
	if json.Unmarshal(raw, &block) != nil {
		return 0
	}
	var score float64
	if json.Unmarshal(block.ArtificialAnalysis[field], &score) != nil {
		return 0
	}
	if score > 0 {
		return score
	}
	return 0
}

// arenaElo digs the best published Design Arena Elo out of a raw benchmarks
// block, and answers zero for every shape it does not recognize.
//
// It decodes into its own struct rather than sharing [intelligenceIndex]'s
// because the two fields have opposite shapes and the same block held both on
// 2026-08-11: `design_arena: []` beside `artificial_analysis: {…}`. A list where
// an object was expected — or the reverse next quarter — must cost this one
// number and never the four hundred models around it, which is why nothing here
// is allowed to fail loudly.
func arenaElo(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var block struct {
		DesignArena []struct {
			Elo float64 `json:"elo"`
		} `json:"design_arena"`
	}
	if json.Unmarshal(raw, &block) != nil {
		return 0
	}
	best := 0.0
	for _, board := range block.DesignArena {
		if board.Elo > best {
			best = board.Elo
		}
	}
	return best
}

type statusError struct{ status string }

func (e *statusError) Error() string { return "model catalog: " + e.status }

// newRows indexes an already-cleaned model list. Every path into it — the
// fetch, the cache read, the built-in fallbacks — has cleaned its own rows, so
// cleaning runs exactly once per catalog rather than once per hand-off.
//
// The index keeps the first row for each normalized id, which is what a scan
// from the top of the list would have found: cleaning dedupes on the literal
// id, so a slug and its "~" variant can both survive it.
func newRows(models []Model) *rows { return newRowsAt(models, time.Time{}) }

// newRowsAt is [newRows] for rows that have a date — everything but the
// built-in fallbacks, which came from nowhere and are dated nowhere.
func newRowsAt(models []Model, fetchedAt time.Time) *rows {
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		id := normalizeID(model.ID)
		if _, seen := byID[id]; !seen {
			byID[id] = model
		}
	}
	return &rows{models: models, byID: byID, fetchedAt: fetchedAt}
}

func cleanModels(models []Model) []Model {
	seen := make(map[string]bool, len(models))
	cleaned := make([]Model, 0, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" || seen[model.ID] {
			continue
		}
		// ":batch" variants only answer on the async batch endpoint; every
		// call this program makes is interactive, so offering one is offering
		// a model that 404s on first use.
		if strings.HasSuffix(model.ID, ":batch") {
			continue
		}
		seen[model.ID] = true
		model.InputModalities = cleanLowerList(model.InputModalities)
		model.OutputModalities = cleanLowerList(model.OutputModalities)
		cleaned = append(cleaned, model)
	}
	return cleaned
}

func cleanLowerList(values []string) []string {
	seen := make(map[string]bool, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func cloneModel(model Model) Model {
	model.InputModalities = append([]string(nil), model.InputModalities...)
	model.OutputModalities = append([]string(nil), model.OutputModalities...)
	model.Parameters = append([]string(nil), model.Parameters...)
	return model
}

// normalizeID is the ONE PLACE a model value somebody wrote becomes a lookup key
// in this table, and two things that are not part of a model id come off here.
//
// The first is the `~` floating-alias marker, which asks a family for its newest
// member. The second is the tier rows' thinking level — `moonshotai/kimi-k3:low`
// — which says how hard to ask a model rather than which model it is
// ([roles.SplitEffort], and see config.ResolveSeats: since the crew reaches every
// headless run, a levelled value is now an ordinary thing to look up). A lookup
// that missed on the level answered zero context, no published price, and
// "cannot say" about every capability — each of which is a caller quietly
// falling back to a literal.
//
// Rows are indexed through this too, so a catalog that one day publishes an id
// ending in a level would still be found by the key it was stored under.
func normalizeID(id string) string {
	model, _ := roles.SplitEffort(id)
	return strings.TrimPrefix(model, "~")
}

// parsePrice reads one per-token price, and says whether the provider actually
// published one. A blank, an unparseable string, or OpenRouter's "-1" — its
// spelling of "this router charges whatever it routes to" — are all the same
// answer: nobody said. Zero is a price and comes back known, because eighteen
// models in the live catalog really are free and a surface must be able to tell
// those apart from the routers.
func parsePrice(raw string) (float64, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	price, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || price < 0 {
		return 0, false
	}
	return price, true
}

func normalizeBase(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return DefaultBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func cachePath(dir, source, base string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = home.Dir()
	}
	base = normalizeBase(base)
	key := CacheKey(source, base)
	if key == "" {
		return filepath.Join(dir, cacheName)
	}
	extension := filepath.Ext(cacheName)
	stem := strings.TrimSuffix(cacheName, extension)
	return filepath.Join(dir, stem+"-"+key+extension)
}

// CacheKey is the shared service-and-base ownership key. Empty is the legacy
// default-service/default-base case; every other pair gets sixteen hex digits.
func CacheKey(source, base string) string {
	source = strings.TrimSpace(source)
	base = normalizeBase(base)
	if source == "" && base == DefaultBaseURL {
		return ""
	}
	digest := sha256.Sum256([]byte(source + "\x00" + base))
	return hex.EncodeToString(digest[:8])
}

func readCache(path, source, base string) (cache, bool) {
	if path == "" {
		return cache{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cache{}, false
	}
	var cached cache
	if json.Unmarshal(raw, &cached) != nil || cached.FetchedAt.IsZero() {
		return cache{}, false
	}
	askedBase := normalizeBase(base)
	askedSource := strings.TrimSpace(source)
	if cached.Base == "" && cached.Source == "" {
		// Legacy caches have no ownership mark. They remain usable for the
		// default base so ordinary installs pay no cold fetch during the
		// upgrade. A custom-base legacy cache can therefore be read once as the
		// default base's cache, bounded by the TTL; the first successful fetch
		// stamps it. Discarding every legacy cache would make every ordinary
		// install pay for the rarer custom-base case.
		if askedSource != "" || askedBase != DefaultBaseURL {
			return cache{}, false
		}
	} else if cached.Base == "" || strings.TrimSpace(cached.Source) != askedSource || normalizeBase(cached.Base) != askedBase {
		return cache{}, false
	}
	// The one cleaning pass for the cached path — an older cache may predate a
	// vocabulary change, so its rows are normalized here and nowhere else.
	cached.Models = cleanModels(cached.Models)
	return cached, len(cached.Models) > 0
}

func writeCache(path string, cached cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// hardcodedFallbacks describes the default base and no other. It is written
// already cleaned — unique ids, lowercase modalities — so it satisfies
// newRows without a cleaning pass of its own.
//
// Every row is PriceUnknown, and that is worth writing out rather than letting
// the zero value speak: these are names this build happens to remember, not
// rows anybody fetched, and a fallback claiming a price of zero would be this
// package inventing economics for a model it could not reach.
//
// The list leads with the name internal/config prefers for each generation
// slot, because config's curated rung is only usable on a cold machine while a
// row here vouches for exactly that id's capabilities.
func hardcodedFallbacks() []Model {
	return []Model{
		{ID: "bytedance-seed/seedream-5-0-pro", PriceUnknown: true, InputModalities: []string{"text", "image"}, OutputModalities: []string{"image"}},
		{ID: "krea/krea-2-medium-turbo", PriceUnknown: true, InputModalities: []string{"text", "image"}, OutputModalities: []string{"image"}},
		{ID: "fish-audio/s2.1-pro", PriceUnknown: true, InputModalities: []string{"text"}, OutputModalities: []string{"speech"}},
		{ID: "fish-audio/s1", PriceUnknown: true, InputModalities: []string{"text"}, OutputModalities: []string{"speech"}},
		{ID: "hexgrad/kokoro-82m", PriceUnknown: true, InputModalities: []string{"text"}, OutputModalities: []string{"speech"}},
		{ID: "openai/gpt-4o-mini-tts", PriceUnknown: true, InputModalities: []string{"text"}, OutputModalities: []string{"speech"}},
		{ID: "google/lyria-3-clip-preview", PriceUnknown: true, InputModalities: []string{"text"}, OutputModalities: []string{"music"}, RequestPrice: 0.04},
		{ID: "google/lyria-3-pro-preview", PriceUnknown: true, InputModalities: []string{"text"}, OutputModalities: []string{"music"}},
		{ID: "bytedance/seedance-2.5", PriceUnknown: true, InputModalities: []string{"text", "image"}, OutputModalities: []string{"video"}},
		{ID: "bytedance/seedance-2.0-mini", PriceUnknown: true, InputModalities: []string{"text", "image"}, OutputModalities: []string{"video"}},
		{ID: "bytedance/seedance-1-5-pro", PriceUnknown: true, InputModalities: []string{"text", "image"}, OutputModalities: []string{"video"}},
	}
}
