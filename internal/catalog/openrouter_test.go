package catalog

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// The captured-response tests. testdata/openrouter-models.json is a REAL
// response from https://openrouter.ai/api/v1/models?output_modalities=all,
// fetched 2026-08-11, reduced to twenty-three rows chosen because each one is a
// shape that broke something or could: a router priced "-1", a genuinely free
// model priced "0", rows with and without the benchmarks block, two floating
// aliases, a transcription model whose context_length is 0, and every output
// modality the catalog answers questions about. Nothing in it is hand-written.
//
// The reduction is the only edit: the same twenty-three rows, byte for byte,
// out of the five hundred and twenty-eight the live listing returned.

func fixtureCatalog(t *testing.T, dir string, options Options) *Catalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "openrouter-models.json"))
	if err != nil {
		t.Fatal(err)
	}
	options.BaseURL = DefaultBaseURL
	options.Dir = dir
	if options.HTTPClient == nil {
		options.HTTPClient = catalogClient(t, http.StatusOK, string(raw), nil)
	}
	return Load(context.Background(), options)
}

func TestTheCapturedListingParsesEveryShapeItActuallyPublishes(t *testing.T) {
	c := fixtureCatalog(t, t.TempDir(), Options{})

	// A priced model: per-TOKEN figures, verbatim. The conversion to dollars
	// per million belongs to whatever renders it, not here.
	opus, ok := c.Model("anthropic/claude-opus-5")
	if !ok {
		t.Fatal("the captured listing lost anthropic/claude-opus-5")
	}
	if opus.PromptPrice != 0.000005 || opus.CompletionPrice != 0.000025 {
		t.Errorf("opus prices = %v/%v, want the published per-token figures", opus.PromptPrice, opus.CompletionPrice)
	}
	if opus.PriceUnknown {
		t.Error("a published price came back unknown")
	}
	if opus.ContextLength != 1_000_000 {
		t.Errorf("opus context = %d, want 1000000", opus.ContextLength)
	}
	if opus.Name != "Claude Opus 5" {
		t.Errorf("opus name = %q", opus.Name)
	}
	// The one published score, carried verbatim from
	// benchmarks.artificial_analysis.intelligence_index.
	if opus.IntelligenceIndex != 63.1 {
		t.Errorf("opus intelligence index = %v, want 63.1", opus.IntelligenceIndex)
	}

	// A row with no benchmarks block at all scores zero, and zero means nobody
	// published one — it is never rendered as a score.
	luna, _ := c.Model("openai/gpt-5.6-luna-pro")
	if luna.IntelligenceIndex != 0 {
		t.Errorf("a model with no benchmarks block scored %v", luna.IntelligenceIndex)
	}

	// OpenRouter's own routers publish "-1", which is "it depends on where I
	// route this". That is not free and not zero: it is unknown.
	router, ok := c.Model("openrouter/auto-beta")
	if !ok {
		t.Fatal("the router row is missing")
	}
	if !router.PriceUnknown {
		t.Error(`a router priced "-1" came back as a known price`)
	}
	if router.PromptPrice != 0 || router.CompletionPrice != 0 {
		t.Errorf("an unknown price kept a figure: %v/%v", router.PromptPrice, router.CompletionPrice)
	}

	// A genuinely free model is priced zero AND known, which is the whole
	// reason the flag exists: a surface must be able to tell the two apart.
	free, ok := c.Model("nvidia/nemotron-3.5-lightning:free")
	if !ok {
		t.Fatal("the free row is missing")
	}
	if free.PriceUnknown || free.PromptPrice != 0 || free.CompletionPrice != 0 {
		t.Errorf("free row = %+v, want a known zero", free)
	}

	// A context length the provider did not publish stays zero rather than
	// being guessed at.
	transcribe, _ := c.Model("openai/gpt-transcribe")
	if transcribe.ContextLength != 0 {
		t.Errorf("an unpublished context length became %d", transcribe.ContextLength)
	}

	// Modalities survive as lists, which is what every capability question is
	// asked in.
	if !c.Supports("openrouter/auto-beta", "input", "image") {
		t.Error("input modalities were dropped")
	}
	if len(c.ModelsWithOutput("text")) == 0 || len(c.ModelsWithOutput("image")) == 0 ||
		len(c.ModelsWithOutput("video")) == 0 || len(c.ModelsWithOutput("transcription")) == 0 {
		t.Error("the captured listing lost a modality")
	}
}

// The alias rows are the reason Concrete exists, and until this test they were
// the rows it silently failed on: OpenRouter repeats the ALIAS in
// canonical_slug and names the real model in alias_target.slug.
func TestAFloatingAliasResolvesToTheModelBehindIt(t *testing.T) {
	c := fixtureCatalog(t, t.TempDir(), Options{})
	// A catalog that knows every dated spelling and both alias targets, which is
	// the shape Concrete was written against before anyone checked.
	knowsEverything := func(string) bool { return true }
	for _, tc := range []struct{ alias, want string }{
		{"~x-ai/grok-latest", "x-ai/grok-4.5"},
		{"x-ai/grok-latest", "x-ai/grok-4.5"},
		{"~deepseek/deepseek-v4-flash-latest", "deepseek/deepseek-v4-flash-0731"},
	} {
		if got := c.Concrete(tc.alias, knowsEverything); got != tc.want {
			t.Errorf("Concrete(%q) = %q, want %q", tc.alias, got, tc.want)
		}
		// The alias target is the one substitution made without a catalog to ask,
		// because it is a fact about THIS catalog rather than a guess about
		// another's spelling.
		if got := c.Concrete(tc.alias, nil); got != tc.want {
			t.Errorf("Concrete(%q) with nobody to ask = %q, want %q", tc.alias, got, tc.want)
		}
	}
	// A target catalog keyed by dated ids gets the dated id.
	dated := func(id string) bool { return id == "anthropic/claude-opus-5-20260723" }
	if got := c.Concrete("anthropic/claude-opus-5", dated); got != "anthropic/claude-opus-5-20260723" {
		t.Errorf("Concrete of a concrete id = %q, want its dated spelling", got)
	}
	// An id this catalog has never heard of passes through verbatim, so the
	// far side's own error is what an operator reads.
	if got := c.Concrete("nobody/nothing", knowsEverything); got != "nobody/nothing" {
		t.Errorf("Concrete of an unknown id = %q", got)
	}
}

// The bug this test exists for cost a run: the target catalog HAD the model
// under the plain name, Concrete swapped it for the dated canonical slug that
// catalog has never published, and the far side died at startup naming an id
// nobody chose.
//
// models.dev on 2026-08-11 carried deepseek/deepseek-v4-flash and
// deepseek/deepseek-v4-flash-0731 and no dated spelling of either. The fixture
// row stands in for that shape: a plain id the target knows, a canonical_slug
// it does not.
func TestConcreteWillNotTradeAnIdTheTargetHasForOneItDoesNot(t *testing.T) {
	c := fixtureCatalog(t, t.TempDir(), Options{})
	const plain = "anthropic/claude-opus-5"
	const dated = "anthropic/claude-opus-5-20260723"
	// The target carries the undated name only — the un-dated/aliased form.
	undatedOnly := func(id string) bool { return id == plain }
	if got := c.Concrete(plain, undatedOnly); got != plain {
		t.Errorf("Concrete(%q) = %q, want the id the target catalog actually has", plain, got)
	}
	if got := c.Concrete(plain, undatedOnly); got == dated {
		t.Errorf("Concrete substituted %q, which the target catalog cannot resolve", dated)
	}
	// And when NOTHING resolves, the id as written is forwarded rather than a
	// spelling the target is known not to have, so the far side's error names
	// the model the operator chose.
	if got := c.Concrete(plain, func(string) bool { return false }); got != plain {
		t.Errorf("Concrete with nothing resolvable = %q, want %q verbatim", got, plain)
	}
}

// One row of a third party's schema must never be able to take the other four
// hundred with it.
func TestARowThatDriftsIsSkippedAndTheRestSurvive(t *testing.T) {
	const drifted = `{"data":[
	  {"id":"good/first","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"}},
	  {"id":"drifted/row","context_length":"a string where a number used to be","architecture":{"output_modalities":["text"]}},
	  {"id":"good/second","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0"}}
	]}`
	c := Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: t.TempDir(),
		HTTPClient: catalogClient(t, http.StatusOK, drifted, nil),
	})
	if _, ok := c.Model("good/first"); !ok {
		t.Error("a row before the drifted one was lost")
	}
	if _, ok := c.Model("good/second"); !ok {
		t.Error("a row after the drifted one was lost")
	}
	if _, ok := c.Model("drifted/row"); ok {
		t.Error("a row that could not be read was kept anyway")
	}
}

// The cache lives under the profile dir, is fetched at most once a day, is
// dated with the provider's own visit rather than the reader's, and can be
// asked for again by hand.
func TestTheCacheIsDailyDatedAndRefreshableByHand(t *testing.T) {
	dir := t.TempDir()
	var fetches atomic.Int32
	raw, err := os.ReadFile(filepath.Join("testdata", "openrouter-models.json"))
	if err != nil {
		t.Fatal(err)
	}
	counting := func() *http.Client {
		return catalogClient(t, http.StatusOK, string(raw), func(*http.Request) { fetches.Add(1) })
	}
	day := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	now := day

	first := fixtureCatalog(t, dir, Options{HTTPClient: counting(), Now: func() time.Time { return now }})
	if fetches.Load() != 1 {
		t.Fatalf("a cold catalog fetched %d times", fetches.Load())
	}
	if !first.FetchedAt().Equal(day) {
		t.Errorf("FetchedAt = %v, want the moment of the fetch", first.FetchedAt())
	}
	// The cache is a file under the profile dir, and nowhere else.
	if _, err := os.Stat(filepath.Join(dir, cacheName)); err != nil {
		t.Fatalf("no cache under the profile dir: %v", err)
	}

	// Inside the day: no network at all, and the catalog still dates itself
	// from the fetch rather than from now.
	now = day.Add(6 * time.Hour)
	warm := fixtureCatalog(t, dir, Options{HTTPClient: counting(), Now: func() time.Time { return now }})
	if fetches.Load() != 1 {
		t.Fatalf("a fresh cache fetched again (%d total)", fetches.Load())
	}
	if !warm.FetchedAt().Equal(day) {
		t.Errorf("a cached catalog dated itself %v, want %v", warm.FetchedAt(), day)
	}
	if _, ok := warm.Model("anthropic/claude-opus-5"); !ok {
		t.Error("the cache round-trip lost a model")
	}
	// The cache carries the facts the fetch parsed, not a re-guess of them.
	cachedRouter, _ := warm.Model("openrouter/auto-beta")
	if !cachedRouter.PriceUnknown {
		t.Error("the cache round-trip turned an unknown price into a known one")
	}
	cachedOpus, _ := warm.Model("anthropic/claude-opus-5")
	if cachedOpus.IntelligenceIndex != 63.1 {
		t.Errorf("the cache round-trip lost the published score: %v", cachedOpus.IntelligenceIndex)
	}

	// A day later, the clock alone is enough.
	now = day.Add(25 * time.Hour)
	fixtureCatalog(t, dir, Options{HTTPClient: counting(), Now: func() time.Time { return now }})
	if fetches.Load() != 2 {
		t.Fatalf("a day-old cache did not refetch (%d fetches)", fetches.Load())
	}

	// And a hand-asked refresh spends the network inside the day.
	now = day.Add(26 * time.Hour)
	fixtureCatalog(t, dir, Options{Refresh: true, HTTPClient: counting(), Now: func() time.Time { return now }})
	if fetches.Load() != 3 {
		t.Fatalf("an explicit refresh did not fetch (%d fetches)", fetches.Load())
	}
}

// A refresh that fails must leave the reader with the facts they already had,
// dated honestly — never with less than they started with.
func TestAFailedRefreshServesTheCacheWithItsOwnDate(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	fixtureCatalog(t, dir, Options{Now: func() time.Time { return day }})

	offline := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	stale := Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: dir, HTTPClient: offline, Refresh: true,
		Now: func() time.Time { return day.Add(72 * time.Hour) },
	})
	if _, ok := stale.Model("anthropic/claude-opus-5"); !ok {
		t.Fatal("a failed refresh threw the cache away")
	}
	if !stale.FetchedAt().Equal(day) {
		t.Errorf("stale catalog dated %v, want the day it was actually fetched", stale.FetchedAt())
	}

	// With no cache at all there is nothing to date, and the built-in
	// fallbacks say so by carrying no date and no prices.
	bare := Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: t.TempDir(), HTTPClient: offline,
	})
	if !bare.FetchedAt().IsZero() {
		t.Errorf("the built-in fallbacks claim a fetch date of %v", bare.FetchedAt())
	}
	for _, model := range bare.ModelsWithOutput("image") {
		if !model.PriceUnknown {
			t.Errorf("fallback %q claims to know its price", model.ID)
		}
	}
}
