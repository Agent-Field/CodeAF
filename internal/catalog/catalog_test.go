package catalog

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func catalogClient(t *testing.T, status int, body string, inspect func(*http.Request)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if inspect != nil {
			inspect(request)
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
}

const catalogPayload = `{"data":[
  {"id":"vision/model","name":"Vision","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"}},
  {"id":"paint/model","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"pricing":{"request":"0.05"}},
  {"id":"voice/model","architecture":{"input_modalities":["text"],"output_modalities":["audio"]}},
  {"id":"song/model","architecture":{"input_modalities":["text"],"output_modalities":["music"]}},
  {"id":"motion/model","architecture":{"input_modalities":["text","image"],"output_modalities":["video"]},"pricing":{"request":"0.25"}},
  {"id":"stt/model","architecture":{"input_modalities":["audio"],"output_modalities":["transcription"]}}
]}`

func TestLoadFetchesParsesCachesAndQueriesModalities(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", APIKey: "secret", Dir: dir,
		HTTPClient: catalogClient(t, http.StatusOK, catalogPayload, func(request *http.Request) {
			calls.Add(1)
			if request.URL.Path != "/api/v1/models" || request.URL.Query().Get("output_modalities") != "all" ||
				request.Header.Get("Authorization") != "Bearer secret" {
				t.Errorf("request = %s?%s auth %q", request.URL.Path, request.URL.RawQuery, request.Header.Get("Authorization"))
			}
		}),
	})
	if calls.Load() != 1 {
		t.Fatalf("fetches = %d, want one", calls.Load())
	}
	if got := c.ModelsWithOutput("image"); len(got) != 1 || got[0].ID != "paint/model" {
		t.Fatalf("image models = %+v", got)
	}
	if !c.Supports("~vision/model", "input", "image") || c.Supports("paint/model", "input", "image") {
		t.Fatal("input-image support query was wrong")
	}
	if got := c.ModelsWithOutput("speech"); len(got) != 1 || got[0].ID != "voice/model" {
		t.Fatalf("speech models = %+v", got)
	}
	if got := c.ModelsWithOutput("music"); len(got) != 2 || got[0].ID != "voice/model" || got[1].ID != "song/model" {
		t.Fatalf("music models = %+v", got)
	}
	if got := c.ModelsWithOutput("video"); len(got) != 1 || got[0].ID != "motion/model" {
		t.Fatalf("video models = %+v", got)
	}
	if model, ok := c.Model("~motion/model"); !ok || model.RequestPrice != 0.25 {
		t.Fatalf("video row = %+v, found %t", model, ok)
	}
	if got := c.ModelsWithOutput("transcription"); len(got) != 1 || got[0].ID != "stt/model" {
		t.Fatalf("transcription models = %+v", got)
	}

	// A fresh cache must avoid the transport entirely.
	offline := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("fresh cache performed a fetch")
		return nil, errors.New("offline")
	})}
	fromCache := Load(context.Background(), Options{BaseURL: "https://openrouter.example/api/v1", Dir: dir, HTTPClient: offline})
	if !fromCache.Supports("vision/model", "input", "image") {
		t.Fatal("fresh cache lost modalities")
	}
}

func TestStaleCacheWinsOverOfflineAndEmptyUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := writeCache(cachePath(dir), cache{FetchedAt: old, Models: []Model{{
		ID: "stale/image", OutputModalities: []string{"image"},
	}}}); err != nil {
		t.Fatal(err)
	}
	offline := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir, HTTPClient: offline,
		Now: func() time.Time { return old.Add(48 * time.Hour) },
	})
	if got := c.ModelsWithOutput("image"); len(got) != 1 || got[0].ID != "stale/image" {
		t.Fatalf("stale fallback = %+v", got)
	}

	empty := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(), HTTPClient: offline,
	})
	if len(empty.ModelsWithOutput("image")) == 0 || len(empty.ModelsWithOutput("speech")) == 0 {
		t.Fatal("hardcoded offline defaults were not available")
	}
}

// reasoningPayload is three rows of the live listing's shape, reduced to the
// distinction that matters: what each model says it will accept.
const reasoningPayload = `{"data":[
  {"id":"dial/model","name":"Dial","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["max_tokens","reasoning","include_reasoning","reasoning_effort"]},
  {"id":"thinks/model","name":"Thinks","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["max_tokens","reasoning","include_reasoning"]},
  {"id":"plain/model","name":"Plain","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["max_tokens","temperature"]}
]}`

func TestCatalogCarriesWhichKnobsAModelAccepts(t *testing.T) {
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(),
		HTTPClient: catalogClient(t, http.StatusOK, reasoningPayload, nil),
	})
	// The adapter's question, and the one that decides whether a knob travels.
	if supported, known := c.SupportsParameter("thinks/model", "reasoning"); !supported || !known {
		t.Fatalf("thinks/model reasoning = %t known %t, want both", supported, known)
	}
	if supported, known := c.SupportsParameter("plain/model", "reasoning"); supported || !known {
		t.Fatalf("plain/model reasoning = %t known %t, want a known no", supported, known)
	}
	// Unknown is its own answer and must never read as a no: a model nobody has
	// heard of is where an operator's explicit setting still gets through.
	if _, known := c.SupportsParameter("absent/model", "reasoning"); known {
		t.Fatal("a model the catalog never saw must answer unknown")
	}

	// The surfaces' question, in the words all three of them show.
	dial, _ := c.Model("dial/model")
	thinks, _ := c.Model("thinks/model")
	plain, _ := c.Model("plain/model")
	if word := ReasoningWord(dial, false); word != "reasoning · effort" {
		t.Fatalf("dial word = %q", word)
	}
	if word := ReasoningWord(thinks, false); word != "reasoning" {
		t.Fatalf("thinks word = %q", word)
	}
	if word := ReasoningWord(plain, false); word != "" {
		t.Fatalf("plain word = %q, want silence", word)
	}
	// The state nobody publishes: learned from a refusal, passed in by the
	// caller, and it outranks the published level — a model that will not stop
	// thinking is not a model whose thinking can be dialled down.
	if word := ReasoningWord(dial, true); word != "reasoning · always on" {
		t.Fatalf("learned word = %q", word)
	}
}

func TestParametersSurviveTheCache(t *testing.T) {
	dir := t.TempDir()
	first := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir,
		HTTPClient: catalogClient(t, http.StatusOK, reasoningPayload, nil),
	})
	if _, known := first.SupportsParameter("dial/model", "reasoning_effort"); !known {
		t.Fatal("the fetch did not keep the parameter list")
	}
	// A second process reads the file the first one wrote and must know the
	// same things; a cache that dropped them would send the knob blind again.
	second := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir,
		HTTPClient: catalogClient(t, http.StatusInternalServerError, "", nil),
	})
	if supported, known := second.SupportsParameter("dial/model", "reasoning_effort"); !supported || !known {
		t.Fatalf("cached row = %t known %t, want the fetched answer", supported, known)
	}
}

// The two figures a session's economics need, in the shapes the live catalog
// published them in on 2026-08-15: `pricing.input_cache_read` beside the prompt
// price, and `benchmarks.design_arena` as a LIST of boards.
const cachePricePayload = `{"data":[
  {"id":"warm/model","name":"Warm","architecture":{"input_modalities":["text"],"output_modalities":["text"]},
   "pricing":{"prompt":"0.00001","completion":"0.00005","input_cache_read":"0.000001","input_cache_write":"0.0000125"},
   "benchmarks":{"design_arena":[{"arena":"agents","category":"fullstack","elo":1290},{"arena":"models","category":"3d","elo":1346}],
                 "artificial_analysis":{"intelligence_index":56}}},
  {"id":"cold/model","name":"Cold","architecture":{"input_modalities":["text"],"output_modalities":["text"]},
   "pricing":{"prompt":"0.000002","completion":"0.000004"},
   "benchmarks":{"design_arena":[],"artificial_analysis":{"intelligence_index":40}}},
  {"id":"router/model","name":"Router","architecture":{"input_modalities":["text"],"output_modalities":["text"]},
   "pricing":{"prompt":"-1","completion":"-1"}}
]}`

// WHAT A CACHE READ COSTS IS THE WHOLE OF WHAT A CACHE IS WORTH. A session
// re-sends its transcript on every step, so the gap between the prompt price and
// the cache-read price is the only number that says what the prefix saved — and
// nothing could ask until the catalog kept it.
func TestTheCatalogCarriesTheCacheReadPriceAndTheArenaElo(t *testing.T) {
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(),
		HTTPClient: catalogClient(t, http.StatusOK, cachePricePayload, nil),
	})

	warm, ok := c.Model("warm/model")
	if !ok {
		t.Fatal("warm/model is missing from the catalog")
	}
	if warm.CacheReadPrice != 0.000001 {
		t.Fatalf("CacheReadPrice = %v, want 0.000001", warm.CacheReadPrice)
	}
	// A tenth of the prompt price: the nine-tenths between them is the saving.
	if warm.PromptPrice != 0.00001 {
		t.Fatalf("PromptPrice = %v, want 0.00001", warm.PromptPrice)
	}
	// The boards are different tasks rather than repeated measurements of one,
	// so the row carries the BEST of them and never their average.
	if warm.ArenaElo != 1346 {
		t.Fatalf("ArenaElo = %v, want the best board's 1346", warm.ArenaElo)
	}
	// The block that held a list beside an object still yields its other score.
	if warm.IntelligenceIndex != 56 {
		t.Fatalf("IntelligenceIndex = %v, want 56", warm.IntelligenceIndex)
	}

	// A row that published neither: zero is absence, and a surface renders it as
	// absence rather than as a free cache and an Elo of nothing.
	cold, _ := c.Model("cold/model")
	if cold.CacheReadPrice != 0 || cold.ArenaElo != 0 {
		t.Fatalf("cold/model invented figures: cache %v elo %v", cold.CacheReadPrice, cold.ArenaElo)
	}

	// And OpenRouter's "-1" is still "nobody knows what this costs".
	router, _ := c.Model("router/model")
	if !router.PriceUnknown {
		t.Fatal("a router's unknown pricing was read as a number")
	}
}

// nearestPayload is a listing shaped like the one question [Catalog.NearestModels]
// answers: which of these could take the conversation the failing model was
// holding? Every distinction the ranking reads is present exactly once — a
// vendor sibling, a stranger that scores closer, a model with no tools, one that
// draws pictures, and one with a window a fraction of the size.
const nearestPayload = `{"data":[
  {"id":"vendor/big","name":"Big","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"],"benchmarks":{"artificial_analysis":{"intelligence_index":60}}},
  {"id":"vendor/sibling","name":"Sibling","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"],"benchmarks":{"artificial_analysis":{"intelligence_index":40}}},
  {"id":"other/closer","name":"Closer","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"],"benchmarks":{"artificial_analysis":{"intelligence_index":59}}},
  {"id":"other/no-tools","name":"NoTools","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["max_tokens"]},
  {"id":"other/draws","name":"Draws","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["image","text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]},
  {"id":"other/tiny","name":"Tiny","context_length":8000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]}
]}`

func TestNearestModelsKeepsTheOnesThatCouldActuallyTakeTheConversation(t *testing.T) {
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(),
		HTTPClient: catalogClient(t, http.StatusOK, nearestPayload, nil),
	})
	// Leaving vendor/big: its own vendor comes first, because one vendor's
	// endpoints are the likeliest to accept the same request shape — and only
	// then the stranger that scores nearest.
	got := c.NearestModels("vendor/big", 3)
	want := []string{"vendor/sibling", "other/closer"}
	if len(got) != len(want) {
		t.Fatalf("NearestModels = %v, want %v", got, want)
	}
	for index, id := range want {
		if got[index] != id {
			t.Fatalf("NearestModels = %v, want %v", got, want)
		}
	}
	// A model the catalog never heard of has no neighbours to offer. Guessing
	// one would be this package inventing a fact about a row it does not hold.
	if got := c.NearestModels("absent/model", 3); got != nil {
		t.Fatalf("NearestModels(absent) = %v, want nothing", got)
	}
	if got := c.NearestModels("vendor/big", 0); got != nil {
		t.Fatalf("NearestModels(limit 0) = %v, want nothing", got)
	}
}

// PriceNow is the request-path read: it never waits, and it separates "the
// provider published nothing" from "the provider published zero". The adapter
// bounds a latency-sorted request against the figure it returns, and a ceiling
// derived from a price nobody published would refuse endpoints on a number that
// does not exist (internal/provider's velocity.go).
func TestPriceNowSeparatesAPublishedZeroFromNoPriceAtAll(t *testing.T) {
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(),
		HTTPClient: catalogClient(t, http.StatusOK, cachePricePayload, nil),
	})

	prompt, completion, known := c.PriceNow("warm/model")
	if !known || prompt != 0.00001 {
		t.Fatalf("warm/model = %v/%v known=%v, want the published per-token figures", prompt, completion, known)
	}
	// OpenRouter's "-1" is "it depends", which is not a price.
	if _, _, known := c.PriceNow("router/model"); known {
		t.Fatal("a row that published no price answered as though it had")
	}
	// A model this catalog has never heard of is the same answer.
	if _, _, known := c.PriceNow("nobody/model"); known {
		t.Fatal("an unknown model answered with a price")
	}
	// And a catalog still warming — every lazy one, on the first call of a run —
	// is one more way of not knowing rather than a wait.
	var cold *Catalog
	if _, _, known := cold.PriceNow("warm/model"); known {
		t.Fatal("a catalog that has not resolved answered with a price")
	}
}

// The row's account of the thinking pass, in the shape the live catalog
// published on 2026-08-28: a `reasoning` block with mandatory, the effort
// words the model takes, and where it sits when nobody sends one.
const reasoningProfilePayload = `{"data":[
  {"id":"always/thinks","name":"Always","architecture":{"input_modalities":["text"],"output_modalities":["text"]},
   "pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["reasoning","reasoning_effort"],
   "reasoning":{"mandatory":true,"default_enabled":true,"supported_efforts":["max","High","low"],"default_effort":"max"}},
  {"id":"quiet/model","name":"Quiet","architecture":{"input_modalities":["text"],"output_modalities":["text"]},
   "pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["temperature"]}
]}`

func TestTheReasoningProfileIsReadFromTheRowAndSurvivesTheCache(t *testing.T) {
	dir := t.TempDir()
	first := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir,
		HTTPClient: catalogClient(t, http.StatusOK, reasoningProfilePayload, nil),
	})
	profile, known := first.ReasoningProfile("always/thinks")
	if !known || !profile.Mandatory || profile.DefaultEffort != "max" {
		t.Fatalf("profile = %+v known %t, want the published block", profile, known)
	}
	if got := strings.Join(profile.Efforts, ","); got != "max,high,low" {
		t.Fatalf("efforts = %q, want the words lowercased in the provider's order", got)
	}
	// A row that published no block is unknown — not a model that can be
	// switched off, and not one that cannot.
	if _, known := first.ReasoningProfile("quiet/model"); known {
		t.Fatal("a row without the block must read as unknown")
	}
	second := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir,
		HTTPClient: catalogClient(t, http.StatusInternalServerError, "", nil),
	})
	if cached, known := second.ReasoningProfile("always/thinks"); !known || !cached.Mandatory || len(cached.Efforts) != 3 {
		t.Fatalf("cached profile = %+v known %t, want the fetched answer", cached, known)
	}
}

const levelledPayload = `{"data":[
  {"id":"vendor/thinker","name":"Thinker","context_length":262144,"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"pricing":{"prompt":"0.00000045","completion":"0.0000032"},"supported_parameters":["max_tokens","reasoning"]}
]}`

// A THINKING LEVEL IS NOT PART OF A MODEL ID, and this table is where that costs
// something when it is forgotten.
//
// `moonshotai/kimi-k3:low` is how a tier row — and now, since the crew reaches
// every headless run (config.ResolveSeats), the default plan seat — says "that
// model, thinking a little". Looked up whole it matched no row, and every caller
// took the fallback for a model nobody has heard of: zero context, so a planner
// sized its material from a literal instead of the window; no published price,
// so a receipt said nothing; and "cannot say" about reasoning, so the adapter
// sent no knob at all. One normalization, so no caller has to know.
func TestALevelledValueResolvesAsItsOwnModel(t *testing.T) {
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(),
		HTTPClient: catalogClient(t, http.StatusOK, levelledPayload, nil),
	})
	want := c.ContextLength("vendor/thinker")
	if want != 262144 {
		t.Fatalf("the bare id reads a window of %d, so this test is about nothing", want)
	}
	for _, id := range []string{"vendor/thinker:low", "vendor/thinker:medium", "~vendor/thinker:high"} {
		if got := c.ContextLength(id); got != want {
			t.Errorf("%s reads a window of %d, want %d", id, got, want)
		}
		if _, ok := c.Model(id); !ok {
			t.Errorf("%s found no row", id)
		}
		if _, _, known := c.PriceNow(id); !known {
			t.Errorf("%s has no published price", id)
		}
		if supported, known := c.SupportsParameter(id, "reasoning"); !supported || !known {
			t.Errorf("%s reasoning = %t known %t, want both", id, supported, known)
		}
		if !c.Supports(id, "input", "image") {
			t.Errorf("%s lost its vision row", id)
		}
		if got := c.Identity(id); got != c.Identity("vendor/thinker") {
			t.Errorf("%s is a second identity (%q), which would split its measured history", id, got)
		}
	}
	// And a suffix that is NOT a level is part of the id: OpenRouter's own
	// variants (`:free`, `:nitro`) name different rows, and eating one would
	// answer about a model nobody asked for.
	if _, ok := c.Model("vendor/thinker:free"); ok {
		t.Fatal("a variant suffix was eaten and answered about the base model")
	}
}
