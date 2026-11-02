package main

import (
	"testing"

	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A STEP OPENS ON ITS OWN OR ITS JOB'S MODEL, ELSE THE FACTORY'S DEFAULT, ELSE
// THE LAUNCH'S. A launch that runs one model for every task keeps it: the
// factory's default never stands in for the one model.
func TestAStepOpensOnTheFactorysDefaultUnlessOneModelRules(t *testing.T) {
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
		if got := stageOpenModel(c.cfg, c.spec); got != c.want {
			t.Errorf("%s: opened on %q, want %q", name, got, c.want)
		}
	}
}
