package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A STEP OPENS ON ITS OWN OR ITS JOB'S MODEL, ELSE THE FACTORY'S DEFAULT, ELSE
// THE LAUNCH'S. A launch that runs one model for every task keeps it: the
// factory's default never stands in for the one model.
func TestAStepOpensOnTheFactorysDefaultUnlessOneModelRules(t *testing.T) {
	_, _, _ = useLocalHome(t)
	profile := config.ProfileDir()
	launch := session.Config{Model: "vendor/launch"}
	for name, c := range map[string]struct {
		cfg  session.Config
		spec factoryrun.ConversationSpec
		want string
	}{
		"job pick":        {launch, factoryrun.ConversationSpec{Model: "vendor/job", FactoryModel: "vendor/factory"}, "vendor/job"},
		"factory default": {launch, factoryrun.ConversationSpec{FactoryModel: "vendor/factory"}, "vendor/factory"},
		"nothing set":     {launch, factoryrun.ConversationSpec{}, "vendor/launch"},
		"one model":       {session.Config{Model: "vendor/one", OneModel: true}, factoryrun.ConversationSpec{FactoryModel: "vendor/factory"}, "vendor/one"},
	} {
		if got := factoryOpenModel(profile, c.cfg, c.spec); got != c.want {
			t.Errorf("%s: opened on %q, want %q", name, got, c.want)
		}
	}
}

// WITH NO FACTORY DEFAULT A STEP RUNS ON THE PERSON'S WORKER MODEL, not the
// launch's built-in default. Before 2026-11-06 a factory with no default model
// fell through to the launch's model, which for a person who never typed one
// is config.DefaultModel; the worker model they chose in Settings was never
// read. A factory default and a one-model launch still come first.
func TestAStepWithNoFactoryDefaultRunsOnThePersonsWorkerModel(t *testing.T) {
	_, _, _ = useLocalHome(t)
	profile := config.ProfileDir()
	if err := config.SetCrewPin(profile, crewroute.Worker, "vendor/worker"); err != nil {
		t.Fatal(err)
	}
	launch := session.Config{Model: config.DefaultModel}
	if got := factoryOpenModel(profile, launch, factoryrun.ConversationSpec{}); got != "vendor/worker" {
		t.Fatalf("no factory default: opened on %q, want the worker model", got)
	}
	if got := factoryOpenModel(profile, launch, factoryrun.ConversationSpec{FactoryModel: "vendor/factory"}); got != "vendor/factory" {
		t.Fatalf("a factory default: opened on %q", got)
	}
	if got := factoryOpenModel(profile, session.Config{Model: "vendor/one", OneModel: true}, factoryrun.ConversationSpec{}); got != "vendor/one" {
		t.Fatalf("one model: opened on %q", got)
	}
}
