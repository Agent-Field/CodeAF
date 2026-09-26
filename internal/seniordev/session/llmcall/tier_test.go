//go:build !windows

package llmcall

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

// tierService wires a router with the given pools to a resolver that accepts
// every "p/<name>" model, so the model a call ends up on names the pool it
// was routed from.
func tierService(low, frontier []adaptive.ModelCandidate) (*Service, *adaptive.AdaptiveModelRouter) {
	seed := float64(3)
	router := adaptive.NewAdaptiveModelRouter(adaptive.AdaptiveRouterConfig{
		HighModels:     []adaptive.ModelCandidate{{ID: "p/high"}},
		LowModels:      low,
		FrontierModels: frontier,
		RandomSeed:     &seed,
	})
	resolver := ModelResolverFunc(func(_ context.Context, provider, model string) (Model, error) {
		if provider != "p" {
			return Model{}, errors.New("missing")
		}
		return Model{ProviderID: provider, ID: model}, nil
	})
	return &Service{Router: router, Models: resolver}, router
}

func TestAgentTierSelectsThePool(t *testing.T) {
	service, router := tierService(
		[]adaptive.ModelCandidate{{ID: "p/low"}},
		[]adaptive.ModelCandidate{{ID: "p/frontier"}},
	)
	for tier, want := range map[adaptive.ModelTier]string{
		adaptive.ModelTierHigh:     "high",
		adaptive.ModelTierLow:      "low",
		adaptive.ModelTierFrontier: "frontier",
		"":                         "high",
	} {
		call, err := service.ResolveAndAssemble(context.Background(), StreamInput{
			Model: Model{ProviderID: "p", ID: "requested"},
			Agent: Agent{Name: "agent", Tier: tier},
		})
		if err != nil {
			t.Fatalf("tier %q: %v", tier, err)
		}
		if call.Model.ID != want {
			t.Errorf("tier %q routed to %q, want %q", tier, call.Model.ID, want)
		}
		router.Register(*call.Choice, 0, 0, nil)
	}
}

func TestAgentTierWithNoPoolRoutesOnHigh(t *testing.T) {
	service, router := tierService(nil, nil)
	for _, tier := range []adaptive.ModelTier{
		adaptive.ModelTierLow, adaptive.ModelTierFrontier,
	} {
		call, err := service.ResolveAndAssemble(context.Background(), StreamInput{
			Model: Model{ProviderID: "p", ID: "requested"},
			Agent: Agent{Name: "agent", Tier: tier},
		})
		if err != nil {
			t.Fatalf("tier %q: %v", tier, err)
		}
		if call.Model.ID != "high" {
			t.Errorf("tier %q routed to %q, want the high pool", tier, call.Model.ID)
		}
		if call.Choice.Tier != adaptive.ModelTierHigh {
			t.Errorf("tier %q recorded %q on the choice", tier, call.Choice.Tier)
		}
		router.Register(*call.Choice, 0, 0, nil)
	}
}
