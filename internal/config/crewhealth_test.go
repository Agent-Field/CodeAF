package config

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
)

// withRouteHistory hands the health reading a history of its own, and no
// last-good crew unless the test says.
func withRouteHistory(t *testing.T, history *[]router.CrewRouteOutcome) {
	t.Helper()
	previous, previousGood := CrewRouteHistory, CrewLastGood
	t.Cleanup(func() { CrewRouteHistory, CrewLastGood = previous, previousGood })
	CrewRouteHistory = func(string) []router.CrewRouteOutcome { return *history }
	CrewLastGood = func(string) *router.CrewRecord { return nil }
}

// EACH KIND OF FAILURE MOVES A ROUTE'S HEALTH ITS OWN WAY, and a later success
// on the same account puts it back.
func TestRouteHealthTransitions(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) time.Time { return now.Add(-ago) }
	reset := now.Add(90 * time.Minute)
	h := crewHealthOf([]router.CrewRouteOutcome{
		{At: at(time.Hour), Send: "a/forbidden:free", Provider: "p1", Kind: "forbidden"},
		{At: at(10 * 24 * time.Hour), Send: "a/old-forbidden", Provider: "p1", Kind: "forbidden"},
		{At: at(time.Minute), Send: "a/limited", Provider: "p2", Kind: "quota", Until: reset},
		{At: at(time.Minute), Send: "a/gone", Provider: "p2", Kind: "unavailable"},
		{At: at(time.Hour), Send: "a/pricey", Provider: "broke", Kind: "payment"},
		{At: at(time.Hour), Send: "a/keyless", Provider: "badkey", Kind: "auth"},
		{At: at(2 * time.Hour), Send: "a/twice", Provider: "p3", Kind: "forbidden"},
		{At: at(time.Hour), Send: "openrouter/a/twice", Provider: "p4", Kind: "forbidden"},
		{At: at(time.Hour), Send: "a/flaky", Provider: "p5", Kind: "transient"},
		{At: at(time.Minute), Send: "a/flaky", Provider: "p5"},
	}, now)
	if _, ok := h.blocked["a/forbidden:free"]; !ok {
		t.Error("a forbidden route was not quarantined")
	}
	if _, ok := h.blocked["a/old-forbidden"]; ok {
		t.Error("a quarantine outlived its week")
	}
	if until := h.blocked["a/limited"]; !until.Equal(reset) {
		t.Errorf("a limited route cools until %v, want its reset %v", until, reset)
	}
	if !h.demoted[crewroute.Lineage("a/gone")] || !h.demoted[crewroute.Lineage("a/twice")] {
		t.Errorf("demoted %v: want the withdrawn model and the one refused on two routes", h.demoted)
	}
	if !h.unaffordable["broke"] || !h.disconnected["badkey"] {
		t.Errorf("unaffordable %v, disconnected %v", h.unaffordable, h.disconnected)
	}
	if rate := h.fail["a/flaky"]; rate <= 0 || rate >= 1 {
		t.Errorf("a route that failed once and answered once reads %v", rate)
	}
	// A paid call answering on the account clears it; so does a call on the
	// provider whose key was refused.
	cleared := crewHealthOf([]router.CrewRouteOutcome{
		{At: at(time.Hour), Send: "a/pricey", Provider: "broke", Kind: "payment"},
		{At: at(time.Minute), Send: "a/pricey", Provider: "broke", Paid: true},
		{At: at(time.Hour), Send: "a/keyless", Provider: "badkey", Kind: "auth"},
		{At: at(time.Minute), Send: "a/keyless", Provider: "badkey"},
	}, now)
	if cleared.unaffordable["broke"] || cleared.disconnected["badkey"] {
		t.Errorf("a later success did not clear: %v %v", cleared.unaffordable, cleared.disconnected)
	}
}

// THE OWNER'S SEQUENCE: a route refused a seat with a 403, and the next task
// was routed onto it again. A forbidden route is quarantined, and the next
// task's crew does not sit on it.
func TestAForbiddenRouteIsNotRoutedToAgain(t *testing.T) {
	dir := crewProfile(t)
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	first, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	worker := first.Seat(crewroute.Worker)
	history = append(history, router.CrewRouteOutcome{At: time.Now(), Seat: "worker", Send: worker.Send, Provider: worker.Provider, Kind: "forbidden"})
	second, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	for _, pick := range second.Crew {
		if pick.Send == worker.Send {
			t.Fatalf("the %s seat went back to the quarantined route %s", pick.Seat, pick.Send)
		}
	}
}

// A ZERO-CREDIT ACCOUNT, end to end: a paid call says payment → the free
// pools are used, with the notice → they are limited → the person's own model
// sits the seat → with no model of their own either, the one action.
// Credit coming back puts normal routing back.
func TestAZeroCreditAccountWalksTheLadder(t *testing.T) {
	dir := crewProfile(t)
	rows := CrewCatalog()
	rows = append(rows, catalog.Model{ID: "z-ai/glm-5.3-flash:free", OpenWeights: true,
		IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}})
	CrewCatalog = func() []catalog.Model { return rows }
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	ask := CrewAsk{Task: crewroute.Task{Text: fixTask}}

	normal, err := RouteCrew(dir, ask)
	if err != nil {
		t.Fatal(err)
	}
	w := normal.Seat(crewroute.Worker)
	if w.Kind != crewroute.Metered {
		t.Fatalf("with credit and free routes off the worker is %+v", w)
	}

	history = append(history, router.CrewRouteOutcome{At: time.Now(), Seat: "worker", Send: w.Send, Provider: w.Provider, Kind: "payment"})
	free, err := RouteCrew(dir, ask)
	if err != nil {
		t.Fatal(err)
	}
	if got := free.Seat(crewroute.Worker); got.Kind != crewroute.Free {
		t.Fatalf("with the account out of credit the worker is %+v, want the free pool", got)
	}
	if !strings.Contains(free.Line("", -1), "free routes may log prompts") {
		t.Errorf("the free pools were used without saying so: %q", free.Line("", -1))
	}

	history = append(history, router.CrewRouteOutcome{At: time.Now(), Seat: "worker", Send: "z-ai/glm-5.3-flash:free", Provider: w.Provider, Kind: "quota"})
	chat, err := RouteCrew(dir, CrewAsk{Task: ask.Task, ChatModel: "somelab/the-chat-model"})
	if err != nil {
		t.Fatal(err)
	}
	if got := chat.Seat(crewroute.Worker); got.Model != "somelab/the-chat-model" || !strings.Contains(chat.Line("", -1), "running on fallback crew") {
		t.Fatalf("with nothing routed reachable the worker is %+v (%q), want the chat model", got, chat.Line("", -1))
	}

	_, err = RouteCrew(dir, ask)
	var stop ErrCrewUnreachable
	if !errors.As(err, &stop) || !strings.Contains(stop.Action, "add credit on openrouter") {
		t.Fatalf("with nothing at all the answer is %v, want the one action", err)
	}

	history = append(history, router.CrewRouteOutcome{At: time.Now(), Seat: "worker", Send: w.Send, Provider: w.Provider, Paid: true})
	back, err := RouteCrew(dir, ask)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Seat(crewroute.Worker); got.Kind != crewroute.Metered || strings.Contains(back.Line("", -1), "free routes") {
		t.Fatalf("with credit back the worker is %+v (%q)", got, back.Line("", -1))
	}
}

// A PICKER OFFERS WHAT A SEAT CAN RUN: no model without tool calls, and no
// model whose every route is quarantined.
func TestCrewOffersOnlyWhatASeatCanRun(t *testing.T) {
	dir := crewProfile(t)
	history := []router.CrewRouteOutcome{{At: time.Now(), Send: "moonshotai/kimi-k3", Provider: "openrouter", Kind: "forbidden"}}
	withRouteHistory(t, &history)
	for _, offer := range CrewOffersAt(dir) {
		switch offer.Model.ID {
		case "vendor/no-tools":
			t.Error("a model that takes no tool calls was offered")
		case "moonshotai/kimi-k3":
			t.Error("a model on a quarantined route was offered")
		}
	}
}

// A DIRECT CONNECTION IS A ROUTE BESIDE THE DEFAULT SERVICE: a model a
// connected provider serves itself is one candidate with both routes, and a
// payment failure on one account leaves the other.
func TestADirectConnectionIsASecondRouteToTheSameModel(t *testing.T) {
	dir := crewProfile(t)
	if err := writeProfileValue(dir, keyModelSources, []PersistedSource{{ID: "deepseek", Written: "deepseek", Key: "sk-deepseek-0123456789", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	var routes []crewroute.Route
	for _, c := range CrewCandidatesAt(dir) {
		if crewroute.Lineage(c.Model.ID) == "deepseek/deepseek-v4-flash" {
			routes = c.Routes
		}
	}
	if len(routes) != 2 || routes[0].Provider != "deepseek" || routes[1].Provider != "openrouter" {
		t.Fatalf("deepseek-v4-flash routes: %+v, want the direct connection then the default service", routes)
	}
	history = append(history, router.CrewRouteOutcome{At: time.Now(), Send: routes[1].Send, Provider: "openrouter", Kind: "payment"})
	for _, c := range CrewCandidatesAt(dir) {
		for _, r := range c.Routes {
			if r.Provider == "openrouter" && r.Kind == crewroute.Metered {
				t.Fatalf("a paid route on the account out of credit is still offered: %+v", r)
			}
		}
	}
}
