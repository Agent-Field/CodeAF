package config

import (
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

// A ZERO-CREDIT ACCOUNT, end to end: a paid call says payment → the next
// task is still routed on the paid route, which its first call probes → the
// seat's rescue is the free pool → with that pool at its limit and the chat
// model on the same account, nothing → the one action. A paid call answering
// clears it, and the rescue has no free pool to offer any more.
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
	probe, err := RouteCrew(dir, ask)
	if err != nil {
		t.Fatalf("an account out of credit refused the task at decision time: %v", err)
	}
	if got := probe.Seat(crewroute.Worker); got.Kind != crewroute.Metered {
		t.Fatalf("the next task's worker is %+v, want the paid route, probed", got)
	}
	rescue := CrewRescue(dir, crewroute.Bugfix, crewroute.Worker, "somelab/the-chat-model")
	if len(rescue) == 0 || rescue[0].Kind != crewroute.Free {
		t.Fatalf("the rescue is %+v, want the free pool", rescue)
	}

	history = append(history, router.CrewRouteOutcome{At: time.Now(), Seat: "worker", Send: rescue[0].Send, Provider: w.Provider, Kind: "quota"})
	for _, r := range CrewRescue(dir, crewroute.Bugfix, crewroute.Worker, "somelab/the-chat-model") {
		if r.Send == rescue[0].Send || r.Model == "somelab/the-chat-model" {
			t.Fatalf("the rescue offers %+v: a pool at its limit, or the chat model on the account out of credit", r)
		}
	}
	if got := crewHealthAt(dir).crewAction(); got != "add credit on openrouter to continue" {
		t.Errorf("the one action is %q", got)
	}

	history = append(history, router.CrewRouteOutcome{At: time.Now(), Seat: "worker", Send: w.Send, Provider: w.Provider, Paid: true})
	for _, r := range CrewRescue(dir, crewroute.Bugfix, crewroute.Worker, "somelab/the-chat-model") {
		if r.Kind == crewroute.Free {
			t.Fatalf("with credit back the rescue still reaches for a free pool: %+v", r)
		}
	}
}

// AN AUXILIARY CALL NEVER ASKS A ROUTE THAT WILL NOT ANSWER: a helper that
// would call a quarantined route, or a paid route on an account out of
// credit, is handed the router's healthy pick instead.
func TestAnAuxiliaryCallIsHandedAHealthyRoute(t *testing.T) {
	dir := crewProfile(t)
	history := []router.CrewRouteOutcome{{At: time.Now(), Send: "z-ai/glm-5.3-flash", Provider: "openrouter", Kind: "forbidden"}}
	withRouteHistory(t, &history)
	crewHealthCache.mu.Lock()
	crewHealthCache.dir = ""
	crewHealthCache.mu.Unlock()
	if got := CrewHealthySend(dir, "z-ai/glm-5.3-flash", "somelab/chat"); got == "z-ai/glm-5.3-flash" || got == "" {
		t.Errorf("a helper on the quarantined route is sent to %q", got)
	}
	if got := CrewHealthySend(dir, "moonshotai/kimi-k3", "somelab/chat"); got != "moonshotai/kimi-k3" {
		t.Errorf("a healthy route was moved to %q", got)
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

// THE FREE SWITCH DECIDES WHETHER A FREE POOL IS A ROUTE AT ALL. Off (the
// default), no candidate, offer or routed seat carries a :free route and no
// :free id is offered; on, the pool is one more route of its paid model.
func TestTheFreeSwitchGovernsFreeRoutesEverywhere(t *testing.T) {
	dir := crewProfile(t)
	rows := CrewCatalog()
	rows = append(rows,
		catalog.Model{ID: "z-ai/glm-5.3-flash:free", OpenWeights: true, IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}},
		catalog.Model{ID: "thinkingmachines/inkling-small:free", ContextLength: 262144, CodingIndex: 60, Parameters: []string{"tools"}})
	CrewCatalog = func() []catalog.Model { return rows }
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	freeRoutes := func() (ids []string, routes int) {
		for _, c := range CrewCandidatesAt(dir) {
			for _, r := range c.Routes {
				if r.Kind == crewroute.Free {
					routes++
				}
			}
		}
		for _, o := range CrewOffersAt(dir) {
			if crewroute.IsFree(o.Model.ID) {
				ids = append(ids, o.Model.ID)
			}
			for _, r := range o.Routes {
				if r.Kind == crewroute.Free {
					routes++
				}
			}
		}
		return ids, routes
	}
	if ids, routes := freeRoutes(); len(ids) > 0 || routes > 0 {
		t.Fatalf("with free routes off: offered %v, %d free routes", ids, routes)
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	if err != nil {
		t.Fatal(err)
	}
	for _, pick := range d.Crew {
		if pick.Kind == crewroute.Free || strings.Contains(pick.Send, ":free") || strings.Contains(pick.Model, "inkling") {
			t.Fatalf("with free routes off the %s seat is %+v", pick.Seat, pick)
		}
	}
	if err := SetCrewFreeRoutes(dir, true); err != nil {
		t.Fatal(err)
	}
	ids, routes := freeRoutes()
	if len(ids) > 0 || routes == 0 {
		t.Fatalf("with free routes on: offered ids %v (want none, a pool is a route), %d free routes (want some)", ids, routes)
	}
}

// A PICKER NEVER OFFERS A MODEL THAT CANNOT HOLD A CONVERSATION: speech,
// transcription and image models are no seat, whatever parameters they list.
func TestCrewOffersNoSpeechOrImageModels(t *testing.T) {
	dir := crewProfile(t)
	rows := CrewCatalog()
	rows = append(rows,
		catalog.Model{ID: "deepgram/aura-2", PromptPrice: 1e-9, CompletionPrice: 1e-9, ContextLength: 262144, InputModalities: []string{"text"}, OutputModalities: []string{"audio"}, Parameters: []string{"tools"}},
		catalog.Model{ID: "deepgram/nova-3", PromptPrice: 1e-9, CompletionPrice: 1e-9, ContextLength: 262144, InputModalities: []string{"audio"}, OutputModalities: []string{"text"}, Parameters: []string{"tools"}},
		catalog.Model{ID: "vendor/flux-image", PromptPrice: 1e-9, CompletionPrice: 1e-9, ContextLength: 262144, InputModalities: []string{"text"}, OutputModalities: []string{"image"}, Parameters: []string{"tools"}})
	CrewCatalog = func() []catalog.Model { return rows }
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	var deepseek bool
	for _, offer := range CrewOffersAt(dir) {
		switch offer.Model.ID {
		case "deepgram/aura-2", "deepgram/nova-3", "vendor/flux-image":
			t.Errorf("%s was offered for a seat", offer.Model.ID)
		case "deepseek/deepseek-v4-flash":
			deepseek = true
		}
	}
	if !deepseek {
		t.Error("a chat model with tools was not offered")
	}
}

// THE FREE RUNG RUNS WITH THE SWITCH OFF when every paid route is out of
// reach: a seat's rescue after a payment failure is a free pool, and the
// person's own model on the account that said no is not.
func TestTheRescueFallsToAFreePoolWhenNothingPaidIsReachable(t *testing.T) {
	dir := crewProfile(t)
	rows := CrewCatalog()
	rows = append(rows, catalog.Model{ID: "z-ai/glm-5.3-flash:free", OpenWeights: true,
		IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}})
	CrewCatalog = func() []catalog.Model { return rows }
	history := []router.CrewRouteOutcome{{At: time.Now(), Seat: "worker", Send: "z-ai/glm-5.3-flash", Provider: "openrouter", Kind: "payment"}}
	withRouteHistory(t, &history)
	if CrewFreeRoutesAt(dir) {
		t.Fatal("the switch should be off")
	}
	rescue := CrewRescue(dir, crewroute.Bugfix, crewroute.Worker, "moonshotai/kimi-k3")
	if len(rescue) == 0 || rescue[0].Kind != crewroute.Free {
		t.Fatalf("rescue %+v, want a free pool first", rescue)
	}
	for _, r := range rescue {
		if r.Model == "moonshotai/kimi-k3" {
			t.Errorf("the chat model rode the account out of credit: %+v", r)
		}
	}
}

// A FREE POOL CAN BE PINNED BY NAME: `:free` is a route, not a thinking level,
// and the pin runs on the pool.
func TestAFreePoolCanBePinnedByName(t *testing.T) {
	pin, auto, err := ParseCrewPin("thinkingmachines/inkling-small:free@openrouter")
	if err != nil || auto || pin.Model != "thinkingmachines/inkling-small:free" || pin.Provider != "openrouter" {
		t.Fatalf("parse: %+v %v %v", pin, auto, err)
	}
	if _, _, err := ParseCrewPin("vendor/model:loud"); err == nil {
		t.Error("an unknown suffix was taken")
	}
	dir := crewProfile(t)
	got := resolveCrewPin(CrewPin{Model: "z-ai/glm-5.3-flash:free"}, CrewProvidersAt(dir))
	if got.Kind != crewroute.Free || got.Send != "z-ai/glm-5.3-flash:free" {
		t.Errorf("a pinned free pool resolves to %+v", got)
	}
}

// A PIN SENDS THE ID THE PERSON WROTE when the catalog lists it, though a
// dated snapshot of the same model is listed first; an id the catalog does
// not list is sent as the variant it resolves to, and the line says which.
func TestAPinSendsExactlyTheIdWritten(t *testing.T) {
	dir := crewProfile(t)
	rows := append([]catalog.Model{{ID: "deepseek/deepseek-v4-flash-0731", OpenWeights: true, PromptPrice: 8e-8, CompletionPrice: 1.6e-7,
		IntelligenceIndex: 24.2, CodingIndex: 56.2, AgenticIndex: 22.2, ContextLength: 1048576, Parameters: []string{"tools"}}}, CrewCatalog()...)
	CrewCatalog = func() []catalog.Model { return rows }
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	if err := SetCrewPin(dir, crewroute.Checker, "deepseek/deepseek-v4-flash"); err != nil {
		t.Fatal(err)
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Seat(crewroute.Checker); got.Send != "deepseek/deepseek-v4-flash" || got.Model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("the pinned checker is %+v, want exactly the id written", got)
	}
	if strings.Contains(d.Line("", -1), "→") {
		t.Errorf("an exact pin reads as resolved: %q", d.Line("", -1))
	}

	if err := SetCrewPin(dir, crewroute.Checker, "z-ai/glm-5.3-flash-latest"); err != nil {
		t.Fatal(err)
	}
	d, err = RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Seat(crewroute.Checker); got.Send != "z-ai/glm-5.3-flash" {
		t.Fatalf("a pin the catalog lists only as a variant is %+v", got)
	}
	if line := d.Line("", -1); !strings.Contains(line, "glm-5.3-flash-latest → glm-5.3-flash") {
		t.Errorf("the resolved pin is not said: %q", line)
	}

	for _, c := range CrewCandidatesAt(dir) {
		if crewroute.Lineage(c.Model.ID) == "deepseek/deepseek-v4-flash" && c.Model.ID != "deepseek/deepseek-v4-flash" {
			t.Errorf("the candidate is read from the snapshot %s, not the model's own row", c.Model.ID)
		}
	}
}

// A MODEL IS PRICED FROM ITS OWN ROW, never from a route spelling of it the
// catalog listed first (`:floor` is the cheapest provider's price, not the
// model's list price).
func TestAModelIsPricedFromItsOwnRow(t *testing.T) {
	dir := crewProfile(t)
	rows := append([]catalog.Model{{ID: "moonshotai/kimi-k3:floor", OpenWeights: true, PromptPrice: 8.8e-7, CompletionPrice: 1.053e-5,
		IntelligenceIndex: 43.6, CodingIndex: 76.2, AgenticIndex: 50, ContextLength: 1048576, Parameters: []string{"tools"}}}, CrewCatalog()...)
	CrewCatalog = func() []catalog.Model { return rows }
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	for _, offer := range CrewOffersAt(dir) {
		if crewroute.Lineage(offer.Model.ID) == "moonshotai/kimi-k3" && (offer.Model.ID != "moonshotai/kimi-k3" || offer.Model.PromptPrice != 3e-6) {
			t.Errorf("kimi-k3 is offered as %s at %v/token in", offer.Model.ID, offer.Model.PromptPrice)
		}
	}
}

// A DECISION ROW EXPLAINS ITSELF: only candidates that can sit a seat are
// named, and each seat carries its best three with route, quality, cost and
// score.
func TestADecisionRowNamesOnlySeatableCandidatesAndItsTopThree(t *testing.T) {
	dir := crewProfile(t)
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	for _, name := range CrewCandidateNames(dir) {
		if name == "vendor/no-tools" {
			t.Errorf("a model that cannot sit a seat is named a candidate")
		}
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	top := crewTop(dir, d)
	worker := top["worker"]
	if len(worker) == 0 || len(worker) > 3 || worker[0].Model != d.Seat(crewroute.Worker).Model || worker[0].Route == "" {
		t.Errorf("the worker's top is %+v, want the pick first with its route", worker)
	}
}
