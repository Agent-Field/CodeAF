package config

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE LADDER, PINNED RUNG BY RUNG. The bug it exists to prevent is not a wrong
// model — it is a rung that answers when a rung above it should have, which is
// invisible in a run's output and shows up as a benchmark measuring a crew it
// was not using (#166). So every rung is asserted for both seats, including the
// order between them.
func TestTheSeatLadderAnswersInItsOwnOrder(t *testing.T) {
	for _, test := range []struct {
		name string
		// crew is written with the same writer /crew uses, or left empty for a
		// profile nobody has touched.
		crew           string
		hand           map[string]string
		env, planEnv   string
		flag, planFlag string

		work, workRung string
		plan, planRung string
	}{
		{
			name: "a profile nobody has touched answers with the build's default",
			work: DefaultModel, workRung: "default",
			plan: "", planRung: "default",
		},
		{
			name: "the crew answers when it is the only thing said",
			crew: CrewFrugal,
			work: "deepseek/deepseek-v4-flash-0731", workRung: "crew frugal",
			plan: "z-ai/glm-5.3-flash:high", planRung: "crew frugal",
		},
		{
			name:    "the environment outranks the crew",
			crew:    CrewFrugal,
			env:     "vendor/from-the-environment",
			planEnv: "vendor/plans-from-the-environment",
			work:    "vendor/from-the-environment", workRung: ModelEnv,
			plan: "vendor/plans-from-the-environment", planRung: PlanModelEnv,
		},
		{
			name:     "the flag outranks the environment",
			crew:     CrewFrugal,
			env:      "vendor/from-the-environment",
			planEnv:  "vendor/plans-from-the-environment",
			flag:     "vendor/from-the-flag",
			planFlag: "vendor/plans-from-the-flag",
			work:     "vendor/from-the-flag", workRung: "--model",
			plan: "vendor/plans-from-the-flag", planRung: "--plan-model",
		},
		{
			// The two seats climb independently: one flag does not settle both.
			name:    "one seat may be flagged while the other still climbs",
			crew:    CrewFrugal,
			planEnv: "vendor/plans-from-the-environment",
			flag:    "vendor/from-the-flag",
			work:    "vendor/from-the-flag", workRung: "--model",
			plan: "vendor/plans-from-the-environment", planRung: PlanModelEnv,
		},
		{
			// The mastermind row is the one the presets differ in, and it is
			// the only one carrying a thinking level. THE LEVEL STAYS ON, the
			// way it stays on a flag: it is applied per call by the role ladder,
			// and the client seam takes it off the slug it sends
			// (Config.providerConfig).
			name: "the crew's thinking level travels with the value",
			crew: CrewMax,
			work: "z-ai/glm-5.3", workRung: "crew max",
			plan: "moonshotai/kimi-k3:high", planRung: "crew max",
		},
		{
			// And a flag carrying one is not shortened either, so the two rungs
			// hand the same kind of value to everything downstream.
			name:     "a flagged level travels the same way",
			planFlag: "moonshotai/kimi-k3:low",
			work:     DefaultModel, workRung: "default",
			plan: "moonshotai/kimi-k3:low", planRung: "--plan-model",
		},
		{
			// One row answered by hand is a custom crew, and the receipt says
			// so rather than naming a preset the four rows do not make.
			name: "a hand-written row reads as the custom crew it makes",
			crew: CrewFrugal,
			// THE WORK SEAT IS THE WORKER ROW, and not the small-work row beside
			// it: the same row a task handed off in conversation rides.
			hand: map[string]string{ModelTierWorker: "vendor/my-own-worker"},
			work: "vendor/my-own-worker", workRung: "crew custom",
			plan: "z-ai/glm-5.3-flash:high", planRung: "crew custom",
		},
		{
			// A row cleared on purpose means "follow the conversation", and a
			// headless run has no conversation to follow.
			name: "a cleared row falls through to the default",
			crew: CrewFrugal,
			hand: map[string]string{ModelTierWorker: "", ModelTierMastermind: ""},
			work: DefaultModel, workRung: "default",
			plan: "", planRung: "default",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.crew != "" {
				if err := ApplyCrew(dir, test.crew); err != nil {
					t.Fatalf("writing the %s crew: %v", test.crew, err)
				}
			}
			for tier, model := range test.hand {
				if err := writeTierModel(dir, tier, model); err != nil {
					t.Fatalf("writing the %s row: %v", tier, err)
				}
			}
			// Set at call time, both of them, so a case that names neither is
			// running against a shell that has.
			t.Setenv(ModelEnv, test.env)
			t.Setenv(PlanModelEnv, test.planEnv)

			seats := ResolveSeats(dir, test.flag, test.planFlag)
			if seats.Work.Model != test.work || seats.Work.Rung() != test.workRung {
				t.Errorf("work seat = %q (%s), want %q (%s)",
					seats.Work.Model, seats.Work.Rung(), test.work, test.workRung)
			}
			if seats.Plan.Model != test.plan || seats.Plan.Rung() != test.planRung {
				t.Errorf("plan seat = %q (%s), want %q (%s)",
					seats.Plan.Model, seats.Plan.Rung(), test.plan, test.planRung)
			}
		})
	}
}

// AN UNTOUCHED PROFILE IS NOT THE CREW ANSWERING, and this is the decision
// written down where it can be argued with.
//
// The four shipped tier defaults ARE the balanced row (crew.go), so [CrewAt]
// says `balanced` about a profile nobody has ever opened. Reading the seats
// through [TierModelAt] would therefore let the crew rung answer for a person
// who has never said anything about models — the bottom rung would become
// unreachable, and this build's headless default model would silently change
// from [DefaultModel] to the low tier's. So the ladder asks whether the key was
// WRITTEN, and `crew` on a receipt means somebody chose a crew.
func TestAnUntouchedProfileReadsTheDefaultAndNotTheBalancedCrew(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")

	if got := CrewAt(dir); got != DefaultCrew {
		t.Fatalf("an untouched profile reads crew %q, so this test is about the wrong thing", got)
	}
	seats := ResolveSeats(dir, "", "")
	if seats.Work.Source != SeatDefault || seats.Work.Model != DefaultModel {
		t.Fatalf("work seat = %q (%s), want the build default", seats.Work.Model, seats.Work.Rung())
	}
	if seats.Plan.Source != SeatDefault || seats.Plan.Model != "" {
		t.Fatalf("plan seat = %q (%s), want an empty seat that follows the work model",
			seats.Plan.Model, seats.Plan.Rung())
	}

	// And the same profile once somebody has actually said `balanced`: the same
	// four models, and now the crew is what answered.
	if err := ApplyCrew(dir, CrewBalanced); err != nil {
		t.Fatal(err)
	}
	seats = ResolveSeats(dir, "", "")
	if seats.Work.Source != SeatCrew || seats.Work.Rung() != "crew balanced" {
		t.Fatalf("after /crew balanced the work seat reads %s", seats.Work.Rung())
	}
}

// The receipt is the whole point of resolving the rung, so its sentence is
// pinned: both seats, each with what chose it, on one line.
func TestTheReceiptNamesBothSeatsAndTheirRungs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "vendor/plans")
	if err := ApplyCrew(dir, CrewFrugal); err != nil {
		t.Fatal(err)
	}
	line := ResolveSeats(dir, "", "").Line()
	frugal, _ := CrewModels(CrewFrugal)
	want := "models: work " + frugal[ModelTierWorker] + " (crew frugal) · plan vendor/plans (" + PlanModelEnv + ")"
	if line != want {
		t.Fatalf("the opening line reads\n\t%s\nwant\n\t%s", line, want)
	}

	// An unfilled plan seat is said in words rather than left as a gap.
	t.Setenv(PlanModelEnv, "")
	empty := ResolveSeats(t.TempDir(), "", "")
	if !strings.Contains(empty.Line(), "plan follows the work model (default)") {
		t.Fatalf("an empty plan seat reads %q", empty.Line())
	}
}

// THE LEVEL COMES OFF AT THE WIRE AND NOWHERE EARLIER.
//
// `moonshotai/kimi-k3:low` is how a tier row, a flag and a variable all say
// "that model, thinking a little". The level is applied per call by the role
// ladder, so the value travels whole; what a provider is asked for is the model
// alone, because the slug with a level on it is one no provider publishes. Sent
// whole it is a 404 on every call the seat makes — which is what the flag path
// did before this seam knew the difference.
func TestTheThinkingLevelNeverReachesTheProviderAsPartOfTheSlug(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		asked = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	settings := Config{APIKey: "k", BaseURL: server.URL, Timeout: DefaultTimeout, MaxTokens: 100}
	ask := func(client router.Client, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		asked = ""
		if _, err := client.CompleteWithMessages(context.Background(),
			[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hi"}}}}); err != nil {
			t.Fatal(err)
		}
		return asked
	}

	// The plan seat, however it was filled.
	if got := ask(settings.ClientFor("moonshotai/kimi-k3:low")); got != "moonshotai/kimi-k3" {
		t.Fatalf("the plan client asked for %q, want the model id alone", got)
	}
	// And the work seat, which reaches the same seam through Client().
	settings.Model = "moonshotai/kimi-k3:high"
	if got := ask(settings.Client()); got != "moonshotai/kimi-k3" {
		t.Fatalf("the work client asked for %q, want the model id alone", got)
	}
	// A suffix that is not a level is part of the id and is left alone.
	if got := ask(settings.ClientFor("vendor/model:free")); got != "vendor/model:free" {
		t.Fatalf("a non-level suffix was eaten: %q", got)
	}
	// The settings keep what the person wrote, so the role ladder still sees
	// the level it has to apply.
	if settings.Model != "moonshotai/kimi-k3:high" {
		t.Fatalf("building a client rewrote the seat to %q", settings.Model)
	}
}

// A PROFILE OLDER THAN A SEAT STILL GETS THE CREW IT CHOSE, AND IS TOLD SO.
//
// This is #302 written down. A crew applied before the worker row existed pinned
// the four rows there were, and the ladder read the fifth key, found nothing,
// and fell past the whole profile to the build's default — so every headless run
// worked on a model the person had never named while their planning ran on the
// one they had. The only tell was one word on a line nobody reads twice.
//
// The rung is the crew, through the row the worker row was split out of, and it
// says `inherited` rather than borrowing the plain crew rung: the person is owed
// the difference between the row they wrote and the row they did not.
func TestACrewWrittenBeforeTheWorkerSeatStillAnswersForItAndSaysSo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	// The pre-#278 crew shape, written the way it was written then: four rows
	// and no worker row.
	for tier, model := range map[string]string{
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash",
		ModelTierHigh:       "qwen/qwen3.8-27b",
		ModelTierMastermind: "deepseek/deepseek-v4-flash",
	} {
		if err := writeTierModel(dir, tier, model); err != nil {
			t.Fatalf("writing the %s row: %v", tier, err)
		}
	}
	if _, held := persistedString(dir, KeyTierWorkerModel); held {
		t.Fatal("the fixture holds a worker row, so it is not the profile this is about")
	}

	seats := ResolveSeats(dir, "", "")
	if seats.Work.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("the work seat runs on %q, want the small-work model the person pinned", seats.Work.Model)
	}
	if seats.Work.Model == DefaultModel {
		t.Errorf("the work seat fell to the build's default %q with a whole crew written above it", DefaultModel)
	}
	if seats.Work.Source != SeatInherited || seats.Work.From != ModelTierLow {
		t.Errorf("the work seat says %s from %q, want the crew through the small-work row",
			seats.Work.Source, seats.Work.From)
	}
	if got := seats.Work.Rung(); got != "crew custom, inherited" {
		t.Errorf("the receipt reads %q, want the crew and the fact that it was inherited", got)
	}
	// The seat the profile does hold a row for is untouched by any of this.
	if seats.Plan.Source != SeatCrew || seats.Plan.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("the plan seat reads %q (%s)", seats.Plan.Model, seats.Plan.Rung())
	}
}

// AND THE PLAIN-INSTALL PATH IS BYTE-IDENTICAL. A profile that has said nothing
// about models has nothing to inherit from, so the bottom rung answers and
// nothing is said about it — the third acceptance of #302, and the rung whose
// unreachability the ladder's own comment warns about.
func TestAProfileThatHasSaidNothingStillRunsTheBuildsDefaultAndSaysNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")

	seats := ResolveSeats(dir, "", "")
	if seats.Work.Source != SeatDefault || seats.Work.Model != DefaultModel {
		t.Fatalf("the work seat reads %q (%s), want the build's default", seats.Work.Model, seats.Work.Rung())
	}
	if notice := seats.Notice(); notice != "" {
		t.Fatalf("a profile with no crew was told about an inheritance that did not happen: %q", notice)
	}
	if seats.Report() != seats.Line() {
		t.Fatalf("the report grew a line: %q", seats.Report())
	}

	// One row written by hand, and the seat that has no row of its own inherits
	// it — the same profile, one key later.
	if err := writeTierModel(dir, ModelTierLow, "vendor/my-small-work"); err != nil {
		t.Fatal(err)
	}
	seats = ResolveSeats(dir, "", "")
	if seats.Work.Source != SeatInherited || seats.Work.Model != "vendor/my-small-work" {
		t.Fatalf("the work seat reads %q (%s), want the small-work row it inherits from",
			seats.Work.Model, seats.Work.Rung())
	}
	// A row cleared ON PURPOSE is an answer, and the answer is "follow the
	// conversation" — which a headless run cannot, so it falls to the default
	// rather than being reported as a crew that chose it.
	if err := writeTierModel(dir, ModelTierLow, ""); err != nil {
		t.Fatal(err)
	}
	if seats := ResolveSeats(dir, "", ""); seats.Work.Source != SeatDefault {
		t.Fatalf("a cleared small-work row was inherited anyway: %q (%s)",
			seats.Work.Model, seats.Work.Rung())
	}
}

// THE LINE IS SAID ONCE, AND ONLY WHEN IT IS TRUE.
//
// A substitution nobody is told about is the defect; a warning on every one of a
// run's calls is the same defect wearing a hat, because a person learns to read
// past it. So it belongs exactly where the seats are reported, once, and the
// register is the one the surface already speaks in — an observation, a middle
// dot, a promise, lowercase, no full stop, no machinery.
func TestTheInheritedSeatIsAnnouncedOnceInTheHousesOwnVoice(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	if err := writeTierModel(dir, ModelTierLow, "vendor/my-small-work"); err != nil {
		t.Fatal(err)
	}
	seats := ResolveSeats(dir, "", "")

	notice := seats.Notice()
	if notice == "" {
		t.Fatal("the seat was inherited and nothing was said about it")
	}
	report := seats.Report()
	if got := strings.Count(report, notice); got != 1 {
		t.Fatalf("the line is printed %d times in one report, want once:\n%s", got, report)
	}
	if !strings.HasPrefix(report, seats.Line()+"\n") {
		t.Fatalf("the report is not the models line and then the reason:\n%s", report)
	}
	if !strings.Contains(notice, "small work") {
		t.Errorf("the line does not name the row a person would go and look at: %q", notice)
	}
	if !strings.Contains(notice, "work seat") {
		t.Errorf("the line does not name the seat that was filled: %q", notice)
	}
	if notice != strings.ToLower(notice) {
		t.Errorf("the line is not lowercase: %q", notice)
	}
	if strings.HasSuffix(notice, ".") {
		t.Errorf("the line ends in a full stop, which makes a remark into an announcement: %q", notice)
	}
	if strings.Count(notice, " · ") != 1 {
		t.Errorf("the line is not the observation-then-promise the surface speaks in: %q", notice)
	}
	if strings.Contains(notice, "\n") {
		t.Errorf("the line is more than one line: %q", notice)
	}
	for _, machinery := range []string{"tier", "models.tiers", "fallback", "resolve", "seatinherited", "config"} {
		if strings.Contains(notice, machinery) {
			t.Errorf("the line says %q, which is machinery: %q", machinery, notice)
		}
	}

	// And a crew that pins every row hears nothing at all: the notice is a fact
	// about this profile, not decoration on the models line.
	pinned := t.TempDir()
	if err := ApplyCrew(pinned, CrewFrugal); err != nil {
		t.Fatal(err)
	}
	if notice := ResolveSeats(pinned, "", "").Notice(); notice != "" {
		t.Fatalf("a fully pinned crew was told about an inheritance: %q", notice)
	}
	// Nor does a seat somebody filled by hand this minute.
	t.Setenv(ModelEnv, "vendor/from-the-environment")
	if notice := ResolveSeats(dir, "", "").Notice(); notice != "" {
		t.Fatalf("a seat filled from the environment was called inherited: %q", notice)
	}
}

// EVERY TIER DECLARES ITS ANCESTRY, OR DECLARES THAT IT HAS NONE.
//
// The worker row will not be the last seat added, and the failure it caused is
// invisible from outside — so the rule lives in one table and this reads the
// table rather than the code that uses it. A tier word added to [ModelTiers]
// without a row here is the same defect again, one seat later, and it fails
// here instead of in somebody's benchmark.
func TestEveryTierDeclaresItsAncestry(t *testing.T) {
	known := make(map[string]bool, len(ModelTiers))
	for _, tier := range ModelTiers {
		known[tier] = true
	}
	for _, tier := range ModelTiers {
		row, ok := tierLineage[tier]
		if !ok {
			t.Errorf("the %s tier declares no ancestry — name the row it was split out of, "+
				"or an empty ancestor to say it was always here", tier)
			continue
		}
		if row.Words == "" {
			t.Errorf("the %s tier has no name a person would recognise, so the line about it "+
				"cannot send them to a row they can find", tier)
		}
		if row.Inherits != "" && !known[row.Inherits] {
			t.Errorf("the %s tier inherits from %q, which is not a tier", tier, row.Inherits)
		}
		// A ring of rows would make the walk depend on where it started.
		seen := map[string]bool{tier: true}
		for step := row.Inherits; step != ""; step = tierLineage[step].Inherits {
			if seen[step] {
				t.Fatalf("the lineage from %s comes back to %s", tier, step)
			}
			seen[step] = true
		}
	}
	for tier := range tierLineage {
		if !known[tier] {
			t.Errorf("the table holds %q, which is not one of this build's tiers", tier)
		}
	}
	// The one ancestry there is today, named rather than merely well-formed: the
	// worker seat came out of the small-work row (#278), and a change of mind
	// about that is a change of what old profiles run on.
	if got := tierLineage[ModelTierWorker].Inherits; got != ModelTierLow {
		t.Errorf("the worker row inherits from %q, want the small-work row it was split out of", got)
	}
}

// APPLYING A CREW PINS EVERY SEAT, so a profile written by this build never
// needs the lineage at all — the inheritance is for the profiles that already
// exist, and it must not become the ordinary path.
func TestApplyingACrewLeavesNoSeatToInherit(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	for _, preset := range CrewPresets {
		dir := t.TempDir()
		if err := ApplyCrew(dir, preset); err != nil {
			t.Fatal(err)
		}
		for _, tier := range ModelTiers {
			if _, held := persistedString(dir, tierKeyFor(tier)); !held {
				t.Errorf("%s leaves the %s row unwritten, so a seat on it inherits", preset, tier)
			}
		}
		if seats := ResolveSeats(dir, "", ""); seats.Work.Source != SeatCrew {
			t.Errorf("%s: the work seat reads %s", preset, seats.Work.Rung())
		}
	}
}

// THE SAME LADDER, READ THE WAY A CONVERSATION READS IT.
//
// #312 is #302 on the other surface. The chat's role map, the settings sheet and
// the crew word all resolve a tier row through [TierModelAt], which knew two
// answers — the row somebody wrote, and this build's choice — so a profile older
// than the worker seat handed every task started in a conversation to the build's
// default while the same profile handed `aforge do` the crew's own model. The
// rung is the same rung; what this table pins is that adding it moved NOTHING
// else: a row written, a row cleared and a profile with no rows at all read
// exactly what they read before, on every tier.
func TestATierRowIsSeatedTheWayAConversationReadsIt(t *testing.T) {
	const small = "vendor/my-small-work"
	for _, test := range []struct {
		name string
		// rows is the profile's config.json, written as a profile of that
		// vintage holds it: a key present with a value, a key present and empty,
		// or a key that is simply not there.
		rows   map[string]string
		tier   string
		model  string
		source SeatSource
		from   string
	}{
		{
			name:  "a key that was never held on a profile older than the seat takes the row it was split out of",
			rows:  map[string]string{KeyTierLowModel: small},
			tier:  ModelTierWorker,
			model: small, source: SeatInherited, from: ModelTierLow,
		},
		{
			name:  "a row somebody wrote is the crew answering and nothing climbs past it",
			rows:  map[string]string{KeyTierLowModel: small, KeyTierWorkerModel: "vendor/my-worker"},
			tier:  ModelTierWorker,
			model: "vendor/my-worker", source: SeatCrew,
		},
		{
			// The decision this whole rung is fenced against: emptying a row is
			// somebody saying "follow the conversation", and a conversation is
			// the one thing that CAN. It must not start inheriting.
			name:  "a row cleared on purpose still follows the conversation",
			rows:  map[string]string{KeyTierLowModel: small, KeyTierWorkerModel: ""},
			tier:  ModelTierWorker,
			model: "", source: SeatDefault,
		},
		{
			name:  "a profile that has said nothing still reads this build's choice",
			rows:  map[string]string{},
			tier:  ModelTierWorker,
			model: DefaultWorkerModel, source: SeatDefault,
		},
		{
			// An ancestor that was itself cleared is not an answer either: the
			// walk stops at the row that was written, and an empty one is not.
			name:  "an ancestor cleared on purpose is not inherited from",
			rows:  map[string]string{KeyTierLowModel: ""},
			tier:  ModelTierWorker,
			model: DefaultWorkerModel, source: SeatDefault,
		},
		{
			// And a tier with no ancestor is untouched by any of it.
			name:  "a row that was always here has nothing to inherit",
			rows:  map[string]string{KeyTierLowModel: small},
			tier:  ModelTierHigh,
			model: DefaultHighModel, source: SeatDefault,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := writeProfileRows(t, test.rows)
			seat := TierSeatAt(dir, test.tier)
			if seat.Model != test.model || seat.Source != test.source || seat.From != test.from {
				t.Errorf("the %s row seats %q (%s from %q), want %q (%s from %q)",
					test.tier, seat.Model, seat.Source, seat.From, test.model, test.source, test.from)
			}
			// AND THE MODEL EVERY OTHER CALLER READS IS THIS SEAT'S MODEL. The
			// sheet, the crew word and the conversation's role map all go
			// through TierModelAt, and a seat that disagreed with it would be a
			// receipt about a model nothing runs on.
			if got := TierModelAt(dir, test.tier); got != seat.Model {
				t.Errorf("TierModelAt reads %q while the seat reads %q", got, seat.Model)
			}
		})
	}
}

// THE LINE IS THE SAME LINE ON BOTH SURFACES, and the noun in it is the row a
// person can go and find. A second wording for one fact is how somebody
// concludes the two surfaces are doing two different things.
func TestAnInheritedTierSeatSaysTheSameSentenceTheRunSays(t *testing.T) {
	dir := writeProfileRows(t, map[string]string{KeyTierLowModel: "vendor/my-small-work"})
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")

	seat := TierSeatAt(dir, ModelTierWorker)
	if got, want := seat.Notice(), ResolveSeats(dir, "", "").Work.Notice(); got != want {
		t.Fatalf("the conversation reads\n\t%s\nand the run reads\n\t%s", got, want)
	}
	if got := seat.FromWords(); got != "small work" {
		t.Errorf("the row is called %q here and `small work` on the settings sheet", got)
	}
	// Every other shape says nothing at all, which is what makes the line worth
	// reading when it is there.
	for name, rows := range map[string]map[string]string{
		"a pinned row":  {KeyTierWorkerModel: "vendor/my-worker"},
		"a cleared row": {KeyTierLowModel: "vendor/my-small-work", KeyTierWorkerModel: ""},
		"no rows":       {},
	} {
		seat := TierSeatAt(writeProfileRows(t, rows), ModelTierWorker)
		if notice := seat.Notice(); notice != "" {
			t.Errorf("%s was told about an inheritance that did not happen: %q", name, notice)
		}
		if words := seat.FromWords(); words != "" {
			t.Errorf("%s names a row it did not inherit from: %q", name, words)
		}
	}
}

// ONE READER OF A TIER ROW, AND EVERY SURFACE BEHIND IT.
//
// The defect was not that the ladder was wrong — it was that the conversation
// had a second, shorter one. So this reads the sources and states the law twice:
// inside this package, only the file that owns the ladder turns a tier key into
// a MODEL (asking whether a key exists is a different question, and
// [CrewConfigured] asks it), and outside it nobody spells a tier key at all. A
// surface that wants a model asks [TierModelAt]; one that also wants the rung
// asks [TierSeatAt]; nobody opens the file for themselves.
func TestEveryReadOfATierRowGoesThroughTheLadder(t *testing.T) {
	root := moduleRoot(t)
	inside, err := filepath.Glob(filepath.Join(root, "internal", "config", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range inside {
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") || base == "seats.go" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "tierKeyFor(") || !strings.Contains(line, "persistedString(") {
				continue
			}
			// `_, ok :=` is the presence question and is nobody's second ladder.
			if !strings.Contains(line, "_, ok :=") && !strings.Contains(line, "_, held :=") {
				t.Errorf("%s reads the VALUE of a tier row for itself: %q\n\tthe ladder is seats.go's, and a "+
					"second reader is how `unset` comes to mean one thing here and another there",
					base, strings.TrimSpace(line))
			}
		}
	}
	// And outside this package, nothing spells a tier key at all.
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case entry.IsDir():
			if name := entry.Name(); name == ".git" || name == "bin" || name == "bench" {
				return filepath.SkipDir
			}
			return nil
		case !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		case strings.HasPrefix(path, filepath.Join(root, "internal", "config")):
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(raw), `"models.tiers.`) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s spells a tier key by hand instead of asking the ladder", rel)
		}
		return nil
	})
	// AND THE CONVERSATION'S OWN ROLE MAP IS ON IT. This is the wiring #312 was
	// about: the three rows the chat resolves from the profile go through this
	// package, which is what makes the worker row inherit there too.
	raw, err := os.ReadFile(filepath.Join(root, "cmd", "aforge", "chatv3.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tier := range []string{"ModelTierReflex", "ModelTierMastermind", "ModelTierWorker"} {
		if want := "config.TierModelAt(c.profileDir, config." + tier + ")"; !strings.Contains(string(raw), want) {
			t.Errorf("the chat door does not seat the %s row through the ladder (%s)", tier, want)
		}
	}
}

// writeProfileRows makes a throwaway profile holding exactly these keys — a
// value for a row somebody wrote, the empty string for a row they cleared, and
// nothing at all for a key of a vintage that never had one.
func writeProfileRows(t *testing.T, rows map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BudgetConfigPath(dir), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// moduleRoot walks up to the directory holding go.mod, for the two tests that
// read the tree they are part of.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// ── THE INHERITED-SEAT NOTICE ENDED IN A DOOR NOBODY COULD FIND ─────────────
//
// The line is printed on four HEADLESS doors — `aforge do`, `aforge plan run`,
// `aforge exec` and `aforge run` — and it ended `until you pick a crew again`.
// There is no way to pick a crew from a terminal: a crew is written by `/crew`
// and by the settings sheet's Providers row, both of which are the
// conversation, and no flag and no verb in the binary sets one. So the one line
// whose whole job is to explain a surprising model choice told somebody to do
// something and gave them nowhere to do it.
//
// The law on both surfaces is CAUSE PLUS WHAT TO DO. A cause plus a dead end is
// the defect, and the fix is the address, not the noun.
func TestTheInheritedSeatNoticeNamesADoorAPersonCanActuallyReach(t *testing.T) {
	dir := writeProfileRows(t, map[string]string{KeyTierLowModel: "vendor/my-small-work"})
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	notice := ResolveSeats(dir, "", "").Work.Notice()
	if notice == "" {
		t.Fatal("a profile older than the worker seat was told nothing at all")
	}
	if !strings.Contains(notice, CrewCommand) {
		t.Fatalf("the remedy names no door at all, so a person reading it in a terminal "+
			"has nowhere to go.\n  said: %q\n  want it to name %q", notice, CrewCommand)
	}
	// AND IT SAYS WHERE THAT DOOR IS. `/crew` alone is the next keystroke to
	// somebody already in the conversation and a riddle to somebody in a shell,
	// and the shell is where four of the five printers of this line stand.
	if !strings.Contains(notice, CrewCommand+" in the conversation") {
		t.Fatalf("the door is named without saying where it is.\n  said: %q", notice)
	}
	// THE REGISTER THE FUNCTION'S OWN COMMENT SETS OUT: an observation, a middle
	// dot, a promise, lowercase, no full stop.
	if !strings.Contains(notice, " · ") {
		t.Errorf("the observation and the promise are not separated by the middle dot: %q", notice)
	}
	if strings.HasSuffix(notice, ".") {
		t.Errorf("the line ends in a full stop, which nothing else in this register does: %q", notice)
	}
	if first := notice[:1]; first != strings.ToLower(first) {
		t.Errorf("the line opens upper-case: %q", notice)
	}
}

// AND `seat` IS THE PRODUCT'S OWN NOUN, NOT MACHINERY.
//
// It was proposed for renaming as machinery vocabulary. It is not: the crew and
// its seats are what the settings sheet, the model picker and the manual's own
// page all teach a person to say, and the vocabulary law bans the PROGRAM'S
// words for its own process — `auditor`, `verdict`, `refuted` — not a domain
// noun the product names things with. Renaming it here would have left every
// other surface, and the page that explains this exact line, speaking a word the
// binary no longer printed.
func TestTheNoticeKeepsTheProductsOwnWordForARowOfTheCrew(t *testing.T) {
	dir := writeProfileRows(t, map[string]string{KeyTierLowModel: "vendor/my-small-work"})
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	notice := ResolveSeats(dir, "", "").Work.Notice()
	for _, want := range []string{"crew", "seat"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("the notice stopped saying %q, and every other crew surface still does:\n%s",
				want, notice)
		}
	}
}
