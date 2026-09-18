package config

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewpick"
)

// A CREW WORD A RUN STORES IS THE BUDGET ITS SEATS RUN AT.
//
// The crew row is derived from the five tier rows and this build never stores
// it — but a run that writes ONE word into config.json instead of the five
// rows (a harness, a hand edit) is asking for a budget, and a word that sits on
// disk unread is a crew word that reached nobody. The seats nobody wrote then
// read that word's own table rows, under whatever family the profile names.
func TestAStoredCrewWordSeatsItsPreset(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")

	// Frugal and max are the two the default family tells apart from balanced
	// in a seat a task pays for: frugal's careful seat and max's worker are the
	// cells that move. Balanced is the preset a profile reads anyway, so it
	// proves nothing here.
	for _, preset := range []string{CrewFrugal, CrewMax} {
		row, ok := CrewModelsForSource(DefaultCrewSource, preset)
		if !ok {
			t.Fatalf("there is no %s preset in the %s family", preset, DefaultCrewSource)
		}
		dir := writeProfileRows(t, map[string]string{KeyCrew: preset})
		for _, tier := range []string{ModelTierWorker, ModelTierHigh} {
			seat := TierSeatAt(dir, tier)
			if seat.Model != row[tier] {
				t.Errorf("crew=%s: the %s seat reads %q, want the preset's own %q",
					preset, tier, seat.Model, row[tier])
			}
		}
		// And the word is the word: the crew reads as the preset the run asked
		// for, not the default it fell to.
		if got := CrewAt(dir); got != preset {
			t.Errorf("crew=%s: the crew word reads %q", preset, got)
		}
	}

	// A word this build does not know is no word at all, and an untouched
	// profile is unchanged — the seam fires only on a preset on disk.
	unknown := writeProfileRows(t, map[string]string{KeyCrew: "knee"})
	if got := TierSeatAt(unknown, ModelTierWorker).Model; got != DefaultWorkerModel {
		t.Errorf("an unknown crew word seated %q, want the build's own default %q", got, DefaultWorkerModel)
	}
	untouched := t.TempDir()
	if got := TierSeatAt(untouched, ModelTierWorker); got.Source != SeatDefault || got.Crew != "" {
		t.Errorf("an untouched profile reads %q (%s), want the default rung as before", got.Model, got.Rung())
	}
}

// A PINNED ROW SURVIVES THE LEARNED PICK.
//
// The knee of a Pareto arm pins the crew table's OWN ids — `glm-5.3-flash` for
// the worker, `fable-5.1` for the careful seat — and the pick used to answer
// them anyway, on the value-comparison rule that a row holding the preset's own
// table value is the preset answering rather than a pin. A hand pin writes ONE
// row, where a `/crew` APPLY writes all five, so a partial write is the person's
// own rows and the pick leaves them alone — with or without a stored crew word.
func TestAPinnedRowSurvivesTheLearnedPick(t *testing.T) {
	restore := AutoModels
	AutoModels = func() []catalog.Model { return autoTestRows() }
	defer func() { AutoModels = restore }()

	balanced, ok := CrewModelsForSource(DefaultCrewSource, CrewBalanced)
	if !ok {
		t.Fatalf("there is no %s preset in the %s family", CrewBalanced, DefaultCrewSource)
	}
	pins := map[string]string{
		tierKeyFor(ModelTierWorker): balanced[ModelTierWorker],
		tierKeyFor(ModelTierHigh):   balanced[ModelTierHigh],
	}
	// Both the Pareto arm's shape (a stored crew word beside its pins) and the
	// bare pin the brief writes are pins: the stored word is not what makes them
	// win, but it must not cost them their rung either.
	for _, tc := range []struct {
		name string
		rows map[string]string
	}{
		{"a stored word beside the pins", map[string]string{KeyCrew: CrewBalanced}},
		{"the pins alone", map[string]string{}},
	} {
		rows := map[string]string{KeyCrewPick: CrewPickLearn}
		for k, v := range tc.rows {
			rows[k] = v
		}
		for k, v := range pins {
			rows[k] = v
		}
		dir := writeProfileRows(t, rows)
		for _, tier := range []string{ModelTierWorker, ModelTierHigh} {
			seat := TierSeatAt(dir, tier)
			if seat.Model != pins[tierKeyFor(tier)] {
				t.Errorf("%s: a pinned %s under %s reads %q, want the pin %q",
					tc.name, tier, CrewPickLearn, seat.Model, pins[tierKeyFor(tier)])
			}
		}
	}

	// THE CONTROL: a seat nobody wrote is still computed — the pick answers for
	// the seats the person did not name.
	dir := writeProfileRows(t, map[string]string{
		KeyCrewPick:                 CrewPickLearn,
		tierKeyFor(ModelTierWorker): balanced[ModelTierWorker],
	})
	mind := TierSeatAt(dir, ModelTierMastermind)
	if mind.Source != SeatLearned {
		t.Errorf("an unwritten seat under %s reads %q (%s), want a learned seat",
			CrewPickLearn, mind.Model, mind.Rung())
	}
}

// A CREW APPLIED WHOLE STILL COMPUTES ITS DIAL SEATS UNDER A PICK.
//
// `/crew balanced` writes all five tier rows at once, at the preset's own ids,
// and the pick is meant to compute those three dial seats at the crew's budget.
// Only a PARTIAL write is a person's pin, so a whole crew keeps the old rule: a
// row holding the preset's own table value is the preset answering.
func TestAWholeCrewApplyStillComputesUnderTheLearnedPick(t *testing.T) {
	restore := AutoModels
	AutoModels = func() []catalog.Model { return autoTestRows() }
	defer func() { AutoModels = restore }()

	dir := t.TempDir()
	if err := ApplyCrew(dir, CrewBalanced); err != nil {
		t.Fatal(err)
	}
	if err := SetCrewPick(dir, CrewPickLearn); err != nil {
		t.Fatal(err)
	}
	if !allTiersWritten(dir) {
		t.Fatal("a crew apply did not write all five tier rows")
	}

	_, front, _ := crewpick.Presets(crewpick.Front(autoCandidates(autoTestRows()), crewpick.DefaultShapes(), crewpick.All))
	for _, tc := range []struct {
		tier string
		want string
	}{
		{ModelTierWorker, front.Worker},
		{ModelTierHigh, front.High},
		{ModelTierMastermind, front.Mastermind},
	} {
		seat := TierSeatAt(dir, tc.tier)
		if seat.Model != tc.want || seat.Source != SeatLearned {
			t.Errorf("the applied crew's %s under %s reads %q (%s), want the computed %q (learned)",
				tc.tier, CrewPickLearn, seat.Model, seat.Source, tc.want)
		}
	}
}
