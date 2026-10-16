package config

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
)

// The fixtures below are UNIT DATA, not provider facts: the catalog rows are
// crewProfile's, and the second provider is a key written into a temporary
// profile. Nothing here reaches a network or a model.

// taskModelsWithAiand is crewProfile with a second connected provider that
// serves the same GLM through its own key, so one model has two rows.
func taskModelsWithAiand(t *testing.T) string {
	t.Helper()
	dir := crewProfile(t)
	if err := writeProfileValue(dir, keyModelSources, []PersistedSource{
		{ID: "aiand", Written: "aiand", Key: "sk-aiand-taskmodels-0123456789ab", Order: 1},
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func applyTaskModels(t *testing.T, dir string, change TaskModelsChange) {
	t.Helper()
	if err := ApplyTaskModels(context.Background(), dir, change); err != nil {
		t.Fatalf("apply %+v: %v", change, err)
	}
}

func modePtr(m crewroute.Mode) *crewroute.Mode { return &m }

// rawRows is the task-model rows' bytes, for "nothing else moved".
func rawRows(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for key, raw := range taskModelsSnapshot(dir) {
		out[key] = string(raw)
	}
	return out
}

// ONE MODEL FOR EVERY TASK IS A ROW READ AT ADMISSION. A decision already made
// is what its task runs; the next admission sees the change, and a reopened
// profile reads the same mode.
func TestOneModelModeIsReadAtAdmissionAndADecisionStaysFrozen(t *testing.T) {
	dir := crewProfile(t)
	running, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	if err != nil {
		t.Fatal(err)
	}
	if running.Mode != crewroute.ModeAuto {
		t.Fatalf("an untouched profile admits in %q, want auto", running.Mode)
	}
	frozen, _ := json.Marshal(running)
	applyTaskModels(t, dir, TaskModelsChange{Mode: modePtr(crewroute.ModeOne), One: &CrewPin{Model: "deepseek/deepseek-v4-flash", Provider: "openrouter"}})
	if got, _ := json.Marshal(running); string(got) != string(frozen) {
		t.Error("the running task's decision moved")
	}
	if TaskModelsModeAt(dir) != crewroute.ModeOne {
		t.Fatal("the mode did not persist")
	}
	next, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}, ChatModel: "anthropic/claude-opus-5"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Mode != crewroute.ModeOne {
		t.Errorf("the next task admitted in %q, want one", next.Mode)
	}
	for _, seat := range crewroute.Seats {
		pick := next.Seat(seat)
		if pick.Model != "deepseek/deepseek-v4-flash" || pick.Provider != "openrouter" {
			t.Errorf("%s ran %s on %s, want the one model on its route", seat, pick.Model, pick.Provider)
		}
	}
	// One-model mode never touches the live process's flag: --one-model is a
	// launch flag and this is a row.
	if TaskModelsModeAt(t.TempDir()) != crewroute.ModeAuto {
		t.Error("a fresh profile is not auto")
	}
}

// SWITCHING MODES KEEPS EVERY PIN AND EVERY TICK, byte for byte, for when the
// person comes back.
func TestAModeFlipKeepsPinsAndTicks(t *testing.T) {
	dir := taskModelsWithAiand(t)
	applyTaskModels(t, dir, TaskModelsChange{
		Pins:  map[crewroute.Seat]*CrewPin{crewroute.Checker: {Model: "deepseek/deepseek-v4-flash"}},
		Ticks: []TaskModelTick{{Model: "z-ai/glm-5.3-flash", Provider: "openrouter", Allowed: false}},
	})
	before := rawRows(t, dir)
	applyTaskModels(t, dir, TaskModelsChange{Mode: modePtr(crewroute.ModeOne), One: &CrewPin{Model: "z-ai/glm-5.3-flash", Provider: "aiand"}})
	applyTaskModels(t, dir, TaskModelsChange{Mode: modePtr(crewroute.ModeAuto)})
	after := rawRows(t, dir)
	for _, key := range []string{KeyCrewAllowed, KeyTierHighModel, KeyCrewRouteChecker} {
		if before[key] != after[key] {
			t.Errorf("%s moved across a mode flip: %q → %q", key, before[key], after[key])
		}
	}
	if one, ok := TaskOneModelAt(dir); !ok || one.String() != "z-ai/glm-5.3-flash@aiand" {
		t.Errorf("the one model is not kept under Auto: %+v %v", one, ok)
	}
}

// THE OLD CHECKS AND PLANNER ROWS ARE THE CHECKER AND PLANNER PINS — the same
// rows, a thinking level and a route included — and reading Task models
// rewrites none of them.
func TestOldChecksAndPlannerRowsReadAsPinsUnchanged(t *testing.T) {
	dir := crewProfile(t)
	if err := writeProfileValues(dir, map[string]any{
		KeyTierHighModel:       "deepseek/deepseek-v4-flash:high",
		KeyTierMastermindModel: "moonshotai/kimi-k3",
		KeyCrewRoutePlanner:    "moonshotai/kimi-k3@openrouter",
	}); err != nil {
		t.Fatal(err)
	}
	before := rawRows(t, dir)
	state, _, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if pin := state.Roles[crewroute.Checker].Pin; pin == nil || pin.String() != "deepseek/deepseek-v4-flash:high" {
		t.Errorf("checker pin %+v, want the Checks row as written", pin)
	}
	if pin := state.Roles[crewroute.Planner].Pin; pin == nil || pin.String() != "moonshotai/kimi-k3@openrouter" {
		t.Errorf("planner pin %+v, want the Planner row with its route", pin)
	}
	if state.Roles[crewroute.Worker].Pin != nil {
		t.Error("the worker reads pinned on a profile that never pinned it")
	}
	// An unrelated change elsewhere on the page leaves both rows alone.
	applyTaskModels(t, dir, TaskModelsChange{Ticks: []TaskModelTick{{Model: "anthropic/claude-opus-5", Provider: "openrouter", Allowed: false}}})
	after := rawRows(t, dir)
	for _, key := range []string{KeyTierHighModel, KeyTierMastermindModel, KeyCrewRoutePlanner} {
		if before[key] != after[key] {
			t.Errorf("%s was rewritten: %q → %q", key, before[key], after[key])
		}
	}
}

// A CHANGE WITH ONE BAD PART WRITES NOTHING: the good tick before it is not
// written either.
func TestAFailedChangeLeavesEveryRowAsItWas(t *testing.T) {
	dir := taskModelsWithAiand(t)
	before := rawRows(t, dir)
	err := ApplyTaskModels(context.Background(), dir, TaskModelsChange{
		Ticks: []TaskModelTick{{Model: "z-ai/glm-5.3-flash", Provider: "openrouter", Allowed: false}},
		Mode:  modePtr(crewroute.ModeAuto),
		Pins:  map[crewroute.Seat]*CrewPin{crewroute.Worker: {Model: "nobody/never-listed"}},
	})
	var refusal TaskModelsRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("a pin to a model nobody lists: %v, want a refusal", err)
	}
	if after := rawRows(t, dir); len(after) != len(before) {
		t.Fatal("rows changed")
	} else {
		for key := range before {
			if before[key] != after[key] {
				t.Errorf("%s moved on a refused change: %q → %q", key, before[key], after[key])
			}
		}
	}
	if err := ApplyTaskModels(context.Background(), dir, TaskModelsChange{Revision: "0000000000000000", Mode: modePtr(crewroute.ModeAuto)}); !errors.Is(err, ErrTaskModelsStale) {
		t.Errorf("a stale revision: %v, want ErrTaskModelsStale", err)
	}
}

// A WRITE THAT LANDS BETWEEN A CHANGE'S READ AND ITS WRITE IS NOT OVERWRITTEN.
func TestAWriteBetweenReadAndWriteIsNotClobbered(t *testing.T) {
	dir := crewProfile(t)
	snapshot := taskModelsSnapshot(dir)
	if err := SetCrewAllowed(dir, "open"); err != nil {
		t.Fatal(err)
	}
	err := writeProfileValuesExpecting(dir, snapshot, map[string]any{KeyCrewAllowed: "all -kimi-k3"})
	if !errors.Is(err, errProfileMoved) {
		t.Fatalf("a write over a moved row: %v", err)
	}
	if got := CrewAllowedAt(dir).String(); got != "open" {
		t.Errorf("the other write was clobbered: %q", got)
	}
}

// ONE MODEL, TWO PROVIDERS, TWO TICKS: unticking GLM on OpenRouter leaves it
// allowed — and routable — on ai&.
func TestATickTogglesOneProvidersRowOnly(t *testing.T) {
	dir := taskModelsWithAiand(t)
	applyTaskModels(t, dir, TaskModelsChange{Ticks: []TaskModelTick{{Model: "z-ai/glm-5.3-flash", Provider: "openrouter", Allowed: false}}})
	_, rows, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	onOpenRouter, _ := taskRowFor(rows, "z-ai/glm-5.3-flash", "openrouter")
	onAiand, ok := taskRowFor(rows, "z-ai/glm-5.3-flash", "aiand")
	if !ok || onOpenRouter.Allowed || !onAiand.Allowed {
		t.Fatalf("openrouter allowed=%v, aiand allowed=%v (listed %v)", onOpenRouter.Allowed, onAiand.Allowed, ok)
	}
	for _, c := range CrewCandidatesAt(dir) {
		if crewroute.Lineage(c.Model.ID) != "z-ai/glm-5.3-flash" {
			continue
		}
		for _, r := range c.Routes {
			if r.Provider == "openrouter" {
				t.Errorf("the unticked row is still a route: %+v", r)
			}
		}
	}
}

// UNTICKING A PINNED MODEL SUCCEEDS, THE PIN STAYS SAVED, and the role is
// routed with a typed warning on the decision.
func TestUntickingAPinnedModelKeepsThePinAndRoutesTheRole(t *testing.T) {
	dir := crewProfile(t)
	applyTaskModels(t, dir, TaskModelsChange{Pins: map[crewroute.Seat]*CrewPin{crewroute.Worker: {Model: "z-ai/glm-5.3-flash", Provider: "openrouter"}}})
	applyTaskModels(t, dir, TaskModelsChange{Ticks: []TaskModelTick{{Model: "z-ai/glm-5.3-flash", Provider: "openrouter", Allowed: false}}})
	if pin, ok := CrewPinAt(dir, crewroute.Worker); !ok || pin.Model != "z-ai/glm-5.3-flash" {
		t.Fatalf("the pin was dropped: %+v %v", pin, ok)
	}
	state, _, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Roles[crewroute.Worker].Warning; got != string(crewroute.PinNotAllowed) {
		t.Errorf("worker warning %q, want pin_not_allowed", got)
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	worker := d.Seat(crewroute.Worker)
	if worker.Pinned || worker.Model == "z-ai/glm-5.3-flash" {
		t.Errorf("the worker ran %+v, want Auto's pick off the set-aside pin", worker)
	}
	if len(d.Warnings) != 1 || d.Warnings[0].Seat != crewroute.Worker || d.Warnings[0].Kind != crewroute.PinNotAllowed {
		t.Errorf("decision warnings %+v, want the worker's pin_not_allowed", d.Warnings)
	}
	// A pin may not be SET to a model that is not allowed.
	err = ApplyTaskModels(context.Background(), dir, TaskModelsChange{Pins: map[crewroute.Seat]*CrewPin{crewroute.Planner: {Model: "z-ai/glm-5.3-flash", Provider: "openrouter"}}})
	var refusal TaskModelsRefusal
	if !errors.As(err, &refusal) {
		t.Errorf("pinning a disallowed model: %v, want a refusal", err)
	}
	// Allow it again: the pin is back in force.
	applyTaskModels(t, dir, TaskModelsChange{Ticks: []TaskModelTick{{Model: "z-ai/glm-5.3-flash", Provider: "openrouter", Allowed: true}}})
	d, err = RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Seat(crewroute.Worker).Pinned || len(d.Warnings) != 0 {
		t.Errorf("allowed again: worker %+v warnings %+v", d.Seat(crewroute.Worker), d.Warnings)
	}
}

// AN ELIGIBLE PIN IS NEVER CHANGED BY AUTO, whatever the task or the effort.
func TestAnEligiblePinIsNeverChanged(t *testing.T) {
	dir := crewProfile(t)
	applyTaskModels(t, dir, TaskModelsChange{Pins: map[crewroute.Seat]*CrewPin{crewroute.Checker: {Model: "deepseek/deepseek-v4-flash"}}})
	for _, effort := range []crewroute.Effort{crewroute.EffortKnee, crewroute.EffortBest, crewroute.EffortCheap} {
		for _, text := range []string{fixTask, openTask} {
			d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: text}, Effort: effort})
			if err != nil {
				t.Fatal(err)
			}
			if c := d.Seat(crewroute.Checker); !c.Pinned || c.Model != "deepseek/deepseek-v4-flash" {
				t.Errorf("effort %q: checker %+v, want the pin", effort, c)
			}
		}
	}
}

// A MODEL WITH NO PRICE IS LISTED, TICKED AND PINNABLE, AND AUTO NEVER PICKS IT.
func TestAnUnpricedModelIsPinnableButNeverAutoPicked(t *testing.T) {
	dir := crewProfile(t)
	widenCrewCatalog(t, catalog.Model{ID: "z-ai/glm-next", PriceUnknown: true, OpenWeights: true,
		IntelligenceIndex: 60, CodingIndex: 90, AgenticIndex: 70, ContextLength: 1048576, Parameters: []string{"tools"}})
	_, rows, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	row, ok := taskRowFor(rows, "z-ai/glm-next", "openrouter")
	if !ok {
		t.Fatal("the unpriced model is not listed")
	}
	if row.Price != nil || !row.Allowed || row.AutoEligible || row.Reason != crewroute.RejectNoPrice {
		t.Errorf("row %+v, want listed, allowed, no price, not Auto's", row)
	}
	for _, text := range []string{fixTask, openTask} {
		d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: text}, Effort: crewroute.EffortBest})
		if err != nil {
			t.Fatal(err)
		}
		for _, pick := range d.Crew {
			if pick.Model == "z-ai/glm-next" {
				t.Errorf("Auto picked the unpriced model for the %s", pick.Seat)
			}
		}
	}
	applyTaskModels(t, dir, TaskModelsChange{Pins: map[crewroute.Seat]*CrewPin{crewroute.Worker: {Model: "z-ai/glm-next"}}})
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	if err != nil {
		t.Fatal(err)
	}
	if w := d.Seat(crewroute.Worker); w.Model != "z-ai/glm-next" || !w.Pinned {
		t.Errorf("the pinned unpriced worker ran %+v", w)
	}
}

// OPENROUTER'S PRICE IS OPENROUTER'S: a direct provider's row carries only what
// its own listing published, and nothing when it published nothing.
func TestARowCarriesOnlyItsOwnProvidersPrice(t *testing.T) {
	dir := taskModelsWithAiand(t)
	_, rows, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	or, _ := taskRowFor(rows, "z-ai/glm-5.3-flash", "openrouter")
	if or.Price == nil || or.Price.InPerM < 0.149 || or.Price.InPerM > 0.151 || or.Price.OutPerM < 0.499 || or.Price.OutPerM > 0.501 {
		t.Errorf("openrouter's row price %+v, want its own 0.15 / 0.50", or.Price)
	}
	direct, ok := taskRowFor(rows, "z-ai/glm-5.3-flash", "aiand")
	if !ok || direct.Price != nil {
		t.Errorf("the direct row %+v carries a price nobody at ai& published", direct.Price)
	}
}

// AN EXPIRED PROVIDER KEEPS ITS ROWS AND THEIR TICKS, says why, and comes back
// by itself.
func TestARefusedKeyKeepsTheProvidersRowsTicked(t *testing.T) {
	dir := crewProfile(t)
	previous := CrewRouteHistory
	t.Cleanup(func() { CrewRouteHistory = previous })
	CrewRouteHistory = func(string) []router.CrewRouteOutcome {
		return []router.CrewRouteOutcome{{At: time.Now().Add(-time.Minute), Send: "z-ai/glm-5.3-flash", Provider: "openrouter", Kind: "auth"}}
	}
	state, rows, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Providers) == 0 || state.Providers[0].ID != "openrouter" || state.Providers[0].State != ProviderStateExpired || state.Providers[0].Reason != ProviderReasonKeyRefused {
		t.Fatalf("providers %+v, want openrouter expired with its key refused", state.Providers)
	}
	row, ok := taskRowFor(rows, "z-ai/glm-5.3-flash", "openrouter")
	if !ok || !row.Allowed || row.Available || row.Reason != crewroute.RejectProviderUnavailable {
		t.Errorf("row %+v (listed %v), want listed, still ticked, unavailable", row, ok)
	}
	if state.Providers[0].Models == 0 || state.Providers[0].Allowed != state.Providers[0].Models {
		t.Errorf("provider counts %+v, want every listed row still allowed", state.Providers[0])
	}
}

// A PROVIDER WITH NO KEY IS A HEADER WITH NO MODELS.
func TestAProviderWithNoKeyIsListedWithNoModels(t *testing.T) {
	dir := crewProfile(t)
	state, _, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range state.Providers {
		if p.ID == "moonshot" {
			found = true
			if p.State != ProviderStateNoKey || p.Models != 0 || p.Connected {
				t.Errorf("moonshot %+v, want no_key and no models", p)
			}
		}
	}
	if !found {
		t.Error("a provider this build can connect is missing")
	}
}

// NOTHING ALLOWED FOR A ROLE IS A TYPED REFUSAL NAMING THE ROLE AND THE REASONS
// — never a task started on the chat model or any model left out.
func TestNothingAllowedIsATypedRefusalNotARescue(t *testing.T) {
	dir := crewProfile(t)
	if err := SetCrewAllowed(dir, "anthropic/claude-opus-5 -claude-opus-5@openrouter"); err != nil {
		t.Fatal(err)
	}
	_, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}, ChatModel: "deepseek/deepseek-v4-flash"})
	var refusal *crewroute.NoEligibleError
	if !errors.As(err, &refusal) {
		t.Fatalf("err %v, want a NoEligibleError", err)
	}
	if refusal.Mode != crewroute.ModeAuto || refusal.Seat == "" || len(refusal.Rejected) == 0 {
		t.Fatalf("refusal %+v", refusal)
	}
	if first := refusal.Rejected[0]; first.Reason != crewroute.RejectNotAllowed {
		t.Errorf("first rejection %+v, want the not-allowed opus", first)
	}
	var unreachable ErrCrewUnreachable
	if !errors.As(err, &unreachable) {
		t.Error("the older ErrCrewUnreachable no longer matches")
	}
}

// AN UNREACHABLE ONE MODEL IS A REFUSAL, NEVER ANOTHER MODEL.
func TestAnUnreachableOneModelRefusesRatherThanFallsBack(t *testing.T) {
	dir := taskModelsWithAiand(t)
	applyTaskModels(t, dir, TaskModelsChange{Mode: modePtr(crewroute.ModeOne), One: &CrewPin{Model: "z-ai/glm-5.3-flash", Provider: "aiand"}})
	if err := writeProfileValue(dir, keyModelSources, []PersistedSource{}); err != nil {
		t.Fatal(err)
	}
	_, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}, ChatModel: "deepseek/deepseek-v4-flash"})
	var refusal *crewroute.NoEligibleError
	if !errors.As(err, &refusal) || refusal.Mode != crewroute.ModeOne || len(refusal.Rejected) != 1 || refusal.Rejected[0].Reason != crewroute.RejectProviderUnavailable {
		t.Fatalf("err %v (%+v), want a one-mode refusal naming the provider", err, refusal)
	}
}

// THE /crew PANEL SAYS A SET-ASIDE PIN BY NAME, so the terminal never hides a
// state the app shows.
func TestTheCrewPanelSaysASetAsidePin(t *testing.T) {
	dir := crewProfile(t)
	applyTaskModels(t, dir, TaskModelsChange{Pins: map[crewroute.Seat]*CrewPin{crewroute.Checker: {Model: "deepseek/deepseek-v4-flash"}}})
	if err := SetCrewAllowed(dir, "-deepseek-v4-flash"); err != nil {
		t.Fatal(err)
	}
	gaps := CrewGapsAt(dir)
	if len(gaps) == 0 || gaps[0].Seat != crewroute.Checker || gaps[0].Line != "checker pinned to deepseek-v4-flash, which the models you allow leave out · routed until you allow it or unpin it" {
		t.Errorf("gaps %+v", gaps)
	}
}

// A ONE MODEL KNOWN NOT TO ANSWER IS REFUSED WHEN IT IS CHOSEN, not accepted
// and then refused at every task.
func TestChoosingAKnownExpiredOneModelIsRefused(t *testing.T) {
	dir := crewProfile(t)
	previous := CrewRouteHistory
	t.Cleanup(func() { CrewRouteHistory = previous })
	CrewRouteHistory = func(string) []router.CrewRouteOutcome {
		return []router.CrewRouteOutcome{{At: time.Now().Add(-time.Minute), Send: "z-ai/glm-5.3-flash", Provider: "openrouter", Kind: "auth"}}
	}
	before := rawRows(t, dir)
	err := ApplyTaskModels(context.Background(), dir, TaskModelsChange{Mode: modePtr(crewroute.ModeOne), One: &CrewPin{Model: "z-ai/glm-5.3-flash", Provider: "openrouter"}})
	var refusal TaskModelsRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("an expired one model: %v, want a refusal", err)
	}
	for key, raw := range rawRows(t, dir) {
		if before[key] != raw {
			t.Errorf("%s moved on a refused choice", key)
		}
	}
}

// A DIRECT ROUTE WITH ITS OWN PUBLISHED PRICE IS AUTO'S TO WEIGH, AT THAT
// PRICE; the row shows that price and nobody else's.
func TestAKnownOwnPriceMakesADirectRouteEligibleAtThatPrice(t *testing.T) {
	dir := taskModelsWithAiand(t)
	seedOwnPrices(t, dir, "aiand", map[string][3]float64{"zai-org/glm-5.3-flash": {2e-7, 8e-7, 0}})
	_, rows, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := taskRowFor(rows, "z-ai/glm-5.3-flash", "aiand")
	if row.Price == nil || row.Price.InPerM < 0.199 || row.Price.InPerM > 0.201 || !row.AutoEligible || row.Reason != "" {
		t.Errorf("row %+v price %+v, want ai&'s own 0.20 and Auto-eligible", row, row.Price)
	}
	other, _ := taskRowFor(rows, "deepseek/deepseek-v4-flash", "aiand")
	if other.AutoEligible || other.Reason != crewroute.RejectNoPrice {
		t.Errorf("a model ai& published no price for: %+v", other)
	}
	for _, c := range CrewCandidatesAt(dir) {
		for _, r := range c.Routes {
			if r.Provider == "aiand" && (r.Price == nil || crewroute.Lineage(c.Model.ID) != "z-ai/glm-5.3-flash") {
				t.Errorf("an ai& route without its own price is a candidate: %s %+v", c.Model.ID, r)
			}
		}
	}
}

// A READ THE PROFILE MOVES UNDER IS STARTED AGAIN, AND ANSWERS WHOLE: the
// answer's revision and its contents agree, and its observation began after
// the write it reflects.
func TestAReadDuringAWriteNeverAnswersHalfOld(t *testing.T) {
	dir := crewProfile(t)
	clock := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	previousClock, previousHook := taskModelsClock, taskModelsMidRead
	t.Cleanup(func() { taskModelsClock, taskModelsMidRead = previousClock, previousHook })
	taskModelsClock = func() time.Time { clock = clock.Add(time.Nanosecond); return clock }
	wrote := false
	taskModelsMidRead = func() {
		if wrote {
			return
		}
		wrote = true
		if err := SetCrewPin(dir, crewroute.Checker, "deepseek/deepseek-v4-flash"); err != nil {
			t.Fatal(err)
		}
	}
	state, _, err := TaskModelsAt(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Revision != TaskModelsRevisionAt(dir) {
		t.Error("the answer's revision is not the profile's")
	}
	if pin := state.Roles[crewroute.Checker].Pin; pin == nil || pin.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("the answer carries the old checker: %+v", pin)
	}
	if !state.ObservedAt.Equal(time.Date(2026, 10, 15, 12, 0, 0, 2, time.UTC)) {
		t.Errorf("observed at %v, want the start of the try that saw the write", state.ObservedAt)
	}
	// A profile that never holds still is refused, never answered from a try.
	n := 0
	taskModelsMidRead = func() {
		n++
		_ = SetCrewAllowed(dir, []string{"all", "open"}[n%2])
	}
	if _, _, err := TaskModelsAt(context.Background(), dir); !errors.Is(err, ErrTaskModelsMoving) {
		t.Errorf("a read the profile never stops moving: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	taskModelsMidRead = nil
	if _, _, err := TaskModelsAt(cancelled, dir); !errors.Is(err, ErrTaskModelsRead) {
		t.Errorf("a cancelled read: %v", err)
	}
}

// A BUDGET IS NOT A TASK-MODELS CHANGE: the caps are Spending's rows.
func TestACapChangeDoesNotMoveTheRevision(t *testing.T) {
	dir := crewProfile(t)
	before := TaskModelsRevisionAt(dir)
	if err := SetCrewTaskCap(dir, "3"); err != nil {
		t.Fatal(err)
	}
	if err := SetCrewCap(dir, "20"); err != nil {
		t.Fatal(err)
	}
	if TaskModelsRevisionAt(dir) != before {
		t.Error("a cap moved the task models' revision")
	}
}
