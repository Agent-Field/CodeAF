package factory

import "strings"

// The rungs of [StepModel], in the order they are tried: which choice named
// the model a step runs on. A surface may say it beside the model ("the
// factory's default"); it never re-derives the model from it.
const (
	// ModelFromStep is a model a person picked for this step on this job
	// (Stage.Model).
	ModelFromStep = "step"
	// ModelFromJob is a model a person picked for the whole job (Item.Model).
	ModelFromJob = "job"
	// ModelFromOneModel is a runner that runs every task on one model
	// (Settings › Task models › One model, or a --one-model launch): the
	// rungs below a person's own pick do not apply.
	ModelFromOneModel = "one_model"
	// ModelFromEffort is the model the step's effort word runs on on the
	// runner (cheap: the small-work model, strong: the checks model).
	ModelFromEffort = "effort"
	// ModelFromFactory is the factory's default model (Spending.DefaultModel).
	ModelFromFactory = "factory"
	// ModelFromWorker is the person's own worker model on the runner
	// (Settings › Models › worker, pinned or routed), the model a task's work
	// runs on there.
	ModelFromWorker = "worker"
	// ModelFromRunner is the runner's own launch model, the last rung.
	ModelFromRunner = "runner"
)

// RunnerModels is what the machine running a job's steps knows about models:
// the rungs of [StepModel] that are the runner's, not the job's. A factory
// that does not run its own jobs (a team server, whose runners are elsewhere)
// knows none of them and passes the zero value.
type RunnerModels struct {
	// OneModel says the runner runs every task on Launch.
	OneModel bool
	// Effort maps a step effort word ([EffortWords]) to the model it runs on.
	Effort map[string]string
	// Worker is the person's worker model, "" when the runner has none.
	Worker string
	// Launch is the runner's own model, the floor.
	Launch string
}

// ModelDefaults is everything below a job's own picks that [StepModel] reads.
type ModelDefaults struct {
	// Factory is the factory's default model as it stands now, read only for a
	// job not yet queued under one (Item.FactoryModel is the one it was).
	Factory string
	Runner  RunnerModels
}

// ModelChoice is a step's model and the rung that named it. The zero value is
// "not known here": a factory whose runner is elsewhere, with no pick of the
// job's and no default of its own.
type ModelChoice struct {
	Model string `json:"model"`
	From  string `json:"from"`
}

// StepModel is THE model a step of the job runs on. It is the one resolution:
// the runner opens a step's conversation on it (cmd/codeaf's stage maker), and
// the wire says it as the job's and each step's runs_on, so what a page shows
// is what runs.
//
//  1. the step's own pick (Stage.Model), then the job's (Item.Model);
//  2. on a runner on one model for every task, that model;
//  3. the model the step's effort word runs on;
//  4. the factory's default the job was queued under, else the current one;
//  5. the person's worker model on the runner;
//  6. the runner's own launch model.
//
// THE FIFTH RUNG IS WHY THIS EXISTS AS ONE FUNCTION. Until 2026-11-06 a step
// with no pick and no factory default fell straight through to the launch's
// model, which for a runner whose person never typed one is the hard-coded
// config.DefaultModel — not the worker model the person chose in Settings, and
// not anything a page could name.
func StepModel(st Stage, it Item, d ModelDefaults) ModelChoice {
	pick := func(model, from string) (ModelChoice, bool) {
		if model = strings.TrimSpace(model); model == "" {
			return ModelChoice{}, false
		}
		return ModelChoice{Model: model, From: from}, true
	}
	r := d.Runner
	factoryDefault := it.FactoryModel
	if strings.TrimSpace(factoryDefault) == "" {
		factoryDefault = d.Factory
	}
	rungs := []struct{ model, from string }{
		{st.Model, ModelFromStep},
		{it.Model, ModelFromJob},
	}
	if r.OneModel {
		rungs = append(rungs, struct{ model, from string }{r.Launch, ModelFromOneModel})
	} else {
		rungs = append(rungs,
			struct{ model, from string }{r.Effort[strings.TrimSpace(st.Effort)], ModelFromEffort},
			struct{ model, from string }{factoryDefault, ModelFromFactory},
			struct{ model, from string }{r.Worker, ModelFromWorker},
			struct{ model, from string }{r.Launch, ModelFromRunner},
		)
	}
	for _, rung := range rungs {
		if c, ok := pick(rung.model, rung.from); ok {
			return c
		}
	}
	return ModelChoice{}
}

// NextModelStage is the conversation step whose model the job's own model
// names: the step the run is on when it is a conversation that has not
// finished, else the first switched-on conversation step the run has not
// finished, in the run's order and then the job's. ok is false when no
// conversation step is left, and the job's model is then its default.
func NextModelStage(it Item) (Stage, bool) {
	stage := func(name string, kind StageKind) Stage {
		if i := StageIndex(it.Stages, name); i >= 0 {
			return it.Stages[i]
		}
		return Stage{Name: name, Kind: kind, On: true}
	}
	finished := map[string]bool{}
	if st := it.Stream; st != nil {
		for _, p := range st.Phases {
			if p.State == PhaseDone || p.State == PhaseSkipped {
				finished[p.Name] = true
			}
		}
		if st.Cur >= 0 && st.Cur < len(st.Phases) {
			p := st.Phases[st.Cur]
			if s := stage(p.Name, p.Kind); IsChatStage(s) && !finished[p.Name] {
				return s, true
			}
		}
		for _, p := range st.Phases {
			if s := stage(p.Name, p.Kind); IsChatStage(s) && !finished[p.Name] {
				return s, true
			}
		}
	}
	for _, s := range it.Stages {
		if s.On && IsChatStage(s) && !finished[s.Name] {
			return s, true
		}
	}
	return Stage{}, false
}

// IsChatStage says whether a step is a conversation, the only kind of step
// that runs on a model.
func IsChatStage(s Stage) bool { return s.Kind == StageChat || s.Kind == "" }
