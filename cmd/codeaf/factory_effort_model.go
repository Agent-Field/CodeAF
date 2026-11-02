package main

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryEffortTiers is the model tier each step effort word runs on: a cheap
// step on the small-work model (models.tiers.low), a strong step on the
// checks model (models.tiers.high). They are the rows Settings already
// shows and sets, so the app reads and changes them where it does today.
// The words come from factory.EffortWords, cheap first.
var factoryEffortTiers = map[string]string{
	factory.EffortWords[0]: config.ModelTierLow,
	factory.EffortWords[1]: config.ModelTierHigh,
}

// factoryStepModel is the model a step's conversation opens on, "" to keep
// the launch's default: the step's or the job's own pick first, then the
// model its effort runs on. A profile on one model for every task keeps it.
func factoryStepModel(profile string, spec factoryrun.ConversationSpec, oneModel bool) string {
	if model := strings.TrimSpace(spec.Model); model != "" {
		return model
	}
	tier, ok := factoryEffortTiers[strings.TrimSpace(spec.StepEffort)]
	if !ok || oneModel {
		return ""
	}
	return strings.TrimSpace(config.TierSeatAt(profile, tier).Model)
}

// factoryOpenModel is the model a step opens on. This first version ignores the
// step's effort word.
func factoryOpenModel(profile string, cfg session.Config, spec factoryrun.ConversationSpec) string {
	return stageOpenModel(cfg, spec)
}
