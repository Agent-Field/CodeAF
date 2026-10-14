package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// Keep this on the shell's actual profile road: the adapter already strips
// the connection and effort, but admission used to price the unsplit word.
func pricedProfileRoad(t *testing.T) carriedRoad {
	t.Helper()
	profile := t.TempDir()
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-fixture")
	t.Setenv("CODEAF_BASE_URL", config.DefaultBaseURL)
	options := catalog.Options{Dir: profile, BaseURL: config.DefaultBaseURL}
	if err := catalog.Remember(options, []catalog.Model{
		{ID: "~fixture/floating-latest", PromptPrice: 0.000001, CompletionPrice: 0.000002},
		{ID: "vendor/native", PromptPrice: 0.000003, CompletionPrice: 0.000004},
		{ID: "vendor/unpriced", PriceUnknown: true},
	}); err != nil {
		t.Fatal(err)
	}
	old := sharedCatalog
	sharedCatalog = func(config.Config) *catalog.Catalog { return catalog.Recall(options) }
	t.Cleanup(func() { sharedCatalog = old })
	road, err := profileRoad()
	if err != nil {
		t.Fatal(err)
	}
	return road
}

func TestShellPricingUsesTheSelectedServiceWithoutStrippingVendorIdentity(t *testing.T) {
	road := pricedProfileRoad(t)
	// The road's catalog can be handed to a differently ordered set without
	// changing any rows. A matching id on a direct service is still unpriced.
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	settings.Models = sharedCatalog(settings)
	custom := modelsource.Connected{Source: modelsource.Source{ID: "custom", Written: "private"}}
	defaultService := modelsource.Connected{Source: modelsource.DefaultSource(config.DefaultBaseURL)}
	for _, tc := range []struct {
		name, model string
		sources     modelsource.Set
		known       bool
	}{
		{"qualified-direct", "private/vendor/native", modelsource.NewSet(defaultService, custom), false},
		{"direct-default", "vendor/native", modelsource.NewSet(custom, defaultService), false},
		{"explicit-openrouter", "openrouter/vendor/native", modelsource.NewSet(custom, defaultService), true},
		{"unconnected-prefix", "private/vendor/native", modelsource.NewSet(defaultService), false},
		{"vendor-prefix", "vendor/native", modelsource.NewSet(defaultService), true},
		{"openrouter-vendor-on-direct", "openrouter/vendor/native", modelsource.NewSet(custom), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.Sources = tc.sources
			in, out, known := carriedModelPrice(settings, tc.model)
			if known != tc.known || (known && (in != 0.000003 || out != 0.000004)) || (!known && (in != 0 || out != 0)) {
				t.Fatalf("price(%q) = (%g, %g, %v), known want %v", tc.model, in, out, known, tc.known)
			}
		})
	}
	if _, _, known := road.modelPrice("native"); known {
		t.Fatal("a vendor suffix alone borrowed a listed model's price")
	}
}

type pricingCompleter func(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error)

func (f pricingCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	return f(ctx, messages, options...)
}

// A tiny positive receipt used to make two half-ceiling reservations no
// longer fit. Exercise the actual API and road, with controlled completions,
// so recognizing a price proves admission and settlement rather than only
// string normalization. Unknown models must still be held behind a call.
func TestShellQualifiedPricingRestoresAdmissionAndKeepsUnknownBounds(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		held        bool
	}{
		{"canonical", "~fixture/floating-latest:low", false},
		{"qualified", "openrouter/~fixture/floating-latest:low", false},
		{"unknown", "openrouter/vendor/missing:low", true},
		{"unpublished-price", "openrouter/vendor/unpriced", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			road := pricedProfileRoad(t)
			started := make(chan struct{}, 2)
			release := make(chan struct{})
			var released sync.Once
			releaseCalls := func() { released.Do(func() { close(release) }) }
			server, err := modelapi.Open(modelapi.Config{Ceiling: 0.25, ModelPrice: road.modelPrice,
				Serves: func(string) bool { return true },
				CompleterFor: func(model string) modelapi.Completer {
					return pricingCompleter(func(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
						if messages[0].Content[0].Text == "hold" {
							started <- struct{}{}
							select {
							case <-release:
							case <-ctx.Done():
								return nil, ctx.Err()
							}
						}
						if sink := provider.BillingSinkFrom(ctx); sink != nil {
							sink(provider.Billed{Model: model, PromptTokens: 1, CompletionTokens: 1, Cost: 0.01})
						}
						return &ai.Response{Model: model, Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "ok"}}}, FinishReason: "stop"}}}, nil
					})
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			// Unblock upstreams before closing the server, even on a failed assertion.
			defer releaseCalls()
			api := server.API()
			type reply struct {
				status int
				held   string
				err    error
			}
			post := func(model, message string, cap int) reply {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}],"max_completion_tokens":%d}`, model, message, cap)
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, modelapi.ChatURL(api.BaseURL), strings.NewReader(body))
				if err != nil {
					return reply{err: err}
				}
				request.Header.Set("Authorization", "Bearer "+api.Token)
				request.Header.Set("Content-Type", "application/json")
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					return reply{err: err}
				}
				defer response.Body.Close()
				_, err = io.Copy(io.Discard, response.Body)
				return reply{response.StatusCode, response.Header.Get(modelapi.HeldHeader), err}
			}
			if got := post("~fixture/floating-latest", "seed", 1); got.err != nil || got.status != 200 {
				t.Fatalf("seed = %+v", got)
			}
			first, second := make(chan reply, 1), make(chan reply, 1)
			go func() { first <- post(tc.model, "hold", 1) }()
			select {
			case <-started:
			case got := <-first:
				t.Fatalf("first call did not start: %+v", got)
			case <-time.After(3 * time.Second):
				t.Fatal("first upstream did not start")
			}
			go func() { second <- post(tc.model, "hold", 1) }()
			if tc.held {
				select {
				case got := <-second:
					if got.err != nil || got.status != 429 || got.held != "ceiling" {
						t.Fatalf("unknown second = %+v", got)
					}
				case <-started:
					t.Fatal("an unknown price admitted both calls despite recorded spend")
				case <-time.After(3 * time.Second):
					t.Fatal("unknown call was not held")
				}
			} else {
				select {
				case <-started:
				case got := <-second:
					t.Fatalf("priced second call did not start: %+v", got)
				case <-time.After(3 * time.Second):
					t.Fatal("priced second upstream did not start")
				}
			}
			// Both admitted calls can now settle.
			releaseCalls()
			if got := <-first; got.err != nil || got.status != 200 {
				t.Fatalf("first = %+v", got)
			}
			if !tc.held {
				if got := <-second; got.err != nil || got.status != 200 {
					t.Fatalf("second = %+v", got)
				}
			}
			wantSpent := 0.03
			if tc.held {
				wantSpent = 0.02
			}
			if got := server.Spent(); math.Abs(got-wantSpent) > 1e-9 {
				t.Fatalf("spent = %g, want %g", got, wantSpent)
			}
			wantHeld := 0
			if tc.held {
				wantHeld = 1
			}
			if server.HeldAtCeiling() != wantHeld {
				t.Fatalf("held = %d, want %d", server.HeldAtCeiling(), wantHeld)
			}
			// A known, oversized reservation remains refused before upstream.
			if got := post("openrouter/~fixture/floating-latest:low", "too expensive", 200000); got.err != nil || got.status != 402 {
				t.Fatalf("oversized priced call = %+v", got)
			}
			if server.RefusedAtCeiling() != 1 || math.Abs(server.Spent()-wantSpent) > 1e-9 {
				t.Fatal("refusal forwarded a call or changed spend")
			}
		})
	}
}

func TestShellPricesCanonicalAndQualifiedModelsOnTheSameProfileRoad(t *testing.T) {
	road := pricedProfileRoad(t)
	for _, tc := range []struct {
		model   string
		in, out float64
		known   bool
	}{
		{"~fixture/floating-latest", 0.000001, 0.000002, true},
		{"~fixture/floating-latest:low", 0.000001, 0.000002, true},
		{"openrouter/~fixture/floating-latest:low", 0.000001, 0.000002, true},
		{"openrouter/vendor/native", 0.000003, 0.000004, true},
		{"vendor/native", 0.000003, 0.000004, true},
		{"vendor/missing", 0, 0, false},
		{"openrouter/vendor/missing:low", 0, 0, false},
		{"openrouter/vendor/unpriced", 0, 0, false},
	} {
		in, out, known := road.modelPrice(tc.model)
		if in != tc.in || out != tc.out || known != tc.known {
			t.Errorf("price(%q) = (%g, %g, %v), want (%g, %g, %v)", tc.model, in, out, known, tc.in, tc.out, tc.known)
		}
	}
}
