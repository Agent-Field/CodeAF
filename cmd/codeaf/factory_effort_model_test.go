package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
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

// THE STEP'S EFFORT WORD BEATS THE FACTORY'S DEFAULT MODEL: a step that says it
// is cheap work runs on the small-work model on a factory with a default model.
// An effort row with no model leaves the factory's default, and a model picked
// for the step or the job and a launch on one model keep their place above both.
func TestAStepsEffortComesBeforeTheFactorysDefaultModel(t *testing.T) {
	_, _, _ = useLocalHome(t)
	profile := config.ProfileDir()
	if err := applyRow(profile, config.KeyTierLowModel, "vendor/small"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		spec factoryrun.ConversationSpec
		one  bool
		want string
	}{
		{"effort over default", factoryrun.ConversationSpec{StepEffort: "cheap", FactoryModel: "vendor/house"}, false, "vendor/small"},
		{"effort row with no model", factoryrun.ConversationSpec{StepEffort: "strong", FactoryModel: "vendor/house"}, false, "vendor/house"},
		{"no effort", factoryrun.ConversationSpec{FactoryModel: "vendor/house"}, false, "vendor/house"},
		{"picked over effort", factoryrun.ConversationSpec{StepEffort: "cheap", FactoryModel: "vendor/house", Model: "vendor/picked"}, false, "vendor/picked"},
		{"one model", factoryrun.ConversationSpec{StepEffort: "cheap", FactoryModel: "vendor/house"}, true, "vendor/launch"},
	} {
		cfg := session.Config{Model: "vendor/launch", OneModel: c.one}
		if got := factoryOpenModel(profile, cfg, c.spec); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
