package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/router"
)

func TestCreditsRecordChangesImplicitDefaultsWithoutSavingModels(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "secret-key"); err != nil {
		t.Fatal(err)
	}
	before := SettingsGeneration()
	if got := ChatDefaultAt(dir); got != DefaultModel {
		t.Fatalf("healthy chat default = %q", got)
	}
	if err := WriteCreditsReading(dir, "secret-key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	if SettingsGeneration() <= before || !CreditsLowAt(dir) || ChatDefaultAt(dir) != FreeChatModel || !crewHealthAt(dir).unaffordable[modelsource.DefaultID] {
		t.Fatalf("low record did not move chat default and crew health: chat=%q", ChatDefaultAt(dir))
	}
	for tier, want := range freeHelperModels {
		if got := TierModelAt(dir, tier); got != want {
			t.Errorf("%s = %q, want %q", tier, got, want)
		}
	}
	data, err := os.ReadFile(ProfilePath(dir, "credits.json"))
	if err != nil {
		t.Fatal(err)
	}
	// THE RECORD HOLDS FOUR FIELDS AND NO NUMBER. A substring search for a
	// dollar figure also matched the fractional seconds of read_at, so the
	// fields are named and every value's type is checked instead.
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for name, value := range fields {
		switch value.(type) {
		case bool, string:
		default:
			t.Fatalf("record field %q holds %v, which is neither a flag nor text: %s", name, value, data)
		}
	}
	if len(fields) != 4 || fields["low"] == nil || fields["known"] == nil || fields["key"] == nil || fields["read_at"] == nil ||
		strings.Contains(string(data), "secret-key") || len(CreditsKeyPrint("secret-key")) != 16 {
		t.Fatalf("record exposes a key or balance: %s", data)
	}
	if !CreditsNeedRead(dir, "different-key") || !CreditsNeedRead(dir, "secret-key") {
		t.Fatal("a low or changed key did not request refresh")
	}
	if err := WriteCreditsReading(dir, "secret-key", credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	if CreditsLowAt(dir) || ChatDefaultAt(dir) != DefaultModel || crewHealthAt(dir).unaffordable[modelsource.DefaultID] || CreditsNeedRead(dir, "secret-key") {
		t.Fatal("healthy re-read did not restore implicit defaults")
	}
}

func TestCannotPayKeepsTheWholeVendorSentence(t *testing.T) {
	said := strings.Repeat("more words ", 20) + "https://openrouter.ai/settings/credits"
	got := ConnectionOutcomeWord("OpenRouter", modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: said})
	if !strings.Contains(got, "https://openrouter.ai/settings/credits") {
		t.Fatalf("top-up link was cut: %q", got)
	}
	refused := ConnectionOutcomeWord("OpenRouter", modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: said})
	if strings.Contains(refused, "https://openrouter.ai/settings/credits") {
		t.Fatalf("ordinary refusal was not bounded: %q", refused)
	}
}

func TestFreeHelpersAreAReadingAndExplicitRowsWin(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "key"); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreditsReading(dir, "key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	if err := WriteChatModel(dir, "openai/gpt-4"); err != nil {
		t.Fatal(err)
	}
	if ChatModelAt(dir) != "openai/gpt-4" {
		t.Fatal("saved talk model changed")
	}
	for _, model := range freeHelperModels {
		if !strings.HasSuffix(model, ":free") {
			t.Fatalf("%q is not free", model)
		}
	}
	if strings.Split(freeHelperModels[ModelTierReflex], "/")[0] == strings.Split(freeHelperModels[ModelTierLow], "/")[0] {
		t.Fatal("reflex and small work share a vendor")
	}
}

// A ROW SOMEBODY WROTE IS NEVER REPLACED by the low reading: a crew pin stays
// the pin, a written helper row stays theirs, a cleared helper row stays
// empty, and no crew seat is handed a model this build chose.
func TestLowBalanceDoesNotReplaceWrittenOrClearedRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		raw  string
		tier string
		want string
	}{
		{"pinned worker", KeyTierWorkerModel, "openai/gpt-4", ModelTierWorker, "openai/gpt-4"},
		{"written reflex row", KeyTierReflexModel, "vendor/reflex", ModelTierReflex, "vendor/reflex"},
		{"cleared small-work row", KeyTierLowModel, "", ModelTierLow, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := WriteAPIKey(dir, "key"); err != nil {
				t.Fatal(err)
			}
			if err := WriteCreditsReading(dir, "key", credits.Reading{Known: true, Low: true}); err != nil {
				t.Fatal(err)
			}
			if err := writeProfileValue(dir, tc.key, tc.raw); err != nil {
				t.Fatal(err)
			}
			if got := TierModelAt(dir, tc.tier); got != tc.want {
				t.Fatalf("explicit row became %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLowCreditsBelongOnlyToTheCurrentKey(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "key-A"); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreditsReading(dir, "key-A", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	if !CreditsLowAt(dir) {
		t.Fatal("key A's low reading was lost")
	}
	if err := WriteAPIKey(dir, "key-B"); err != nil {
		t.Fatal(err)
	}
	if CreditsLowAt(dir) || ChatDefaultAt(dir) != DefaultModel || crewHealthAt(dir).unaffordable[modelsource.DefaultID] {
		t.Fatal("key A's low record changed key B's defaults before B was read")
	}
	if !CreditsNeedRead(dir, APIKeyAt(dir)) {
		t.Fatal("key B was not scheduled for a read")
	}
}

func TestFreeModelNeedsASuffixOrAKnownZeroTariff(t *testing.T) {
	for _, tc := range []struct {
		id                          string
		known                       bool
		prompt, completion, request float64
		free                        bool
	}{
		{"~qwen/qwen3.8-27b:free:high", false, 0, 0, 0, true},
		{"vendor/zero", true, 0, 0, 0, true},
		{"vendor/unknown", false, 0, 0, 0, false},
		{"vendor/request-charge", true, 0, 0, 0.01, false},
		{"openai/gpt-4", true, 0.01, 0.02, 0, false},
	} {
		if got := IsFreeModel(tc.id, tc.known, tc.prompt, tc.completion, tc.request); got != tc.free {
			t.Errorf("IsFreeModel(%q) = %v, want %v", tc.id, got, tc.free)
		}
	}
}

// A BALANCE READ AS LOW PUTS THE CREW ON FREE ROUTES before any call: the
// three seats are routed to free pools, the decision says so once, and no
// paid OpenRouter route is tried first. A healthy reading routes as usual.
func TestALowBalanceRoutesTheCrewToFreePools(t *testing.T) {
	dir := crewProfile(t)
	rows := CrewCatalog()
	rows = append(rows, catalog.Model{ID: "z-ai/glm-5.3-flash:free", OpenWeights: true, IntelligenceIndex: 41.8, CodingIndex: 71.5,
		AgenticIndex: 50.9, ArenaElo: 1348, ContextLength: 1310720, Parameters: []string{"tools"}})
	CrewCatalog = func() []catalog.Model { return rows }
	var history []router.CrewRouteOutcome
	withRouteHistory(t, &history)
	if err := WriteCreditsReading(dir, APIKeyAt(dir), credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	d, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range crewroute.Seats {
		if pick := d.Seat(seat); !crewroute.IsFree(pick.Send) {
			t.Errorf("%s on a low balance sends %q, want a free pool", seat, pick.Send)
		}
	}
	if strings.Count(d.Note, "free routes in use (may log prompts)") != 1 {
		t.Errorf("the decision note is %q, want the free-routes notice once", d.Note)
	}
	if err := WriteCreditsReading(dir, APIKeyAt(dir), credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	d, err = RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if crewroute.IsFree(d.Seat(crewroute.Worker).Send) || strings.Contains(d.Note, "free routes") {
		t.Errorf("a healthy balance still routed free: %s · %q", d.Seat(crewroute.Worker).Send, d.Note)
	}
}
