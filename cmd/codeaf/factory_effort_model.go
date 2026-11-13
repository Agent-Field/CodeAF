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

// factoryRunnerModels is what this machine's runner knows about models, the
// runner's rungs of [factory.StepModel]: the launch's model, whether it runs
// every task on it, the model each effort word runs on and the person's
// worker model (Settings › Models › worker, pinned or routed). A launch on one
// model for every task needs only the first two.
func factoryRunnerModels(profile string, cfg session.Config) factory.RunnerModels {
	r := factory.RunnerModels{OneModel: cfg.OneModel, Launch: strings.TrimSpace(cfg.Model)}
	if cfg.OneModel {
		return r
	}
	r.Effort = make(map[string]string, len(factoryEffortTiers))
	for word, tier := range factoryEffortTiers {
		if model := strings.TrimSpace(config.TierSeatAt(profile, tier).Model); model != "" {
			r.Effort[word] = model
		}
	}
	r.Worker = strings.TrimSpace(config.TierSeatAt(profile, config.ModelTierWorker).Model)
	return r
}

// factoryOpenModel is the model a step's conversation opens on: THE ONE
// RESOLUTION, [factory.StepModel], over what the spec carries of the step and
// the job and what this runner knows. The wire's runs_on says the same
// resolution, so the model a page names is the model the step opens on.
func factoryOpenModel(profile string, cfg session.Config, spec factoryrun.ConversationSpec) string {
	st := factory.Stage{Model: spec.Model, Effort: spec.StepEffort}
	it := factory.Item{FactoryModel: spec.FactoryModel}
	return factory.StepModel(st, it, factory.ModelDefaults{Runner: factoryRunnerModels(profile, cfg)}).Model
}
