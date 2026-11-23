package config

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
)

// A price published on one provider is not evidence that another provider
// publishes a price. A person can pin that route, but Auto cannot choose it.
func TestTaskModelsUnknownOwnPriceCannotEnterAuto(t *testing.T) {
	dir := taskModelsWithAiand(t)
	_, rows, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	row, ok := taskRowFor(rows, "z-ai/glm-5.3-flash", "aiand")
	if !ok || row.Price != nil {
		t.Fatalf("expected direct route with no published price: %+v", row)
	}
	if row.AutoEligible || row.Reason != crewroute.RejectNoPrice {
		t.Fatalf("unpriced direct route offered to Auto: %+v", row)
	}
	if err := SetCrewAllowed(dir, "+z-ai/glm-5.3-flash@aiand -@openrouter"); err != nil {
		t.Fatal(err)
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	if err == nil {
		for _, seat := range crewroute.Seats {
			pick := d.Seat(seat)
			if pick.Provider == "aiand" && !pick.Pinned {
				t.Fatalf("Auto actually selected a route with no published own price: %+v", pick)
			}
		}
	}
}

func TestTaskModelsExpiredOneRouteRefusesAdmission(t *testing.T) {
	dir := crewProfile(t)
	applyTaskModels(t, dir, TaskModelsChange{Mode: modePtr(crewroute.ModeOne), One: &CrewPin{Model: "z-ai/glm-5.3-flash", Provider: "openrouter"}})
	previous := CrewRouteHistory
	t.Cleanup(func() { CrewRouteHistory = previous })
	CrewRouteHistory = func(string) []router.CrewRouteOutcome {
		return []router.CrewRouteOutcome{{At: time.Now().Add(-time.Minute), Send: "z-ai/glm-5.3-flash", Provider: "openrouter", Kind: "auth"}}
	}
	_, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	var refusal *crewroute.NoEligibleError
	if !errors.As(err, &refusal) || refusal.Mode != crewroute.ModeOne {
		t.Fatalf("known expired one-model route must refuse admission, got %v", err)
	}
}

func TestTaskModelsAllowedSavedPinNeverSubstitutesAnExpiredRoute(t *testing.T) {
	dir := taskModelsWithAiand(t)
	seedOwnPrices(t, dir, "aiand", map[string][3]float64{"zai-org/glm-5.3-flash": {0.00000015, 0.0000005, 0}})
	applyTaskModels(t, dir, TaskModelsChange{Pins: map[crewroute.Seat]*CrewPin{crewroute.Worker: {Model: "z-ai/glm-5.3-flash", Provider: "openrouter"}}})
	previous := CrewRouteHistory
	t.Cleanup(func() { CrewRouteHistory = previous })
	CrewRouteHistory = func(string) []router.CrewRouteOutcome {
		return []router.CrewRouteOutcome{{At: time.Now().Add(-time.Minute), Send: "z-ai/glm-5.3-flash", Provider: "openrouter", Kind: "auth"}}
	}
	_, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	var refusal *crewroute.NoEligibleError
	if !errors.As(err, &refusal) || refusal.Seat != crewroute.Worker {
		t.Fatalf("allowed explicit worker pin must ask/fix its expired route, got %v", err)
	}
}
