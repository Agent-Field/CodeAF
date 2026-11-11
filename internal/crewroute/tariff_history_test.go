package crewroute

import (
	"encoding/json"
	"testing"
)

func TestAcceptedTariffIsFrozenForAutomaticAndPinnedDecisions(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "pinned"}[pinned], func(t *testing.T) {
			price := &RoutePrice{Prompt: 0.000001, Completion: 0.000003}
			candidate := candidateOf(glmFlash)
			candidate.Routes[0].Price = price
			request := Request{Class: Bugfix, Candidates: []Candidate{candidate}}
			if pinned {
				request.Pins = map[Seat]Pin{}
				for _, seat := range Seats {
					route := candidate.Routes[0]
					request.Pins[seat] = Pin{Model: candidate.Model.ID, Provider: route.Provider, Send: route.Send, Kind: route.Kind}
				}
			}
			decision, err := Decide(request)
			if err != nil {
				t.Fatal(err)
			}
			price.Prompt = 99 // later catalog mutation must not edit admitted facts
			data, err := json.Marshal(decision)
			if err != nil {
				t.Fatal(err)
			}
			var restored Decision
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			for _, seat := range Seats {
				pick := restored.Seat(seat)
				if pick.Price == nil || pick.Price.Prompt != 0.000001 || pick.Price.Completion != 0.000003 {
					t.Fatalf("%s lost its admitted tariff: %+v", seat, pick.Price)
				}
			}
		})
	}
}
