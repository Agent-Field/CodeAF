package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// /crew, FROM THE FOUR SIDES A PERSON MEETS IT: the chooser, applying one, a word
// that is not one of the three, and the reading over a crew somebody assembled
// themselves.

// The bare form is the three presets with yours marked, each naming the four
// models it would set, and opens on the current one.
func TestCrewOpensTheThreePresetsOnYours(t *testing.T) {
	a, _ := sheetApp(t)
	a.width = 240
	a.slash("/crew")

	if !a.crewPick.open {
		t.Fatal("bare /crew did not open the chooser")
	}
	if a.crewPick.cursor != 1 {
		t.Fatalf("the cursor opened on row %d, want balanced at 1", a.crewPick.cursor)
	}
	text := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	for _, preset := range config.CrewPresets {
		if !strings.Contains(text, preset) {
			t.Errorf("the listing does not mention %q:\n%s", preset, text)
		}
		if !strings.Contains(text, config.CrewLine(preset)) {
			t.Errorf("the listing does not say what %q is:\n%s", preset, text)
		}
		models, _ := config.CrewModels(preset)
		for _, tier := range roles.Tiers {
			if !strings.Contains(text, models[string(tier)]) {
				t.Errorf("the listing does not name %s's %s model:\n%s", preset, tier, text)
			}
		}
	}
	// THE SHIPPED CREW IS MARKED, and it is balanced — the four tier defaults are
	// that row (internal/config's crew.go).
	if !strings.Contains(text, "· "+config.CrewBalanced) {
		t.Errorf("the current preset is not marked:\n%s", text)
	}
	if strings.Contains(text, "· "+config.CrewFrugal) {
		t.Errorf("a preset nobody is on is marked:\n%s", text)
	}
	// The four classes are named in the words their own settings rows use, so
	// somebody told "mastermind" here can find the row there.
	for _, tier := range roles.Tiers {
		want := settingUI[tierSettingKey(tier)].label
		if !strings.Contains(text, want) {
			t.Errorf("the listing does not use the row's own word %q:\n%s", want, text)
		}
	}
}

func TestCrewChooserAppliesAndCloses(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	toProviders(t, a)
	a.slash("/crew")
	a.crewPickerKey(key("down"))
	a.crewPickerKey(key("enter"))

	if a.crewPick.open {
		t.Fatal("enter left the crew chooser open")
	}
	if got := config.CrewAt(dir); got != config.CrewMax {
		t.Fatalf("enter applied %q, want max", got)
	}
	if got := lastNote(t, a); !strings.Contains(got, "crew → max") {
		t.Fatalf("the chooser noted %q", got)
	}
	refreshed := false
	for _, row := range a.sheet.rows {
		if row.Key == config.KeyCrew && row.Value() == config.CrewMax {
			refreshed = true
		}
	}
	if !refreshed {
		t.Fatal("the open settings rows were not refreshed to max")
	}
}

func TestCrewChooserEscChangesNothing(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew")
	drive(t, a, key("down"))
	drive(t, a, key("esc"))

	if a.crewPick.open {
		t.Fatal("esc left the crew chooser open")
	}
	if got := config.CrewAt(dir); got != config.CrewBalanced {
		t.Fatalf("esc changed the crew to %q", got)
	}
}

// A preset writes all four classes and confirms in one line.
func TestCrewAppliesAPresetAndConfirmsInOneLine(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew max")

	want, _ := config.CrewModels(config.CrewMax)
	for _, tier := range config.ModelTiers {
		if got := config.TierModelAt(dir, tier); got != want[tier] {
			t.Errorf("after /crew max the %s class reads %q, want %q", tier, got, want[tier])
		}
	}
	text := lastNote(t, a)
	if strings.Count(text, "\n") != 0 {
		t.Fatalf("the confirmation is more than one line:\n%s", text)
	}
	for _, part := range []string{"crew → max", "brain kimi-k3:high", "hands deepseek-v4-pro", "checks kimi-k3"} {
		if !strings.Contains(text, part) {
			t.Errorf("the confirmation is missing %q: %q", part, text)
		}
	}
}

// AN UNKNOWN WORD CHANGES NOTHING AND SHOWS THE THREE.
func TestCrewRefusesAWordThatIsNotOneOfTheThree(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew cheap")

	if got := config.CrewAt(dir); got != config.DefaultCrew {
		t.Fatalf("an unknown word changed the crew to %q", got)
	}
	text := lastNote(t, a)
	if !strings.Contains(text, "not one of the three") {
		t.Fatalf("the refusal reads %q", text)
	}
	for _, preset := range config.CrewPresets {
		if !strings.Contains(text, preset) {
			t.Errorf("the refusal does not offer %q:\n%s", preset, text)
		}
	}
}

// A CREW SOMEBODY ASSEMBLED THEMSELVES READS AS CUSTOM, and the listing says how
// to put it back rather than pretending custom is a fourth choice.
func TestCrewReadsCustomOverAHandSetClass(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew balanced")
	a.openSettings()
	toProviders(t, a)
	setRow(t, a, config.KeyTierMastermindModel, "openai/gpt-5")
	a.closeSettings()

	if got := config.CrewAt(dir); got != config.CrewCustom {
		t.Fatalf("a hand-set class left the crew reading %q", got)
	}
	a.slash("/crew")
	text := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	if !strings.Contains(text, "none of the three") {
		t.Fatalf("the chooser does not say the crew is nobody's preset:\n%s", text)
	}
	if !strings.Contains(text, "picking one puts all four back") {
		t.Fatalf("the chooser does not say how to put it back:\n%s", text)
	}
	if strings.Contains(text, "· "+config.CrewBalanced) {
		t.Fatalf("balanced is marked over a custom crew:\n%s", text)
	}

	// And one word heals all four.
	a.slash("/crew balanced")
	if got := config.CrewAt(dir); got != config.CrewBalanced {
		t.Fatalf("the crew reads %q after balanced was set again", got)
	}
}

// THE CREW ROW CYCLES, like every other enum row on the sheet, and a custom
// reading enters the cycle at its first step rather than sticking.
func TestTheCrewRowCyclesThroughTheThreePresets(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	toProviders(t, a)
	cursorTo(t, a, config.KeyCrew)

	drive(t, a, key("enter"))
	if got := config.CrewAt(dir); got != config.CrewMax {
		t.Fatalf("enter on balanced landed on %q, want %q", got, config.CrewMax)
	}
	drive(t, a, key("enter"))
	if got := config.CrewAt(dir); got != config.CrewFrugal {
		t.Fatalf("enter on max landed on %q, want %q", got, config.CrewFrugal)
	}
	drive(t, a, key("enter"))
	if got := config.CrewAt(dir); got != config.CrewBalanced {
		t.Fatalf("enter on frugal landed on %q, want %q", got, config.CrewBalanced)
	}

	// From custom the cycle starts at the cheapest, which is the one answer that
	// cannot surprise anybody: it never spends more than the person asked for.
	setRow(t, a, config.KeyTierLowModel, "openai/gpt-5")
	if got := config.CrewAt(dir); got != config.CrewCustom {
		t.Fatalf("the crew reads %q over a hand-set class", got)
	}
	cursorTo(t, a, config.KeyCrew)
	drive(t, a, key("enter"))
	if got := config.CrewAt(dir); got != config.CrewFrugal {
		t.Fatalf("enter on custom landed on %q, want %q", got, config.CrewFrugal)
	}
}

// THE CREW ROW SAYS WHAT EACH OF THE THREE IS, in the line under it. A cycle row
// walks its choices in place — there is never a moment where three rows are shown
// side by side — so this line is the only place the comparison can happen, and it
// is built from the same table /crew prints from.
func TestTheCrewRowsLineNamesAllThreePresets(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	toProviders(t, a)
	cursorTo(t, a, config.KeyCrew)

	item, ok := a.sheet.current()
	if !ok {
		t.Fatal("the cursor is not on the crew row")
	}
	for _, preset := range config.CrewPresets {
		if !strings.Contains(item.meta.about, preset) {
			t.Errorf("the crew row's line does not name %q: %q", preset, item.meta.about)
		}
		if !strings.Contains(item.meta.about, config.CrewLine(preset)) {
			t.Errorf("the crew row's line does not say what %q is: %q", preset, item.meta.about)
		}
	}
	if !strings.Contains(item.meta.about, config.CrewCustom) {
		t.Errorf("the crew row's line does not say what makes it custom: %q", item.meta.about)
	}
	// And it is drawn, which is the whole point of a line nobody can see three
	// rows for.
	if !strings.Contains(plain(frame(a)), config.CrewLine(config.CrewBalanced)) {
		t.Fatalf("the crew row's line is not on the screen:\n%s", plain(frame(a)))
	}
}

// THE CLASS WORDS ON DISK AND THE TIER NAMES IN THE REGISTRY ARE ONE SET. The
// listing indexes the preset table by [roles.Tier], and internal/config keys that
// table by its own tier words; a rename on either side would silently print a
// blank model beside a class.
func TestTheTierWordsAndTheClassKeysAreTheSameSet(t *testing.T) {
	if len(roles.Tiers) != len(config.ModelTiers) {
		t.Fatalf("internal/roles has %d tiers and internal/config has %d classes",
			len(roles.Tiers), len(config.ModelTiers))
	}
	for at, tier := range roles.Tiers {
		if string(tier) != config.ModelTiers[at] {
			t.Errorf("tier %d is %q in internal/roles and %q in internal/config",
				at, tier, config.ModelTiers[at])
		}
		if settingUI[tierSettingKey(tier)].label == "" {
			t.Errorf("tier %q has no row of its own on the sheet", tier)
		}
	}
}
