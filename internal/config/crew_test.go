package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// THE CREW, from the three sides a person meets it: what a preset writes, what
// the row says about four values it did not write, and what the row refuses.

// THE SHIPPED DEFAULTS ARE A PRESET AND NOT FOUR OPINIONS. This is the identity
// the whole derived reading rests on: if the four defaults were not exactly one
// preset's row, a profile nobody had touched would read "custom" about values
// this build chose itself.
func TestTheShippedTierDefaultsAreExactlyTheBalancedCrew(t *testing.T) {
	balanced, ok := CrewModels(CrewBalanced)
	if !ok {
		t.Fatal("there is no balanced preset")
	}
	for _, c := range []struct{ tier, want string }{
		{ModelTierReflex, DefaultReflexModel},
		{ModelTierLow, DefaultLowModel},
		{ModelTierWorker, DefaultWorkerModel},
		{ModelTierHigh, DefaultHighModel},
		{ModelTierMastermind, DefaultMastermindModel},
	} {
		if balanced[c.tier] != c.want {
			t.Errorf("balanced sets %s to %q, and the shipped default is %q",
				c.tier, balanced[c.tier], c.want)
		}
	}
	if got := CrewAt(t.TempDir()); got != DefaultCrew {
		t.Fatalf("a profile nobody has touched reads the crew as %q, want %q", got, DefaultCrew)
	}
}

func TestCrewPresetsNameTheApprovedModels(t *testing.T) {
	want := map[string]map[string]string{
		CrewFrugal: {
			ModelTierReflex:     "mistralai/mistral-nemo",
			ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
			ModelTierWorker:     "deepseek/deepseek-v4-flash-0731",
			ModelTierHigh:       "z-ai/glm-5.3-flash",
			ModelTierMastermind: "z-ai/glm-5.3",
		},
		CrewBalanced: {
			ModelTierReflex:     "mistralai/mistral-nemo",
			ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
			ModelTierWorker:     "z-ai/glm-5.3-flash",
			ModelTierHigh:       "moonshotai/kimi-k3",
			ModelTierMastermind: "moonshotai/kimi-k3",
		},
		CrewMax: {
			ModelTierReflex:     "mistralai/mistral-nemo",
			ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
			ModelTierWorker:     "z-ai/glm-5.3",
			ModelTierHigh:       "moonshotai/kimi-k3",
			ModelTierMastermind: "moonshotai/kimi-k3",
		},
	}
	for preset, expected := range want {
		got, ok := CrewModels(preset)
		if !ok {
			t.Fatalf("there is no %s preset", preset)
		}
		for _, tier := range ModelTiers {
			if got[tier] != expected[tier] {
				t.Errorf("%s sets %s to %q, want %q", preset, tier, got[tier], expected[tier])
			}
		}
	}
}

// Every preset names every class, and every model in it is a whole slug. A
// preset with a gap in it would write a blank into a tier row, which means
// "follow the conversation" — the opposite of choosing a crew.
func TestEveryPresetNamesEveryClass(t *testing.T) {
	for _, preset := range CrewPresets {
		models, ok := CrewModels(preset)
		if !ok {
			t.Fatalf("%s is in CrewPresets and has no models", preset)
		}
		for _, tier := range ModelTiers {
			model := models[tier]
			if strings.TrimSpace(model) == "" {
				t.Errorf("%s leaves %s blank, which means follow the conversation", preset, tier)
			}
			if !strings.Contains(model, "/") {
				t.Errorf("%s sets %s to %q, which is not a provider-qualified id", preset, tier, model)
			}
			// A level is only ever legible on a value the gate accepts.
			if err := ValidateTierValue(model); err != nil {
				t.Errorf("%s sets %s to %q, which its own row would refuse: %v", preset, tier, model, err)
			}
		}
		if CrewLine(preset) == "" {
			t.Errorf("%s has no line to say about itself", preset)
		}
	}
	if CrewLine(CrewCustom) != "" {
		t.Error("custom has a line, and custom is a reading rather than a choice")
	}
	if _, ok := CrewModels(CrewCustom); ok {
		t.Error("custom names four models, and it is not a preset")
	}
}

// Setting a preset writes all five tier keys, and the row reads that preset back.
func TestSettingAPresetWritesAllFiveTiers(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)
	crew := mustRow(t, rows, KeyCrew)

	if err := crew.Apply(CrewMax); err != nil {
		t.Fatalf("setting the crew to max: %v", err)
	}
	want, _ := CrewModels(CrewMax)
	for _, tier := range ModelTiers {
		if got := TierModelAt(dir, tier); got != want[tier] {
			t.Errorf("after max, the %s tier reads %q, want %q", tier, got, want[tier])
		}
	}
	if got := CrewAt(dir); got != CrewMax {
		t.Fatalf("the crew reads %q after max was set", got)
	}
	// THE FIVE KEYS ARE ON DISK, all of them, in one file — a preset is not a
	// word stored beside four rows it claims to have written.
	values := map[string]json.RawMessage{}
	raw, err := os.ReadFile(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	for _, tier := range ModelTiers {
		if _, written := values[tierKeyFor(tier)]; !written {
			t.Errorf("%s is not in config.json after a preset was applied", tierKeyFor(tier))
		}
	}
	if _, written := values[KeyCrew]; written {
		t.Error("models.crew was stored — the reading is derived, and a stored word can become a lie")
	}

	if err := crew.Apply(CrewFrugal); err != nil {
		t.Fatalf("setting the crew to frugal: %v", err)
	}
	if got := CrewAt(dir); got != CrewFrugal {
		t.Fatalf("the crew reads %q after frugal was set", got)
	}
}

// ANSWERING ONE TIER ROW TURNS THE CREW TO CUSTOM, because that is what happened.
func TestAnsweringOneTierRowMakesTheCrewCustom(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)

	if err := mustRow(t, rows, KeyCrew).Apply(CrewBalanced); err != nil {
		t.Fatal(err)
	}
	if err := mustRow(t, rows, KeyTierMastermindModel).Apply("openai/gpt-5"); err != nil {
		t.Fatal(err)
	}
	if got := CrewAt(dir); got != CrewCustom {
		t.Fatalf("the crew reads %q over a hand-set mastermind, want %q", got, CrewCustom)
	}
	// And the row itself says so, which is the whole point: the summary cannot
	// disagree with the four rows it summarizes.
	if got := mustRow(t, registry(t, dir), KeyCrew).Value(); got != CrewCustom {
		t.Fatalf("the crew row reads %q", got)
	}

	// A tier CLEARED on purpose is custom too — "one of these follows the
	// conversation" is not any of the three presets.
	if err := mustRow(t, rows, KeyTierMastermindModel).Apply(""); err != nil {
		t.Fatal(err)
	}
	if got := CrewAt(dir); got != CrewCustom {
		t.Fatalf("the crew reads %q with a cleared mastermind, want %q", got, CrewCustom)
	}

	// Putting the preset back is one keystroke and heals all five.
	if err := mustRow(t, registry(t, dir), KeyCrew).Apply(CrewBalanced); err != nil {
		t.Fatal(err)
	}
	if got := CrewAt(dir); got != CrewBalanced {
		t.Fatalf("the crew reads %q after balanced was set again", got)
	}
}

// The row refuses a word that is not a preset, in the words every choice row
// refuses in — and it refuses "custom", which is a reading and not a choice.
func TestTheCrewRowRefusesAWordThatIsNotAPreset(t *testing.T) {
	dir := t.TempDir()
	crew := mustRow(t, registry(t, dir), KeyCrew)
	for _, word := range []string{"cheap", CrewCustom, ""} {
		err := crew.Apply(word)
		if err == nil {
			t.Fatalf("the crew row took %q", word)
		}
		if !strings.Contains(err.Error(), CrewBalanced) {
			t.Errorf("refusing %q does not name the choices: %v", word, err)
		}
	}
	if got := CrewAt(dir); got != DefaultCrew {
		t.Fatalf("a refused write changed the crew to %q", got)
	}
}

// ── the family the three words draw from ────────────────────────────────────

// THE CAREFUL SEAT IS ALWAYS A SECOND VENDOR, IN EVERY PRESET OF EVERY FAMILY.
// A check from the same vendor as the work it checks is a check that shares
// that vendor's blind spots, and the two tables are one law: a preset that
// moved to the all family and put the same vendor on worker and careful would
// break the property the open crew was built on while pretending to keep it.
func TestTheCarefulSeatIsAlwaysASecondVendorInEveryFamily(t *testing.T) {
	vendor := func(id string) string {
		if at := strings.Index(id, "/"); at > 0 {
			return id[:at]
		}
		return id
	}
	for _, family := range CrewSources {
		for _, preset := range CrewPresets {
			models, ok := CrewModelsForSource(family, preset)
			if !ok {
				t.Fatalf("there is no %s preset in the %s family", preset, family)
			}
			if vendor(models[ModelTierWorker]) == vendor(models[ModelTierHigh]) {
				t.Errorf("%s under %s has %q working and %q checking, and a check from "+
					"the same vendor catches what that vendor lets through",
					preset, family, models[ModelTierWorker], models[ModelTierHigh])
			}
		}
	}
}

// THE ALL FAMILY IS LOCKED, not derived: this is the page that says which ids
// the three words mean. A wrong id here is a person's bill, and a silent
// rewrite of the locked table would be a decision made by a diff.
func TestTheAllFamilyNamesTheLockedModels(t *testing.T) {
	want := map[string]map[string]string{
		CrewFrugal: {
			ModelTierReflex:     "google/gemini-2.5-flash",
			ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
			ModelTierWorker:     "openai/gpt-5.6-sol",
			ModelTierHigh:       "google/gemini-3.8-flash",
			ModelTierMastermind: "anthropic/claude-opus-5",
		},
		CrewBalanced: {
			ModelTierReflex:     "google/gemini-2.5-flash",
			ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
			ModelTierWorker:     "openai/gpt-5.6-sol",
			ModelTierHigh:       "anthropic/claude-opus-5",
			ModelTierMastermind: "anthropic/claude-fable-5.1",
		},
		CrewMax: {
			ModelTierReflex:     "google/gemini-2.5-flash",
			ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
			ModelTierWorker:     "anthropic/claude-fable-5.1",
			ModelTierHigh:       "openai/gpt-6-astra",
			ModelTierMastermind: "anthropic/claude-fable-5.1",
		},
	}
	for preset, expected := range want {
		got, ok := CrewModelsForSource(CrewSourceAll, preset)
		if !ok {
			t.Fatalf("there is no %s preset in the all family", preset)
		}
		for _, tier := range ModelTiers {
			if got[tier] != expected[tier] {
				t.Errorf("%s under all sets %s to %q, want %q", preset, tier, got[tier], expected[tier])
			}
			if !strings.Contains(got[tier], "/") {
				t.Errorf("%s under all sets %s to %q, which is not a provider-qualified id", preset, tier, got[tier])
			}
			if err := ValidateTierValue(got[tier]); err != nil {
				t.Errorf("%s under all sets %s to %q, which its own row would refuse: %v", preset, tier, got[tier], err)
			}
		}
	}
	// THE LINES ARE PART OF THE TABLE, and they are the words a person reads while
	// deciding to spend frontier money: a line naming a model the preset does not
	// pick is the contradiction the chooser exists to prevent.
	wantLines := map[string]map[string]string{
		CrewSourceOpen: {
			CrewFrugal:   "deepseek works, glm-5.3 thinks · pennies a day",
			CrewBalanced: "glm-flash works, kimi-k3 checks and thinks",
			CrewMax:      "glm-5.3 works, kimi-k3 thinks and checks",
		},
		CrewSourceAll: {
			CrewFrugal:   "gpt-5.6-sol works, gemini-flash checks, opus thinks",
			CrewBalanced: "gpt-5.6-sol works, opus checks, fable thinks",
			CrewMax:      "fable works and thinks, astra checks",
		},
	}
	for family, lines := range wantLines {
		for preset, line := range lines {
			if got := CrewLineFor(family, preset); got != line {
				t.Errorf("the %s family's %s line is %q, want %q", family, preset, got, line)
			}
		}
	}
}

// A SOURCE NOBODY SET IS THE OPEN FAMILY. The all table may exist, but a profile
// that never answered the row resolves the open family: the shipped defaults,
// the open presets under the same words, and a write that lands the open ids.
// This is the default-preservation law for the FAMILY; the open roster moved in
// the same change, which the change entry owns. An unknown word is held to the
// same law, silently, the way a retired choice reads everywhere else.
func TestASourceNobodyAnsweredReadsTheOpenFamily(t *testing.T) {
	dir := t.TempDir()
	if got := CrewSourceAt(dir); got != DefaultCrewSource {
		t.Fatalf("an untouched profile reads the source as %q, want %q", got, DefaultCrewSource)
	}
	if got := CrewAt(dir); got != DefaultCrew {
		t.Fatalf("an untouched profile reads the crew as %q, want %q", got, DefaultCrew)
	}
	if err := ApplyCrew(dir, CrewMax); err != nil {
		t.Fatal(err)
	}
	want, _ := CrewModels(CrewMax)
	for _, tier := range ModelTiers {
		if got := TierModelAt(dir, tier); got != want[tier] {
			t.Errorf("with no source set, max wrote %s to %q, want the open %q", tier, got, want[tier])
		}
	}
	// A WORD THIS BUILD DOES NOT KNOW READS AS OPEN, silently. Being told a
	// preference from an older build is now an error is the one thing a
	// retirement must never do, and a family is not a thing to guess either.
	if err := writeProfileValue(dir, KeyCrewSource, "wide"); err != nil {
		t.Fatal(err)
	}
	if got := CrewSourceAt(dir); got != CrewSourceOpen {
		t.Fatalf("an unknown source word reads as %q, want %q", got, CrewSourceOpen)
	}
	if got := CrewAt(dir); got != CrewMax {
		t.Fatalf("an unknown source word moved the crew to %q", got)
	}
}

// FLIPPING TO ALL MAKES THE SAME WORDS MEAN THE ALL FAMILY. Seats nobody pinned
// move at once, because an unwritten seat is the default crew resolved in the
// current family; the five ids already written are theirs, so a flip is a
// meaning change to them rather than a write, and they read custom against the
// new family until one keystroke re-applies them.
func TestFlippingToAllMakesTheWordsMeanTheAllFamily(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)
	if err := mustRow(t, rows, KeyCrewSource).Apply(CrewSourceAll); err != nil {
		t.Fatalf("setting the family to all: %v", err)
	}
	if got := CrewSourceAt(dir); got != CrewSourceAll {
		t.Fatalf("the source row reads %q after all was set", got)
	}
	// The crew row never writes the source: a stored tier id is not a family claim,
	// and the family is not stored in the crew row either.
	if err := mustRow(t, registry(t, dir), KeyCrew).Apply(CrewBalanced); err != nil {
		t.Fatalf("setting the crew to balanced: %v", err)
	}
	want, _ := CrewModelsForSource(CrewSourceAll, CrewBalanced)
	for _, tier := range ModelTiers {
		if got := TierModelAt(dir, tier); got != want[tier] {
			t.Errorf("under all, balanced wrote %s to %q, want the all-family %q", tier, got, want[tier])
		}
	}
	if got := CrewAt(dir); got != CrewBalanced {
		t.Fatalf("the crew reads %q after balanced was applied under all", got)
	}

	// And the same five ids are NOT balanced in the open family, which is the
	// point of the toggle: the words changed family, and the reading follows.
	if err := mustRow(t, registry(t, dir), KeyCrewSource).Apply(CrewSourceOpen); err != nil {
		t.Fatal(err)
	}
	if got := CrewAt(dir); got != CrewCustom {
		t.Fatalf("after flipping back to open, the all-family five read %q, want %q", got, CrewCustom)
	}
	// One keystroke heals it, and heals it open.
	if err := mustRow(t, registry(t, dir), KeyCrew).Apply(CrewBalanced); err != nil {
		t.Fatal(err)
	}
	if got := CrewAt(dir); got != CrewBalanced {
		t.Fatalf("the crew reads %q after balanced was applied under open again", got)
	}
	open, _ := CrewModels(CrewBalanced)
	for _, tier := range ModelTiers {
		if got := TierModelAt(dir, tier); got != open[tier] {
			t.Errorf("after re-applying under open, %s reads %q, want the open %q", tier, got, open[tier])
		}
	}
}

// ── the level on a tier value ───────────────────────────────────────────────

// A LEVEL IS ACCEPTED AND ANY OTHER SUFFIX IS REFUSED IN WORDS.
func TestTheTierRowsTakeALevelAndRefuseAMisspeltOne(t *testing.T) {
	dir := t.TempDir()
	row := mustRow(t, registry(t, dir), KeyTierMastermindModel)

	for _, value := range []string{
		"moonshotai/kimi-k3:low",
		"moonshotai/kimi-k3:medium",
		"moonshotai/kimi-k3:high",
		"deepseek/deepseek-v4-flash",
		"",
	} {
		if err := row.Apply(value); err != nil {
			t.Errorf("the mastermind row refused %q: %v", value, err)
		}
	}

	for _, value := range []string{
		// off is an effort word this build knows and a tier value may not carry
		// it: it is a different request shape, and some endpoints refuse it.
		"moonshotai/kimi-k3:off",
		"moonshotai/kimi-k3:max",
		"moonshotai/kimi-k3:none",
		"moonshotai/kimi-k3:xhigh",
		"moonshotai/kimi-k3:ultra",
		"moonshotai/kimi-k3:",
	} {
		err := row.Apply(value)
		if err == nil {
			t.Errorf("the mastermind row took %q", value)
			continue
		}
		for _, level := range roles.Efforts {
			if !strings.Contains(err.Error(), level) {
				t.Errorf("refusing %q does not name %q: %v", value, level, err)
			}
		}
	}

	// EVERY TIER ROW HAS THE SAME GATE. A sixth tier must not be able to arrive
	// with the check missing.
	for _, key := range []string{KeyTierReflexModel, KeyTierLowModel, KeyTierWorkerModel, KeyTierHighModel} {
		if err := mustRow(t, registry(t, dir), key).Apply("some/model:off"); err == nil {
			t.Errorf("%s took a level it cannot honour", key)
		}
	}
}

// A level written into a tier row reaches [roles.ResolveCall] as its own half,
// and never as part of the model id.
func TestALevelOnATierRowResolvesAsAnEffortAndNotAsAnId(t *testing.T) {
	dir := t.TempDir()
	if err := mustRow(t, registry(t, dir), KeyTierMastermindModel).Apply("moonshotai/kimi-k3:high"); err != nil {
		t.Fatal(err)
	}
	source := roles.Source(func(key string) (string, bool) {
		if key == roles.TierKey(roles.TierMastermind) {
			return TierModelAt(dir, ModelTierMastermind), true
		}
		return "", false
	})
	call, err := roles.ResolveCall(source, roles.RolePlanner, "vendor/conversation")
	if err != nil {
		t.Fatal(err)
	}
	if call.Model != "moonshotai/kimi-k3" || call.Effort != "high" {
		t.Fatalf("the planner resolved to %+v", call)
	}
}

// Every persisted write bumps the generation, which is the signal a live crew
// source invalidates its snapshot on (cmd/codeaf's v3RolesSource).
func TestEverySettingsWriteBumpsTheGeneration(t *testing.T) {
	dir := t.TempDir()
	before := SettingsGeneration()
	if err := mustRow(t, registry(t, dir), KeyCrew).Apply(CrewMax); err != nil {
		t.Fatal(err)
	}
	after := SettingsGeneration()
	if after <= before {
		t.Fatalf("the generation went %d → %d across a write", before, after)
	}
	// A refused write changes nothing, so it must not move the counter — a
	// reader that rebuilt on every rejected keystroke would be paying for
	// somebody's typing.
	_ = mustRow(t, registry(t, dir), KeyCrew).Apply("nonsense")
	if got := SettingsGeneration(); got != after {
		t.Fatalf("a refused write moved the generation to %d", got)
	}
}

// THE FAMILY ROW REFUSES A WORD THAT IS NOT A FAMILY, the way the crew row
// refuses a word that is not a preset, and a refused write leaves the row absent
// rather than half-written.
func TestSetCrewSourceRefusesAWordThatIsNotAFamily(t *testing.T) {
	dir := t.TempDir()
	if err := SetCrewSource(dir, "wide"); err == nil {
		t.Fatal("SetCrewSource accepted a word that is not a family")
	}
	if got := CrewSourceAt(dir); got != DefaultCrewSource {
		t.Fatalf("a refused write left the row reading %q", got)
	}
	if CrewConfigured(dir) {
		t.Fatal("a refused write marked the crew as answered")
	}
}

// AN UNWRITTEN SEAT FOLLOWS THE FAMILY, which is what makes the hint true: a
// profile on the all family with no rows runs the frontier crew without anyone
// picking it, and the crew word already reads the default preset.
func TestAnUnwrittenSeatFollowsTheFamily(t *testing.T) {
	dir := t.TempDir()
	if err := SetCrewSource(dir, CrewSourceAll); err != nil {
		t.Fatal(err)
	}
	want, ok := CrewModelsForSource(CrewSourceAll, DefaultCrew)
	if !ok {
		t.Fatal("there is no default preset in the all family")
	}
	for tier, model := range want {
		if got := TierModelAt(dir, tier); got != model {
			t.Fatalf("%s on an all profile with no row is %q, want the all default %q", tier, got, model)
		}
	}
	if got := CrewAt(dir); got != CrewBalanced {
		t.Fatalf("an all profile with no rows reads the crew as %q, want %q", got, CrewBalanced)
	}
}
