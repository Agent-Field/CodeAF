//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

// tierRouter builds the run's router from flag values exactly as a run does,
// with a seed so the picks are reproducible.
func tierRouter(args cliArgs) *adaptive.AdaptiveModelRouter {
	seed := 17.0
	return adaptive.NewAdaptiveModelRouter(adaptive.AdaptiveRouterConfig{
		HighModels:     configuredCandidates(args.High, adaptive.ModelTierHigh),
		LowModels:      configuredCandidates(args.Low, adaptive.ModelTierLow),
		FrontierModels: configuredCandidates(args.Frontier, adaptive.ModelTierFrontier),
		RandomSeed:     &seed,
	})
}

func agentTier(agent string) adaptive.ModelTier {
	return adaptive.ModelTier(baked.TierFor(agent))
}

func TestLowPoolChangesWhereTheCompactionSummaryRoutes(t *testing.T) {
	// The compaction summary is the low tier's consumer. Without --low it
	// routes on --high; with --low it routes on the low pool, and the coder
	// stays on --high either way.
	without := tierRouter(cliArgs{High: "openrouter/qwen/high-only"})
	with := tierRouter(cliArgs{
		High: "openrouter/qwen/high-only", Low: "openrouter/qwen/cheap",
	})
	for _, test := range []struct {
		router *adaptive.AdaptiveModelRouter
		agent  string
		want   string
		tier   adaptive.ModelTier
	}{
		{without, "coder", "openrouter/qwen/high-only", adaptive.ModelTierHigh},
		{without, "compaction", "openrouter/qwen/high-only", adaptive.ModelTierHigh},
		{with, "coder", "openrouter/qwen/high-only", adaptive.ModelTierHigh},
		{with, "compaction", "openrouter/qwen/cheap", adaptive.ModelTierLow},
	} {
		choice := test.router.PickSync(test.agent, agentTier(test.agent))
		if choice.Candidate.ID != test.want {
			t.Errorf("%s routed to %q, want %q", test.agent, choice.Candidate.ID, test.want)
		}
		if choice.Tier != test.tier {
			t.Errorf("%s routed on tier %q, want %q", test.agent, choice.Tier, test.tier)
		}
		test.router.Register(choice, 1, 10, nil)
	}
}

func TestFrontierPoolIsOptional(t *testing.T) {
	router := tierRouter(cliArgs{High: "openrouter/qwen/high-only"})
	choice := router.PickSync("coder", adaptive.ModelTierFrontier)
	if choice.Candidate.ID != "openrouter/qwen/high-only" {
		t.Fatalf("frontier routed to %q with no frontier pool", choice.Candidate.ID)
	}
	router.Register(choice, 1, 10, nil)

	configured := tierRouter(cliArgs{
		High: "openrouter/qwen/high-only", Frontier: "openrouter/anthropic/big",
	})
	choice = configured.PickSync("coder", adaptive.ModelTierFrontier)
	if choice.Candidate.ID != "openrouter/anthropic/big" {
		t.Fatalf("frontier routed to %q, want the frontier pool", choice.Candidate.ID)
	}
}

func TestASingleHighPoolRoutesEveryTierIdentically(t *testing.T) {
	// The guarantee for a run that passes nothing but --high: the tier
	// dimension must be invisible. `tiered` asks for each agent's configured
	// tier, which degrades to high because no low or frontier pool exists;
	// `flat` asks for high directly, which is the one code path a router with
	// only a high pool had before tiers came back. Same seed, same pool, so
	// every pick and every emitted event must agree field for field — the
	// tier field itself excepted, since that is the field being added.
	args := cliArgs{High: DefaultHighModels}
	tiered, flat := tierRouter(args), tierRouter(args)
	for round := 0; round < 6; round++ {
		for _, agent := range []string{"coder", "compaction"} {
			got := tiered.PickSync(agent, agentTier(agent))
			want := flat.PickSync(agent, adaptive.ModelTierHigh)
			if got.Tier != adaptive.ModelTierHigh {
				t.Fatalf("round %d: %s routed on %q, want a degraded high", round, agent, got.Tier)
			}
			if got != want {
				t.Fatalf("round %d: %s chose\n %+v\n want %+v", round, agent, got, want)
			}
			gotEvent := tiered.Register(got, 1.5, 120, nil)
			wantEvent := flat.Register(want, 1.5, 120, nil)
			if gotEvent.Tier != adaptive.ModelTierHigh {
				t.Fatalf("round %d: event tier = %q", round, gotEvent.Tier)
			}
			gotEvent.Tier, wantEvent.Tier = "", ""
			if gotEvent != wantEvent {
				t.Fatalf("round %d: %s event\n %+v\n want %+v", round, agent, gotEvent, wantEvent)
			}
		}
	}
}

func TestPoolResolverDegradesEmptyTiersToHigh(t *testing.T) {
	resolver := poolResolver{high: []string{"a/one", "a/two"}}
	for _, tier := range []baked.Tier{baked.TierHigh, baked.TierLow, baked.TierFrontier} {
		if got := strings.Join(resolver.values(tier), ","); got != "a/one,a/two" {
			t.Errorf("values(%q) = %q, want the high pool", tier, got)
		}
	}
	resolver.low = []string{"b/cheap"}
	resolver.frontier = []string{"c/big"}
	for tier, want := range map[baked.Tier]string{
		baked.TierHigh:     "a/one,a/two",
		baked.TierLow:      "b/cheap",
		baked.TierFrontier: "c/big",
	} {
		if got := strings.Join(resolver.values(tier), ","); got != want {
			t.Errorf("values(%q) = %q, want %q", tier, got, want)
		}
	}
}

func TestRunContractRecordsThePoolEachTierRoutesOn(t *testing.T) {
	workspace := gitWorkspace(t, map[string]string{
		"README.md": "base\n",
		"Makefile":  "build:\n\t@true\n\ntest:\n\t@true\n",
	})
	var events bytes.Buffer
	runner := newPipeline(
		cliArgs{High: "a/one,a/two", Low: "b/cheap"},
		workspace,
		pipelineDeps{
			Backend: &soloScriptedBackend{},
			Events:  newEventWriter(&events), Notes: io.Discard,
		},
	)
	defer runner.runtime.Close()
	if _, err := runner.run(context.Background(), "Add the feature."); err != nil {
		t.Fatal(err)
	}
	contract := map[string]any{}
	for _, line := range bytes.Split(bytes.TrimSpace(events.Bytes()), []byte("\n")) {
		var value event
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatalf("invalid NDJSON event %q: %v", line, err)
		}
		if value.Stage == "run-contract" {
			contract = value.Data
		}
	}
	for field, want := range map[string][]any{
		"high_models":     {"a/one", "a/two"},
		"low_models":      {"b/cheap"},
		"frontier_models": {"a/one", "a/two"},
	} {
		got, _ := contract[field].([]any)
		if len(got) != len(want) {
			t.Fatalf("%s = %v, want %v", field, contract[field], want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s = %v, want %v", field, got, want)
			}
		}
	}
}
