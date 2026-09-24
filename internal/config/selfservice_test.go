package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// registryForGuards builds the whole sheet over a profile nobody has written,
// which is enough for every question about who may write a row: the guard is a
// fact about the row's declaration, never about its value.
func registryForGuards(t *testing.T) *Settings {
	t.Helper()
	return NewSettings(SettingsOptions{ProfileDir: filepath.Join(t.TempDir(), "profile")})
}

// The guard list is a list of KEYS, and a key that no longer names a row is a
// rail that stopped guarding anything the day somebody renamed it. Nothing else
// would notice: the lookup would simply miss, the row would become writable, and
// the only sign would be a model quietly turning something off.
func TestEveryGuardedKeyNamesARealSettingsRow(t *testing.T) {
	registry := registryForGuards(t)
	for key := range selfServiceGuards {
		if _, found := registry.Row(key); !found {
			t.Errorf("selfServiceGuards names %q, which is not a settings row — a guard on a key nobody declares guards nothing", key)
		}
	}
}

// The rows that exist to restrain the model, named one at a time. This test is
// the law written down: if a change makes any of these writable by the chat, it
// fails here rather than in somebody's config.json.
func TestTheRestraintRowsAreNotSelfService(t *testing.T) {
	restrained := []string{
		// What may run without asking.
		KeyToolApprovalMode, KeyToolApprovals, KeyBashApprovals,
		KeyGuardian, KeyConsentTimeout, KeyTaskAutoApprove,
		// What may be spent without asking.
		KeyDailyBudget, KeyPlanConsent, KeyPracticeBudget, KeySpendRail,
		KeyTaskRepairRounds, KeyWorkingSet, KeyContextReuse,
		// How hard the machine may be worked.
		KeyTaskParallel, KeyTaskMaxLoad, KeyTaskMinFreeMB, KeyBashBackgroundAfter,
		// What every team inherits: a manager is a model.
		KeyTeamsCapUSDDay, KeyTeamsSubSharePct, KeyTeamsDepthLimit, KeyTeamsQuestionsUp, KeyTeamsWake,
		// Whether the work is checked, and how it is signed.
		KeyTaskAudit, KeyAttribution,
		// The credentials.
		KeyExaKey, KeyFirecrawlKey, KeyJinaKey, KeyGoogleOAuthClient, KeyGoogleOAuthSecret, KeySlackOAuthClient,
	}
	registry := registryForGuards(t)
	for _, key := range restrained {
		row, found := registry.Row(key)
		if !found {
			t.Fatalf("no settings row %q", key)
		}
		if row.SelfService() {
			t.Errorf("%s is self-service, and it restrains the model", key)
		}
		refusal := row.SelfServiceRefusal()
		if refusal == "" {
			t.Errorf("%s refuses with nothing", key)
			continue
		}
		// The refusal has to be usable by both of its readers: the person
		// recognises the label, the model needs the key, and neither can act on
		// a sentence that does not name the door.
		for _, want := range []string{row.Label, key, "/settings"} {
			if !strings.Contains(refusal, want) {
				t.Errorf("%s refuses with %q, which does not name %q", key, refusal, want)
			}
		}
	}
}

// A credential row is guarded by its [Setting.Secret] flag rather than by being
// listed, so the next secret row somebody adds is guarded without anybody
// remembering this file exists.
func TestEverySecretRowIsGuardedByItsFlagAlone(t *testing.T) {
	registry := registryForGuards(t)
	secrets := 0
	for _, row := range registry.Rows() {
		if !row.Secret {
			continue
		}
		secrets++
		if row.SelfService() {
			t.Errorf("the secret row %s is self-service", row.Key)
		}
		if _, listed := selfServiceGuards[row.Key]; listed {
			t.Errorf("the secret row %s is listed in selfServiceGuards as well as flagged — one rule, not two", row.Key)
		}
	}
	if secrets == 0 {
		t.Fatal("no secret rows were found, so this test proved nothing")
	}
}

// And the other half of the law: a preference that restrains nobody is the
// model's to change, because a settings tool that refused everything would be a
// settings tool nobody could use.
func TestOrdinaryPreferencesAreSelfService(t *testing.T) {
	ordinary := []string{
		KeyTimestamps, KeyMouse, KeyMemoryEnabled,
		KeyTierLowModel, KeyTierHighModel, KeyModelRoles, KeyModelFallbacks,
		KeyTaskModel, KeyRouting, KeyVisionModel, KeyDocumentEngine,
		KeySearchProvider, KeyHistoryEnabled, KeyDraftPersist,
	}
	registry := registryForGuards(t)
	for _, key := range ordinary {
		row, found := registry.Row(key)
		if !found {
			t.Fatalf("no settings row %q", key)
		}
		if !row.SelfService() {
			t.Errorf("%s is guarded, and it restrains nothing", key)
		}
		if refusal := row.SelfServiceRefusal(); refusal != "" {
			t.Errorf("%s is self-service but still refuses with %q", key, refusal)
		}
	}
}

// Accepts is the writer's own sentence read forwards, and a row that cannot say
// what it takes sends a model guessing.
func TestEveryRowSaysWhatItAccepts(t *testing.T) {
	registry := registryForGuards(t)
	for _, row := range registry.Rows() {
		accepts := row.Accepts()
		if strings.TrimSpace(accepts) == "" {
			t.Errorf("%s says nothing about what it accepts", row.Key)
		}
		if row.Kind == SettingChoice && !strings.Contains(accepts, row.Choices[0]) {
			t.Errorf("%s is a choice row and %q does not list its choices", row.Key, accepts)
		}
	}
}

// A PIN FOR A WORD THAT IS NOT A ROLE IS DROPPED FROM A PROFILE THAT STILL
// HOLDS IT, and the rest of that person's row keeps working.
//
// THE MEASURED FAILURE THIS IS WRITTEN AGAINST. `compaction` was a role for
// months: it had a tier, a description, and a row in the settings panel, and
// nothing ever called it. Deleting it took the row off the panel and left the
// word in every config.json that had pinned it. The panel re-serialises the
// WHOLE `models.roles` string on any change, so the next time that person moved
// ANY OTHER pin the write came back `"compaction" is not a role` — and every
// role pin on that machine was unchangeable until somebody opened config.json by
// hand. A deletion with no reading for what it deleted is a deletion that breaks
// the people who used the thing.
//
// THE READING DOES NOT CARE WHY THE WORD IS DEAD. A name this build retired and
// a name somebody mistyped are the same thing on the way in — a pin nothing will
// consult — so there is no ledger of retired names to keep up to date, and the
// day nobody updates it is not a day this comes back.
func TestAPinForARetiredRoleIsDroppedAndTheRestOfTheRowStillMoves(t *testing.T) {
	profile := t.TempDir()
	// The string a profile written before the role was deleted actually holds.
	const before = "compaction:openai/gpt-5-mini, title:openai/gpt-5-mini, planner:deepseek/deepseek-v4-pro"

	pins, err := ParseModelRoles(before)
	if err != nil {
		t.Fatalf("a profile that pins a retired role could not be read at all: %v", err)
	}
	if _, still := pins["compaction"]; still {
		t.Error("the retired role came back as a live pin")
	}
	for _, want := range []string{"title", "planner"} {
		if pins[want] == "" {
			t.Errorf("the pin for %q was lost along with the retired one: %v", want, pins)
		}
	}

	// AND THE NEXT CHANGE TO ANY OTHER PIN GOES THROUGH — the whole failure.
	//
	// The row is seeded the way a profile written before the deletion holds it:
	// straight into the file, because the writer is exactly what would refuse it
	// today and the person whose machine this is never typed it today either.
	if err := writeText(profile, KeyModelRoles, before); err != nil {
		t.Fatalf("seeding the profile: %v", err)
	}
	if err := writeModelRoles(profile, before); err != nil {
		t.Fatalf("a row carrying a retired role was refused: %v", err)
	}
	// AND THE DEAD WORD IS NOT WRITTEN BACK, so the next read has nothing to
	// forgive and the row stops carrying a sentence about nothing.
	stored, ok := persistedString(profile, KeyModelRoles)
	if !ok {
		t.Fatal("the row was not stored at all")
	}
	if strings.Contains(stored, "compaction") {
		t.Errorf("the retired role was written back: %q", stored)
	}
	for _, want := range []string{"title:openai/gpt-5-mini", "planner:deepseek/deepseek-v4-pro"} {
		if !strings.Contains(stored, want) {
			t.Errorf("the row that was kept lost %q: %q", want, stored)
		}
	}
	// AND THE PERSON IS TOLD, in a sentence and not a log line.
	if note := RetiredPinNote(before); !strings.Contains(note, "compaction") {
		t.Errorf("the surface is handed %q, want a line naming the pin being ignored", note)
	}
}

// AND A WORD NOBODY EVER PINNED IS STILL REFUSED WHEN IT IS BEING ADDED, which
// is the other half of the same bargain: the refusal earns its keep at the one
// moment a person can act on it, and nowhere else.
func TestANewPinForAWordThatIsNotARoleIsStillRefused(t *testing.T) {
	profile := t.TempDir()
	if err := writeModelRoles(profile, "title:openai/gpt-5-mini"); err != nil {
		t.Fatalf("a real role name was refused: %v", err)
	}
	if err := writeModelRoles(profile, "title:openai/gpt-5-mini, harness_designer:some/model"); err == nil {
		t.Fatal("a pin against a word that is not a role was accepted as it was added")
	}
}

// AND THE SAME WORD IS REFUSED WHEN IT IS TYPED AND FORGIVEN WHEN IT IS FOUND,
// which is the whole asymmetry in one test. `compaction` sitting in a file since
// before the deletion is a sentence about a world that used to exist and the
// person cannot act on being told about it; `compaction` typed into the row just
// now is a person asking for something, who is owed an answer while they are
// still looking at the box.
//
// THE BUG THIS PINS: the check asked [parseModelRoles] whether the word was
// already stored, and that reader drops every retired word by design — so the
// stored half read as the typed half and a machine with an old pin could not
// save any change to any other pin. Nothing failed loudly; the row just would
// not move.
func TestATypedRetiredRoleIsRefusedEvenThoughAStoredOneIsForgiven(t *testing.T) {
	profile := t.TempDir()
	const stored = "compaction:openai/gpt-5-mini, title:openai/gpt-5-mini"
	if err := writeText(profile, KeyModelRoles, stored); err != nil {
		t.Fatalf("seeding the profile: %v", err)
	}

	// Found in the file: forgiven, and the rest of the row moves.
	if err := writeModelRoles(profile, stored); err != nil {
		t.Fatalf("the stored retired pin was refused: %v", err)
	}

	// Typed just now into a row that no longer has it: refused, by name.
	err := writeModelRoles(profile, "title:openai/gpt-5-mini, compaction:some/model")
	if err == nil {
		t.Fatal("a retired role typed into the row was accepted silently")
	}
	if !strings.Contains(err.Error(), "compaction") {
		t.Errorf("the refusal does not name the word the person typed: %v", err)
	}
}

// AND A ROLE WITH NO TIER IS STILL A ROLE. `imagegen` never calls
// roles.Register — a painter is chosen by a pin or not at all — so it is in the
// vocabulary and not in the registry, and a reader that asked the registry threw
// the pin away on the way in. This is the case that keeps generate_image on a
// belt somebody configured.
func TestAPinForARoleWithNoTierSurvivesTheRead(t *testing.T) {
	pins, err := ParseModelRoles("imagegen:paint/model, title:openai/gpt-5-mini")
	if err != nil {
		t.Fatalf("the row was unreadable: %v", err)
	}
	if pins["imagegen"] != "paint/model" {
		t.Fatalf("the painter pin read back as %q; a role with no tier is still a role", pins["imagegen"])
	}
	profile := t.TempDir()
	if err := writeModelRoles(profile, "imagegen:paint/model"); err != nil {
		t.Fatalf("pinning a painter was refused: %v", err)
	}
	if stored, _ := persistedString(profile, KeyModelRoles); !strings.Contains(stored, "imagegen") {
		t.Fatalf("the painter pin was not written back: %q", stored)
	}
}

// A ROLE NOBODY RECOGNISES FAILS SILENTLY FOREVER, which is why this row
// validates the name and the model slug beside it does not. A pin written
// against a misspelled role is stored, reads back exactly as typed, shows in
// the panel, and is consulted by nothing — the only evidence being work that
// keeps coming out on the wrong model. The settings pair made it likely: a
// person picks the role off the list printed above the row, a model guesses.
func TestARolePinRefusesAnUnknownRoleAndNamesTheRealOnes(t *testing.T) {
	profile := t.TempDir()
	if err := writeModelRoles(profile, "harness_designer:some/model"); err == nil {
		t.Fatal("a pin against a role that does not exist was accepted")
	} else {
		if !strings.Contains(err.Error(), "harness_designer") {
			t.Errorf("the refusal does not say back the word that was wrong: %v", err)
		}
		if !strings.Contains(err.Error(), "designer") || !strings.Contains(err.Error(), "planner") {
			t.Errorf("the refusal does not name the roles there are: %v", err)
		}
	}
	// And the real names land, including the two this feature was asked for.
	if err := writeModelRoles(profile, "designer:deepseek/deepseek-v4-pro, planner:deepseek/deepseek-v4-pro"); err != nil {
		t.Fatalf("the real role names were refused: %v", err)
	}
}
