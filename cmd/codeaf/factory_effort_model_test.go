package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// A CHEAP STEP OPENS ON THE SMALL-WORK MODEL AND A STRONG STEP ON THE CHECKS
// MODEL, the two rows Settings already keeps (models.tiers.low and
// models.tiers.high); a model the person picked for the step or the job wins,
// and a profile on one model for everything keeps its one model.
func TestAStepsEffortChoosesItsModel(t *testing.T) {
	_, _, _ = useLocalHome(t)
	profile := config.ProfileDir()
	if err := applyRow(profile, config.KeyTierLowModel, "vendor/small"); err != nil {
		t.Fatal(err)
	}
	if err := applyRow(profile, config.KeyTierHighModel, "vendor/strong"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		spec factoryrun.ConversationSpec
		one  bool
		want string
	}{
		{factoryrun.ConversationSpec{StepEffort: "cheap"}, false, "vendor/small"},
		{factoryrun.ConversationSpec{StepEffort: "strong"}, false, "vendor/strong"},
		{factoryrun.ConversationSpec{}, false, ""},
		{factoryrun.ConversationSpec{StepEffort: "cheap", Model: " vendor/picked "}, false, "vendor/picked"},
		{factoryrun.ConversationSpec{StepEffort: "strong"}, true, ""},
	} {
		if got := factoryStepModel(profile, c.spec, c.one); got != c.want {
			t.Errorf("%+v one-model %v: %q, want %q", c.spec, c.one, got, c.want)
		}
	}
}
