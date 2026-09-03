//go:build e2e

package provider_test

// ── THE SELF-INFLICTED REFUSAL, AGAINST THE REAL ROUTER ─────────────────────
//
// selfemptied_test.go proves the law against a router-shaped fake whose serving
// set is a constant somebody typed. That is the right instrument for the rule
// and the wrong one for the claim it was written for, which is about the world:
// the canary's rows came from a real model with a real number of machines
// behind it, and the whole question is whether this process's own list can
// cover that number.
//
// So this file THINS A REAL SERVING SET. Round after round it strikes every
// machine it has seen answer, until the vetoes it sends cover everything the
// router will offer. Before the law, every request from that point on is
// refused before any endpoint is asked; after it, the router's sentence is
// heard once and the requests land.
//
// ── WHAT IT MEASURED ────────────────────────────────────────────────────────
//
// 2026-09-03, deepseek/deepseek-v4-flash, 22 rounds, sixteen machines:
//
//	dev 713945e3b   36 HTTP calls   8 refusal rows (one per request from round 15)   1m09.8s
//	with the law    23 HTTP calls   1 refusal row  (the one that teaches)            31.5s
//
// The thirteen calls that disappear are the ladder's wasted round trips.
//
// ── WHAT IT COSTS ───────────────────────────────────────────────────────────
//
// Every call asks for one word under a small max_tokens, and the run prints the
// cost the router billed. On the model it is written against the whole run is a
// fraction of a cent. Nothing here retries and nothing here loops.
//
// ── HOW TO RUN IT ───────────────────────────────────────────────────────────
//
//	go test -tags e2e ./internal/provider/ -run TestRealRouter -v
//
// It skips, green and loudly, with no key. The key is resolved exactly as a
// session resolves it (internal/config's APIKeyAt): the OpenRouter variable,
// then the OpenAI one, then the profile file.
//
// IN PRACTICE THAT MEANS THE VARIABLE. This package's TestMain re-roots
// AFORGE_HOME to a throwaway directory before any test runs (calllog_test.go),
// so the profile rung resolves under that root and finds nothing — which is the
// right behaviour for every other test here and the reason the skip sentence
// names the variable first. Export it from the profile if that is where the key
// lives.

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// realIgnoreModel is a model the router serves from more than a dozen machines,
// which is what makes the run meaningful: a set this process has to work at
// covering is a set a person would never expect it to empty.
const realIgnoreModel = "deepseek/deepseek-v4-flash"

// realIgnoreRounds is enough requests to strike every machine the router keeps
// offering and then several more, so that "refused once" is distinguishable
// from "refused every time" rather than from "not refused yet".
const realIgnoreRounds = 22

// refusalCounter reads what the router ACTUALLY answered, whatever the ladder
// does with it afterwards — which is the only honest place to count from, since
// a rung that absorbs a refusal leaves no end row behind it (calllog.go).
type refusalCounter struct {
	inner   http.RoundTripper
	calls   int
	refused int
}

func (r *refusalCounter) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := r.inner.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	r.calls++
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if provider.E2ESaysTheSetWasEmptied(body) {
			r.refused++
		}
		response.Body = io.NopCloser(strings.NewReader(string(body)))
	}
	return response, err
}

// TestRealRouterNeverRefusesItselfTwice is the whole of this file.
func TestRealRouterNeverRefusesItselfTwice(t *testing.T) {
	key := strings.TrimSpace(config.APIKeyAt(os.Getenv("AFORGE_PROFILE_DIR")))
	if key == "" {
		t.Skipf("no provider key: set %s (or OPENAI_API_KEY, or the profile's api_key)", config.APIKeyEnv)
	}
	// A home of its own, so nothing this run learns can reach the state root a
	// person is actually using.
	t.Setenv(home.EnvVar, t.TempDir())

	tally := &refusalCounter{inner: http.DefaultTransport}
	client, err := provider.NewClient(provider.Config{
		APIKey:     key,
		BaseURL:    "https://openrouter.ai/api/v1",
		Model:      realIgnoreModel,
		Routing:    provider.StaticRouting(provider.RoutingLatency),
		HTTPClient: &http.Client{Transport: tally, Timeout: 90 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	began := time.Now()
	struck := map[string]bool{}
	firstRefusedAt, refusedAfterTheFirst := -1, 0
	for round := range realIgnoreRounds {
		// Everything this process has seen serve the model is struck again, which
		// is what a pool answering 429 does round after round: the ledger keeps
		// learning the same thing, and the question is whether the request keeps
		// refusing itself over it.
		for name := range struck {
			client.E2EStrikeLane(realIgnoreModel, name, 5*time.Minute)
		}
		before := tally.refused
		answer, err := client.CompleteWithMessages(ctx,
			[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "Reply with the single word: ready."}}}},
			ai.WithMaxTokens(8))
		if err != nil {
			t.Fatalf("round %d of %d: %v", round, realIgnoreRounds, err)
		}
		if answer == nil || len(answer.Choices) == 0 {
			t.Fatalf("round %d: the router answered with no choice at all", round)
		}
		if served := client.E2EServedBy(realIgnoreModel); served != "" {
			struck[served] = true
		}
		if tally.refused > before {
			if firstRefusedAt < 0 {
				firstRefusedAt = round
			} else {
				refusedAfterTheFirst += tally.refused - before
			}
		}
		t.Logf("round %2d  machines struck %2d  ignore %2d  refusals +%d",
			round, len(struck), len(client.E2EVetoes(realIgnoreModel)), tally.refused-before)
	}
	t.Logf("WALL %s  HTTP CALLS %d  MACHINES %d  REFUSAL ROWS %d",
		time.Since(began).Round(time.Millisecond), tally.calls, len(struck), tally.refused)

	// THE ROUTER IS THE ONLY AUTHORITY ON THE SIZE OF THE SET, so being refused
	// ONCE is the design working: it is the sentence this process cannot derive
	// for itself. Being refused twice is the defect.
	if refusedAfterTheFirst > 0 {
		t.Fatalf("%d requests were refused after the router had already said the set was empty "+
			"(first at round %d of %d): the sentence taught this process nothing",
			refusedAfterTheFirst, firstRefusedAt, realIgnoreRounds)
	}
	// AND THE RUN HAS TO HAVE BEEN HARD ENOUGH TO MEAN ANYTHING. A model served
	// by two machines, or a key whose account reaches only one, would pass the
	// assertion above without ever exercising it.
	if len(struck) < 8 {
		t.Fatalf("only %d machines served %s in %d rounds; this run never thinned a set worth thinning",
			len(struck), realIgnoreModel, realIgnoreRounds)
	}
}
