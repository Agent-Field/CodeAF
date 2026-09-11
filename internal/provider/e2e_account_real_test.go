//go:build e2e

package provider_test

// ── THE ACCOUNT'S OWN EXCLUSION, AGAINST THE REAL ROUTER ────────────────────
//
// recovery_test.go proves the law against lanestub, whose account-excluded lane
// answers with a body copied from the live router. This file asks the live
// router itself, on the owner's account, which on 2026-09-10 excludes the
// DeepSeek first-party machine under the "paid model training" privacy switch —
// for deepseek/deepseek-v4.1-flash AND for deepseek/deepseek-v4-pro-0813, which
// is the whole claim: the exclusion is the account's and the machine's, not a
// model's.
//
// THREE CALLS, AND THE ASSERTION IS ABOUT THE WIRE BETWEEN THEM:
//
//  1. v4.1-flash, demanding DeepSeek, with other machines on the frontier. The
//     router refuses with the account's sentence (the one refusal this process
//     should ever pay), and the call still lands — on the FIRST attempt.
//  2. v4-pro-0813, demanding a machine that does not serve it, with DeepSeek
//     next on the frontier. The walk must step past DeepSeek to a machine that
//     answers. Before the fix it demanded DeepSeek and paid the 404 again.
//  3. The same, from a fresh client after this process's memory is emptied —
//     the next process on the same home. It must not demand DeepSeek either.
//
// It costs a handful of one-word answers: a fraction of a cent.
//
//	go test -tags e2e ./internal/provider/ -run TestRealRouterAccountExclusion -v
//
// It SKIPS green without OPENROUTER_API_KEY (this package's TestMain re-roots
// AFORGE_HOME, so the variable is the way in), and it SKIPS, saying so, when
// the account no longer excludes DeepSeek — a probe asks first, with a bare
// request, so a changed setting is reported as a changed setting and never as
// a pass.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/home"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	accountModelA    = "deepseek/deepseek-v4.1-flash"
	accountModelB    = "deepseek/deepseek-v4-pro-0813"
	accountExcluded  = "DeepSeek"
	accountNotServed = "Wafer"
	accountRouterURL = "https://openrouter.ai/api/v1"
)

// accountTally counts, off the real wire, how often a request DEMANDED the
// excluded machine and how often the router answered with the account's
// sentence.
type accountTally struct {
	inner http.RoundTripper

	mu        sync.Mutex
	calls     int
	demanded  int
	refusals  int
	lastError string
}

func (a *accountTally) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.GetBody != nil && strings.HasSuffix(request.URL.Path, "/chat/completions") {
		if body, err := request.GetBody(); err == nil {
			var sent struct {
				Provider *struct {
					Only []string `json:"only"`
				} `json:"provider"`
			}
			if json.NewDecoder(body).Decode(&sent) == nil && sent.Provider != nil {
				for _, lane := range sent.Provider.Only {
					if strings.EqualFold(lane, accountExcluded) {
						a.mu.Lock()
						a.demanded++
						a.mu.Unlock()
					}
				}
			}
			body.Close()
		}
	}
	response, err := a.inner.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if response.StatusCode >= 400 {
		payload, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if provider.E2ESaysTheAccountExcluded(payload) {
			a.refusals++
		}
		a.lastError = string(payload)
		response.Body = io.NopCloser(bytes.NewReader(payload))
	}
	return response, err
}

func (a *accountTally) read() (calls, demanded, refusals int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls, a.demanded, a.refusals
}

// accountStillExcludes asks the router, bare, whether the account still
// excludes the machine this test is about.
func accountStillExcludes(t *testing.T, key string) (bool, string) {
	t.Helper()
	body := `{"model":"` + accountModelA + `","messages":[{"role":"user","content":"hi"}],"max_tokens":4,` +
		`"provider":{"only":["` + accountExcluded + `"],"allow_fallbacks":false}}`
	request, err := http.NewRequest(http.MethodPost, accountRouterURL+"/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: time.Minute}).Do(request)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return response.StatusCode == http.StatusNotFound && provider.E2ESaysTheAccountExcluded(payload), string(payload)
}

func TestRealRouterAccountExclusionIsPaidOnceForEveryModel(t *testing.T) {
	key := strings.TrimSpace(config.APIKeyAt(os.Getenv("AFORGE_PROFILE_DIR")))
	if key == "" {
		t.Skipf("no provider key: set %s (or OPENAI_API_KEY, or the profile's api_key)", config.APIKeyEnv)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	if excluded, said := accountStillExcludes(t, key); !excluded {
		t.Skipf("this account no longer excludes %s for %s, so there is nothing to learn; the router said: %s",
			accountExcluded, accountModelA, said)
	}
	lanes.ForgetRefusals()
	t.Cleanup(lanes.ForgetRefusals)
	// THE PURSE THE MEASURED SESSION HAD. A process that has spent nothing has a
	// purse whose share rule funds no walk at all, and then every refusal door
	// climbs the ladder on the spot and nothing is ever deferred — which is not
	// the race of 2026-09-10, a turn twenty-five messages into a conversation
	// with a spending history. Six rescues in twenty and no share limit is what
	// the stub rig states for the same reason (hedge_test.go's newLaneRig).

	tally := &accountTally{inner: http.DefaultTransport}
	newClient := func() *provider.Client {
		client, err := provider.NewClient(provider.Config{
			APIKey:     key,
			BaseURL:    accountRouterURL,
			Model:      accountModelA,
			Routing:    provider.StaticRouting(provider.RoutingLatency),
			HTTPClient: &http.Client{Transport: tally, Timeout: 90 * time.Second},
		})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	frontier := func(model string, lanesOn ...string) []lanes.Scored {
		var scored []lanes.Scored
		for _, lane := range lanesOn {
			scored = append(scored, lanes.Scored{ID: lanes.ID{Model: model, Lane: lane}, TTFT: 2, Rate: 100, Price: 0.0001})
		}
		return scored
	}
	ask := func(client *provider.Client, model string, choice lanes.Choice) {
		t.Helper()
		ctx, cancel := context.WithTimeout(provider.WithRole(context.Background(), lanes.RoleTalk), 3*time.Minute)
		defer cancel()
		ctx = provider.WithLaneChoice(ctx, choice)
		began := time.Now()
		answer, err := client.CompleteWithMessages(ctx,
			[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "Reply with the single word: ready."}}}},
			ai.WithModel(model), ai.WithMaxTokens(512))
		calls, demanded, refusals := tally.read()
		t.Logf("%-32s  %6s  err=%v  calls=%d demanded(%s)=%d account-refusals=%d",
			model, time.Since(began).Round(time.Millisecond), err, calls, accountExcluded, demanded, refusals)
		if err != nil {
			t.Fatalf("%s did not land on the first attempt: %v", model, err)
		}
		if answer == nil || len(answer.Choices) == 0 {
			t.Fatalf("%s: the router answered with no choice at all", model)
		}
	}

	first := newClient()
	// 1. The demand the account refuses. Other machines are on the frontier, so
	// the walk has somewhere to go — and must go there on this same call.
	ask(first, accountModelA, lanes.Choice{
		Only:     []string{accountExcluded},
		Frontier: frontier(accountModelA, accountExcluded, "Novita", "GMICloud", "DeepInfra"),
	})
	if _, demanded, refusals := tally.read(); demanded != 1 || refusals != 1 {
		t.Fatalf("after the first call: demanded %d, account refusals %d — want the one of each that teaches", demanded, refusals)
	}
	if lanes.Serves(accountModelB, accountExcluded) {
		t.Fatalf("after one account refusal on %s, %s is still believed to serve %s", accountModelA, accountExcluded, accountModelB)
	}

	// 2. Another model. The primary demands a machine that does not serve it,
	// and DeepSeek is the walk's next candidate: it must be stepped past.
	onB := lanes.Choice{
		Only:     []string{accountNotServed},
		Frontier: frontier(accountModelB, accountNotServed, accountExcluded, "Novita", "GMICloud", "DeepInfra"),
	}
	ask(first, accountModelB, onB)

	// 3. The next process on the same home.
	lanes.ForgetAccountExclusionsInMemory()
	ask(newClient(), accountModelB, onB)

	calls, demanded, refusals := tally.read()
	t.Logf("TOTAL  HTTP CALLS %d  DEMANDED %s %d  ACCOUNT REFUSALS %d", calls, accountExcluded, demanded, refusals)
	if demanded != 1 || refusals != 1 {
		t.Fatalf("%s was demanded %d times and the account's refusal paid %d times across two models and two clients; want once each",
			accountExcluded, demanded, refusals)
	}
}

// accountPaced is the machine whose pool the router reported full on this
// account for [accountModelA] when this was written (2026-09-10 20:10 EDT:
// `429 … temporarily rate-limited upstream`), which is what the measured race's
// last arm died of.
const accountPaced = "Fireworks"

// laneIsPaced asks the router, bare, whether a demand of that machine is being
// answered with a 429 right now.
func laneIsPaced(t *testing.T, key, lane string) (bool, string) {
	t.Helper()
	body := `{"model":"` + accountModelA + `","messages":[{"role":"user","content":"hi"}],"max_tokens":4,` +
		`"provider":{"only":["` + lane + `"],"allow_fallbacks":false}}`
	request, err := http.NewRequest(http.MethodPost, accountRouterURL+"/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: time.Minute}).Do(request)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return strings.Contains(string(payload), `"code":429`) || response.StatusCode == http.StatusTooManyRequests, string(payload)
}

// TestRealRouterTheMeasuredRaceLandsOnTheFirstAttempt is the 19:32 race on the
// live router: the primary demands a machine the account excludes (a routing
// 404 the door hands to the walk), the walk's only other machine is a full pool
// (a 429), and nothing is left. Before the fix the race settled on the primary's
// 404 and the call failed; now the deferred ladder runs and the router's free
// choice answers — on this one call.
func TestRealRouterTheMeasuredRaceLandsOnTheFirstAttempt(t *testing.T) {
	key := strings.TrimSpace(config.APIKeyAt(os.Getenv("AFORGE_PROFILE_DIR")))
	if key == "" {
		t.Skipf("no provider key: set %s (or OPENAI_API_KEY, or the profile's api_key)", config.APIKeyEnv)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	if excluded, said := accountStillExcludes(t, key); !excluded {
		t.Skipf("this account no longer excludes %s; the router said: %s", accountExcluded, said)
	}
	if paced, said := laneIsPaced(t, key, accountPaced); !paced {
		t.Skipf("%s is not answering 429 for %s right now, so the race's last arm cannot be staged; it said: %s",
			accountPaced, accountModelA, said)
	}
	lanes.ForgetRefusals()
	t.Cleanup(lanes.ForgetRefusals)

	tally := &accountTally{inner: http.DefaultTransport}
	client, err := provider.NewClient(provider.Config{
		APIKey:     key,
		BaseURL:    accountRouterURL,
		Model:      accountModelA,
		Routing:    provider.StaticRouting(provider.RoutingLatency),
		HTTPClient: &http.Client{Transport: tally, Timeout: 90 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	var said []string
	var mu sync.Mutex
	ctx, cancel := context.WithTimeout(provider.WithRole(context.Background(), lanes.RoleTalk), 3*time.Minute)
	defer cancel()
	ctx = provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		if event.Kind == provider.StreamNotice {
			mu.Lock()
			said = append(said, event.Delta)
			mu.Unlock()
		}
	})
	ctx = provider.WithLaneChoice(ctx, lanes.Choice{
		Only: []string{accountExcluded},
		Frontier: []lanes.Scored{
			{ID: lanes.ID{Model: accountModelA, Lane: accountExcluded}, TTFT: 2, Rate: 100, Price: 0.0001},
			{ID: lanes.ID{Model: accountModelA, Lane: accountPaced}, TTFT: 2, Rate: 100, Price: 0.0001},
		},
	})
	began := time.Now()
	answer, err := client.CompleteWithMessages(ctx,
		[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "Reply with the single word: ready."}}}},
		ai.WithMaxTokens(512))
	calls, demanded, refusals := tally.read()
	mu.Lock()
	t.Logf("%s  err=%v  calls=%d demanded(%s)=%d account-refusals=%d  notices=%q",
		time.Since(began).Round(time.Millisecond), err, calls, accountExcluded, demanded, refusals, said)
	mu.Unlock()
	if err != nil {
		t.Fatalf("the measured race did not land on the first attempt: %v", err)
	}
	if answer == nil || len(answer.Choices) == 0 {
		t.Fatal("the router answered with no choice at all")
	}
	if refusals != 1 {
		t.Fatalf("the account's refusal was paid %d times on one call, want once", refusals)
	}
}
