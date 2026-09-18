package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// THE TASK DOOR HONORS THE CONFIGURED CREW WORD.
//
// The worker seat of every task a proposal hands off is read through
// internal/roles ([session]'s defaultTaskModel asks for [roles.TierWorker]) off
// the key map this door builds ([v3RolesSource]). A run that wrote ONE word —
// `models.crew=frugal`, `models.crew=max` — into config.json used to read the
// balanced crew for every arm, because the word was never read: the preset is
// derived from the five tier rows, and the run wrote none of them. The worker
// and the checker (the careful-work seat) must answer the word the run asked
// for, not the default it fell to.
func TestTheTaskDoorHonorsTheConfiguredCrewWord(t *testing.T) {
	for _, preset := range []string{config.CrewFrugal, config.CrewMax} {
		dir := t.TempDir()
		handEdit(t, dir, config.KeyCrew, preset)

		read, err := v3RolesSource(t.TempDir(), dir)
		if err != nil {
			t.Fatalf("building the door's key map for crew=%s: %v", preset, err)
		}
		row, ok := config.CrewModelsForSource(config.CrewSourceAt(dir), preset)
		if !ok {
			t.Fatalf("there is no %s preset in the family the profile names", preset)
		}
		// The door's own two questions, in its own words.
		worker, ok := roles.TierModel(roles.Source(read), roles.TierWorker)
		if !ok || worker != row[config.ModelTierWorker] {
			t.Errorf("crew=%s: the door's worker seat reads %q, want the preset's own %q",
				preset, worker, row[config.ModelTierWorker])
		}
		checker, ok := roles.TierModel(roles.Source(read), roles.TierHigh)
		if !ok || checker != row[config.ModelTierHigh] {
			t.Errorf("crew=%s: the door's checker seat reads %q, want the preset's own %q",
				preset, checker, row[config.ModelTierHigh])
		}
	}
}

// AND A PIN THE RUN WROTE SURVIVES THE LEARNED PICK AT THE SAME DOOR. The knee
// arm pins the crew table's own ids and runs under `models.crew.pick=learn`;
// the pick used to recompute both seats. The door must answer exactly the ids
// the run pinned.
func TestTheTaskDoorKeepsAPinnedSeatUnderLearn(t *testing.T) {
	balanced, ok := config.CrewModelsForSource(config.DefaultCrewSource, config.CrewBalanced)
	if !ok {
		t.Fatalf("there is no %s preset in the %s family", config.CrewBalanced, config.DefaultCrewSource)
	}
	// A catalog is held, so the learned pick has something to recompute with —
	// the shape the benchmark ran, where the pick overrode the pin.
	previous := config.AutoModels
	config.AutoModels = func() []catalog.Model { return seatPickRows() }
	defer func() { config.AutoModels = previous }()

	for _, tc := range []struct {
		name     string
		withWord bool
	}{
		{"with the stored crew word", true},
		{"with the pins alone", false},
	} {
		dir := t.TempDir()
		handEdit(t, dir, config.KeyCrewPick, config.CrewPickLearn)
		if tc.withWord {
			handEdit(t, dir, config.KeyCrew, config.CrewBalanced)
		}
		handEdit(t, dir, config.KeyTierWorkerModel, balanced[config.ModelTierWorker])
		handEdit(t, dir, config.KeyTierHighModel, balanced[config.ModelTierHigh])

		read, err := v3RolesSource(t.TempDir(), dir)
		if err != nil {
			t.Fatalf("%s: building the door's key map: %v", tc.name, err)
		}
		for _, seat := range []struct {
			tier roles.Tier
			want string
		}{
			{roles.TierWorker, balanced[config.ModelTierWorker]},
			{roles.TierHigh, balanced[config.ModelTierHigh]},
		} {
			got, ok := roles.TierModel(roles.Source(read), seat.tier)
			if !ok || got != seat.want {
				t.Errorf("%s: the door's %s seat under %s reads %q, want the pin %q",
					tc.name, seat.tier, config.CrewPickLearn, got, seat.want)
			}
		}
	}
}
