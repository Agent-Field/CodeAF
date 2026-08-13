package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// windowCatalog is two models and one difference: what they can hold. The wide
// one publishes a 200k window, the quiet one publishes none — which is how a
// real catalog says "I cannot tell you", and the case every budget in the tree
// has to fall back from rather than guess at.
func windowCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	client := &http.Client{Transport: voiceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		payload := `{"data":[
			{"id":"vendor/wide","context_length":200000,
			 "architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"vendor/quiet",
			 "architecture":{"input_modalities":["text"],"output_modalities":["text"]}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	return catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://example.invalid/api/v1", Dir: t.TempDir(), HTTPClient: client,
	})
}

// The defect this budget was written for: a leaf on a 200k-token model was
// handed 4 KiB for every dependency it had put together, while the same work run
// headless took 6 KiB for each one. The pot is now the leaf's own window, and
// the only thing that still takes the old literal is a leaf nobody could size.
func TestALeafsDependencyPotIsSizedFromItsOwnWindow(t *testing.T) {
	models := windowCatalog(t)

	wide := leafBuild{models: models, model: "vendor/wide"}.dependencyPot()
	if wide <= store.MaxDigestBytes {
		t.Fatalf("a 200k-token leaf was given %d bytes for everything feeding it; "+
			"the whole point is that it is far more than the %d-byte fallback", wide, store.MaxDigestBytes)
	}
	// Not merely larger — larger by the order the window implies. 200k tokens
	// filled to 60% is 120k, and even after the completion reserve and the
	// prompt floor the half-share left is tens of thousands of tokens.
	if wide < 16*store.MaxDigestBytes {
		t.Fatalf("dependency pot for a 200k window = %d bytes; that is not a window-sized budget", wide)
	}

	// A model the catalog cannot place, a catalog that never loaded, and a
	// build with neither: all three are "nobody could say", and all three get
	// exactly what every leaf got before the window was consulted.
	for name, pot := range map[string]int{
		"an unlisted window": leafBuild{models: models, model: "vendor/quiet"}.dependencyPot(),
		"an unknown model":   leafBuild{models: models, model: "vendor/nobody"}.dependencyPot(),
		"no catalog at all":  leafBuild{model: "vendor/wide"}.dependencyPot(),
	} {
		if pot != store.MaxDigestBytes {
			t.Errorf("%s gave %d bytes, want exactly the old literal %d", name, pot, store.MaxDigestBytes)
		}
	}
}

// The pot decides how many inputs are carried and how hard each is clipped, so
// it is read once per worker build and must not move underneath a pass.
func TestTheDependencyPotIsStableForOneBuild(t *testing.T) {
	build := leafBuild{models: windowCatalog(t), model: "vendor/wide"}
	first := build.dependencyPot()
	for attempt := 0; attempt < 4; attempt++ {
		if again := build.dependencyPot(); again != first {
			t.Fatalf("the pot moved between reads: %d then %d", first, again)
		}
	}
}
