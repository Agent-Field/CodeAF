// Package catalog owns OpenRouter model discovery across every modality.
// Callers ask narrow capability questions; fetching, TTLs, and offline
// fallbacks stay behind this seam so chat, graph tools, and future voice input
// do not grow separate model caches.
package catalog

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

const (
	TTL             = 24 * time.Hour
	maxCatalogBytes = 16 << 20
	cacheName       = "model-catalog.json"
)

// Model is the small, durable part of one OpenRouter catalog row. Pricing is
// display-ready economics for the existing picker; architecture is retained
// verbatim for modality queries.
type Model struct {
	ID string `json:"id"`
	// CanonicalSlug is the concrete model behind a floating alias. OpenRouter
	// publishes ids like `deepseek/deepseek-v4-flash-latest` that resolve, at
	// request time and on its side, to whatever is current — which is why every
	// call aforge makes with the alias simply works. A second catalog that does
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
	// IntelligenceIndex is the one published score in the catalog, carried
	// verbatim and never computed here.
	//
	// OpenRouter's rows may carry a `benchmarks` block, and inside it an
	// `artificial_analysis` object with `intelligence_index`, `coding_index`
	// and `agentic_index` — Artificial Analysis's numbers, republished. 155 of
	// 528 rows had one on 2026-08-11. Zero means NOBODY published a score, and
	// never a model that scored zero: a surface showing this must render the
	// zero as absence the way it renders an absent price.
	//
	// Only the intelligence index is kept. The other two are the same source
	// saying the same thing at a different angle, and a catalog row is not the
	// place to hold a benchmark suite.
	IntelligenceIndex float64  `json:"intelligence_index,omitempty"`
	InputModalities   []string `json:"input_modalities,omitempty"`
	OutputModalities  []string `json:"output_modalities,omitempty"`
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
// v2 palette, and `aforge models` — and three spellings of one fact is how a
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
}

// Options describes the one catalog fetch. Dir is the Aforge configuration
// directory (AFORGE_PROFILE_DIR when configured, ~/.aforge otherwise).
type Options struct {
	BaseURL    string
	APIKey     string
	Dir        string
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
	// warm is the resolved value published the instant resolution finishes, so
	// a question that must not wait can still be answered once the answer
	// exists. [Catalog.rows] blocks on the future; [Catalog.rowsNow] reads this
	// and takes "not yet" for an answer.
	warm atomic.Pointer[rows]
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
// to a stale cache, then to a very small set of known modality defaults.
func Load(ctx context.Context, options Options) *Catalog {
	return &Catalog{ready: loadOrFallback(ctx, options)}
}

// loadOrFallback is the only way a catalog is resolved, because a fault in
// discovery must degrade the way a failed fetch does — to the known defaults —
// rather than escape. Inside a sync.OnceValue it would escape twice over: once
// on the warming goroutine, and again on whichever caller first asked a
// capability question, since the future replays the panic to every reader.
func loadOrFallback(ctx context.Context, options Options) (resolved *rows) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("catalog/load", recovered)
			resolved = newRows(hardcodedFallbacks())
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
	resolved := &Catalog{}
	resolve := sync.OnceValue(func() *rows {
		loaded := loadOrFallback(ctx, options)
		resolved.warm.Store(loaded)
		return loaded
	})
	resolved.resolve = resolve
	guard.Go("catalog/warm", func() { resolve() })
	return resolved
}

func load(ctx context.Context, options Options) *rows {
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	path := cachePath(options.Dir)
	cached, cachedOK := readCache(path)
	if cachedOK && !options.Refresh && now().Before(cached.FetchedAt.Add(TTL)) {
		return newRowsAt(cached.Models, cached.FetchedAt)
	}

	models, err := fetch(ctx, options)
	if err == nil && len(models) > 0 {
		fetchedAt := now().UTC()
		if path != "" {
			_ = writeCache(path, cache{FetchedAt: fetchedAt, Models: models})
		}
		return newRowsAt(models, fetchedAt)
	}
	if cachedOK {
		// The network is gone and the cache is old. It is still the truest
		// answer anyone has, so it is served WITH ITS DATE rather than
		// withheld: a stale catalog a surface can date is worth more than an
		// empty one it cannot explain.
		return newRowsAt(cached.Models, cached.FetchedAt)
	}
	return newRows(hardcodedFallbacks())
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
// network and not about the change under review. cmd/aforge's launch pins read
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

// ModelsNow is the whole model list for a caller that MUST NOT WAIT, and nil
// while a lazily loaded catalog is still warming.
//
// Every other listing here resolves through [Catalog.rows], which on a cold
// cache means a fifteen-second fetch — fine for `aforge models`, wrong for a
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
// Nothing inside aforge needs it: an alias is a model id OpenRouter accepts,
// and every call aforge makes with one is answered. It matters at exactly one
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
// slug unasked is what broke the swe leaf on the default model of every install:
// models.dev carries deepseek/deepseek-v4-flash and deepseek/deepseek-v4-flash-0731
// and has never heard of deepseek/deepseek-v4-flash-20260423, so a translation
// meant to save the engine handed it a name that could not exist.
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
			IntelligenceIndex: intelligenceIndex(item.Benchmarks),
			InputModalities:   cleanLowerList(item.Architecture.Input),
			OutputModalities:  cleanLowerList(item.Architecture.Output),
			Parameters:        cleanLowerList(item.SupportedParameters),
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
// package reads it. Everything absent from it — description, created,
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
	Architecture  struct {
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
	// Benchmarks stays raw so its shape cannot break the row around it. It
	// carried an object beside a LIST on 2026-08-11 (`design_arena: []` next
	// to `artificial_analysis: {…}`), which is exactly the kind of thing that
	// becomes an object next quarter.
	Benchmarks json.RawMessage `json:"benchmarks"`
}

// intelligenceIndex digs the one published score out of a raw benchmarks
// block, and answers zero for every shape it does not recognize. Nothing here
// is allowed to fail loudly: a score is a nicety on a row, and a catalog that
// refused to load because a benchmark changed shape would have traded four
// hundred models for one number.
func intelligenceIndex(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var block struct {
		ArtificialAnalysis struct {
			IntelligenceIndex float64 `json:"intelligence_index"`
		} `json:"artificial_analysis"`
	}
	if json.Unmarshal(raw, &block) != nil {
		return 0
	}
	if score := block.ArtificialAnalysis.IntelligenceIndex; score > 0 {
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

func normalizeID(id string) string { return strings.TrimPrefix(strings.TrimSpace(id), "~") }

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

func cachePath(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = home.Dir()
	}
	return filepath.Join(dir, cacheName)
}

func readCache(path string) (cache, bool) {
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

// hardcodedFallbacks is written already cleaned — unique ids, lowercase
// modalities — so it satisfies newRows without a cleaning pass of its own.
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
