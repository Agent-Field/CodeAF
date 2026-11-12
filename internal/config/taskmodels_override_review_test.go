package config

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/crewroute"
)

func TestExplicitAutoSeatsOnlyBypassesNamedSavedPolicies(t *testing.T) {
	for _, one := range []bool{false, true} {
		t.Run(map[bool]string{false: "role-pins", true: "one-model"}[one], func(t *testing.T) {
			dir := crewProfile(t)
			pin := &CrewPin{Model: "deepseek/deepseek-v4-flash", Provider: "openrouter"}
			change := TaskModelsChange{Pins: map[crewroute.Seat]*CrewPin{crewroute.Worker: pin, crewroute.Planner: pin, crewroute.Checker: pin}}
			if one {
				change.Mode, change.One = modePtr(crewroute.ModeOne), pin
			}
			applyTaskModels(t, dir, change)
			before := rawRows(t, dir)
			decision, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}, Effort: crewroute.EffortBest, AutoSeats: map[crewroute.Seat]bool{crewroute.Worker: true}})
			if err != nil {
				t.Fatal(err)
			}
			if decision.Seat(crewroute.Worker).Pinned || decision.Mode != crewroute.ModeAuto {
				t.Fatalf("explicit worker opt-out did not route: %+v", decision.Seat(crewroute.Worker))
			}
			for _, seat := range []crewroute.Seat{crewroute.Planner, crewroute.Checker} {
				pick := decision.Seat(seat)
				if !pick.Pinned || pick.Model != pin.Model || pick.Provider != pin.Provider {
					t.Fatalf("unaffected %s moved: %+v", seat, pick)
				}
			}
			for key, value := range rawRows(t, dir) {
				if before[key] != value {
					t.Fatalf("per-task override persisted %s", key)
				}
			}
			explicit := CrewPin{Model: "z-ai/glm-5.3-flash", Provider: "openrouter"}
			decision, err = RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}, AutoSeats: map[crewroute.Seat]bool{crewroute.Worker: true}, Pins: map[crewroute.Seat]CrewPin{crewroute.Worker: explicit}})
			if err != nil || !decision.Seat(crewroute.Worker).Pinned || decision.Seat(crewroute.Worker).Model != explicit.Model {
				t.Fatalf("explicit per-task pin must win: %+v, %v", decision.Seat(crewroute.Worker), err)
			}
		})
	}
}
