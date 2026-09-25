package config

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// crewProfile is a profile with an OpenRouter key and a small catalog, the
// ordinary state a crew is routed in. Every variable a seat or a key reads
// is cleared so the machine running the test cannot answer for it.
func crewProfile(t *testing.T) string {
	t.Helper()
	for _, name := range []string{APIKeyEnv, "OPENAI_API_KEY", ModelEnv, PlanModelEnv, CheckModelEnv, "CODEAF_BASE_URL",
		"DEEPSEEK_API_KEY", "ZHIPU_API_KEY", "MOONSHOT_API_KEY", "MINIMAX_API_KEY", "DASHSCOPE_API_KEY"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "sk-or-v1-crewtest-0123456789"); err != nil {
		t.Fatal(err)
	}
	previous := CrewCatalog
	t.Cleanup(func() { CrewCatalog = previous })
	rows := []catalog.Model{
		{ID: "z-ai/glm-5.3-flash", OpenWeights: true, PromptPrice: 1.5e-7, CompletionPrice: 5e-7, CacheReadPrice: 5e-8,
			IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}},
		{ID: "moonshotai/kimi-k3", OpenWeights: true, PromptPrice: 3e-6, CompletionPrice: 1.5e-5, CacheReadPrice: 3e-7,
			IntelligenceIndex: 43.6, CodingIndex: 76.2, AgenticIndex: 50, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "deepseek/deepseek-v4-flash", OpenWeights: true, PromptPrice: 8.246e-8, CompletionPrice: 1.6492e-7, CacheReadPrice: 1.6492e-8,
			IntelligenceIndex: 24.2, CodingIndex: 56.2, AgenticIndex: 22.2, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "anthropic/claude-opus-5", PromptPrice: 5e-6, CompletionPrice: 2.5e-5, CacheReadPrice: 5e-7,
			IntelligenceIndex: 50.8, CodingIndex: 78, AgenticIndex: 56.5, ContextLength: 1000000, Parameters: []string{"tools"}},
		{ID: "vendor/no-tools", PromptPrice: 1e-9, CompletionPrice: 1e-9, Parameters: []string{"temperature"}},
		{ID: "vendor/unpriced", PriceUnknown: true, Parameters: []string{"tools"}},
	}
	CrewCatalog = func() []catalog.Model { return rows }
	return dir
}

const (
	fixTask  = "fix: crash when the config file is empty\n\nTraceback (most recent call last):\nValueError: empty"
	openTask = "Add a --json flag to the status command so scripts can read it"
)

func TestAnUntouchedProfileRoutesEverySeat(t *testing.T) {
	dir := crewProfile(t)
	seats, err := ResolveSeats(dir, SeatFlags{}, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range []Seat{seats.Work, seats.Plan, seats.Check} {
		if seat.Source != SeatRouted || seat.Model != "z-ai/glm-5.3-flash" {
			t.Errorf("%s: %+v, want routed to glm-5.3-flash on a fix", seat.Role, seat)
		}
	}
	if seats.Crew == nil || seats.Crew.Class != crewroute.Bugfix {
		t.Fatalf("crew %+v, want a bugfix decision", seats.Crew)
	}
	seats, err = ResolveSeats(dir, SeatFlags{}, CrewAsk{Task: crewroute.Task{Text: openTask}})
	if err != nil {
		t.Fatal(err)
	}
	if seats.Check.Model != "moonshotai/kimi-k3" || seats.Work.Model != "z-ai/glm-5.3-flash" {
		t.Errorf("open-ended: worker %s, checker %s; want flash worker and a kimi checker", seats.Work.Model, seats.Check.Model)
	}
	for _, m := range []string{seats.Work.Model, seats.Plan.Model, seats.Check.Model} {
		if strings.Contains(m, "opus") || strings.Contains(m, "fable") {
			t.Errorf("a seat fell to %s", m)
		}
	}
}

// THE CHECK SEAT NEVER QUIETLY INHERITS THE PLANNER. A person who pinned the
// planner said something about planning; the checker is routed for the task.
func TestAPlanFlagDoesNotSeatTheChecker(t *testing.T) {
	dir := crewProfile(t)
	seats, err := ResolveSeats(dir, SeatFlags{PlanModel: "anthropic/claude-opus-5"}, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if seats.Plan.Source != SeatFlag || seats.Plan.Model != "anthropic/claude-opus-5" {
		t.Errorf("planner %+v, want the flag", seats.Plan)
	}
	if seats.Check.Source != SeatRouted || seats.Check.Model == "anthropic/claude-opus-5" {
		t.Errorf("checker %+v inherited the planner's flag", seats.Check)
	}
	if !seats.Crew.Seat(crewroute.Planner).Pinned {
		t.Error("the flag did not reach the router as a one-task pin")
	}
}

func TestTheLadderFlagThenEnvThenPinThenRouter(t *testing.T) {
	dir := crewProfile(t)
	if err := SetCrewPin(dir, crewroute.Checker, "moonshotai/kimi-k3"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ModelEnv, "deepseek/deepseek-v4-flash")
	seats, err := ResolveSeats(dir, SeatFlags{}, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if seats.Work.Source != SeatEnv || seats.Work.Rung() != ModelEnv {
		t.Errorf("worker %+v, want the variable", seats.Work)
	}
	if seats.Check.Source != SeatPinned || seats.Check.Model != "moonshotai/kimi-k3" {
		t.Errorf("checker %+v, want the profile pin", seats.Check)
	}
	if seats.Plan.Source != SeatRouted {
		t.Errorf("planner %+v, want routed", seats.Plan)
	}
	seats, _ = ResolveSeats(dir, SeatFlags{Model: "z-ai/glm-5.3-flash"}, CrewAsk{Task: crewroute.Task{Text: fixTask},
		Pins: map[crewroute.Seat]CrewPin{crewroute.Checker: {Model: "deepseek/deepseek-v4-flash"}}})
	if seats.Work.Source != SeatFlag {
		t.Errorf("a flag lost to the variable: %+v", seats.Work)
	}
	if seats.Check.Model != "deepseek/deepseek-v4-flash" || seats.Check.Source != SeatPinned {
		t.Errorf("a one-task --pin lost to the profile pin: %+v", seats.Check)
	}
	// And the one-task pin did not persist.
	if pin, _ := CrewPinAt(dir, crewroute.Checker); pin.Model != "moonshotai/kimi-k3" {
		t.Errorf("the profile pin moved to %q", pin.Model)
	}
}

func TestPinsRoundTripAndRefuseWhatTheRuleLeavesOut(t *testing.T) {
	dir := crewProfile(t)
	if err := SetCrewPin(dir, crewroute.Worker, "z-ai/glm-5.3-flash@openrouter"); err != nil {
		t.Fatal(err)
	}
	pin, ok := CrewPinAt(dir, crewroute.Worker)
	if !ok || pin.Model != "z-ai/glm-5.3-flash" || pin.Provider != "openrouter" {
		t.Fatalf("pin read back %+v, %v", pin, ok)
	}
	if got := mustRow(t, registry(t, dir), KeyTierWorkerModel).Value(); got != "z-ai/glm-5.3-flash@openrouter" {
		t.Errorf("the worker row reads %q", got)
	}
	if err := SetCrewAllowed(dir, "open"); err != nil {
		t.Fatal(err)
	}
	if err := SetCrewPin(dir, crewroute.Planner, "anthropic/claude-opus-5"); err == nil || !strings.Contains(err.Error(), "outside the models you allow") {
		t.Errorf("a closed model pinned under `open`: %v", err)
	}
	if err := SetCrewPin(dir, crewroute.Planner, "moonshotai/kimi-k3@fireworks"); err == nil || !strings.Contains(err.Error(), "not a connected provider") {
		t.Errorf("a pin on an unconnected provider: %v", err)
	}
	// A rule that would strand a pin is refused and names the pin.
	if err := SetCrewAllowed(dir, "≤0.1/0.2"); err == nil || !strings.Contains(err.Error(), "pinned to z-ai/glm-5.3-flash") {
		t.Errorf("a rule that strands the worker pin: %v", err)
	}
	if err := SetCrewPin(dir, crewroute.Worker, "auto"); err != nil {
		t.Fatal(err)
	}
	if _, ok := CrewPinAt(dir, crewroute.Worker); ok {
		t.Error("`auto` did not unpin the worker")
	}
	if got := mustRow(t, registry(t, dir), KeyTierWorkerModel).Value(); got != CrewAuto {
		t.Errorf("an unpinned worker row reads %q, want auto", got)
	}
}

func TestTheAllowedRuleNarrowsTheCandidates(t *testing.T) {
	dir := crewProfile(t)
	ids := func() []string { return crewroute.Names(CrewCandidatesAt(dir)) }
	all := ids()
	for _, want := range []string{"anthropic/claude-opus-5", "z-ai/glm-5.3-flash"} {
		if !slices.Contains(all, want) {
			t.Errorf("all: %v is missing %s", all, want)
		}
	}
	if slices.Contains(all, "vendor/unpriced") {
		t.Errorf("an unpriced row is a candidate: %v", all)
	}
	if err := ModifyCrewAllowed(dir, false, "anthropic"); err != nil {
		t.Fatal(err)
	}
	if got := ids(); slices.Contains(got, "anthropic/claude-opus-5") {
		t.Errorf("-anthropic left %v", got)
	}
	if got := CrewAllowedAt(dir).String(); got != "all -anthropic" {
		t.Errorf("rule reads %q", got)
	}
}

func TestADailyCapPacesAndThenStops(t *testing.T) {
	dir := crewProfile(t)
	if err := SetCrewCap(dir, "1"); err != nil {
		t.Fatal(err)
	}
	previous := CrewHistory
	t.Cleanup(func() { CrewHistory = previous })
	spent := 0.99
	CrewHistory = func(string) CrewDay { return CrewDay{SpentUSD: spent} }
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Seat(crewroute.Checker).Model == "moonshotai/kimi-k3" {
		t.Error("at 99% of the cap an open-ended task still bought the dear checker")
	}
	spent = 1.2
	if _, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: openTask}}); !errors.Is(err, ErrCrewAtCap) {
		t.Errorf("over the cap: %v, want ErrCrewAtCap", err)
	}
}

func TestALearnedOffsetStartsARedoneClassHigher(t *testing.T) {
	dir := crewProfile(t)
	previous := CrewHistory
	t.Cleanup(func() { CrewHistory = previous })
	CrewHistory = func(string) CrewDay {
		return CrewDay{Offsets: map[string]int{OffsetKey("repo", crewroute.Bugfix): 2}}
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}, Repo: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Seat(crewroute.Worker).Model != "moonshotai/kimi-k3" {
		t.Errorf("a fix class redone twice here starts on %s", d.Seat(crewroute.Worker).Model)
	}
	d, _ = RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}, Repo: "elsewhere"})
	if d.Seat(crewroute.Worker).Model != "z-ai/glm-5.3-flash" {
		t.Errorf("another repository inherited the offset: %s", d.Seat(crewroute.Worker).Model)
	}
}

// A CONNECTED PLAN IS PREFERRED: its marginal cost is nothing, and the send
// goes out through the plan's own prefix.
func TestAPlanRouteIsFreeAndCollidingIdsKeepTheirRouterSpelling(t *testing.T) {
	dir := crewProfile(t)
	if err := writeProfileValue(dir, keyModelSources, []PersistedSource{{ID: "z-ai", Written: "z-ai", Key: "zai-key-0123456789", Door: "coding-plan", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	var zai CrewProvider
	for _, p := range CrewProvidersAt(dir) {
		if p.ID == "z-ai" {
			zai = p
		}
	}
	if zai.Kind != crewroute.Plan {
		t.Fatalf("z-ai on its coding plan: %+v", zai)
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	w := d.Seat(crewroute.Worker)
	if w.Provider != "z-ai" || w.CostUSD != 0 || w.Send != "z-ai/glm-5.3-flash" {
		t.Errorf("worker %+v, want glm-5.3-flash on the coding plan", w)
	}
	// A z-ai model pinned to OpenRouter is spelled so the call goes there.
	if err := SetCrewPin(dir, crewroute.Planner, "z-ai/glm-5.3-flash@openrouter"); err != nil {
		t.Fatal(err)
	}
	d, _ = RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if p := d.Seat(crewroute.Planner); p.Send != "openrouter/z-ai/glm-5.3-flash" || p.Provider != "openrouter" {
		t.Errorf("planner pinned @openrouter: %+v", p)
	}
}

func TestMigratingARetiredCrew(t *testing.T) {
	dir := crewProfile(t)
	// A balanced preset applied in the `open` family: all five rows written,
	// the three seat rows exactly the preset's, and the family and pick rows.
	// The ids stay pins: no row says which hand wrote it.
	if err := writeProfileValues(dir, map[string]any{
		KeyTierReflexModel: "mistralai/mistral-nemo", KeyTierLowModel: "deepseek/deepseek-v4-flash-0731",
		KeyTierWorkerModel: "z-ai/glm-5.3-flash", KeyTierHighModel: "moonshotai/kimi-k3", KeyTierMastermindModel: "z-ai/glm-5.3",
		legacyKeyCrewSource: "open", legacyKeyCrewPick: "learn",
	}); err != nil {
		t.Fatal(err)
	}
	line, err := MigrateCrew(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "kept your pins: checker moonshotai/kimi-k3, planner z-ai/glm-5.3, worker z-ai/glm-5.3-flash") || !strings.Contains(line, "allowed models: open") {
		t.Errorf("notice %q", line)
	}
	if got := CrewAllowedAt(dir).String(); got != "open" {
		t.Errorf("the open family became %q", got)
	}
	for _, key := range []string{legacyKeyCrewSource, legacyKeyCrewPick} {
		if _, held := persistedValue(dir, key); held {
			t.Errorf("%s survived the migration", key)
		}
	}
	if pins := CrewPinsAt(dir); len(pins) != 3 {
		t.Errorf("the seat rows are pins %v, want all three kept", pins)
	}
	if got := TierModelAt(dir, ModelTierReflex); got != "mistralai/mistral-nemo" {
		t.Errorf("the reflex row, not a crew seat, moved to %q", got)
	}
	// Once: the second run has nothing to say.
	if again, _ := MigrateCrew(dir); again != "" {
		t.Errorf("a second migration said %q", again)
	}
}

// THE OWNER'S PROFILE: the three seats all hold the id an old preset shipped,
// beside written low and reflex rows. Nothing retired is on it, so nothing
// moves: all three stay pins, every run.
func TestAPresetsIdsAPersonWroteStayPinned(t *testing.T) {
	dir := crewProfile(t)
	if err := writeProfileValues(dir, map[string]any{
		KeyTierReflexModel: "mistralai/mistral-nemo", KeyTierLowModel: "z-ai/glm-5.3-flash",
		KeyTierWorkerModel: "z-ai/glm-5.3-flash", KeyTierHighModel: "z-ai/glm-5.3-flash", KeyTierMastermindModel: "z-ai/glm-5.3-flash",
	}); err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if line, err := MigrateCrew(dir); err != nil || line != "" {
			t.Fatalf("run %d migrated a profile with nothing retired: %q %v", run, line, err)
		}
	}
	for _, seat := range crewroute.Seats {
		if pin, ok := CrewPinAt(dir, seat); !ok || pin.Model != "z-ai/glm-5.3-flash" {
			t.Errorf("the %s is %+v, %v, want its pin kept", seat, pin, ok)
		}
	}
	for _, key := range []string{KeyTierWorkerModel, KeyTierHighModel, KeyTierMastermindModel, KeyTierLowModel, KeyTierReflexModel} {
		if _, held := persistedValue(dir, key); !held {
			t.Errorf("%s was deleted", key)
		}
	}
}

// A CHECKER PINNED ALONE BESIDE A RETIRED WORD IS KEPT, and the line says so;
// migrating again changes nothing.
func TestAPinnedCheckerSurvivesMigration(t *testing.T) {
	dir := crewProfile(t)
	if err := writeProfileValues(dir, map[string]any{KeyTierHighModel: "moonshotai/kimi-k3", legacyKeyCrew: "balanced"}); err != nil {
		t.Fatal(err)
	}
	line, err := MigrateCrew(dir)
	if err != nil || !strings.Contains(line, "kept your pins: checker moonshotai/kimi-k3") {
		t.Fatalf("notice %q %v", line, err)
	}
	if again, _ := MigrateCrew(dir); again != "" {
		t.Errorf("a second migration said %q", again)
	}
	if pin, ok := CrewPinAt(dir, crewroute.Checker); !ok || pin.Model != "moonshotai/kimi-k3" {
		t.Errorf("the checker is %+v, %v", pin, ok)
	}
}

func TestMigrationKeepsAHandWrittenIdAsAPin(t *testing.T) {
	dir := crewProfile(t)
	if err := writeProfileValues(dir, map[string]any{
		KeyTierHighModel: "anthropic/claude-opus-5", KeyTierWorkerModel: "auto", legacyKeyCrew: "frugal",
	}); err != nil {
		t.Fatal(err)
	}
	line, err := MigrateCrew(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "kept your pins: checker anthropic/claude-opus-5") {
		t.Errorf("notice %q", line)
	}
	if pin, ok := CrewPinAt(dir, crewroute.Checker); !ok || pin.Model != "anthropic/claude-opus-5" {
		t.Errorf("the hand-written checker is %+v, %v", pin, ok)
	}
	if _, ok := CrewPinAt(dir, crewroute.Worker); ok {
		t.Error("an `auto` row became a pin")
	}
	if _, held := persistedValue(dir, legacyKeyCrew); held {
		t.Error("the preset word survived")
	}
}

func TestParseCrewPin(t *testing.T) {
	cases := []struct {
		raw   string
		pin   CrewPin
		auto  bool
		fails bool
	}{
		{"", CrewPin{}, true, false},
		{"auto", CrewPin{}, true, false},
		{"moonshotai/kimi-k3", CrewPin{Model: "moonshotai/kimi-k3"}, false, false},
		{"moonshotai/kimi-k3:high@OpenRouter", CrewPin{Model: "moonshotai/kimi-k3:high", Provider: "openrouter"}, false, false},
		{"moonshotai/kimi-k3@", CrewPin{}, false, true},
		{"moonshotai/kimi-k3:hgih", CrewPin{}, false, true},
	}
	for _, tc := range cases {
		pin, auto, err := ParseCrewPin(tc.raw)
		if (err != nil) != tc.fails || auto != tc.auto || (!tc.fails && pin != tc.pin) {
			t.Errorf("ParseCrewPin(%q) = %+v, %v, %v", tc.raw, pin, auto, err)
		}
	}
}

// THE PICKER'S OFFERS ARE EVERY REACHABLE MODEL, and the rule only marks them:
// a model the rule leaves out is still offered, as not allowed.
func TestCrewOffersMarkWhatTheRuleLeavesOut(t *testing.T) {
	dir := crewProfile(t)
	if err := SetCrewAllowed(dir, "open"); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, offer := range CrewOffersAt(dir) {
		if len(offer.Routes) == 0 {
			t.Errorf("%s was offered with no route", offer.Model.ID)
		}
		allowed[offer.Model.ID] = offer.Allowed
	}
	if got, ok := allowed["anthropic/claude-opus-5"]; !ok || got {
		t.Fatalf("a closed model under `open` reads offered=%v allowed=%v", ok, got)
	}
	if !allowed["moonshotai/kimi-k3"] {
		t.Fatal("an open model under `open` is not allowed")
	}
	if _, ok := allowed["vendor/unpriced"]; ok {
		t.Fatal("an unpriced row was offered")
	}
}

// UNDO PUTS THE ROWS BACK, absent ones included, in one write.
func TestCrewStateRestoresTheRows(t *testing.T) {
	dir := crewProfile(t)
	before := CrewStateAt(dir)
	if err := SetCrewPin(dir, crewroute.Checker, "moonshotai/kimi-k3"); err != nil {
		t.Fatal(err)
	}
	if err := SetCrewCap(dir, "5"); err != nil {
		t.Fatal(err)
	}
	if err := SetCrewAllowedRule(dir, crewroute.Allowed{Base: crewroute.BaseOpen}); err != nil {
		t.Fatal(err)
	}
	if err := RestoreCrewState(dir, before); err != nil {
		t.Fatal(err)
	}
	if _, ok := CrewPinAt(dir, crewroute.Checker); ok {
		t.Fatal("the restored profile still pins the checker")
	}
	if CrewCapAt(dir) != 0 || CrewAllowedAt(dir).String() != "all" {
		t.Fatalf("restored cap %v rule %q", CrewCapAt(dir), CrewAllowedAt(dir).String())
	}
	if _, held := persistedValue(dir, KeyCrewCap); held {
		t.Fatal("an absent row came back as a written one")
	}
}

// A PROVIDER TURNED OFF IS A ROUTE TAKEN AWAY, in a row of its own: the router
// never picks through it, the offers mark a model only it reached as not
// allowed, the last provider on cannot be turned off, a pin naming it is
// refused, and the undo puts the row back with the others.
func TestCrewProvidersTurnedOff(t *testing.T) {
	dir := crewProfile(t)
	if err := writeProfileValue(dir, keyModelSources, []PersistedSource{{ID: "z-ai", Written: "z-ai", Key: "zai-key-0123456789", Door: "coding-plan", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	before := CrewStateAt(dir)
	for _, p := range CrewProvidersAt(dir) {
		if !p.On {
			t.Fatalf("a connection nobody turned off reads off: %+v", p)
		}
	}
	if err := SetCrewProviderOn(dir, "z-ai", false); err != nil {
		t.Fatal(err)
	}
	if got := CrewProvidersOffAt(dir); !got["z-ai"] || len(got) != 1 {
		t.Fatalf("the row reads %v", got)
	}
	if rule := CrewAllowedAt(dir).String(); rule != "all" {
		t.Fatalf("turning a provider off wrote the allowed rule: %q", rule)
	}
	for _, c := range CrewCandidatesAt(dir) {
		for _, r := range c.Routes {
			if r.Provider == "z-ai" {
				t.Fatalf("%s is still routed through a provider that is off", c.Model.ID)
			}
		}
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if w := d.Seat(crewroute.Worker); w.Provider != "openrouter" {
		t.Fatalf("the worker rode %s with z-ai off", w.Provider)
	}
	if err := SetCrewPin(dir, crewroute.Worker, "z-ai/glm-5.3-flash@z-ai"); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("a pin through a provider that is off: %v", err)
	}
	// THE LAST ONE ON STAYS ON.
	if err := SetCrewProviderOn(dir, "openrouter", false); !errors.Is(err, ErrCrewLastProvider) {
		t.Fatalf("turning off the last provider on: %v", err)
	}
	if err := SetCrewProviderOn(dir, "nobody", false); err == nil {
		t.Fatal("a provider that is not connected was turned off")
	}
	// ONLY OPENROUTER ON, AND ONLY IT SERVES: every offer is still served.
	for _, offer := range CrewOffersAt(dir) {
		if !offer.Served {
			t.Errorf("%s reads unserved while openrouter is on", offer.Model.ID)
		}
	}
	if err := RestoreCrewState(dir, before); err != nil {
		t.Fatal(err)
	}
	if _, held := persistedValue(dir, KeyCrewProvidersOff); held {
		t.Fatal("undo left the providers row written")
	}
	// TURNING THE LAST ONE BACK ON REMOVES THE ROW rather than writing an empty list.
	if err := SetCrewProviderOn(dir, "z-ai", false); err != nil {
		t.Fatal(err)
	}
	if err := SetCrewProviderOn(dir, "z-ai", true); err != nil {
		t.Fatal(err)
	}
	if _, held := persistedValue(dir, KeyCrewProvidersOff); held {
		t.Fatal("every provider on left the row written")
	}
}

// THE GUARD IS ON ROUTING, NOT ON COUNTING: a custom endpoint left on alone
// routes no seat, so turning OpenRouter off beside it is refused — unless
// every seat is pinned to a provider still on.
func TestCrewProvidersKeepOneThatRoutes(t *testing.T) {
	dir := crewProfile(t)
	custom := PrepareCustomSource(dir, "http://127.0.0.1:9001/v1", "my-vllm")
	custom.Key, custom.Order = "sk-my-vllm-0123456789", 1
	if err := WriteSources(dir, []PersistedSource{custom}); err != nil {
		t.Fatal(err)
	}
	if err := SetCrewProviderOn(dir, "openrouter", false); !errors.Is(err, ErrCrewNoRoutableProvider) {
		t.Fatalf("turning off the one provider that routes beside a custom endpoint: %v", err)
	}
	for _, seat := range crewroute.Seats {
		if err := SetCrewPin(dir, seat, "my-vllm/qwen-coder"); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetCrewProviderOn(dir, "openrouter", false); err != nil {
		t.Fatalf("every seat pinned to the custom endpoint, and still refused: %v", err)
	}
}
