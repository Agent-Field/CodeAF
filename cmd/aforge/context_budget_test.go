package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/exec"
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
	build := leafBuild{models: windowCatalog(t), model: "vendor/wide",
		fanIn: store.DependencyFanIn{Count: 8, Bytes: 8 * 16 << 10}}
	first := build.dependencyPot()
	for attempt := 0; attempt < 4; attempt++ {
		if again := build.dependencyPot(); again != first {
			t.Fatalf("the pot moved between reads: %d then %d", first, again)
		}
	}
}

// The other half of the incident: the join that blew its budget was granted the
// flat leaf defaults, which are the defaults for a leaf that gathers nothing.
// The assembler ran out of room partway through the assembly and had to be
// bought a paid continuation splice to finish typing results it had already
// read. Its grant is now arithmetic over what actually landed in it.
func TestAGatheringLeafsGrantIsSizedFromWhatLandedInIt(t *testing.T) {
	// A leaf nothing fed measures zero on both terms and keeps, byte for byte,
	// the envelope every leaf had before any of this existed.
	if turns, tokens := gatheringGrant(chatLeafTurns, chatLeafTokens, store.DependencyFanIn{}); turns != chatLeafTurns || tokens != chatLeafTokens {
		t.Fatalf("an ungathered leaf got %d turns / %d tokens, want the flat %d / %d",
			turns, tokens, chatLeafTurns, chatLeafTokens)
	}

	// Eight dependencies at the record's own bound apiece: 128 KiB of upstream
	// text, which the shared estimator makes 32k tokens.
	wide := store.DependencyFanIn{Count: 8, Bytes: 8 * 16 << 10}
	landed := wide.Bytes / ctxbudget.BytesPerToken
	turns, tokens := gatheringGrant(chatLeafTurns, chatLeafTokens, wide)
	// One turn per dependency, because opening one handle is one tool call.
	if turns != chatLeafTurns+wide.Count {
		t.Fatalf("turns = %d, want %d: a node told to pull needs a hand per handle",
			turns, chatLeafTurns+wide.Count)
	}
	// Twice the landed tokens, because a gathering node reads all of it in and
	// writes an assembly that cannot exceed what it assembles.
	if tokens != chatLeafTokens+2*landed {
		t.Fatalf("tokens = %d, want %d", tokens, chatLeafTokens+2*landed)
	}

	// Monotone in the measurement, with no threshold anywhere: twice the landed
	// bytes is strictly more budget, and a two-way join is strictly more than a
	// leaf that gathers nothing.
	previous := chatLeafTokens
	for _, bytes := range []int{1 << 10, 64 << 10, 8 * 16 << 10, 1 << 20} {
		_, grant := gatheringGrant(chatLeafTurns, chatLeafTokens, store.DependencyFanIn{Count: 2, Bytes: bytes})
		if grant <= previous {
			t.Fatalf("%d landed bytes granted %d tokens, no more than the %d before it",
				bytes, grant, previous)
		}
		previous = grant
	}

	// The reserve rises with the same measurement, and the pot it leaves falls.
	// That trade is the whole policy in one inequality: the more there is
	// upstream, the more of it a gathering node pulls on demand and the less of
	// it is pushed into its prompt whether it will read it or not.
	if gatheringReserve(wide) <= gatheringReserve(store.DependencyFanIn{}) {
		t.Fatalf("the completion reserve did not move with the fan-in: %d vs %d",
			gatheringReserve(wide), gatheringReserve(store.DependencyFanIn{}))
	}
	models := windowCatalog(t)
	alone := leafBuild{models: models, model: "vendor/wide"}.dependencyPot()
	gathering := leafBuild{models: models, model: "vendor/wide", fanIn: wide}.dependencyPot()
	if gathering >= alone {
		t.Fatalf("a join's push budget (%d) did not fall below an ordinary leaf's (%d) "+
			"even though its reply now has to carry the assembly", gathering, alone)
	}
	// And it never falls to nothing: the clamp in ctxbudget stops the reserve
	// where the prompt's remaining room reaches the prompt's own fixed cost.
	huge := leafBuild{models: models, model: "vendor/wide",
		fanIn: store.DependencyFanIn{Count: 400, Bytes: 64 << 20}}.dependencyPot()
	if huge <= 0 {
		t.Fatalf("an enormous fan-in starved the pot to %d; a node that cannot read "+
			"its inputs cannot assemble them either", huge)
	}
}

// The fold's grant, and the shape it is arithmetic over.
//
// A gathering leaf's grant buys hands to fetch with — a turn per dependency, and
// twice the landed text for reading it in and writing it back out. A fold
// fetches nothing: its material is in its prompt, so its run is a prompt and an
// answer, and its grant is that, times the two calls the loop permits it, and
// never more than the open shape it replaces would have taken.
func TestAFoldsGrantIsSizedForThePassItActuallyMakes(t *testing.T) {
	const window = 200_000
	// The measured shape: four producers, about 28 KB of material between them.
	const pushed = 28 << 10
	_, open := gatheringGrant(chatLeafTurns, chatLeafTokens,
		store.DependencyFanIn{Count: 4, Bytes: pushed})
	turns, tokens := foldGrant(window, exec.FoldTurns, pushed, open)

	// The saving that is actually the point: the measured join ran thirteen turns
	// and 181,354 tokens over a single pass worth about 7,700.
	if turns != exec.FoldTurns {
		t.Fatalf("turns = %d, want the %d the loop permits", turns, exec.FoldTurns)
	}
	if turns >= chatLeafTurns+4 {
		t.Fatalf("a fold was granted %d turns; the open shape's own grant is %d",
			turns, chatLeafTurns+4)
	}
	// And the guarantee that the token ceiling does not quietly hand it back: the
	// completion reserve is a generous per-call output cap, and two of them plus
	// two prompts can outrun the open shape's whole envelope.
	if tokens > open {
		t.Fatalf("a fold was granted %d tokens against the open shape's %d; the cheaper "+
			"mode must not buy the bigger allowance", tokens, open)
	}

	// It is not a fixed number either: more material pushed is more room to write
	// the assembly of it, monotonically and with no threshold anywhere. Measured
	// below the ceiling, where the fold's own arithmetic is what answers.
	previous := 0
	for _, bytes := range []int{0, 4 << 10, 16 << 10} {
		_, grant := foldGrant(window, exec.FoldTurns, bytes, 0)
		if grant <= previous {
			t.Fatalf("%d pushed bytes granted %d tokens, no more than the %d before it",
				bytes, grant, previous)
		}
		previous = grant
	}

	// A window nobody could size keeps the process-wide reserve rather than
	// guessing small, which is what every budget in the tree does when nobody
	// can say how large the window is.
	_, unknown := foldGrant(0, exec.FoldTurns, pushed, 0)
	want := exec.FoldTurns * (leafPromptFloorTokens + pushed/ctxbudget.BytesPerToken +
		ctxbudget.CompletionReserve())
	if unknown != want {
		t.Fatalf("an unsized window granted %d tokens, want the process-wide %d", unknown, want)
	}
}
