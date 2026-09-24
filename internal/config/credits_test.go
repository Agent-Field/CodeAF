package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/modelsource"
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
	if SettingsGeneration() <= before || !CreditsLowAt(dir) || ChatDefaultAt(dir) != FreeChatModel || CrewAt(dir) != CrewFree {
		t.Fatalf("low record did not move chat and crew defaults: chat=%q crew=%q", ChatDefaultAt(dir), CrewAt(dir))
	}
	for tier, want := range freeCrewModels {
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
	if CreditsLowAt(dir) || ChatDefaultAt(dir) != DefaultModel || CrewAt(dir) != DefaultCrew || CreditsNeedRead(dir, "secret-key") {
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

func TestFreeCrewIsAReadingAndExplicitRowsWin(t *testing.T) {
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
	if err := ApplyCrew(dir, CrewMax); err != nil {
		t.Fatal(err)
	}
	if CrewAt(dir) != CrewMax {
		t.Fatalf("explicit crew became %q", CrewAt(dir))
	}
	if TierModelAt(dir, ModelTierWorker) == FreeChatModel {
		t.Fatal("a written seat was replaced")
	}
	if strings.Split(freeCrewModels[ModelTierHigh], "/")[0] == strings.Split(freeCrewModels[ModelTierWorker], "/")[0] {
		t.Fatal("careful and worker share a vendor")
	}
	for _, model := range freeCrewModels {
		if !strings.HasSuffix(model, ":free") {
			t.Fatalf("%q is not free", model)
		}
	}
}

func TestLowBalanceDoesNotReplaceClearedStoredOrComputedSeats(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(string) error
		tier  string
		want  string
	}{
		{"cleared work row", func(dir string) error { return writeProfileValue(dir, KeyTierWorkerModel, "") }, ModelTierWorker, ""},
		{"stored crew word", func(dir string) error { return writeProfileValue(dir, KeyCrew, CrewMax) }, ModelTierWorker, crewAllModels[CrewMax][ModelTierWorker]},
		{"catalog pick", func(dir string) error { return SetCrewPick(dir, CrewPickCatalog) }, ModelTierReflex, DefaultReflexModel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := WriteAPIKey(dir, "key"); err != nil {
				t.Fatal(err)
			}
			if err := WriteCreditsReading(dir, "key", credits.Reading{Known: true, Low: true}); err != nil {
				t.Fatal(err)
			}
			if err := tc.write(dir); err != nil {
				t.Fatal(err)
			}
			if got := TierModelAt(dir, tc.tier); got != tc.want {
				t.Fatalf("explicit seat became %q, want %q", got, tc.want)
			}
			if tc.name == "cleared work row" && ResolveSeats(dir, "", "").Work.Model != DefaultModel {
				t.Fatal("cleared headless work row became the free implicit default")
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
	if CreditsLowAt(dir) || ChatDefaultAt(dir) != DefaultModel || CrewAt(dir) != DefaultCrew {
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
