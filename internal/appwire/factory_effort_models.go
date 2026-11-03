package appwire

// ── factory.effort_models: which model a cheap or strong step runs on ──────
//
// No method of its own. A factory step marked cheap opens on the small-work
// model (models.tiers.low, the task models' auxiliary pin) and one marked
// strong on the checks model (models.tiers.high, the checker pin), unless the
// step or the job has its own model; on one model for every task, every step
// keeps that one. The app reads both through settings.models.get
// (roles.auxiliary.pin, roles.checker.pin; null is the build's or the
// router's own choice) and sets them through settings.models.set
// (pins.auxiliary, pins.checker), the doors that already change them.

// FeatureFactoryEffortModels is announced in hello by an engine whose factory
// steps run their effort on those two models and whose settings serve them.
const FeatureFactoryEffortModels = "factory.effort_models"

func withFactoryEffortModelsFeature(hello Hello, e Engine) Hello {
	_, factory := e.(Factory)
	_, models := e.(TaskModelsLister)
	if factory && models {
		hello = withFeature(hello, FeatureFactoryEffortModels)
	}
	return hello
}
