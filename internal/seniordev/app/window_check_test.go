//go:build !windows

package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/modelsdev"
)

// fixtureModelsDev is senior-dev's catalog fixture: models.dev's shape, with
// openrouter/fixture/vendor-model at 240,000 tokens.
func fixtureModelsDev(t *testing.T) modelsdev.Catalog {
	t.Helper()
	path, err := filepath.Abs("../modelsdev/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	client, err := modelsdev.New(modelsdev.Options{CatalogPath: path, DisableFetch: true, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := client.Get(context.Background())
	if err != nil || len(loaded) == 0 {
		t.Fatalf("fixture catalog = %v, %v", loaded, err)
	}
	return loaded
}

// codeafKnows is a stand-in for the catalog codeaf keeps: the windows it
// lists, by the id codeaf spells them with.
func codeafKnows(windows map[string]int) func(string) int {
	return func(model string) int { return windows[model] }
}

// A MODEL'S WINDOW COMES FROM models.dev FIRST, codeaf's own catalog second,
// and the guess last. With models.dev unreadable the model is still sized at
// its real window when codeaf lists it, which is the whole of what the
// CyberGym run that compacted every few thousand tokens was missing.
func TestAModelIsSizedByModelsDevThenCodeafThenTheGuess(t *testing.T) {
	codeaf := codeafKnows(map[string]int{
		"deepseek/deepseek-v4.1-flash": 1_048_576,
		"fixture/vendor-model":         999,
	})
	cases := []struct {
		name       string
		catalog    modelsdev.Catalog
		windowFor  func(string) int
		provider   string
		model      string
		wantSource string
		wantWindow float64
	}{
		{"models.dev answers first", fixtureModelsDev(t), codeaf, "openrouter", "fixture/vendor-model", sizedByModelsDev, 240_000},
		{"codeaf answers what models.dev never listed", fixtureModelsDev(t), codeaf, "openrouter", "deepseek/deepseek-v4.1-flash", sizedByCodeaf, 1_048_576},
		{"codeaf answers when models.dev is unreadable", modelsdev.Catalog{}, codeaf, "openrouter", "deepseek/deepseek-v4.1-flash", sizedByCodeaf, 1_048_576},
		{"a provider-prefixed spelling finds codeaf's row", modelsdev.Catalog{}, codeaf, "deepseek", "deepseek-v4.1-flash", sizedByCodeaf, 1_048_576},
		{"nothing knows it: the guess", modelsdev.Catalog{}, codeaf, "openrouter", "vendor/offline-model", sizedByGuess, guessedContextTokens},
		{"no codeaf lookup at all: the guess", modelsdev.Catalog{}, nil, "openrouter", "deepseek/deepseek-v4.1-flash", sizedByGuess, guessedContextTokens},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := newModelAPIBackend(testModelAPI, "")
			backend.catalog = tc.catalog
			backend.windowFor = tc.windowFor
			models := seniorDevModels{backend: backend}
			metadata, source, err := models.sizedModel(tc.provider, tc.model)
			if err != nil || source != tc.wantSource || metadata.Limit.Context != tc.wantWindow {
				t.Fatalf("sized = %v from %q (%v), want %v from %q", metadata.Limit.Context, source, err, tc.wantWindow, tc.wantSource)
			}
		})
	}
	// A loaded models.dev that does not know a model codeaf does not know
	// either is still an unknown model, which is refused as it always was.
	backend := newModelAPIBackend(testModelAPI, "")
	backend.catalog = fixtureModelsDev(t)
	backend.windowFor = codeaf
	if _, _, err := (seniorDevModels{backend: backend}).sizedModel("openrouter", "vendor/never-heard-of"); err == nil {
		t.Fatal("a model nothing can size was sized")
	}
}

// A MODEL KNOWN TO BE TOO SMALL IS REFUSED BY NAME AND SIZE; a guessed window
// is never refused, only said. 32,768 tokens is refused and one more is not.
func TestATinyKnownWindowIsRefusedAndAGuessIsOnlySaid(t *testing.T) {
	backend := newModelAPIBackend(testModelAPI, "")
	backend.catalog = modelsdev.Catalog{}
	backend.windowFor = codeafKnows(map[string]int{
		"vendor/small":  16_384,
		"vendor/edge":   32_768,
		"vendor/enough": 32_769,
	})
	models := seniorDevModels{backend: backend}

	refusal, guessed := windowCheck(cliArgs{High: "openrouter/vendor/enough", Low: "openrouter/vendor/small,openrouter/vendor/edge"}, models)
	want := "senior-dev cannot work with vendor/small (16,384 tokens), vendor/edge (32,768 tokens): a run needs a model that holds more than 32,768 tokens"
	if !strings.HasPrefix(refusal, want) || !strings.Contains(refusal, "nothing was started") || len(guessed) != 0 {
		t.Fatalf("refusal = %q, guessed = %v", refusal, guessed)
	}

	refusal, guessed = windowCheck(cliArgs{High: "openrouter/vendor/enough,openrouter/vendor/offline", Low: "openrouter/vendor/offline"}, models)
	if refusal != "" || strings.Join(guessed, ",") != "vendor/offline" {
		t.Fatalf("refusal = %q, guessed = %v; want no refusal and the offline model named once", refusal, guessed)
	}
	var notes bytes.Buffer
	sayGuessedWindows(guessed, &notes, nil)
	if !strings.Contains(notes.String(), "knows how much vendor/offline can hold; assuming 16,384 tokens") {
		t.Fatalf("notes = %q", notes.String())
	}
}

// THE REFUSAL HAPPENS BEFORE ANYTHING IS SPENT. A run asked to work on a model
// codeaf's catalog lists at 16,384 tokens ends with the sentence that names it,
// and never reaches its first stage, let alone a model call.
func TestARunOnATinyModelIsRefusedBeforeItStarts(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_BASE_URL", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("SENIOR_DEV_MODELS_PATH", "")
	t.Setenv("SENIOR_DEV_DISABLE_MODELS_FETCH", "1")
	if err := catalog.Remember(catalog.Options{Dir: profile}, []catalog.Model{
		{ID: "vendor/small", ContextLength: 16_384},
	}); err != nil {
		t.Fatal(err)
	}
	host := &testHost{workspace: t.TempDir(), api: delegate.ModelAPI{BaseURL: "http://127.0.0.1:1/v1", Token: "run-token"}}
	ending := Run(context.Background(), host, Options{Goal: "fix the bug", High: "openrouter/vendor/small"}, nil)
	if ending.Status != delegate.StatusCrashed ||
		!strings.HasPrefix(ending.Message, "senior-dev cannot work with vendor/small (16,384 tokens)") {
		t.Fatalf("ending = %+v", ending)
	}
	if len(host.stages) != 0 {
		t.Fatalf("a refused run reported stages: %v", host.stages)
	}
}
