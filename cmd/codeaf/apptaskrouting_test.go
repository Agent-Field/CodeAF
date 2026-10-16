package main

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
)

// tasks.routing.get and .act forward to the engine's one door with the
// agent's own task number and turn its record into the wire's shapes; a
// budget refusal is an answer carrying the cap it needs, never an error.
func TestTaskRoutingReachesTheEngineAndRefusalsAreAnswers(t *testing.T) {
	est := 0.12
	conv := &fakeConversation{routing: session.TaskRoutingView{
		State: session.TaskRoutingRouted, Mode: crewroute.ModeAuto, Running: true, EstimateUSD: &est, SpentUSD: 0.07, Calls: 3, UnknownCalls: 1, ChangeCount: 1,
		Roles: []session.TaskRoutingRole{
			{Seat: crewroute.Worker, Model: "z-ai/glm-5.3", Provider: "OpenRouter", Kind: crewroute.Metered, Source: session.TaskRoleAuto, Status: session.TaskRoleUsed, SpentUSD: 0.06, SpentKnown: true, Calls: 2,
				Price: &crewroute.RoutePrice{Prompt: 4e-7, Completion: 1.6e-6}},
			{Seat: crewroute.Planner, Model: "ds-flash", Provider: "openrouter", Source: session.TaskRoleAuto, Status: session.TaskRoleUsed},
			{Seat: crewroute.Checker, Model: "ds-flash", Provider: "openrouter", Source: session.TaskRolePinned, Status: session.TaskRoleNotRunYet},
		},
		Changes: []session.TaskCrewChange{{Seat: crewroute.Worker, From: "glm-flash", FromProvider: "openrouter", To: "z-ai/glm-5.3", ToProvider: "openrouter", By: session.TaskCrewByEngine, Kind: session.TaskCrewChangeFallback, Why: "limit"}},
	}}
	backend, _, agent := workBackend(t, conv)
	id := agent.ID + ":4"
	got, err := backend.TaskRouting(id, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != id || got.Roles[0].Model.Provider != "openrouter" || got.Roles[0].Source != "auto" || got.Roles[2].Status != "not_run_yet" || *got.EstimateUSD != 0.12 || got.UnknownCalls != 1 {
		t.Fatalf("routing %+v", got)
	}
	if got.Changes[0].From.ID != "glm-flash" || got.Changes[0].By != "engine" || got.Changes[0].Reason != "limit" {
		t.Fatalf("change %+v", got.Changes[0])
	}
	raw, _ := json.Marshal(got)
	if got.Roles[0].Price == nil || got.Roles[0].Price.InPerM < 0.399 || got.Roles[0].Price.InPerM > 0.401 || *got.Roles[0].SpentUSD != 0.06 || got.Roles[1].SpentUSD != nil {
		t.Fatalf("role price/spend %+v %+v", got.Roles[0], got.Roles[1])
	}
	opts, err := backend.TaskRoutingOptions(id)
	if err != nil || opts.Scope != "next_step" || opts.Stronger.Moves[0].To.ID != "sonnet" || opts.Stronger.Moves[0].ToPrice.InPerM != 3 {
		t.Fatalf("options %+v %v", opts, err)
	}
	for _, key := range []string{`"estimate_usd":0.12`, `"unknown_calls":1`, `"task_cap_usd":null`, `"spent_usd":null`} {
		if !json.Valid(raw) || !contains(string(raw), key) {
			t.Fatalf("wire lacks %s: %s", key, raw)
		}
	}

	conv.mu.Lock()
	conv.routingErr = &session.TaskRoutingRefusal{Code: session.TaskRoutingOverCap, Message: "over", RequiredUSD: 3, CapUSD: 2}
	conv.mu.Unlock()
	answer, err := backend.ActOnTaskRouting(appwire.TaskRoutingActParams{ID: id, Action: appwire.TaskRoutingBest})
	if err != nil || answer.Refused == nil || answer.Refused.Code != "over_cap" || *answer.Refused.RequiredCapUSD != 3 || *answer.Refused.CapUSD != 2 {
		t.Fatalf("refusal %+v %v", answer.Refused, err)
	}
	conv.mu.Lock()
	conv.routingErr = nil
	conv.mu.Unlock()
	answer, err = backend.ActOnTaskRouting(appwire.TaskRoutingActParams{ID: id, Action: appwire.TaskRoutingPick, Picks: map[string]*appwire.ModelRef{"checker": {ID: "anthropic/claude-sonnet-4.6", Provider: "openrouter"}, "planner": nil}, RaiseCapUSD: 3})
	if err != nil || len(answer.Applied) != 1 || answer.Applied[0] != "worker" {
		t.Fatalf("applied %+v %v", answer, err)
	}
	conv.mu.Lock()
	act := conv.routingActs[len(conv.routingActs)-1]
	conv.mu.Unlock()
	if act.Kind != "pick" || act.Picks[crewroute.Checker] != "anthropic/claude-sonnet-4.6" || act.Picks[crewroute.Planner] != "" || act.RaiseCapUSD != 3 {
		t.Fatalf("engine was asked %+v", act)
	}
	conv.mu.Lock()
	conv.routingErr = &session.TaskRoutingRefusal{Code: session.TaskRoutingNotFound, Message: "no such task"}
	conv.mu.Unlock()
	if _, err = backend.ActOnTaskRouting(appwire.TaskRoutingActParams{ID: id, Action: appwire.TaskRoutingAuto}); err == nil {
		t.Fatal("not found was answered as a refusal")
	}
}

// The task row carries its models in brief from the published decision.
func TestTheTaskRowCarriesItsModelsChip(t *testing.T) {
	d := &crewroute.Decision{Mode: crewroute.ModeAuto, Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "glm-5.3", Provider: "openrouter"},
		{Seat: crewroute.Planner, Model: "ds-flash", Provider: "openrouter"},
		{Seat: crewroute.Checker, Model: "ds-flash", Provider: "openrouter", Pinned: true},
	}}
	chip := appModelsChip(session.TaskRoutingRouted, d, nil, "")
	if chip.Mode != "auto" || chip.Distinct != 2 || chip.Pinned != 1 || chip.Roles.Checker.ID != "ds-flash" {
		t.Fatalf("chip %+v", chip)
	}
	if chip := appModelsChip(session.TaskRoutingSingle, nil, nil, "glm-5.3"); chip.State != "single" || chip.Roles.Worker.ID != "glm-5.3" || chip.Roles.Planner != nil {
		t.Fatalf("single %+v", chip)
	}
	if chip := appNoticeModels(&session.TaskNotice{Waiting: session.TaskWaitingNoModel}, false); chip.State != "blocked" {
		t.Fatalf("blocked %+v", chip)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
